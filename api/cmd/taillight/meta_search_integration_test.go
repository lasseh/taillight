package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lasseh/taillight/internal/handler"
	"github.com/lasseh/taillight/internal/postgres"
)

// TestIntegration_MetaEndpointsSearchPastMetaLimit serves the netlog host
// list and the analysis host picker over the real store after 10001 names,
// more than the list cap of 10000, have been cached ahead of a real host:
// the default lists leave the host out, and a search returns it.
func TestIntegration_MetaEndpointsSearchPastMetaLimit(t *testing.T) {
	pool := applogTestPool(t)
	ctx := context.Background()
	reset := func() {
		if _, err := pool.Exec(ctx, "TRUNCATE netlog_meta_cache, netlog_events"); err != nil {
			t.Fatalf("truncate: %v", err)
		}
	}
	reset()
	t.Cleanup(reset)

	if _, err := pool.Exec(ctx, `INSERT INTO netlog_meta_cache (column_name, value)
		SELECT 'hostname', 'a-' || lpad(g::text, 6, '0') FROM generate_series(1, 10001) g`); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO netlog_events (received_at, reported_at, hostname, fromhost_ip, severity, facility, message)
		VALUES (now(), now(), 'zz-core-1', '10.0.0.1'::inet, 6, 1, 'link up')`); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	store := postgres.NewStore(pool)
	meta := handler.NewNetlogMetaHandler(store)
	analysis := handler.NewAnalysisHandler(store, nil)
	for _, tc := range []struct {
		name, target string
		serve        http.HandlerFunc
		wantListed   bool
	}{
		{"netlog hosts", "/api/v1/netlog/meta/hosts", meta.Hosts, false},
		{"netlog hosts search", "/api/v1/netlog/meta/hosts?search=zz-core-1", meta.Hosts, true},
		{"analysis hosts", "/api/v1/analysis/hosts?feed=netlog", analysis.Hosts, false},
		{"analysis hosts search", "/api/v1/analysis/hosts?feed=netlog&search=zz-core-1", analysis.Hosts, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, tc.target, nil)
			rec := httptest.NewRecorder()
			tc.serve(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			if listed := strings.Contains(rec.Body.String(), `"zz-core-1"`); listed != tc.wantListed {
				t.Errorf("zz-core-1 listed = %v, want %v", listed, tc.wantListed)
			}
		})
	}
}
