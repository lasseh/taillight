package handler

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/model"
)

// recordSink is an in-memory sseSink that records what the streamer emits,
// so streaming invariants can be asserted without an HTTP server.
type recordSink struct {
	mu         sync.Mutex
	ids        []int64
	heartbeats int
}

func (s *recordSink) writeEvent(id int64, _ string, _ []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ids = append(s.ids, id)
	return nil
}

func (s *recordSink) writeHeartbeat() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeats++
	return nil
}

func (s *recordSink) flush() {}

func (s *recordSink) setWriteDeadline(time.Time) error { return nil }

func (s *recordSink) snapshot() ([]int64, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.ids...), s.heartbeats
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func srvlogEvents(ids ...int64) []model.SrvlogEvent {
	out := make([]model.SrvlogEvent, len(ids))
	for i, id := range ids {
		out[i] = model.SrvlogEvent{ID: id}
	}
	return out
}

// waitForSubscriber blocks until the broker has at least one subscriber.
func waitForSubscriber(t *testing.T, b *broker.SrvlogBroker) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.Len() >= 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("broker never reached 1 subscriber (have %d)", b.Len())
}

// waitForIDs polls the sink until it has at least n ids or the deadline passes.
func waitForIDs(t *testing.T, s *recordSink, n int) []int64 {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ids, _ := s.snapshot()
		if len(ids) >= n {
			return ids
		}
		time.Sleep(time.Millisecond)
	}
	ids, _ := s.snapshot()
	t.Fatalf("sink never reached %d ids (have %v)", n, ids)
	return nil
}

func newSrvlogStreamer(b *broker.SrvlogBroker, recent, since func() ([]model.SrvlogEvent, error)) sseStreamer[model.SrvlogEvent, model.SrvlogFilter] {
	return sseStreamer[model.SrvlogEvent, model.SrvlogFilter]{
		broker:  b,
		label:   "srvlog",
		eventID: func(e model.SrvlogEvent) int64 { return e.ID },
		logger:  testLogger(),
		recent: func(context.Context, model.SrvlogFilter, int) ([]model.SrvlogEvent, error) {
			return recent()
		},
		since: func(context.Context, model.SrvlogFilter, int64, int) ([]model.SrvlogEvent, error) {
			return since()
		},
	}
}

// Backfill (newest-first DESC) is emitted oldest-first, then live events follow.
func TestSSEStreamer_BackfillThenLive(t *testing.T) {
	b := broker.NewSrvlogBroker(testLogger())
	s := newSrvlogStreamer(b,
		func() ([]model.SrvlogEvent, error) { return srvlogEvents(3, 2, 1), nil }, // DESC
		func() ([]model.SrvlogEvent, error) { return nil, nil },
	)
	sink := &recordSink{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = s.run(ctx, sink, model.SrvlogFilter{}, 0, "test") }()
	waitForSubscriber(t, b)
	waitForIDs(t, sink, 3) // backfill drained

	b.Broadcast(model.SrvlogEvent{ID: 4})
	b.Broadcast(model.SrvlogEvent{ID: 5})

	ids := waitForIDs(t, sink, 5)
	cancel()
	want := []int64{1, 2, 3, 4, 5}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
}

// A live event whose ID was already covered by backfill must be skipped.
func TestSSEStreamer_DedupBackfilledLive(t *testing.T) {
	b := broker.NewSrvlogBroker(testLogger())
	s := newSrvlogStreamer(b,
		func() ([]model.SrvlogEvent, error) { return srvlogEvents(3, 2, 1), nil },
		func() ([]model.SrvlogEvent, error) { return nil, nil },
	)
	sink := &recordSink{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = s.run(ctx, sink, model.SrvlogFilter{}, 0, "test") }()
	waitForSubscriber(t, b)
	waitForIDs(t, sink, 3)

	b.Broadcast(model.SrvlogEvent{ID: 3}) // duplicate of backfill — must be skipped
	b.Broadcast(model.SrvlogEvent{ID: 4}) // new — must be delivered

	ids := waitForIDs(t, sink, 4)
	cancel()
	want := []int64{1, 2, 3, 4}
	if len(ids) != 4 {
		t.Fatalf("ids = %v, want exactly %v (no duplicate 3)", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
}

// An event broadcast while the backfill query is running must not be lost,
// because Subscribe happens before backfill.
func TestSSEStreamer_NoLossDuringBackfill(t *testing.T) {
	b := broker.NewSrvlogBroker(testLogger())
	release := make(chan struct{})
	s := newSrvlogStreamer(b,
		func() ([]model.SrvlogEvent, error) {
			<-release // block backfill until the test releases it
			return srvlogEvents(2, 1), nil
		},
		func() ([]model.SrvlogEvent, error) { return nil, nil },
	)
	sink := &recordSink{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = s.run(ctx, sink, model.SrvlogFilter{}, 0, "test") }()
	waitForSubscriber(t, b) // subscribed, but backfill is blocked

	b.Broadcast(model.SrvlogEvent{ID: 10}) // arrives during backfill
	close(release)

	ids := waitForIDs(t, sink, 3)
	cancel()
	want := []int64{1, 2, 10}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v (event during backfill lost)", ids, want)
		}
	}
}

// With a Last-Event-ID the streamer backfills via since() (ASC) then streams live.
func TestSSEStreamer_SinceMode(t *testing.T) {
	b := broker.NewSrvlogBroker(testLogger())
	s := newSrvlogStreamer(b,
		func() ([]model.SrvlogEvent, error) { return nil, nil },
		func() ([]model.SrvlogEvent, error) { return srvlogEvents(6, 7), nil }, // ASC
	)
	sink := &recordSink{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = s.run(ctx, sink, model.SrvlogFilter{}, 5, "test") }()
	waitForSubscriber(t, b)
	waitForIDs(t, sink, 2)

	b.Broadcast(model.SrvlogEvent{ID: 7}) // <= last backfilled (7) → skipped
	b.Broadcast(model.SrvlogEvent{ID: 8}) // new → delivered

	ids := waitForIDs(t, sink, 3)
	cancel()
	want := []int64{6, 7, 8}
	if len(ids) != 3 {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
}

// Heartbeats fire when the stream is idle.
func TestSSEStreamer_Heartbeat(t *testing.T) {
	b := broker.NewSrvlogBroker(testLogger())
	s := newSrvlogStreamer(b,
		func() ([]model.SrvlogEvent, error) { return nil, nil },
		func() ([]model.SrvlogEvent, error) { return nil, nil },
	)
	s.heartbeat = 10 * time.Millisecond
	sink := &recordSink{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = s.run(ctx, sink, model.SrvlogFilter{}, 0, "test") }()
	waitForSubscriber(t, b)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, hb := sink.snapshot(); hb > 0 {
			cancel()
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("no heartbeat fired")
}

// Cancelling the context returns cleanly with no events written.
func TestSSEStreamer_ContextCancel(t *testing.T) {
	b := broker.NewSrvlogBroker(testLogger())
	s := newSrvlogStreamer(b,
		func() ([]model.SrvlogEvent, error) { return nil, nil },
		func() ([]model.SrvlogEvent, error) { return nil, nil },
	)
	sink := &recordSink{}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		_ = s.run(ctx, sink, model.SrvlogFilter{}, 0, "test")
		close(done)
	}()
	waitForSubscriber(t, b)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after context cancel")
	}
	if ids, hb := sink.snapshot(); len(ids) != 0 || hb != 0 {
		t.Fatalf("expected no output, got ids=%v heartbeats=%d", ids, hb)
	}
}
