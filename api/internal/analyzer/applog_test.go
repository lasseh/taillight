package analyzer

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// applogStub returns canned applog data and records what gather asked for.
// fail, when set, is returned by every best-effort lookup; the load-bearing
// stats and top-template lookups keep succeeding so the degradation path
// can be told apart from a hard failure.
type applogStub struct {
	stubStore
	stats    []model.AppLogServiceStats
	top      []model.AppLogTemplate
	newTmpl  []model.AppLogTemplate
	samples  map[model.AppLogTemplateKey]model.AppLogSample
	timeline []model.AnalysisVolumeBucket
	hygiene  model.AppLogHygiene
	fail     error

	gotTopServices []string
	gotSampleKeys  []model.AppLogTemplateKey
}

func (s *applogStub) GetAppLogServiceStats(context.Context, model.AnalysisScope, time.Time, time.Time) ([]model.AppLogServiceStats, error) {
	return s.stats, nil
}

func (s *applogStub) GetAppLogTopTemplates(_ context.Context, _ time.Time, services []string, _, _ int) ([]model.AppLogTemplate, error) {
	s.gotTopServices = services
	return s.top, nil
}

func (s *applogStub) GetAppLogNewTemplates(context.Context, model.AnalysisScope, time.Time, time.Time) ([]model.AppLogTemplate, error) {
	return s.newTmpl, s.fail
}

func (s *applogStub) GetAppLogTemplateSamples(_ context.Context, _ time.Time, keys []model.AppLogTemplateKey, _ int) (map[model.AppLogTemplateKey]model.AppLogSample, error) {
	s.gotSampleKeys = keys
	return s.samples, s.fail
}

func (s *applogStub) GetAppLogVolumeTimeline(context.Context, model.AnalysisScope, time.Time, time.Time, int) ([]model.AnalysisVolumeBucket, error) {
	return s.timeline, s.fail
}

func (s *applogStub) GetAppLogHygiene(context.Context, model.AnalysisScope, time.Time, float64, int64, int) (model.AppLogHygiene, error) {
	return s.hygiene, s.fail
}

func tmpl(service, component, pattern, level string, count int64) model.AppLogTemplate {
	return model.AppLogTemplate{
		AppLogTemplateKey: model.AppLogTemplateKey{Service: service, Component: component, Pattern: pattern, Level: level},
		Count:             count,
	}
}

// rankingFixture is five services chosen so every ranking rule and every
// derived list has exactly one expected outcome.
func rankingFixture() *applogStub {
	return &applogStub{
		stats: []model.AppLogServiceStats{
			// Steady warnings, no change: active, ranks last among actives.
			{Service: "chatty", Current: model.AppLogLevelCounts{Total: 1000, Warn: 10}, Baseline: model.AppLogLevelCounts{Total: 7000, Warn: 70}},
			// Errors five times the baseline rate: ranks by error delta.
			{Service: "spiky", Current: model.AppLogLevelCounts{Total: 500, Error: 50}, Baseline: model.AppLogLevelCounts{Total: 3500, Error: 70}},
			// No baseline at all and a new template: new service, ranks first.
			{Service: "fresh", Current: model.AppLogLevelCounts{Total: 20, Error: 2}},
			// Logged 100/day before, nothing now: silent, not active.
			{Service: "quiet", Baseline: model.AppLogLevelCounts{Total: 700}},
			// INFO only: neither active nor silent.
			{Service: "infoonly", Current: model.AppLogLevelCounts{Total: 300}, Baseline: model.AppLogLevelCounts{Total: 2100}},
			// One warning, flat baseline: active, alphabetically after chatty.
			{Service: "tail", Current: model.AppLogLevelCounts{Total: 5, Warn: 1}, Baseline: model.AppLogLevelCounts{Total: 7, Warn: 7}},
		},
		newTmpl: []model.AppLogTemplate{tmpl("fresh", "boot", "panic: nil deref", "ERROR", 5)},
		top: []model.AppLogTemplate{
			tmpl("fresh", "http", "boom", "ERROR", 2),
			tmpl("spiky", "db", "db timeout after <n>ms", "ERROR", 30),
			tmpl("spiky", "db", "retrying", "ERROR", 20),
			tmpl("spiky", "http", "slow query <n>ms", "WARN", 5),
		},
		samples: map[model.AppLogTemplateKey]model.AppLogSample{
			{Service: "fresh", Component: "boot", Pattern: "panic: nil deref", Level: "ERROR"}: {
				Host: "h1", Level: "ERROR", Msg: "panic: nil deref",
				Attrs: `{"stack":"a\nb\nc\nd\ne","err":"x"}`,
			},
		},
	}
}

