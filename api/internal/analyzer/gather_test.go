package analyzer

import (
	"context"
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/ollama"
)

// stubStore is a no-op Store impl that returns canned data for gather tests.
// Only fields touched by the test under exercise need to be populated.
type stubStore struct {
	sevComparison model.SeverityComparison
}

func (s stubStore) GetTopMsgIDs(context.Context, string, time.Time, int) ([]model.MsgIDCount, error) {
	return nil, nil
}

func (s stubStore) GetSeverityComparison(context.Context, string, time.Time, time.Time) (model.SeverityComparison, error) {
	// Return a deep-enough copy that the analyzer can mutate without
	// affecting the next call.
	out := model.SeverityComparison{Levels: make([]model.SeverityLevelComparison, len(s.sevComparison.Levels))}
	copy(out.Levels, s.sevComparison.Levels)
	return out, nil
}

func (s stubStore) GetTopErrorHosts(context.Context, string, time.Time, int) ([]model.HostErrorCount, error) {
	return nil, nil
}

func (s stubStore) GetNewMsgIDs(context.Context, string, time.Time, time.Time) ([]string, error) {
	return nil, nil
}

func (s stubStore) GetEventClusters(context.Context, string, time.Time, int) ([]model.EventCluster, error) {
	return nil, nil
}

func (s stubStore) GetMsgIDSamples(context.Context, string, time.Time, []string, int) (map[string][]model.SampleMessage, error) {
	return nil, nil
}

func (s stubStore) GetTopPrograms(context.Context, string, time.Time, int) ([]model.ProgramCount, error) {
	return nil, nil
}

func (s stubStore) GetTopFacilities(context.Context, string, time.Time, int) ([]model.FacilityCount, error) {
	return nil, nil
}

func (s stubStore) GetVolumeTimeline(context.Context, string, time.Time, time.Time, int) ([]model.AnalysisVolumeBucket, error) {
	return nil, nil
}

func (s stubStore) LookupJuniperRefs(context.Context, []string) (map[string]model.JuniperNetlogRef, error) {
	return nil, nil
}

// TestSparklineMath spot-checks the ceil-scaling behavior. The crucial
// property: the single max value in the input must map to the tallest
// block, and zeros must map to the blank cell — otherwise the model
// can't distinguish "quiet hour" from "low activity".
func TestSparklineMath(t *testing.T) {
	cases := []struct {
		name string
		in   []int64
		want string
	}{
		{name: "empty", in: nil, want: ""},
		{name: "all zero", in: []int64{0, 0, 0}, want: "   "},
		{name: "single peak", in: []int64{0, 0, 100, 0}, want: "  █ "},
		{name: "monotonic ramp", in: []int64{1, 2, 3, 4, 5, 6, 7, 8}, want: "▁▂▃▄▅▆▇█"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sparkline(tc.in)
			if got != tc.want {
				t.Errorf("sparkline(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestPickBucketMinutes ensures the period → bucket mapping stays within
// the readable cell-count range (12–72 cells) across the supported
// periods. A regression here would either crush the sparkline to a few
// cells (loses signal) or balloon it past comprehension.
func TestPickBucketMinutes(t *testing.T) {
	cases := []struct {
		period time.Duration
		bucket int
		cells  int // period / bucket
	}{
		{period: 1 * time.Hour, bucket: 5, cells: 12},
		{period: 6 * time.Hour, bucket: 5, cells: 72},
		{period: 24 * time.Hour, bucket: 60, cells: 24},
		{period: 7 * 24 * time.Hour, bucket: 360, cells: 28},
		{period: 30 * 24 * time.Hour, bucket: 1440, cells: 30},
	}
	for _, tc := range cases {
		got := pickBucketMinutes(tc.period)
		if got != tc.bucket {
			t.Errorf("pickBucketMinutes(%v) = %d, want %d", tc.period, got, tc.bucket)
		}
	}
}

// TestGatherSeverityNormalization verifies that the per-day-rate normalization
// applies across window lengths, not only multi-day ones. Before the fix, a 1h
// incident window would compare "10 raw events" to "50/day baseline" and look
// quiet when the actual per-day-equivalent rate is 240 — a 4.8× spike.
func TestGatherSeverityNormalization(t *testing.T) {
	// Same input data every time: 10 raw "current" events, 50/day baseline.
	// Store baseline is already normalized to per-day; "current" arrives as
	// the raw count over the window.
	makeStore := func() stubStore {
		return stubStore{
			sevComparison: model.SeverityComparison{
				Levels: []model.SeverityLevelComparison{
					{Severity: 3, Label: "err", Current: 10, BaselineAvg: 50},
				},
			},
		}
	}

	cases := []struct {
		name             string
		period           time.Duration
		wantCurrent      float64 // per-day-equivalent rate
		wantChangePctMin float64 // inclusive lower bound (handles float jitter)
		wantChangePctMax float64
	}{
		{
			name:             "1h window: 10 events extrapolates to 240/day, ~+380% vs 50/day",
			period:           time.Hour,
			wantCurrent:      240,
			wantChangePctMin: 379,
			wantChangePctMax: 381,
		},
		{
			name:             "24h window: 10 events is 10/day, -80% vs 50/day baseline",
			period:           24 * time.Hour,
			wantCurrent:      10,
			wantChangePctMin: -81,
			wantChangePctMax: -79,
		},
		{
			name:             "7d window: 10 events over 7 days is ~1.43/day, ~-97% vs 50/day",
			period:           7 * 24 * time.Hour,
			wantCurrent:      10.0 / 7.0,
			wantChangePctMin: -98,
			wantChangePctMax: -97,
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := New(makeStore(), ollama.New(""), Config{}, logger)
			data, err := a.gather(context.Background(), feedNetlog, tc.period, time.Now().UTC())
			if err != nil {
				t.Fatalf("gather: %v", err)
			}
			if len(data.SeverityComparison.Levels) != 1 {
				t.Fatalf("expected 1 severity level, got %d", len(data.SeverityComparison.Levels))
			}
			got := data.SeverityComparison.Levels[0]
			if math.Abs(got.Current-tc.wantCurrent) > 0.01 {
				t.Errorf("Current per-day rate: got %.2f, want %.2f", got.Current, tc.wantCurrent)
			}
			if got.ChangePct < tc.wantChangePctMin || got.ChangePct > tc.wantChangePctMax {
				t.Errorf("ChangePct: got %.2f, want in [%.2f, %.2f]", got.ChangePct, tc.wantChangePctMin, tc.wantChangePctMax)
			}
		})
	}
}
