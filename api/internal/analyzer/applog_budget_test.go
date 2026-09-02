package analyzer

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// applogBudgetFixture fills every cap in DefaultAppLogCaps to the brim with
// realistic-length names, patterns, and attrs, so the rendered prompt is
// the worst case the defaults allow.
func applogBudgetFixture() applogData {
	caps := DefaultAppLogCaps()
	now := time.Date(2026, 9, 1, 6, 0, 0, 0, time.UTC)
	longPattern := "upstream payments-gateway returned <n> for POST /v<n>/orders/<n>/capture after <n>ms (attempt <n> of <n>)"
	attrs := strings.Repeat(`{"err":"context deadline exceeded","upstream":"payments-gateway","status":504,"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","path":"/v2/orders/123/capture"}`, 3)
	sample := func(i int) *model.AppLogSample {
		return &model.AppLogSample{
			Host: fmt.Sprintf("orders-api-%02d.prod.example.internal", i), Level: "ERROR",
			ReceivedAt: now.Add(-time.Duration(i) * time.Minute),
			Msg:        strings.Repeat("upstream payments-gateway returned 504 for POST /v2/orders/123/capture ", 5)[:caps.SampleMsgChars],
			Attrs:      compactAttrs(attrs, caps.SampleAttrsBytes),
		}
	}
	tmpl := func(svc string, i int, level string, withSample bool) model.AppLogTemplate {
		t := model.AppLogTemplate{
			AppLogTemplateKey: model.AppLogTemplateKey{Service: svc, Component: "http-client", Pattern: fmt.Sprintf("%s [%d]", longPattern, i), Level: level},
			Count:             int64(5000 - i*7), HostCount: 12,
			FirstSeen: now.Add(-23 * time.Hour), LastSeen: now,
		}
		if withSample {
			t.Sample = sample(i)
		}
		return t
	}

	data := applogData{
		Feed: model.AnalysisFeedApplog, PeriodLabel: "24 hours", PeriodStart: now.Add(-24 * time.Hour), PeriodEnd: now,
		ActiveServices: 300, Caps: caps, Unavailable: map[string]bool{},
		Drift: []applogLevelDrift{
			{Label: "FATAL", Current: 3, BaselineAvg: 1, ChangePct: 200},
			{Label: "ERROR", Current: 5200, BaselineAvg: 1000, ChangePct: 420},
			{Label: "WARN", Current: 41000, BaselineAvg: 45000, ChangePct: -8.9},
			{Label: "all levels", Current: 5000000, BaselineAvg: 4800000, ChangePct: 4.2},
		},
		VolumeBucketLabel: "1 hour",
		VolumeSparkline:   strings.Repeat("▁▂▃▅▇█▆▄", 3),
		ErrorSparkline:    strings.Repeat("▁▁▁▂▄█▆▃", 3),
		VolumePeaks:       []string{"09-01 03:00 (4000 err / 600000 total)", "09-01 04:00 (900 err / 500000 total)", "09-01 05:00 (300 err / 400000 total)"},
	}
	samplesLeft := caps.TemplateSamples
	for i := range caps.RankedServices {
		svc := fmt.Sprintf("orders-fulfilment-service-%02d", i)
		rep := applogServiceReport{applogServiceRow: applogServiceRow{
			Service:       svc,
			Current:       model.AppLogLevelCounts{Total: 400000, Warn: 3000, Error: 800, Fatal: 1},
			CurrentPerDay: applogPerDay{Total: 400000, Warn: 3000, Error: 800, Fatal: 1},
			Baseline:      applogPerDay{Total: 380000, Warn: 3100, Error: 120},
			NewTemplates:  2,
		}}
		for j := range caps.ErrorTemplatesPerService {
			rep.ErrorTemplates = append(rep.ErrorTemplates, tmpl(svc, i*10+j, "ERROR", samplesLeft > 0))
			if samplesLeft > 0 {
				samplesLeft--
			}
		}
		for j := range caps.WarnTemplatesPerService {
			rep.WarnTemplates = append(rep.WarnTemplates, tmpl(svc, i*10+5+j, "WARN", false))
		}
		data.Ranked = append(data.Ranked, rep)
	}
	for i := range caps.LongTailServices {
		data.LongTail = append(data.LongTail, applogServiceRow{
			Service: fmt.Sprintf("catalog-search-indexer-%02d", i),
			Current: model.AppLogLevelCounts{Total: 20000, Warn: 400, Error: 40}, NewTemplates: i % 2,
		})
	}
	data.Remainder = 300 - caps.RankedServices - caps.LongTailServices
	data.RemainderWarnPlus = 12345
	for i := range caps.NewTemplates {
		level := "ERROR"
		if i%3 == 2 {
			level = "WARN"
		}
		data.NewTemplates = append(data.NewTemplates, tmpl(fmt.Sprintf("billing-reconciliation-worker-%02d", i), 900+i, level, true))
	}
	for i := range caps.SilentServices {
		data.Silent = append(data.Silent, applogServiceRow{Service: fmt.Sprintf("nightly-export-cron-%02d", i), Baseline: applogPerDay{Total: 12000}})
		data.NewServices = append(data.NewServices, applogServiceRow{Service: fmt.Sprintf("search-v2-shard-%02d", i), Current: model.AppLogLevelCounts{Total: 30000, Warn: 20, Error: 3}})
	}
	data.Hygiene = model.AppLogHygiene{WarnPlusRows: 46200, EmptyComponent: 4000, OversizeAttrs: 300}
	for i := range hygieneDominantLimit {
		data.Hygiene.Dominant = append(data.Hygiene.Dominant, model.AppLogDominantTemplate{
			AppLogTemplateKey: model.AppLogTemplateKey{Service: fmt.Sprintf("orders-fulfilment-service-%02d", i), Component: "cache", Pattern: "retrying call to cache attempt <n>"},
			Count:             9000, ServiceTotal: 9500,
		})
	}
	return data
}

// applogPromptBudgetBytes is the ceiling for the rendered user prompt at
// the default caps. At roughly 4 bytes per token for English prose and
// less for JSON, 72 KB is about 18k tokens, which with the ~5k-token
// system prompt leaves the 32k window room for the reply. Raising a cap
// should move this number on purpose, not by accident.
const applogPromptBudgetBytes = 72 * 1024

// TestAppLogPromptBudget renders the worst case the default caps allow and
// checks it fits the budget the caps were sized for.
func TestAppLogPromptBudget(t *testing.T) {
	sys, usr, err := buildAppLogPrompt(applogBudgetFixture(), "", modeDaily)
	if err != nil {
		t.Fatalf("buildAppLogPrompt: %v", err)
	}
	t.Logf("applog daily prompt at default caps: system %d bytes, user %d bytes (~%d tokens)",
		len(sys), len(usr), (len(sys)+len(usr))/4)
	if len(usr) > applogPromptBudgetBytes {
		t.Errorf("user prompt is %d bytes, over the %d-byte budget the default caps were sized for", len(usr), applogPromptBudgetBytes)
	}
	// Every cap-bounded section made it in; a template change that drops
	// one would pass the size check by accident.
	for _, want := range []string{"### 12. `orders-fulfilment-service-11`", "## Long tail (next 25 active services)", "(+263 more active services", "`nightly-export-cron-19`", "`search-v2-shard-19`", "billing-reconciliation-worker-19"} {
		if !strings.Contains(usr, want) {
			t.Errorf("rendered prompt missing %q", want)
		}
	}
}
