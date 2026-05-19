package notification

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

type recordedFlush struct {
	ruleID   int64
	groupKey string
	payload  Payload
}

// faninEngine builds an engine whose suppressor records flushes synchronously
// (coalesce == 0 fires onFlush inline), so the rule fan-in can be asserted
// without running the dispatch worker.
func faninEngine(t *testing.T, rules []Rule) (*Engine, func() []recordedFlush) {
	t.Helper()
	cfg := Config{
		DispatchBuffer:    1,
		DefaultSilence:    time.Hour, // keep the silence timer from firing mid-test
		DefaultSilenceMax: time.Hour,
		DefaultCoalesce:   0, // fire first match immediately, synchronously
	}
	e := NewEngine(&fakeStore{}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	var mu sync.Mutex
	var got []recordedFlush
	e.suppressor = NewSuppressor(func(ruleID int64, groupKey string, p Payload) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, recordedFlush{ruleID, groupKey, p})
	})
	t.Cleanup(e.suppressor.Stop)

	e.cacheMu.Lock()
	e.rules = rules
	e.cacheMu.Unlock()

	return e, func() []recordedFlush {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedFlush(nil), got...)
	}
}

func TestHandleEvent_FanInPerPlane(t *testing.T) {
	rules := []Rule{
		{ID: 1, Name: "srv", Enabled: true, EventKind: EventKindSrvlog, Hostname: "router1"},
		{ID: 2, Name: "net", Enabled: true, EventKind: EventKindNetlog, Hostname: "fw1"},
		{ID: 3, Name: "app", Enabled: true, EventKind: EventKindAppLog, Service: "api"},
		{ID: 4, Name: "disabled", Enabled: false, EventKind: EventKindSrvlog, Hostname: "router1"},
		{ID: 5, Name: "nomatch", Enabled: true, EventKind: EventKindSrvlog, Hostname: "other"},
		{ID: 6, Name: "wrongkind", Enabled: true, EventKind: EventKindNetlog, Hostname: "router1"},
	}
	e, collect := faninEngine(t, rules)

	srvTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	e.HandleSrvlogEvent(model.SrvlogEvent{ID: 10, Hostname: "router1", ReceivedAt: srvTime})

	netTime := time.Date(2025, 2, 2, 0, 0, 0, 0, time.UTC)
	e.HandleNetlogEvent(model.NetlogEvent{ID: 20, Hostname: "fw1", ReceivedAt: netTime})

	// applog's payload timestamp must come from the event's Timestamp, not
	// ReceivedAt — the one real srvlog/applog divergence in the fan-in.
	appRecv := time.Date(2025, 3, 3, 0, 0, 0, 0, time.UTC)
	appTime := time.Date(2025, 4, 4, 0, 0, 0, 0, time.UTC)
	e.HandleAppLogEvent(model.AppLogEvent{ID: 30, Service: "api", Host: "node1", ReceivedAt: appRecv, Timestamp: appTime})

	got := collect()
	if len(got) != 3 {
		t.Fatalf("got %d flushes, want 3 (disabled/non-match/wrong-kind rules must be skipped): %+v", len(got), got)
	}

	byRule := map[int64]recordedFlush{}
	for _, f := range got {
		byRule[f.ruleID] = f
	}

	srv, ok := byRule[1]
	if !ok {
		t.Fatal("srvlog rule 1 did not fire")
	}
	if srv.payload.Kind != EventKindSrvlog || srv.payload.RuleName != "srv" {
		t.Errorf("srv payload kind/name = %q/%q", srv.payload.Kind, srv.payload.RuleName)
	}
	if !srv.payload.Timestamp.Equal(srvTime) {
		t.Errorf("srv timestamp = %v, want %v", srv.payload.Timestamp, srvTime)
	}
	if srv.payload.SrvlogEvent == nil || srv.payload.SrvlogEvent.ID != 10 {
		t.Errorf("srv event pointer = %+v, want ID 10", srv.payload.SrvlogEvent)
	}
	if srv.groupKey != "router1" {
		t.Errorf("srv groupKey = %q, want router1 (default hostname grouping)", srv.groupKey)
	}

	net, ok := byRule[2]
	if !ok {
		t.Fatal("netlog rule 2 did not fire")
	}
	if net.payload.Kind != EventKindNetlog || net.payload.NetlogEvent == nil || net.payload.NetlogEvent.ID != 20 {
		t.Errorf("net payload = %+v", net.payload)
	}
	if !net.payload.Timestamp.Equal(netTime) {
		t.Errorf("net timestamp = %v, want %v", net.payload.Timestamp, netTime)
	}

	app, ok := byRule[3]
	if !ok {
		t.Fatal("applog rule 3 did not fire")
	}
	if app.payload.Kind != EventKindAppLog || app.payload.AppLogEvent == nil || app.payload.AppLogEvent.ID != 30 {
		t.Errorf("app payload = %+v", app.payload)
	}
	if !app.payload.Timestamp.Equal(appTime) {
		t.Errorf("app timestamp = %v, want %v (event Timestamp, not ReceivedAt)", app.payload.Timestamp, appTime)
	}
}
