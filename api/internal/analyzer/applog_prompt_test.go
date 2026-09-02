package analyzer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/ollama"
)

// applogFixtureData returns an applogData with every section populated so
// the templates exercise each branch.
func applogFixtureData() applogData {
	now := time.Date(2026, 9, 1, 6, 0, 0, 0, time.UTC)
	sample := &model.AppLogSample{Host: "web-1", Level: "ERROR", ReceivedAt: now.Add(-time.Hour), Msg: "db timeout after 30ms", Attrs: `{"err":"context deadline exceeded","db":"orders"}`}
	spiky := applogServiceRow{
		Service:       "orders-api",
		Current:       model.AppLogLevelCounts{Total: 500, Warn: 40, Error: 50},
		CurrentPerDay: applogPerDay{Total: 500, Warn: 40, Error: 50},
		Baseline:      applogPerDay{Total: 480, Warn: 38, Error: 10},
		NewTemplates:  1,
	}
	return applogData{
		Feed:           model.AnalysisFeedApplog,
		Period:         24 * time.Hour,
		PeriodLabel:    "24 hours",
		PeriodStart:    now.Add(-24 * time.Hour),
		PeriodEnd:      now,
		ActiveServices: 30,
		Drift: []applogLevelDrift{
			{Label: "FATAL", Current: 0, BaselineAvg: 0},
			{Label: "ERROR", Current: 52, BaselineAvg: 10, ChangePct: 420},
			{Label: "WARN", Current: 41, BaselineAvg: 45, ChangePct: -8.9},
			{Label: "all levels", Current: 5000, BaselineAvg: 4800, ChangePct: 4.2},
		},
		Ranked: []applogServiceReport{{
			applogServiceRow: spiky,
			ErrorTemplates: []model.AppLogTemplate{{
				AppLogTemplateKey: model.AppLogTemplateKey{Service: "orders-api", Component: "db", Pattern: "db timeout after <n>ms", Level: "ERROR"},
				Count:             50, HostCount: 2, FirstSeen: now.Add(-20 * time.Hour), LastSeen: now.Add(-time.Hour), Sample: sample,
			}},
			WarnTemplates: []model.AppLogTemplate{{
				AppLogTemplateKey: model.AppLogTemplateKey{Service: "orders-api", Pattern: "slow query <n>ms", Level: "WARN"},
				Count:             40, HostCount: 1, FirstSeen: now.Add(-23 * time.Hour), LastSeen: now,
			}},
		}},
		LongTail:  []applogServiceRow{{Service: "billing", Current: model.AppLogLevelCounts{Total: 90, Warn: 9, Error: 1}}},
		Remainder: 3, RemainderWarnPlus: 12,
		NewTemplates: []model.AppLogTemplate{{
			AppLogTemplateKey: model.AppLogTemplateKey{Service: "orders-api", Component: "boot", Pattern: "panic: nil deref", Level: "ERROR"},
			Count:             1, HostCount: 1, FirstSeen: now.Add(-2 * time.Hour), LastSeen: now.Add(-2 * time.Hour),
			Sample: &model.AppLogSample{Host: "web-2", Level: "ERROR", ReceivedAt: now.Add(-2 * time.Hour), Msg: "panic: nil deref", Attrs: `{"stack":"main.go:12"}`},
		}},
		Silent:            []applogServiceRow{{Service: "cron-runner", Baseline: applogPerDay{Total: 120}}},
		NewServices:       []applogServiceRow{{Service: "search-v2", Current: model.AppLogLevelCounts{Total: 300, Warn: 2}}},
		VolumeBucketLabel: "1 hour",
		VolumeSparkline:   "▁▂▃▅▇█▆▄▂▁",
		ErrorSparkline:    "▁▁▁▂▄█▆▃▁▁",
		VolumePeaks:       []string{"09-01 03:00 (40 err / 600 total)"},
		Hygiene: model.AppLogHygiene{
			WarnPlusRows: 400, EmptyComponent: 20, OversizeAttrs: 3,
			Dominant: []model.AppLogDominantTemplate{{AppLogTemplateKey: model.AppLogTemplateKey{Service: "billing", Pattern: "retrying payment <n>", Level: "WARN"}, Count: 80, ServiceTotal: 90}},
		},
		Caps:        DefaultAppLogCaps(),
		Unavailable: map[string]bool{},
	}
}

func TestReportKind(t *testing.T) {
	if got := reportKind(model.AnalysisFeedApplog, modeDaily); got != kindApplogDaily {
		t.Errorf("reportKind(applog, daily) = %q, want %q", got, kindApplogDaily)
	}
	if got := reportKind(feedNetlog, modeDaily); got != modeDaily {
		t.Errorf("reportKind(netlog, daily) = %q, want %q", got, modeDaily)
	}
	if got := briefingTitle(kindApplogDaily); got != "Daily Application Log Briefing" {
		t.Errorf("briefingTitle(applog daily) = %q", got)
	}
}

