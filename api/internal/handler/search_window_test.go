package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/httputil"
	"github.com/lasseh/taillight/internal/model"
)

// The list routes refuse a message search with no lower time bound and serve
// one that has it.
func TestListSearchRequiresFrom(t *testing.T) {
	feeds := []struct {
		name string
		path string
		list http.HandlerFunc
	}{
		{"srvlog", "/api/v1/srvlog", NewSrvlogHandler(&mockSrvlogStore{}, markRules()).List},
		{"netlog", "/api/v1/netlog", NewNetlogHandler(&recordingNetlogStore{}, markRules()).List},
		{"applog", "/api/v1/applog", NewAppLogHandler(&mockAppLogStore{}).List},
	}
	queries := []struct {
		name  string
		query string
		want  int
	}{
		{"search without a window", "?search=zq", http.StatusBadRequest},
		{"search with only to", "?search=zq&to=2026-11-03T00:00:00Z", http.StatusBadRequest},
		{"search with from", "?search=zq&from=2026-11-02T00:00:00Z&to=2026-11-03T00:00:00Z", http.StatusOK},
		{"search over the span", "?search=zq&from=2026-10-01T00:00:00Z&to=2026-11-03T00:00:00Z", http.StatusBadRequest},
		{"search from long ago", "?search=zq&from=1970-01-01T00:00:00Z", http.StatusBadRequest},
		{"no search", "", http.StatusOK},
	}
	for _, f := range feeds {
		for _, q := range queries {
			t.Run(f.name+"/"+q.name, func(t *testing.T) {
				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, f.path+q.query, nil)
				rec := httptest.NewRecorder()
				f.list(rec, req)
				if rec.Code != q.want {
					t.Fatalf("status = %d, want %d; body %s", rec.Code, q.want, rec.Body.String())
				}
				if q.want != http.StatusBadRequest {
					return
				}
				var body httputil.ErrorBody
				if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if body.Error.Code != "invalid_filter" {
					t.Errorf("error = %+v, want invalid_filter", body.Error)
				}
			})
		}
	}
}

func TestSearchBackfillFrom(t *testing.T) {
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	now := time.Now()

	tests := []struct {
		name   string
		search string
		from   *time.Time
		to     *time.Time
		want   *time.Time // nil: expect nil; zero: expect about now - window
	}{
		{"no search keeps no window", "", nil, nil, nil},
		{"search keeps the given from", "disk", &from, nil, &from},
		{"search without a window ends now", "disk", nil, nil, &time.Time{}},
		{"search with only to ends at to", "disk", nil, &to, new(to.Add(-sseSearchWindow))},
		{"search from long ago is clamped", "disk", new(to.Add(-90 * 24 * time.Hour)), &to, new(to.Add(-maxSearchSpan))},
		{"no search keeps a long window", "", new(to.Add(-90 * 24 * time.Hour)), &to, new(to.Add(-90 * 24 * time.Hour))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := searchBackfillFrom(tt.search, tt.from, tt.to)
			switch {
			case tt.want == nil:
				if got != nil {
					t.Errorf("from = %v, want nil", *got)
				}
			case tt.want.IsZero():
				if got == nil || got.Sub(now.Add(-sseSearchWindow)).Abs() > time.Minute {
					t.Errorf("from = %v, want about %v", got, now.Add(-sseSearchWindow))
				}
			default:
				if got == nil || !got.Equal(*tt.want) {
					t.Errorf("from = %v, want %v", got, *tt.want)
				}
			}
		})
	}
}

// backfillFilterStore hands the filter of each recent-events query to the
// test.
type backfillFilterStore struct {
	SrvlogStore // nil: any method not overridden below panics
	got         chan model.SrvlogFilter
}

func (s *backfillFilterStore) ListSrvlogs(_ context.Context, f model.SrvlogFilter, _ *model.Cursor, _ int) ([]model.SrvlogEvent, *model.Cursor, error) {
	s.got <- f
	return nil, nil, nil
}

// A searching stream that sets no window backfills only the default window.
func TestStreamSearchBackfillHasWindow(t *testing.T) {
	logger := testLogger()
	store := &backfillFilterStore{got: make(chan model.SrvlogFilter, 1)}
	h := NewSrvlogSSEHandler(broker.NewSrvlogBroker(logger), store, markRules(), logger)
	srv := httptest.NewServer(http.HandlerFunc(h.Stream))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"?search=disk", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	select {
	case f := <-store.got:
		want := time.Now().Add(-sseSearchWindow)
		if f.From == nil || f.From.Sub(want).Abs() > time.Minute {
			t.Errorf("backfill from = %v, want about %v", f.From, want)
		}
		if f.Search != "disk" {
			t.Errorf("backfill search = %q, want %q", f.Search, "disk")
		}
	case <-ctx.Done():
		t.Fatal("stream ran no backfill query")
	}
}
