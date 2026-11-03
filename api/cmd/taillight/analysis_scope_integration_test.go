package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/handler"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/postgres"
)

// acceptingEnqueuer accepts every report without running it.
type acceptingEnqueuer struct{}

func (acceptingEnqueuer) Enqueue(_ context.Context, req model.AnalysisReport) (model.AnalysisReport, error) {
	return req, nil
}

// TestIntegration_AnalysisScopePastMetaLimit creates scoped reports over the
// real store after 10001 names, more than the meta list cap of 10000, have
// been cached ahead of the real ones: a logged host or service is accepted
// and a never-logged one is refused, on every feed.
func TestIntegration_AnalysisScopePastMetaLimit(t *testing.T) {
	pool := applogTestPool(t)
	ctx := context.Background()
	tables := "netlog_meta_cache, srvlog_meta_cache, applog_meta_cache, netlog_events, srvlog_events, applog_events"
	reset := func() {
		if _, err := pool.Exec(ctx, "TRUNCATE "+tables); err != nil {
			t.Fatalf("truncate: %v", err)
		}
	}
	reset()
	t.Cleanup(reset)

	for _, c := range []struct{ table, column string }{
		{"netlog_meta_cache", "hostname"}, {"srvlog_meta_cache", "hostname"}, {"applog_meta_cache", "service"},
	} {
		//nolint:gosec // test-only table names, not user input
		query := "INSERT INTO " + c.table + ` (column_name, value)
			SELECT $1, 'a-' || lpad(g::text, 6, '0') FROM generate_series(1, 10001) g`
		if _, err := pool.Exec(ctx, query, c.column); err != nil {
			t.Fatalf("seed %s: %v", c.table, err)
		}
	}
	// The real names arrive the way production caches them: by logging.
	for _, table := range []string{"netlog_events", "srvlog_events"} {
		//nolint:gosec // test-only table names, not user input
		if _, err := pool.Exec(ctx, "INSERT INTO "+table+` (received_at, reported_at, hostname, fromhost_ip, severity, facility, message)
			VALUES (now(), now(), 'zz-core-1', '10.0.0.1'::inet, 6, 1, 'link up')`); err != nil {
			t.Fatalf("insert %s: %v", table, err)
		}
	}
	store := postgres.NewStore(pool)
	if _, err := store.InsertLogBatch(ctx, []model.AppLogEvent{{Timestamp: time.Now().UTC(), Level: "INFO", Service: "zz-core-api", Host: "h", Msg: "started"}}); err != nil {
		t.Fatalf("insert applog event: %v", err)
	}

	h := handler.NewAnalysisHandler(store, acceptingEnqueuer{})
	for _, tc := range []struct {
		name, body, code string
		want             int
	}{
		{"netlog logged host", `{"feed":"netlog","hosts":["zz-core-1"]}`, "", http.StatusCreated},
		{"netlog never-logged host", `{"feed":"netlog","hosts":["never-logged"]}`, "unknown_hosts", http.StatusBadRequest},
		{"srvlog logged host", `{"feed":"srvlog","hosts":["zz-core-1"]}`, "", http.StatusCreated},
		{"srvlog never-logged host", `{"feed":"srvlog","hosts":["never-logged"]}`, "unknown_hosts", http.StatusBadRequest},
		{"applog logged service", `{"feed":"applog","services":["zz-core-api"]}`, "", http.StatusCreated},
		{"applog never-logged service", `{"feed":"applog","services":["never-logged"]}`, "unknown_services", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/analysis/reports", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.Create(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.code != "" && !strings.Contains(rec.Body.String(), tc.code) {
				t.Errorf("body missing %q: %s", tc.code, rec.Body.String())
			}
		})
	}
}