// chatReplyApplog is a minimal applog daily reply that passes validateReport.
const chatReplyApplog = `## New errors and warnings
**Status: WATCH** — one new error signature in ` + "`orders-api`" + `.
- **[ERR]** ` + "`orders-api` / `boot` — `panic: nil deref`" + ` — 1 event · 1 host

## Top recurring errors and warnings
- **` + "`orders-api`" + `** — errors 50 (baseline 10/day, +400%)

## Volume vs last week
- ERROR 52/day vs 10/day (+420%)

## Silent and new services
- Silent: ` + "`cron-runner`" + ` — 120/day over 7 days, nothing this period.

## Log hygiene
_Nothing to flag._

*Baseline: ERROR 52/day vs 7-day 10/day (+420%) · WARN 41/day vs 45/day (-10%) · 30 services active*
`

func TestApplogDailySpec(t *testing.T) {
	if err := validateReport(chatReplyApplog, kindApplogDaily); err != nil {
		t.Errorf("well-formed applog reply rejected: %v", err)
	}
	if err := validateReport(chatReplyDaily, kindApplogDaily); err == nil {
		t.Error("syslog-shaped reply accepted for the applog kind")
	}
	if got := reportLineCap[kindApplogDaily]; got != 80 {
		t.Errorf("applog line cap = %d, want 80", got)
	}

	// Every required header is spelled out verbatim in the system prompt.
	src, err := loadPromptSource("", model.AnalysisFeedApplog+"/"+modeDaily, systemPromptFile)
	if err != nil {
		t.Fatalf("load applog system prompt: %v", err)
	}
	for _, h := range requiredHeaders[kindApplogDaily] {
		if !strings.Contains(src, "## "+h+"\n") {
			t.Errorf("applog system prompt does not spell out header %q", h)
		}
	}
	if !strings.Contains(structureCorrection(errors.New("too long"), kindApplogDaily), "under 80 non-blank lines") {
		t.Error("correction message does not carry the applog line cap")
	}
}

func TestBuildAppLogPrompt(t *testing.T) {
	sys, usr, err := buildAppLogPrompt(applogFixtureData(), "", modeDaily)
	if err != nil {
		t.Fatalf("buildAppLogPrompt: %v", err)
	}
	for _, want := range []string{
		logDataBegin, logDataEnd,
		"## Level drift", "## Volume timeline (1 hour per cell",
		"### 1. `orders-api`", "`db timeout after <n>ms` — 50 events · 2 host(s)",
		"attrs: `{\"err\":\"context deadline exceeded\",\"db\":\"orders\"}`",
		"## Long tail (next 1 active services)", "`billing` — 1 errors / 9 warnings",
		"(+3 more active services, 12 warn+ rows combined)",
		"## New signatures (not seen", "`panic: nil deref` — 1 events",
		"`cron-runner` — baseline 120/day", "`search-v2` — 300 events this period (0 errors, 2 warnings)",
		"Rows: 400 · empty component: 20 · attrs over 1024 bytes: 3",
		"Dominant: `billing` (no component) `retrying payment <n>` — 80 of 90 WARN rows",
		"WARN (no component) `slow query <n>ms`",
	} {
		if !strings.Contains(usr, want) {
			t.Errorf("user prompt missing %q:\n%s", want, usr)
		}
	}
	for _, absent := range []string{"MSGID", "Severity legend", "Scope:", "_Unavailable", "programname", "facility"} {
		if strings.Contains(usr, absent) {
			t.Errorf("user prompt contains syslog-only or wrong text %q", absent)
		}
	}
	if strings.Contains(sys, scopedGuardSystemPreamble) || strings.Contains(sys, applogScopedGuardSystemPreamble) {
		t.Error("unscoped run got a scope guard")
	}
	if !strings.Contains(sys, logDataBegin) || !strings.Contains(sys, "## New errors and warnings") {
		t.Error("system prompt missing the sentinel or the required headers")
	}
}

func TestBuildAppLogPromptScoped(t *testing.T) {
	data := applogFixtureData()
	data.Services = []string{"billing", "orders-api"}
	sys, usr, err := buildAppLogPrompt(data, "", modeDaily)
	if err != nil {
		t.Fatalf("buildAppLogPrompt: %v", err)
	}
	if !strings.HasPrefix(sys, applogScopedGuardSystemPreamble) {
		t.Error("scoped run missing the service scope guard")
	}
	if !strings.Contains(usr, "Scope: billing, orders-api (2 services)") {
		t.Errorf("user prompt missing the scope line:\n%s", usr)
	}
}

func TestBuildAppLogPromptMarksUnavailable(t *testing.T) {
	data := applogFixtureData()
	data.NewTemplates = nil
	data.VolumeSparkline = ""
	data.Hygiene = model.AppLogHygiene{}
	data.Unavailable = map[string]bool{
		unavailableNewTemplates: true,
		unavailableVolume:       true,
		unavailableHygiene:      true,
		unavailableSamples:      true,
	}
	_, usr, err := buildAppLogPrompt(data, "", modeDaily)
	if err != nil {
		t.Fatalf("buildAppLogPrompt: %v", err)
	}
	if got := strings.Count(usr, "_Unavailable —"); got != 3 {
		t.Errorf("got %d unavailable markers, want 3:\n%s", got, usr)
	}
	if !strings.Contains(usr, "Samples: unavailable for this run") {
		t.Error("samples failure not surfaced")
	}
	if strings.Contains(usr, "## New signatures\n_None._") {
		t.Error("failed lookup narrated as a confirmed absence")
	}
}