func TestGatherAppLogRanksAndCaps(t *testing.T) {
	store := rankingFixture()
	a := &Analyzer{store: store, logger: discardLogger(), cfg: Config{AppLog: AppLogCaps{
		RankedServices: 2, LongTailServices: 1, TemplateSamples: 2, SilentMinEventsPerDay: 50,
	}}}

	// periodEnd on the hour keeps the floored window exactly 24h so the
	// per-day rates below are the raw counts.
	data, err := a.gatherAppLog(context.Background(), model.AnalysisScope{Feed: model.AnalysisFeedApplog}, 24*time.Hour, time.Now().UTC().Truncate(time.Hour))
	if err != nil {
		t.Fatalf("gatherAppLog: %v", err)
	}

	if data.ActiveServices != 4 {
		t.Errorf("ActiveServices = %d, want 4 (chatty, spiky, fresh, tail)", data.ActiveServices)
	}
	if got, want := store.gotTopServices, []string{"fresh", "spiky"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ranked services = %v, want %v", got, want)
	}
	if len(data.LongTail) != 1 || data.LongTail[0].Service != "chatty" {
		t.Errorf("LongTail = %+v, want [chatty]", data.LongTail)
	}
	if data.Remainder != 1 || data.RemainderWarnPlus != 1 {
		t.Errorf("Remainder = %d (%d warn+), want 1 (1)", data.Remainder, data.RemainderWarnPlus)
	}

	// Template split per ranked service.
	spiky := data.Ranked[1]
	if len(spiky.ErrorTemplates) != 2 || len(spiky.WarnTemplates) != 1 {
		t.Errorf("spiky templates: %d error, %d warn; want 2, 1", len(spiky.ErrorTemplates), len(spiky.WarnTemplates))
	}
	if data.Ranked[0].NewTemplates != 1 {
		t.Errorf("fresh NewTemplates = %d, want 1", data.Ranked[0].NewTemplates)
	}

	// Samples: two error slots consumed in rank order, warn skipped, new
	// template always included.
	wantKeys := []model.AppLogTemplateKey{
		{Service: "fresh", Component: "http", Pattern: "boom", Level: "ERROR"},
		{Service: "spiky", Component: "db", Pattern: "db timeout after <n>ms", Level: "ERROR"},
		{Service: "fresh", Component: "boot", Pattern: "panic: nil deref", Level: "ERROR"},
	}
	if !reflect.DeepEqual(store.gotSampleKeys, wantKeys) {
		t.Errorf("sample keys = %v, want %v", store.gotSampleKeys, wantKeys)
	}
	sample := data.NewTemplates[0].Sample
	if sample == nil {
		t.Fatal("new template has no sample attached")
	}
	if !strings.Contains(sample.Attrs, `"stack":"a\nb\nc …"`) || strings.Contains(sample.Attrs, "d") {
		t.Errorf("attrs not compacted to three lines: %s", sample.Attrs)
	}

	if len(data.Silent) != 1 || data.Silent[0].Service != "quiet" {
		t.Errorf("Silent = %+v, want [quiet]", data.Silent)
	}
	if len(data.NewServices) != 1 || data.NewServices[0].Service != "fresh" {
		t.Errorf("NewServices = %+v, want [fresh]", data.NewServices)
	}

	// Drift: ERROR 52/day now vs 10/day baseline.
	var errDrift applogLevelDrift
	for _, d := range data.Drift {
		if d.Label == "ERROR" {
			errDrift = d
		}
	}
	if errDrift.Current != 52 || errDrift.BaselineAvg != 10 || errDrift.ChangePct != 420 {
		t.Errorf("ERROR drift = %+v, want current 52, baseline 10, +420%%", errDrift)
	}
}

