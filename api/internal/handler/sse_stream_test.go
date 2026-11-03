package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lasseh/taillight/internal/auth"
	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/model"
)

// recordSink is an in-memory sseSink that records what the streamer emits,
// so streaming invariants can be asserted without an HTTP server.
type recordSink struct {
	mu         sync.Mutex
	ids        []int64
	heartbeats int
	resyncAt   []int // len(ids) when each resync was written
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

func (s *recordSink) writeResync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resyncAt = append(s.resyncAt, len(s.ids))
	return nil
}

func (s *recordSink) flush() {}

func (s *recordSink) setWriteDeadline(time.Time) error { return nil }

func (s *recordSink) snapshot() ([]int64, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.ids...), s.heartbeats
}

func (s *recordSink) resyncs() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.resyncAt...)
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

// srvlogRange returns n events with ascending IDs starting at first.
func srvlogRange(first int64, n int) []model.SrvlogEvent {
	out := make([]model.SrvlogEvent, n)
	for i := range out {
		out[i] = model.SrvlogEvent{ID: first + int64(i)}
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

// Events reach the broker out of ID order, so the live loop skips only the IDs
// the backfill sent: a live event below the highest backfilled ID, or below
// the resume cursor, was committed after the backfill query and is new to the
// client.
func TestSSEStreamer_LiveDedupSkipsOnlyBackfilledIDs(t *testing.T) {
	tests := []struct {
		name        string
		lastEventID int64
		recent      []model.SrvlogEvent // DESC
		since       []model.SrvlogEvent // ASC
		want        []int64
	}{
		{"fresh connect", 0, srvlogEvents(7, 6, 5), nil, []int64{5, 6, 7, 4, 8}},
		{"resume", 2, nil, srvlogEvents(5, 6, 7), []int64{5, 6, 7, 4, 8}},
		{"resume with nothing to backfill", 7, nil, nil, []int64{6, 4, 8}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := broker.NewSrvlogBroker(testLogger())
			s := newSrvlogStreamer(b,
				func() ([]model.SrvlogEvent, error) { return tt.recent, nil },
				func() ([]model.SrvlogEvent, error) { return tt.since, nil },
			)
			sink := &recordSink{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() { _ = s.run(ctx, sink, model.SrvlogFilter{}, tt.lastEventID, "test") }()
			waitForSubscriber(t, b)
			// Buffered until the backfill is done. 8 comes last, so once it
			// is written the earlier two have been delivered or skipped.
			for _, id := range []int64{6, 4, 8} {
				b.Broadcast(model.SrvlogEvent{ID: id})
			}

			ids := waitForIDs(t, sink, len(tt.want))
			cancel()
			if !slices.Equal(ids, tt.want) {
				t.Fatalf("ids = %v, want %v", ids, tt.want)
			}
		})
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

	b.Broadcast(model.SrvlogEvent{ID: 7}) // already backfilled → skipped
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

// The stream ends on its own once its lifetime runs out, although the client
// is still connected, and its subscription goes with it, so later events
// reach nobody.
func TestSSEStreamer_LifetimeEndsStream(t *testing.T) {
	b := broker.NewSrvlogBroker(testLogger())
	s := newSrvlogStreamer(b,
		func() ([]model.SrvlogEvent, error) { return nil, nil },
		func() ([]model.SrvlogEvent, error) { return nil, nil },
	)
	s.lifetime = 200 * time.Millisecond
	sink := &recordSink{}

	done := make(chan struct{})
	go func() {
		_ = s.run(context.Background(), sink, model.SrvlogFilter{}, 0, "test")
		close(done)
	}()
	waitForSubscriber(t, b)
	b.Broadcast(model.SrvlogEvent{ID: 1})
	waitForIDs(t, sink, 1)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return when its lifetime ran out")
	}
	if n := b.Len(); n != 0 {
		t.Fatalf("broker has %d subscribers after the stream ended, want 0", n)
	}
	b.Broadcast(model.SrvlogEvent{ID: 2})
	if ids, _ := sink.snapshot(); !slices.Equal(ids, []int64{1}) {
		t.Fatalf("ids = %v, want [1]: the ended stream kept receiving", ids)
	}
}

// Each stream draws its default lifetime within sseLifetimeJitter of
// sseMaxLifetime, so streams opened together do not end together. A lifetime
// set by a test is used exactly.
func TestSSEStreamer_LifetimeJitter(t *testing.T) {
	var s sseStreamer[model.SrvlogEvent, model.SrvlogFilter]
	drawn := make(map[time.Duration]bool)
	for range 20 {
		d := s.maxLifetime()
		if d < sseMaxLifetime-sseLifetimeJitter || d > sseMaxLifetime+sseLifetimeJitter {
			t.Fatalf("lifetime %v outside %v ± %v", d, sseMaxLifetime, sseLifetimeJitter)
		}
		drawn[d] = true
	}
	if len(drawn) < 2 {
		t.Fatalf("20 streams all drew the same lifetime %v", drawn)
	}

	s.lifetime = 200 * time.Millisecond
	if d := s.maxLifetime(); d != s.lifetime {
		t.Fatalf("set lifetime came back as %v, want exactly %v", d, s.lifetime)
	}
}

// largeEventBody makes each event a quarter of the broker's 16 MiB
// per-subscription byte budget, so a few events pass it.
var largeEventBody = strings.Repeat("x", 4<<20)

// gatedSink blocks in its first writeEvent until release is closed, standing
// in for a client that reads slower than events arrive.
type gatedSink struct {
	recordSink
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (s *gatedSink) writeEvent(id int64, event string, data []byte) error {
	s.once.Do(func() {
		close(s.entered)
		<-s.release
	})
	return s.recordSink.writeEvent(id, event, data)
}

// A stream whose backlog passes the broker's byte budget ends the way a broker
// shutdown ends it: once the write in progress finishes, run returns without
// writing the discarded backlog.
func TestSSEStreamer_EvictedSlowReaderEndsStream(t *testing.T) {
	b := broker.NewSrvlogBroker(testLogger())
	s := newSrvlogStreamer(b,
		func() ([]model.SrvlogEvent, error) { return nil, nil },
		func() ([]model.SrvlogEvent, error) { return nil, nil },
	)
	sink := &gatedSink{entered: make(chan struct{}), release: make(chan struct{})}

	done := make(chan struct{})
	go func() {
		_ = s.run(context.Background(), sink, model.SrvlogFilter{}, 0, "test")
		close(done)
	}()
	waitForSubscriber(t, b)
	b.Broadcast(model.SrvlogEvent{ID: 1, Message: largeEventBody})
	select {
	case <-sink.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first event never reached the sink")
	}

	for id := int64(2); b.Len() > 0; id++ {
		if id > 10 {
			t.Fatal("subscriber still subscribed with 9 large events queued")
		}
		b.Broadcast(model.SrvlogEvent{ID: id, Message: largeEventBody})
	}
	close(sink.release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after the broker evicted the subscriber")
	}
	if ids, _ := sink.snapshot(); !slices.Equal(ids, []int64{1}) {
		t.Fatalf("ids = %v, want [1]: the evicted backlog was written", ids)
	}
}

// When the lifetime runs out, the events already queued on the subscription
// are written before the stream ends, so events broadcast just before the end
// are not discarded.
func TestSSEStreamer_LifetimeEndWritesQueuedEvents(t *testing.T) {
	b := broker.NewSrvlogBroker(testLogger())
	s := newSrvlogStreamer(b,
		func() ([]model.SrvlogEvent, error) { return nil, nil },
		func() ([]model.SrvlogEvent, error) { return nil, nil },
	)
	s.lifetime = 200 * time.Millisecond
	sink := &gatedSink{entered: make(chan struct{}), release: make(chan struct{})}

	done := make(chan struct{})
	go func() {
		_ = s.run(context.Background(), sink, model.SrvlogFilter{}, 0, "test")
		close(done)
	}()
	waitForSubscriber(t, b)
	b.Broadcast(model.SrvlogEvent{ID: 1})
	select {
	case <-sink.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first event never reached the sink")
	}

	// Queued behind the write in progress, which outlasts the lifetime.
	want := []int64{1}
	for id := int64(2); id <= 11; id++ {
		b.Broadcast(model.SrvlogEvent{ID: id})
		want = append(want, id)
	}
	time.Sleep(2 * s.lifetime)
	close(sink.release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after its lifetime ran out")
	}
	if ids, _ := sink.snapshot(); !slices.Equal(ids, want) {
		t.Fatalf("ids = %v, want %v: queued events were discarded", ids, want)
	}
}

// A stream that keeps up is never evicted, however many bytes pass through
// it: the streamer releases each event from the byte budget as it takes it.
func TestSSEStreamer_ReaderKeepingUpStaysSubscribed(t *testing.T) {
	b := broker.NewSrvlogBroker(testLogger())
	s := newSrvlogStreamer(b,
		func() ([]model.SrvlogEvent, error) { return nil, nil },
		func() ([]model.SrvlogEvent, error) { return nil, nil },
	)
	sink := &recordSink{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = s.run(ctx, sink, model.SrvlogFilter{}, 0, "test") }()
	waitForSubscriber(t, b)

	const events = 6 // 24 MiB in total, past the 16 MiB budget
	for id := int64(1); id <= events; id++ {
		b.Broadcast(model.SrvlogEvent{ID: id, Message: largeEventBody})
		waitForIDs(t, sink, int(id))
	}
	if n := b.Len(); n != 1 {
		t.Fatalf("broker has %d subscribers, want 1: a reader that kept up was evicted", n)
	}
}

// A resuming client further behind than one backfill gets the first
// sseBackfillLimit events, then a resync before any live event, so it reloads
// history instead of keeping a silent gap. A backfill that reaches the live
// stream sends no resync.
func TestSSEStreamer_ResumeResync(t *testing.T) {
	tests := []struct {
		name        string
		lastEventID int64
		stored      int // events after lastEventID (or in total, for a fresh connect)
		sinceErr    error
		wantIDs     int   // backfilled events before the live one
		wantResync  []int // len(ids) at each resync
	}{
		{"gap above the limit", 5, sseBackfillLimit + 50, nil, sseBackfillLimit, []int{sseBackfillLimit}},
		{"gap one above the limit", 5, sseBackfillLimit + 1, nil, sseBackfillLimit, []int{sseBackfillLimit}},
		{"gap exactly at the limit", 5, sseBackfillLimit, nil, sseBackfillLimit, nil},
		{"gap below the limit", 5, 3, nil, 3, nil},
		{"resume query fails", 5, 0, errors.New("query failed"), 0, []int{0}},
		{"fresh connect with a full page", 0, sseBackfillLimit + 50, nil, sseBackfillLimit, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored := srvlogRange(6, tt.stored)
			b := broker.NewSrvlogBroker(testLogger())
			s := sseStreamer[model.SrvlogEvent, model.SrvlogFilter]{
				broker:  b,
				label:   "srvlog",
				eventID: func(e model.SrvlogEvent) int64 { return e.ID },
				logger:  testLogger(),
				since: func(_ context.Context, _ model.SrvlogFilter, _ int64, limit int) ([]model.SrvlogEvent, error) {
					if tt.sinceErr != nil {
						return nil, tt.sinceErr
					}
					return stored[:min(limit, len(stored))], nil // ASC
				},
				recent: func(_ context.Context, _ model.SrvlogFilter, limit int) ([]model.SrvlogEvent, error) {
					out := slices.Clone(stored[max(0, len(stored)-limit):])
					slices.Reverse(out) // DESC
					return out, nil
				},
			}
			sink := &recordSink{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() { _ = s.run(ctx, sink, model.SrvlogFilter{}, tt.lastEventID, "test") }()
			waitForSubscriber(t, b)
			const live = 100_000
			b.Broadcast(model.SrvlogEvent{ID: live}) // buffered until backfill is done

			ids := waitForIDs(t, sink, tt.wantIDs+1)
			cancel()
			if len(ids) != tt.wantIDs+1 || ids[tt.wantIDs] != live {
				t.Fatalf("got %d ids ending %v, want %d backfilled then live %d", len(ids), ids[max(0, len(ids)-2):], tt.wantIDs, live)
			}
			first := int64(6)
			if tt.lastEventID == 0 {
				first = stored[len(stored)-tt.wantIDs].ID // the most recent page
			}
			for i, id := range ids[:tt.wantIDs] {
				if want := first + int64(i); id != want {
					t.Fatalf("backfilled ids[%d] = %d, want %d", i, id, want)
				}
			}
			if got := sink.resyncs(); !slices.Equal(got, tt.wantResync) {
				t.Errorf("resync written at %v, want %v", got, tt.wantResync)
			}
		})
	}
}

// A backfill query that cannot finish (for example while it waits for a
// database connection) is cut off at the backfill deadline and live delivery
// starts. A resume cut off that way is a failed query, not a cancel, so the
// client still gets its resync.
func TestSSEStreamer_BackfillDeadline(t *testing.T) {
	tests := []struct {
		name        string
		lastEventID int64
		wantResync  []int
	}{
		{"fresh connect", 0, nil},
		{"resume", 5, []int{0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked := func(ctx context.Context) ([]model.SrvlogEvent, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			b := broker.NewSrvlogBroker(testLogger())
			s := sseStreamer[model.SrvlogEvent, model.SrvlogFilter]{
				broker:          b,
				label:           "srvlog",
				eventID:         func(e model.SrvlogEvent) int64 { return e.ID },
				logger:          testLogger(),
				backfillTimeout: 50 * time.Millisecond,
				since: func(ctx context.Context, _ model.SrvlogFilter, _ int64, _ int) ([]model.SrvlogEvent, error) {
					return blocked(ctx)
				},
				recent: func(ctx context.Context, _ model.SrvlogFilter, _ int) ([]model.SrvlogEvent, error) {
					return blocked(ctx)
				},
			}
			sink := &recordSink{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() { _ = s.run(ctx, sink, model.SrvlogFilter{}, tt.lastEventID, "test") }()
			waitForSubscriber(t, b)
			b.Broadcast(model.SrvlogEvent{ID: 42}) // buffered until the backfill gives up

			if ids := waitForIDs(t, sink, 1); ids[0] != 42 {
				t.Fatalf("ids = %v, want the live event 42", ids)
			}
			if got := sink.resyncs(); !slices.Equal(got, tt.wantResync) {
				t.Errorf("resync written at %v, want %v", got, tt.wantResync)
			}
		})
	}
}

// revocableKey is an auth lookup that knows one read-scoped API key and no
// sessions. Revoking the key makes every later lookup fail, as the store does.
type revocableKey struct {
	hash    string
	revoked atomic.Bool
}

func (k *revocableKey) GetSessionUser(context.Context, string) (*model.User, error) {
	return nil, pgx.ErrNoRows
}

func (k *revocableKey) GetAPIKeyUser(_ context.Context, keyHash string) (*model.User, []string, pgtype.UUID, error) {
	if keyHash != k.hash || k.revoked.Load() {
		return nil, nil, pgtype.UUID{}, pgx.ErrNoRows
	}
	user := &model.User{ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, Username: "reader", IsActive: true}
	return user, []string{"read"}, pgtype.UUID{Bytes: [16]byte{2}, Valid: true}, nil
}

// Every feed's stream ends after its lifetime behind the real auth
// middleware, and the reconnect is authorized again: once the key is revoked
// the client gets 401 instead of a new stream.
func TestSSEStreamLifetimeForcesReauthorization(t *testing.T) {
	const (
		key      = "tl_sse-lifetime-test-key"
		lifetime = 200 * time.Millisecond
	)
	logger := slog.New(slog.DiscardHandler)
	srvlogBroker := broker.NewSrvlogBroker(logger)
	netlogBroker := broker.NewNetlogBroker(logger)
	applogBroker := broker.NewAppLogBroker(logger)
	srvlogH := NewSrvlogSSEHandler(srvlogBroker, &recordingSrvlogStore{}, markRules(), logger)
	srvlogH.lifetime = lifetime
	netlogH := NewNetlogSSEHandler(netlogBroker, &recordingNetlogStore{}, markRules(), logger)
	netlogH.lifetime = lifetime
	applogH := NewAppLogSSEHandler(applogBroker, &mockAppLogStore{}, logger)
	applogH.lifetime = lifetime

	feeds := []struct {
		name        string
		stream      http.HandlerFunc
		subscribers func() int
	}{
		{"srvlog", srvlogH.Stream, srvlogBroker.Len},
		{"netlog", netlogH.Stream, netlogBroker.Len},
		{"applog", applogH.Stream, applogBroker.Len},
	}
	for _, f := range feeds {
		t.Run(f.name, func(t *testing.T) {
			keys := &revocableKey{hash: auth.HashToken(key)}
			srv := httptest.NewServer(auth.SessionOrAPIKey(keys, keys)(auth.RequireScope("read")(f.stream)))
			defer srv.Close()

			// The client never hangs up; only the server can end the read
			// before this deadline.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			connect := func() *http.Response {
				t.Helper()
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
				if err != nil {
					t.Fatalf("new request: %v", err)
				}
				req.Header.Set("Authorization", "Bearer "+key)
				resp, err := srv.Client().Do(req)
				if err != nil {
					t.Fatalf("connect: %v", err)
				}
				return resp
			}

			resp := connect()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("first connect status = %d, want 200", resp.StatusCode)
			}
			start := time.Now()
			_, err := io.Copy(io.Discard, resp.Body)
			resp.Body.Close() //nolint:errcheck,gosec // test read side
			if err != nil {
				t.Fatalf("stream did not end on its own: %v", err)
			}
			if elapsed := time.Since(start); elapsed < lifetime/2 {
				t.Fatalf("stream ended after %v, before its %v lifetime", elapsed, lifetime)
			}
			if n := f.subscribers(); n != 0 {
				t.Fatalf("broker has %d subscribers after the stream ended, want 0", n)
			}

			keys.revoked.Store(true)
			resp = connect()
			resp.Body.Close() //nolint:errcheck,gosec // status is all that matters
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("reconnect with a revoked key: status = %d, want 401", resp.StatusCode)
			}
		})
	}
}
