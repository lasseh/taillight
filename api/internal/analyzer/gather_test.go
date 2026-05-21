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

func (s stubStore) LookupJuniperRefs(context.Context, []string) (map[string]model.JuniperNetlogRef, error) {
	return nil, nil
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
