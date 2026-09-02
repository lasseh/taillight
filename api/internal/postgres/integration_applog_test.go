package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/taillight/internal/model"
)

func insertAppLogEvent(t *testing.T, pool *pgxpool.Pool, at time.Time, service, component, host, level, msg, attrs string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO applog_events (received_at, timestamp, level, service, component, host, msg, attrs)
		 VALUES ($1, $1, $2, $3, $4, $5, $6, NULLIF($7, '')::jsonb)`,
		at, level, service, component, host, msg, attrs); err != nil {
		t.Fatalf("insert applog event: %v", err)
	}
}

// applogFixture is the window the applog analysis tests query, seeded by
// seedAppLogFixture with rows whose answers are known.
type applogFixture struct {
	now, since, baselineSince time.Time
}

// seedAppLogFixture inserts: api with three of the same error on two hosts,
// five warnings, ten info rows, and one new error with an empty component
// and oversized attrs; worker with the same error now and in the baseline,
// so it is not new.
func seedAppLogFixture(t *testing.T, pool *pgxpool.Pool) applogFixture {
	t.Helper()
	now := time.Now().UTC()
	inWindow := now.Add(-time.Hour)
	baseline := now.Add(-72 * time.Hour)

	for i := range 3 {
		host := "h1"
		if i == 2 {
			host = "h2"
		}
		insertAppLogEvent(t, pool, inWindow, "api", "db", host, "ERROR", "db timeout after 30ms", `{"err":"timeout"}`)
	}
	for range 5 {
		insertAppLogEvent(t, pool, inWindow, "api", "http", "h1", "WARN", "slow query 120ms", "")
	}
	for range 10 {
		insertAppLogEvent(t, pool, inWindow, "api", "http", "h1", "INFO", "request ok", "")
	}
	bigAttrs := `{"trace":"` + strings.Repeat("f", model.AttrsPreviewLimit) + `"}`
	insertAppLogEvent(t, pool, inWindow, "api", "", "h1", "ERROR", "panic: nil deref", bigAttrs)
	insertAppLogEvent(t, pool, inWindow, "worker", "queue", "h3", "ERROR", "queue full", "")
	insertAppLogEvent(t, pool, baseline, "worker", "queue", "h3", "ERROR", "queue full", "")

	since := now.Add(-24 * time.Hour)
	return applogFixture{now: now, since: since, baselineSince: since.Add(-7 * 24 * time.Hour)}
}

// TestIntegration_AppLogAnalysisQueries runs every applog analysis query
// against a real TimescaleDB: the real-time aggregate, the msg_pattern
// trigger, the anti-join for new templates, and the unnest sample join all
// have to agree on one fixture.
func TestIntegration_AppLogAnalysisQueries(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "applog_events")
	store := NewStore(pool)
	fx := seedAppLogFixture(t, pool)

	t.Run("service stats", func(t *testing.T) { checkAppLogServiceStats(t, store, fx) })
	t.Run("top templates", func(t *testing.T) { checkAppLogTopTemplates(t, store, fx) })
	t.Run("new templates", func(t *testing.T) { checkAppLogNewTemplates(t, store, fx) })
	t.Run("samples", func(t *testing.T) { checkAppLogSamples(t, store, fx) })
	t.Run("hygiene", func(t *testing.T) { checkAppLogHygiene(t, store, fx) })
	t.Run("volume timeline", func(t *testing.T) { checkAppLogVolume(t, store, fx) })
	t.Run("no CTE scan", func(t *testing.T) { checkAppLogNoCTEScan(t, pool, fx) })
}

func checkAppLogServiceStats(t *testing.T, store *Store, fx applogFixture) {
	stats, err := store.GetAppLogServiceStats(context.Background(), model.AnalysisScope{Feed: "applog"}, fx.since, fx.baselineSince)
	if err != nil {
		t.Fatalf("GetAppLogServiceStats: %v", err)
	}
	byName := map[string]model.AppLogServiceStats{}
	for _, s := range stats {
		byName[s.Service] = s
	}
	api := byName["api"]
	if api.Current.Total != 19 || api.Current.Error != 4 || api.Current.Warn != 5 || api.Current.Fatal != 0 {
		t.Errorf("api current = %+v, want total 19, error 4, warn 5", api.Current)
	}
	worker := byName["worker"]
	if worker.Current.Error != 1 || worker.Baseline.Error != 1 || worker.Baseline.Total != 1 {
		t.Errorf("worker = %+v, want 1 error now and 1 in baseline", worker)
	}
}

func checkAppLogTopTemplates(t *testing.T, store *Store, fx applogFixture) {
	top, err := store.GetAppLogTopTemplates(context.Background(), fx.since, []string{"api"}, 5, 3)
	if err != nil {
		t.Fatalf("GetAppLogTopTemplates: %v", err)
	}
	if len(top) != 3 {
		t.Fatalf("got %d templates (%+v), want 3", len(top), top)
	}
	if top[0].Pattern != "db timeout after <n>ms" || top[0].Count != 3 || top[0].HostCount != 2 || top[0].Level != "ERROR" {
		t.Errorf("top[0] = %+v, want db timeout error, count 3, 2 hosts", top[0])
	}
	if top[1].Pattern != "panic: nil deref" || top[1].Component != "" {
		t.Errorf("top[1] = %+v, want the panic with empty component", top[1])
	}
	if top[2].Pattern != "slow query <n>ms" || top[2].Level != "WARN" || top[2].Count != 5 {
		t.Errorf("top[2] = %+v, want slow query warning, count 5", top[2])
	}
}

func checkAppLogNewTemplates(t *testing.T, store *Store, fx applogFixture) {
	newT, err := store.GetAppLogNewTemplates(context.Background(), model.AnalysisScope{Feed: "applog"}, fx.since, fx.baselineSince)
	if err != nil {
		t.Fatalf("GetAppLogNewTemplates: %v", err)
	}
	if len(newT) == 0 {
		t.Fatal("no new templates reported")
	}
	patterns := make([]string, 0, len(newT))
	for _, n := range newT {
		patterns = append(patterns, n.Pattern)
	}
	joined := strings.Join(patterns, "|")
	if strings.Contains(joined, "queue full") {
		t.Errorf("template seen in baseline reported as new: %v", patterns)
	}
	if !strings.Contains(joined, "panic: nil deref") || !strings.Contains(joined, "slow query <n>ms") {
		t.Errorf("new templates missing: %v", patterns)
	}
	if newT[len(newT)-1].Level != "WARN" {
		t.Errorf("warnings should sort last: %v", newT)
	}
}

func checkAppLogSamples(t *testing.T, store *Store, fx applogFixture) {
	key := model.AppLogTemplateKey{Service: "api", Component: "", Pattern: "panic: nil deref"}
	samples, err := store.GetAppLogTemplateSamples(context.Background(), fx.since,
		[]model.AppLogTemplateKey{key, {Service: "nope", Pattern: "missing"}}, 300)
	if err != nil {
		t.Fatalf("GetAppLogTemplateSamples: %v", err)
	}
	sm, ok := samples[key]
	if !ok {
		t.Fatalf("no sample for %+v: %+v", key, samples)
	}
	if sm.Host != "h1" || sm.Level != "ERROR" || sm.Msg != "panic: nil deref" || !strings.Contains(sm.Attrs, `"trace"`) {
		t.Errorf("sample = %+v", sm)
	}
	if len(samples) != 1 {
		t.Errorf("unknown key produced a sample: %+v", samples)
	}
}

func checkAppLogHygiene(t *testing.T, store *Store, fx applogFixture) {
	h, err := store.GetAppLogHygiene(context.Background(), model.AnalysisScope{Feed: "applog"}, fx.since, 0.3, 1, 5)
	if err != nil {
		t.Fatalf("GetAppLogHygiene: %v", err)
	}
	if h.WarnPlusRows != 10 || h.EmptyComponent != 1 || h.OversizeAttrs != 1 {
		t.Errorf("hygiene counts = %+v, want 10 warn+ rows, 1 empty component, 1 oversize attrs", h)
	}
	found := false
	for _, d := range h.Dominant {
		if d.Service == "api" && d.Pattern == "slow query <n>ms" && d.Count == 5 && d.ServiceTotal == 5 {
			found = true
		}
	}
	if !found {
		t.Errorf("api's dominant template not reported: %+v", h.Dominant)
	}
	for _, d := range h.Dominant {
		if d.Service == "worker" {
			t.Errorf("worker's ERROR template counted as a dominant warning: %+v", d)
		}
	}
}

func checkAppLogVolume(t *testing.T, store *Store, fx applogFixture) {
	buckets, err := store.GetAppLogVolumeTimeline(context.Background(), model.AnalysisScope{Feed: "applog"}, fx.since, fx.now, 60)
	if err != nil {
		t.Fatalf("GetAppLogVolumeTimeline: %v", err)
	}
	var total, errs int64
	for _, b := range buckets {
		total += b.Total
		errs += b.ErrorCount
	}
	if total != 20 || errs != 5 {
		t.Errorf("timeline sums = %d total, %d errors; want 20, 5 (%+v)", total, errs, buckets)
	}
}

// checkAppLogNoCTEScan is the outage guard for the applog queries: a CTE
// Scan in a plan means the time predicate is fenced off from the scan.
func checkAppLogNoCTEScan(t *testing.T, pool *pgxpool.Pool, fx applogFixture) {
	ctx := context.Background()
	for _, sc := range applogScopes() {
		for _, q := range []struct {
			name string
			sql  string
			args []any
		}{
			{"top", applogTopTemplatesQuery(), []any{fx.since, applogAnalysisLevels, []string{"api"}, 5, 3, appLevelWarn}},
			{"new", applogNewTemplatesQuery(sc.scope), appendServicesArg([]any{fx.since, fx.baselineSince, applogAnalysisLevels, appLevelWarn}, sc.scope)},
			{"dominant", applogDominantTemplatesQuery(sc.scope), appendServicesArg([]any{fx.since, []string{appLevelWarn}, int64(1), 0.3, 5}, sc.scope)},
		} {
			plan := explainPlan(t, pool, ctx, q.sql, q.args)
			if strings.Contains(plan, "CTE Scan") {
				t.Errorf("%s/%s plan contains a CTE Scan:\n%s", sc.name, q.name, plan)
			}
		}
	}
}

func explainPlan(t *testing.T, pool *pgxpool.Pool, ctx context.Context, sql string, args []any) string {
	t.Helper()
	rows, err := pool.Query(ctx, "EXPLAIN "+sql, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	return plan.String()
}

// TestIntegration_AppLogReportServicesScope covers migration 23: the
// services column round-trips through insert, get, and list; the active
// report index keys on it; and the service picker query reads the meta
// cache and aggregate the fixture populated.
func TestIntegration_AppLogReportServicesScope(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "analysis_reports", "applog_events")
	store := NewStore(pool)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Minute)

	base := model.AnalysisReport{
		Feed:        model.AnalysisFeedApplog,
		PromptMode:  model.AnalysisModeDaily,
		PeriodStart: now.Add(-24 * time.Hour),
		PeriodEnd:   now,
	}
	first := base
	first.Services = []string{"worker", "api", "api"}
	inserted, err := store.InsertPendingReport(ctx, first)
	if err != nil {
		t.Fatalf("InsertPendingReport: %v", err)
	}
	if len(inserted.Services) != 2 || inserted.Services[0] != "api" || inserted.Services[1] != "worker" {
		t.Errorf("services not normalized on insert: %v", inserted.Services)
	}

	got, err := store.GetReportBySlug(ctx, inserted.Slug)
	if err != nil {
		t.Fatalf("GetReportBySlug: %v", err)
	}
	if len(got.Services) != 2 || got.Services[1] != "worker" || len(got.Hosts) != 0 {
		t.Errorf("round-trip = services %v hosts %v", got.Services, got.Hosts)
	}
	list, err := store.ListReports(ctx, 10)
	if err != nil {
		t.Fatalf("ListReports: %v", err)
	}
	if len(list) != 1 || len(list[0].Services) != 2 {
		t.Errorf("summary services = %+v", list)
	}

	// A different scope for the same window runs concurrently; the same
	// scope collides.
	other := base
	other.Services = []string{"billing"}
	if _, err := store.InsertPendingReport(ctx, other); err != nil {
		t.Errorf("different service scope should not collide: %v", err)
	}
	same := base
	same.Services = []string{"api", "worker"}
	if _, err := store.InsertPendingReport(ctx, same); !errors.Is(err, ErrDuplicateActiveReport) {
		t.Errorf("same service scope = %v, want ErrDuplicateActiveReport", err)
	}

	seedAppLogFixture(t, pool)
	entries, err := store.ListAnalysisServiceEntries(ctx)
	if err != nil {
		t.Fatalf("ListAnalysisServiceEntries: %v", err)
	}
	byName := map[string]model.AnalysisServiceEntry{}
	for _, e := range entries {
		byName[e.Service] = e
	}
	for _, svc := range []string{"api", "worker"} {
		e, ok := byName[svc]
		if !ok || e.LastSeen == nil {
			t.Errorf("service %q missing or without last_seen: %+v", svc, entries)
		}
	}
}
