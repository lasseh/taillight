package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/handler"
	"github.com/lasseh/taillight/internal/postgres"
)

// TestIntegration_IngestStoresEntriesWithNULBytes sends NUL bytes through the
// ingest handler into Postgres, which rejects them in text and jsonb. Without
// the replacement at ingest, the entry beside them is lost too.
func TestIntegration_IngestStoresEntriesWithNULBytes(t *testing.T) {
	pool := applogTestPool(t)
	ctx := context.Background()

	logger := slog.New(slog.DiscardHandler)
	ingest := handler.NewAppLogIngestHandler(postgres.NewStore(pool), broker.NewAppLogBroker(logger), logger, nil)

	ts := time.Now().UTC().Format(time.RFC3339)
	body := `{"logs":[` +
		`{"timestamp":"` + ts + `","level":"INFO","msg":"clean","service":"api","host":"web01"},` +
		`{"timestamp":"` + ts + `","level":"INFO","msg":"a\u0000b","service":"api","host":"web01","attrs":{"k":"v\u0000"}}` +
		`]}`
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/applog/ingest", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ingest.Ingest(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("got status %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	var msg, attr string
	if err := pool.QueryRow(ctx, `SELECT msg, attrs->>'k' FROM applog_events WHERE attrs IS NOT NULL`).Scan(&msg, &attr); err != nil {
		t.Fatalf("query stored entry: %v", err)
	}
	if msg != "a\ufffdb" || attr != "v\ufffd" {
		t.Errorf("stored msg = %q, attr = %q, want the NUL replaced", msg, attr)
	}

	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM applog_events`).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 2 {
		t.Errorf("stored %d entries, want 2", total)
	}
}