func TestBuildAppLogPromptEmptyData(t *testing.T) {
	data := applogData{Feed: model.AnalysisFeedApplog, PeriodLabel: "24 hours", PeriodStart: time.Now().Add(-24 * time.Hour), PeriodEnd: time.Now(), Caps: DefaultAppLogCaps(), Unavailable: map[string]bool{}}
	if _, usr, err := buildAppLogPrompt(data, "", modeDaily); err != nil {
		t.Fatalf("buildAppLogPrompt on empty data: %v", err)
	} else if !strings.Contains(usr, "_None — no service logged at WARN or above this period._") {
		t.Errorf("empty ranked list not stated:\n%s", usr)
	}
}

// TestBuildAppLogPromptFallsBackWhenOverrideLacksApplog covers a prompts_dir
// laid out before the applog feed existed: the embedded applog prompts
// apply. A partial applog subtree stays an error.
func TestBuildAppLogPromptFallsBackWhenOverrideLacksApplog(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "daily"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, usr, err := buildAppLogPrompt(applogFixtureData(), dir, modeDaily); err != nil {
		t.Fatalf("syslog-only prompts_dir should fall back to the embedded applog prompts: %v", err)
	} else if !strings.Contains(usr, "## Ranked services") {
		t.Errorf("fallback did not render the embedded applog prompt:\n%.200s", usr)
	}
	if err := os.MkdirAll(filepath.Join(dir, "applog", "daily"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "applog", "daily", "system.md"), []byte("override"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := buildAppLogPrompt(applogFixtureData(), dir, modeDaily); err == nil {
		t.Fatal("a missing user.md inside an existing applog subtree must fail, not fall back")
	}
}

func TestBuildAppLogPromptRejectsOtherModes(t *testing.T) {
	for _, mode := range []string{modeWeekly, modeIncident, "bogus"} {
		if _, _, err := buildAppLogPrompt(applogFixtureData(), "", mode); err == nil || !strings.Contains(err.Error(), "unknown prompt mode") {
			t.Errorf("mode %q: err = %v, want unknown prompt mode", mode, err)
		}
	}
}

func TestRunAppLogShortCircuitsOnEmptyData(t *testing.T) {
	srv := newFakeOllamaServer()
	defer srv.Close()

	a := New(&applogStub{}, ollama.New(srv.srv.URL, 5*time.Second), Config{Model: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := a.Run(context.Background(), RunParams{Feed: model.AnalysisFeedApplog, Period: 24 * time.Hour, Mode: modeDaily})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := srv.chatCalls.Load(); got != 0 {
		t.Errorf("Chat call count: got %d, want 0", got)
	}
	if !strings.Contains(res.Report, "No warnings or errors recorded on the applog feed") {
		t.Errorf("Report missing applog empty-state body; got:\n%s", res.Report)
	}
	if !strings.HasPrefix(res.Report, "# Daily Application Log Briefing") {
		t.Errorf("Report missing applog title; got:\n%s", res.Report)
	}
}

func TestRunAppLogCallsLLMOnData(t *testing.T) {
	srv := newFakeOllamaServer()
	srv.chatReply = chatReplyApplog
	defer srv.Close()

	a := New(rankingFixture(), ollama.New(srv.srv.URL, 5*time.Second), Config{Model: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := a.Run(context.Background(), RunParams{Feed: model.AnalysisFeedApplog, Period: 24 * time.Hour, Mode: modeDaily})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := srv.chatCalls.Load(); got != 1 {
		t.Errorf("Chat call count: got %d, want 1 (reply validates, so no retry)", got)
	}
	if !strings.Contains(res.Report, "## New errors and warnings") || !strings.HasPrefix(res.Report, "# Daily Application Log Briefing") {
		t.Errorf("Report not assembled from the applog reply:\n%s", res.Report)
	}
}

func TestRunAppLogRejectsWeeklyMode(t *testing.T) {
	srv := newFakeOllamaServer()
	defer srv.Close()

	a := New(rankingFixture(), ollama.New(srv.srv.URL, 5*time.Second), Config{Model: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := a.Run(context.Background(), RunParams{Feed: model.AnalysisFeedApplog, Period: 7 * 24 * time.Hour, Mode: modeWeekly})
	if err == nil || !strings.Contains(err.Error(), "unknown prompt mode") {
		t.Fatalf("Run(applog, weekly) = %v, want unknown prompt mode error", err)
	}
	if got := srv.chatCalls.Load(); got != 0 {
		t.Errorf("Chat called %d times on an unsupported mode", got)
	}
}
