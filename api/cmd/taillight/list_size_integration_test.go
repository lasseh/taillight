package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/handler"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/mute"
	"github.com/lasseh/taillight/internal/postgres"
)

// Sizes of the largest row rsyslog writes from one 64 KiB message.
const (
	maxMessageBytes = 65441
	maxRawBytes     = 65483
)

// listFeeds pairs each syslog feed's table with its list handler.
func listFeeds(store *postgres.Store) map[string]http.HandlerFunc {
	mutes := mute.NewSet(nil, time.Now, slog.New(slog.DiscardHandler))
	return map[string]http.HandlerFunc{
		"srvlog": handler.NewSrvlogHandler(store, mutes).List,
		"netlog": handler.NewNetlogHandler(store, mutes).List,
	}
}

func getList(t *testing.T, h http.HandlerFunc, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/?"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET ?%s: status %d", query, rec.Code)
	}
	return rec
}

// TestIntegration_ListBodyStaysUnderByteBudget fills one full page plus one
// row of each syslog feed with the largest rows rsyslog writes, all '<'
// (six bytes each in JSON), and checks the list body at limit=1000 stays
// under a budget set by the preview limits.
func TestIntegration_ListBodyStaysUnderByteBudget(t *testing.T) {
	pool := migratedTestPool(t, "srvlog_events", "netlog_events")
	store := postgres.NewStore(pool)
	ctx := context.Background()

	const limit = 1000
	const rowOverhead = 1024 // every field but the three text fields
	budget := limit * (6*(model.MessagePreviewLimit+2*model.RawPreviewLimit) + rowOverhead)

	for feed, list := range listFeeds(store) {
		t.Run(feed, func(t *testing.T) {
			if _, err := pool.Exec(ctx,
				`INSERT INTO `+feed+`_events (received_at, reported_at, hostname, fromhost_ip, severity, facility, message, raw_message, structured_data)
				 SELECT now() - i * interval '1 second', now(), 'big', '10.0.0.1'::inet, 6, 1,
				        repeat('<', $1), repeat('<', $2), repeat('<', $1)
				 FROM generate_series(1, $3) AS i`, maxMessageBytes, maxRawBytes, limit+1); err != nil {
				t.Fatalf("seed: %v", err)
			}

			rec := getList(t, list, "limit=1000")
			size := rec.Body.Len()
			var resp struct {
				Data []struct {
					Truncated bool `json:"truncated"`
				} `json:"data"`
				HasMore bool `json:"has_more"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(resp.Data) != limit || !resp.HasMore {
				t.Fatalf("got %d rows, has_more %v; want a full page of %d", len(resp.Data), resp.HasMore, limit)
			}
			for i, row := range resp.Data {
				if !row.Truncated {
					t.Fatalf("row %d not flagged truncated", i)
				}
			}
			if size > budget {
				t.Errorf("body is %d bytes, over the %d-byte budget", size, budget)
			}
			t.Logf("%s list body at limit=%d with max-size rows: %d bytes (%.1f MB, budget %d)", feed, limit, size, float64(size)/1e6, budget)
		})
	}
}

// TestIntegration_ExportKeepsFullText checks that CSV export returns the
// whole message of a row whose list entry is cut.
func TestIntegration_ExportKeepsFullText(t *testing.T) {
	pool := migratedTestPool(t, "srvlog_events", "netlog_events")
	store := postgres.NewStore(pool)
	ctx := context.Background()
	export := handler.NewExportHandler(store, store, store)
	exports := map[string]http.HandlerFunc{"srvlog": export.ExportSrvlogs, "netlog": export.ExportNetlogs}
	msg := strings.Repeat("m", model.MessagePreviewLimit) + " end of message"

	for feed, list := range listFeeds(store) {
		t.Run(feed, func(t *testing.T) {
			if _, err := pool.Exec(ctx,
				`INSERT INTO `+feed+`_events (received_at, reported_at, hostname, fromhost_ip, severity, facility, message)
				 VALUES (now(), now(), 'long', '10.0.0.1'::inet, 6, 1, $1)`, msg); err != nil {
				t.Fatalf("seed: %v", err)
			}

			var page struct {
				Data []struct {
					Message   string `json:"message"`
					Truncated bool   `json:"truncated"`
				} `json:"data"`
			}
			if err := json.Unmarshal(getList(t, list, "hostname=long").Body.Bytes(), &page); err != nil {
				t.Fatalf("decode list: %v", err)
			}
			if len(page.Data) != 1 || len(page.Data[0].Message) != model.MessagePreviewLimit || !page.Data[0].Truncated {
				t.Fatalf("list row = %+v, want the message cut to %d bytes and flagged", page.Data, model.MessagePreviewLimit)
			}

			q := url.Values{
				"hostname": {"long"},
				"from":     {time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)},
				"to":       {time.Now().Add(time.Hour).UTC().Format(time.RFC3339)},
			}
			rec := httptest.NewRecorder()
			exports[feed](rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/?"+q.Encode(), nil))
			records, err := csv.NewReader(rec.Body).ReadAll()
			if err != nil {
				t.Fatalf("read csv: %v", err)
			}
			if len(records) != 2 {
				t.Fatalf("csv has %d records, want header and one row", len(records))
			}
			if got := records[1][len(records[1])-1]; got != msg {
				t.Errorf("csv message is %d bytes, want the full %d", len(got), len(msg))
			}
		})
	}
}
