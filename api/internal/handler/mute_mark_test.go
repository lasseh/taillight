package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/mute"
)

// Read-side marking and hide_muted exclusion, tested through the netlog and
// srvlog read handlers with a fake store that records the filter it gets and
// a real mute.Set on a fixed clock.

var markNow = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

// markRules is one active rule per feed (7 netlog, 8 srvlog) plus an expired
// netlog rule that must neither mark nor exclude.
func markRules() *mute.Set {
	s := mute.NewSet(nil, func() time.Time { return markNow }, slog.New(slog.DiscardHandler))
	s.Replace([]mute.Rule{
		{ID: 3, Feed: mute.FeedNetlog, Hostname: "leaf-02", ExpiresAt: markNow.Add(-time.Minute)},
		{ID: 7, Feed: mute.FeedNetlog, Hostname: "leaf-02", Search: "ae30", ExpiresAt: markNow.Add(time.Hour)},
		{ID: 8, Feed: mute.FeedSrvlog, Hostname: "web01", Programname: "cron", ExpiresAt: markNow.Add(time.Hour)},
	})
	return s
}

// The first event of each pair matches its feed's active rule; the second
// does not.
var (
	markNetlogEvents = []model.NetlogEvent{
		{ID: 2, Hostname: "leaf-02", Message: "bundle AE30 link down"},
		{ID: 1, Hostname: "leaf-02", Message: "bgp neighbor up"},
	}
	markSrvlogEvents = []model.SrvlogEvent{
		{ID: 2, Hostname: "web01", Programname: "cron", Message: "job started"},
		{ID: 1, Hostname: "web01", Programname: "sshd", Message: "accepted key"},
	}
)

type recordingNetlogStore struct {
	NetlogStore // nil: any method not overridden below panics
	events      []model.NetlogEvent
	gotFilter   *model.NetlogFilter
}

func (s *recordingNetlogStore) ListNetlogs(_ context.Context, f model.NetlogFilter, _ *model.Cursor, _ int) ([]model.NetlogEvent, *model.Cursor, error) {
	s.gotFilter = &f
	return slices.Clone(s.events), nil, nil
}

func (s *recordingNetlogStore) ListNetlogsSince(_ context.Context, _ model.NetlogFilter, _ int64, _ int) ([]model.NetlogEvent, error) {
	out := slices.Clone(s.events)
	slices.Reverse(out) // since is ASC
	return out, nil
}

func (s *recordingNetlogStore) GetNetlog(_ context.Context, id int64) (model.NetlogEvent, error) {
	for _, e := range s.events {
		if e.ID == id {
			return e, nil
		}
	}
	return model.NetlogEvent{}, pgx.ErrNoRows
}

func (s *recordingNetlogStore) GetNetlogDeviceSummary(_ context.Context, hostname string) (model.NetlogDeviceSummary, error) {
	return model.NetlogDeviceSummary{Hostname: hostname, CriticalLogs: slices.Clone(s.events)}, nil
}

type recordingSrvlogStore struct {
	SrvlogStore // nil: any method not overridden below panics
	events      []model.SrvlogEvent
	gotFilter   *model.SrvlogFilter
}

func (s *recordingSrvlogStore) ListSrvlogs(_ context.Context, f model.SrvlogFilter, _ *model.Cursor, _ int) ([]model.SrvlogEvent, *model.Cursor, error) {
	s.gotFilter = &f
	return slices.Clone(s.events), nil, nil
}

func (s *recordingSrvlogStore) ListSrvlogsSince(_ context.Context, _ model.SrvlogFilter, _ int64, _ int) ([]model.SrvlogEvent, error) {
	out := slices.Clone(s.events)
	slices.Reverse(out)
	return out, nil
}

func (s *recordingSrvlogStore) GetSrvlog(_ context.Context, id int64) (model.SrvlogEvent, error) {
	for _, e := range s.events {
		if e.ID == id {
			return e, nil
		}
	}
	return model.SrvlogEvent{}, pgx.ErrNoRows
}

