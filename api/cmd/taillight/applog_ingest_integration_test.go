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

// TestIntegration_IngestAttrsStoredTextStaysWithinCap ingests the largest
// attrs the size limit admits when it is made of numbers written with
// exponents, and checks that Postgres prints the stored value within 64 KB.
func TestIntegration_IngestAttrsStoredTextStaysWithinCap(t *testing.T) {
	pool := applogTestPool(t)
	ctx := context.Background()

	logger := slog.New(slog.DiscardHandler)
	ingest := handler.NewAppLogIngestHandler(postgres.NewStore(pool), broker.NewAppLogBroker(logger), logger, nil)

	numbers := func(num string, n int) string {
		return "[" + strings.TrimSuffix(strings.Repeat(num+",", n), ",") + "]"
	}
	tests := []struct {
		name  string
		attrs string
	}{
		{name: "positive exponents", attrs: numbers("1e400", 161)},
		{name: "negative exponents", attrs: `{"n":` + numbers("1e-400", 161) + `}`},
		{name: "long mantissas", attrs: numbers("9.87654321e-400", 157)},
		{name: "smallest float64", attrs: numbers("5e-324", 197)},
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, "TRUNCATE applog_events"); err != nil {
				t.Fatalf("truncate: %v", err)
			}
			body := `{"logs":[{"timestamp":"` + ts + `","level":"INFO","msg":"m","service":"api","host":"web01","attrs":` + tt.attrs + `}]}`
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/applog/ingest", strings.NewReader(body))
			rec := httptest.NewRecorder()
			ingest.Ingest(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("got status %d, want %d; body: %.300s", rec.Code, http.StatusAccepted, rec.Body.String())
			}

			var printed int
			if err := pool.QueryRow(ctx, `SELECT octet_length(attrs::text) FROM applog_events`).Scan(&printed); err != nil {
				t.Fatalf("query stored attrs: %v", err)
			}
			if printed > 64*1024 {
				t.Errorf("stored attrs prints %d bytes from %d raw, want at most %d", printed, len(tt.attrs), 64*1024)
			}
			t.Logf("raw %d bytes, stored text %d bytes", len(tt.attrs), printed)
		})
	}
}