func TestGatherAppLogSurvivesOptionalLookupFailures(t *testing.T) {
	store := rankingFixture()
	store.fail = errors.New("statement timeout")
	a := &Analyzer{store: store, logger: discardLogger()}

	data, err := a.gatherAppLog(context.Background(), model.AnalysisScope{Feed: model.AnalysisFeedApplog}, 24*time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatalf("gatherAppLog = %v, want nil: an optional lookup must not fail the run", err)
	}
	for _, section := range []string{unavailableNewTemplates, unavailableSamples, unavailableVolume, unavailableHygiene} {
		if !data.Unavailable[section] {
			t.Errorf("section %q not marked unavailable after its lookup failed", section)
		}
	}
	// The load-bearing part still ran.
	if len(data.Ranked) == 0 || len(data.Ranked[1].ErrorTemplates) == 0 {
		t.Errorf("ranked templates missing after optional failures: %+v", data.Ranked)
	}
}

func TestGatherAppLogPropagatesDeadContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := rankingFixture()
	store.fail = context.Canceled
	a := &Analyzer{store: store, logger: discardLogger()}

	if _, err := a.gatherAppLog(ctx, model.AnalysisScope{Feed: model.AnalysisFeedApplog}, 24*time.Hour, time.Now().UTC()); !errors.Is(err, context.Canceled) {
		t.Fatalf("gatherAppLog = %v, want context.Canceled", err)
	}
}

// TestGatherAppLogFloorsWindowToTheHour pins the whole-hour window: the
// aggregate cannot split an hour, so the raw-row queries must start on the
// same boundary and the per-day rate must use the window actually read.
func TestGatherAppLogFloorsWindowToTheHour(t *testing.T) {
	store := rankingFixture()
	a := &Analyzer{store: store, logger: discardLogger()}
	end := time.Date(2026, 9, 2, 10, 37, 0, 0, time.UTC)
	data, err := a.gatherAppLog(context.Background(), model.AnalysisScope{Feed: model.AnalysisFeedApplog}, 24*time.Hour, end)
	if err != nil {
		t.Fatalf("gatherAppLog: %v", err)
	}
	if want := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC); !data.PeriodStart.Equal(want) {
		t.Errorf("PeriodStart = %v, want %v", data.PeriodStart, want)
	}
	// spiky: 50 errors over 24h37m is 48.8/day, not 50.
	if got := data.Ranked[1].CurrentPerDay.Error; got < 48.7 || got > 48.9 {
		t.Errorf("per-day error rate over the floored window = %.2f, want ~48.8", got)
	}
}

// TestSampleKeysBudgetCountsUniqueKeys pins that a duplicate key does not
// consume a sample slot.
func TestSampleKeysBudgetCountsUniqueKeys(t *testing.T) {
	dup := tmpl("a", "c", "p", "ERROR", 9)
	ranked := []applogServiceReport{{ErrorTemplates: []model.AppLogTemplate{dup, dup, tmpl("a", "c", "q", "ERROR", 8)}}}
	keys := sampleKeys(ranked, nil, 2)
	if len(keys) != 2 || keys[1].Pattern != "q" {
		t.Errorf("sampleKeys = %v, want the two distinct keys", keys)
	}
}