func (s *recordingSrvlogStore) GetSrvlogDeviceSummary(_ context.Context, hostname string) (model.SrvlogDeviceSummary, error) {
	return model.SrvlogDeviceSummary{Hostname: hostname, CriticalLogs: slices.Clone(s.events)}, nil
}

// markedRow is the part of a netlog or srvlog event these tests read.
type markedRow struct {
	ID      int64  `json:"id"`
	MutedBy *int64 `json:"muted_by"`
}

// mutedByID maps event id to muted_by for the rows that carry the field.
func mutedByID(t *testing.T, rows []markedRow) map[int64]int64 {
	t.Helper()
	out := map[int64]int64{}
	for _, r := range rows {
		if r.MutedBy != nil {
			out[r.ID] = *r.MutedBy
		}
	}
	return out
}

func serve(t *testing.T, pattern, target string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Get(pattern, h)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil))
	return rec
}

func decodeData[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var resp struct {
		Data T `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.Data
}

func TestNetlogListHideMuted(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		wantExclude []model.NetlogFilter
	}{
		{"hide_muted=true passes the active rules as exclusions", "?hide_muted=true", []model.NetlogFilter{{Hostname: "leaf-02", Search: "ae30"}}},
		{"hide_muted absent passes no exclusions", "", nil},
		{"hide_muted=false passes no exclusions", "?hide_muted=false", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &recordingNetlogStore{events: markNetlogEvents}
			h := NewNetlogHandler(store, markRules())

			rec := serve(t, "/netlog", "/netlog"+tt.query, h.List)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
			}
			if !reflect.DeepEqual(store.gotFilter.Exclude, tt.wantExclude) {
				t.Errorf("Exclude = %#v, want %#v", store.gotFilter.Exclude, tt.wantExclude)
			}
			// Rows are marked either way; a hiding client drops any that
			// still come back marked.
			got := mutedByID(t, decodeData[[]markedRow](t, rec))
			if !reflect.DeepEqual(got, map[int64]int64{2: 7}) {
				t.Errorf("muted_by by id = %v, want only 2:7", got)
			}
		})
	}
}

func TestSrvlogListHideMuted(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		wantExclude []model.SrvlogFilter
	}{
		{"hide_muted=true passes the active rules as exclusions", "?hide_muted=true", []model.SrvlogFilter{{Hostname: "web01", Programname: "cron"}}},
		{"hide_muted absent passes no exclusions", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &recordingSrvlogStore{events: markSrvlogEvents}
			h := NewSrvlogHandler(store, markRules())

			rec := serve(t, "/srvlog", "/srvlog"+tt.query, h.List)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
			}
			if !reflect.DeepEqual(store.gotFilter.Exclude, tt.wantExclude) {
				t.Errorf("Exclude = %#v, want %#v", store.gotFilter.Exclude, tt.wantExclude)
			}
			got := mutedByID(t, decodeData[[]markedRow](t, rec))
			if !reflect.DeepEqual(got, map[int64]int64{2: 8}) {
				t.Errorf("muted_by by id = %v, want only 2:8", got)
			}
		})
	}
}

func TestListHideMutedGarbageIs400(t *testing.T) {
	netStore := &recordingNetlogStore{}
	srvStore := &recordingSrvlogStore{}
	recs := map[string]*httptest.ResponseRecorder{
		"netlog": serve(t, "/netlog", "/netlog?hide_muted=maybe", NewNetlogHandler(netStore, markRules()).List),
		"srvlog": serve(t, "/srvlog", "/srvlog?hide_muted=maybe", NewSrvlogHandler(srvStore, markRules()).List),
	}
	for feed, rec := range recs {
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_filter") {
			t.Errorf("%s: status %d body %s, want 400 invalid_filter", feed, rec.Code, rec.Body.String())
		}
	}
	if netStore.gotFilter != nil || srvStore.gotFilter != nil {
		t.Error("store was queried despite the invalid hide_muted")
	}
}

func TestGetMarksEvent(t *testing.T) {
	netH := NewNetlogHandler(&recordingNetlogStore{events: markNetlogEvents}, markRules())
	srvH := NewSrvlogHandler(&recordingSrvlogStore{events: markSrvlogEvents}, markRules())

	netlog := decodeData[struct{ Event markedRow }](t, serve(t, "/netlog/{id}", "/netlog/2", netH.Get))
	if netlog.Event.MutedBy == nil || *netlog.Event.MutedBy != 7 {
		t.Errorf("netlog data.event.muted_by = %v, want 7", netlog.Event.MutedBy)
	}
	netlogPlain := decodeData[struct{ Event markedRow }](t, serve(t, "/netlog/{id}", "/netlog/1", netH.Get))
	if netlogPlain.Event.MutedBy != nil {
		t.Errorf("unmatched netlog data.event.muted_by = %v, want absent", *netlogPlain.Event.MutedBy)
	}

	srvlog := decodeData[markedRow](t, serve(t, "/srvlog/{id}", "/srvlog/2", srvH.Get))
	if srvlog.MutedBy == nil || *srvlog.MutedBy != 8 {
		t.Errorf("srvlog data.muted_by = %v, want 8", srvlog.MutedBy)
	}
}

func TestDeviceMarksCriticalLogs(t *testing.T) {
	type summary struct {
		CriticalLogs []markedRow `json:"critical_logs"`
	}
	netH := NewNetlogDeviceHandler(&recordingNetlogStore{events: markNetlogEvents}, markRules())
	srvH := NewSrvlogDeviceHandler(&recordingSrvlogStore{events: markSrvlogEvents}, markRules())

	netlog := decodeData[summary](t, serve(t, "/d/{hostname}", "/d/leaf-02", netH.Get))
	if got := mutedByID(t, netlog.CriticalLogs); !reflect.DeepEqual(got, map[int64]int64{2: 7}) {
		t.Errorf("netlog critical_logs muted_by = %v, want only 2:7", got)
	}
	srvlog := decodeData[summary](t, serve(t, "/d/{hostname}", "/d/web01", srvH.Get))
	if got := mutedByID(t, srvlog.CriticalLogs); !reflect.DeepEqual(got, map[int64]int64{2: 8}) {
		t.Errorf("srvlog critical_logs muted_by = %v, want only 2:8", got)
	}
}

// sseBackfill connects to a stream handler and returns the first n events'
// data payloads. A Last-Event-ID selects the since backfill, none the recent
// one.
func sseBackfill(t *testing.T, h http.HandlerFunc, lastEventID string, n int) []markedRow {
	t.Helper()
	srv := httptest.NewServer(h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test read side

	var rows []markedRow
	sc := bufio.NewScanner(resp.Body)
	for len(rows) < n && sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok || data == "" {
			continue
		}
		var row markedRow
		if err := json.Unmarshal([]byte(data), &row); err != nil {
			t.Fatalf("decode event %q: %v", data, err)
		}
		rows = append(rows, row)
	}
	if len(rows) < n {
		t.Fatalf("got %d backfill events, want %d (scan err %v)", len(rows), n, sc.Err())
	}
	return rows
}

func TestSSEBackfillIsMarked(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	netH := NewNetlogSSEHandler(broker.NewNetlogBroker(logger), &recordingNetlogStore{events: markNetlogEvents}, markRules(), logger)
	srvH := NewSrvlogSSEHandler(broker.NewSrvlogBroker(logger), &recordingSrvlogStore{events: markSrvlogEvents}, markRules(), logger)

	for _, mode := range []struct{ name, lastEventID string }{{"recent", ""}, {"since", "1"}} {
		t.Run(mode.name, func(t *testing.T) {
			if got := mutedByID(t, sseBackfill(t, netH.Stream, mode.lastEventID, 2)); !reflect.DeepEqual(got, map[int64]int64{2: 7}) {
				t.Errorf("netlog backfill muted_by = %v, want only 2:7", got)
			}
			if got := mutedByID(t, sseBackfill(t, srvH.Stream, mode.lastEventID, 2)); !reflect.DeepEqual(got, map[int64]int64{2: 8}) {
				t.Errorf("srvlog backfill muted_by = %v, want only 2:8", got)
			}
		})
	}
}