// TestSilentServicesScaleToTheWindow pins the incident-window rule: a
// service that logs 100 times a day is not "silent" for one quiet hour, but
// one that logs 2400 times a day is.
func TestSilentServicesScaleToTheWindow(t *testing.T) {
	store := &applogStub{stats: []model.AppLogServiceStats{
		{Service: "hourly-ish", Baseline: model.AppLogLevelCounts{Total: 700}},
		{Service: "chatty", Baseline: model.AppLogLevelCounts{Total: 16800}},
	}}
	a := &Analyzer{store: store, logger: discardLogger()}
	end := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)

	hour, err := a.gatherAppLog(context.Background(), model.AnalysisScope{Feed: model.AnalysisFeedApplog}, time.Hour, end)
	if err != nil {
		t.Fatalf("gatherAppLog(1h): %v", err)
	}
	if len(hour.Silent) != 1 || hour.Silent[0].Service != "chatty" {
		t.Errorf("1h window silent = %+v, want only chatty", hour.Silent)
	}
	day, err := a.gatherAppLog(context.Background(), model.AnalysisScope{Feed: model.AnalysisFeedApplog}, 24*time.Hour, end)
	if err != nil {
		t.Fatalf("gatherAppLog(24h): %v", err)
	}
	if len(day.Silent) != 2 {
		t.Errorf("24h window silent = %+v, want both", day.Silent)
	}
}

func TestIsEmptyAppLogData(t *testing.T) {
	if !isEmptyAppLogData(applogData{}) {
		t.Error("zero data should be empty")
	}
	if isEmptyAppLogData(applogData{ActiveServices: 1}) {
		t.Error("active service should not be empty")
	}
	if isEmptyAppLogData(applogData{Silent: []applogServiceRow{{Service: "x"}}}) {
		t.Error("a silent service is worth a report")
	}
}

func TestAppLogCapsWithDefaults(t *testing.T) {
	got := AppLogCaps{RankedServices: 3}.withDefaults()
	if got.RankedServices != 3 {
		t.Errorf("explicit cap overwritten: %d", got.RankedServices)
	}
	if got.NewTemplates != DefaultAppLogCaps().NewTemplates {
		t.Errorf("zero cap not defaulted: %d", got.NewTemplates)
	}
}

func TestCompactAttrs(t *testing.T) {
	long := strings.Repeat("x", 200)
	tests := []struct {
		name     string
		raw      string
		maxBytes int
		want     string
	}{
		{"empty", "", 400, ""},
		{"short passes through", `{"a":1,"b":"two"}`, 400, `{"a":1,"b":"two"}`},
		{"long string cut", `{"s":"` + long + `"}`, 400, `{"s":"` + strings.Repeat("x", 120) + `…"}`},
		{"stack keeps three lines", `{"stack":"l1\nl2\nl3\nl4"}`, 400, `{"stack":"l1\nl2\nl3 …"}`},
		{"nested", `{"o":{"s":"a\nb\nc\nd"}}`, 400, `{"o":{"s":"a\nb\nc …"}}`},
		{"html not escaped", `{"q":"a<b"}`, 400, `{"q":"a<b"}`},
		{"big integers kept", `{"order_id":12345678901234567890,"span_id":9007199254740993}`, 400, `{"order_id":12345678901234567890,"span_id":9007199254740993}`},
		{"byte cap", `{"a":"0123456789"}`, 8, `{"a":"01…`},
		{"invalid json cut as text", `not json at all`, 8, `not json…`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := compactAttrs(tc.raw, tc.maxBytes); got != tc.want {
				t.Errorf("compactAttrs() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTruncateBytesRuneBoundary(t *testing.T) {
	// "ab" + a 3-byte rune: cutting at 4 would split the rune.
	got := truncateBytes("ab€cd", 4)
	if got != "ab…" {
		t.Errorf("truncateBytes = %q, want %q", got, "ab…")
	}
}
