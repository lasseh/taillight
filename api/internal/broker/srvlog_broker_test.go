package broker

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

func newTestBroker() *SrvlogBroker {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewSrvlogBroker(logger)
}

func mustSubscribe(t *testing.T, b *SrvlogBroker, filter model.SrvlogFilter) *SrvlogSubscription {
	t.Helper()
	sub, err := b.Subscribe(filter, "")
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	return sub
}

func TestSubscribeUnsubscribe(t *testing.T) {
	b := newTestBroker()

	if b.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", b.Len())
	}

	sub1 := mustSubscribe(t, b, model.SrvlogFilter{})
	if b.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", b.Len())
	}

	sub2 := mustSubscribe(t, b, model.SrvlogFilter{Hostname: "router1"})
	if b.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", b.Len())
	}

	b.Unsubscribe(sub1)
	if b.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", b.Len())
	}

	b.Unsubscribe(sub2)
	if b.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", b.Len())
	}
}

func TestBroadcast_NoSubscribers(_ *testing.T) {
	b := newTestBroker()

	// Should not panic with zero subscribers.
	b.Broadcast(model.SrvlogEvent{ID: 1, Hostname: "router1"})
}

func TestBroadcast_AllReceive(t *testing.T) {
	b := newTestBroker()

	sub := mustSubscribe(t, b, model.SrvlogFilter{})
	defer b.Unsubscribe(sub)

	event := model.SrvlogEvent{
		ID:            1,
		Hostname:      "router1",
		Programname:   "rpd",
		Severity:      3,
		Facility:      23,
		Message:       "test message",
		SeverityLabel: "err",
		FacilityLabel: "local7",
	}

	b.Broadcast(event)

	select {
	case msg := <-sub.Chan():
		if msg.ID != 1 {
			t.Errorf("msg.ID = %d, want 1", msg.ID)
		}
		if len(msg.Data) == 0 {
			t.Error("msg.Data is empty")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for broadcast")
	}
}

func TestBroadcast_FilteredOut(t *testing.T) {
	b := newTestBroker()

	// Subscribe with hostname filter.
	sub := mustSubscribe(t, b, model.SrvlogFilter{Hostname: "router2"})
	defer b.Unsubscribe(sub)

	// Broadcast event for router1 — should not reach subscriber.
	b.Broadcast(model.SrvlogEvent{ID: 1, Hostname: "router1"})

	select {
	case <-sub.Chan():
		t.Fatal("received event that should have been filtered out")
	case <-time.After(50 * time.Millisecond):
		// Expected: no message received.
	}
}

func TestBroadcast_FilterMatch(t *testing.T) {
	b := newTestBroker()

	sub := mustSubscribe(t, b, model.SrvlogFilter{Hostname: "router1", Severity: new(3)})
	defer b.Unsubscribe(sub)

	// Matching event.
	b.Broadcast(model.SrvlogEvent{ID: 1, Hostname: "router1", Severity: 3})

	select {
	case msg := <-sub.Chan():
		if msg.ID != 1 {
			t.Errorf("msg.ID = %d, want 1", msg.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for matching event")
	}

	// Non-matching severity.
	b.Broadcast(model.SrvlogEvent{ID: 2, Hostname: "router1", Severity: 6})

	select {
	case <-sub.Chan():
		t.Fatal("received event that should have been filtered by severity")
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}
}

func TestBroadcast_SlowClient(t *testing.T) {
	b := newTestBroker()

	sub := mustSubscribe(t, b, model.SrvlogFilter{})
	defer b.Unsubscribe(sub)

	// Fill the channel (capacity = subscriptionBufferSize = 512).
	for i := range 513 {
		b.Broadcast(model.SrvlogEvent{ID: int64(i), Hostname: "router1"})
	}

	// Drain and count — should get exactly 512 (channel capacity).
	count := 0
	for {
		select {
		case <-sub.Chan():
			count++
		default:
			goto done
		}
	}
done:
	if count != 512 {
		t.Errorf("received %d messages, want 512 (channel capacity)", count)
	}
}

// maxSizeEvent is a srvlog event at the size limit of the syslog input: a
// 64 KB message stored twice (message and raw_message), made of characters
// json.Marshal escapes into six bytes each, about 786 KB marshalled.
func maxSizeEvent(id int64) model.SrvlogEvent {
	body := strings.Repeat("<", 65430)
	return model.SrvlogEvent{ID: id, Hostname: "router1", Message: body, RawMessage: &body}
}

func marshalledSize(t *testing.T, e model.SrvlogEvent) int64 {
	t.Helper()
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return int64(len(data))
}

// A subscriber that never reads holds at most its byte budget of max-size
// events. The event that would pass the budget evicts it instead: the queued
// events are discarded, its channel is closed, and its client slot is freed.
func TestBroadcast_ByteBudgetEvictsSubscriberThatNeverDrains(t *testing.T) {
	b := newTestBroker()
	size := marshalledSize(t, maxSizeEvent(1))
	b.byteBudget = 3*size + size/2

	sub, err := b.Subscribe(model.SrvlogFilter{}, "ip:1.2.3.4")
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	for id := int64(1); id <= 3; id++ {
		b.Broadcast(maxSizeEvent(id))
		if q := sub.queued.Load(); q > b.byteBudget {
			t.Fatalf("after event %d queued = %d bytes, over the %d budget", id, q, b.byteBudget)
		}
		if b.Len() != 1 {
			t.Fatalf("after event %d Len() = %d, want 1 while under budget", id, b.Len())
		}
	}

	b.Broadcast(maxSizeEvent(4))
	if b.Len() != 0 {
		t.Fatalf("Len() = %d after the budget was passed, want 0", b.Len())
	}
	if msg, ok := <-sub.Chan(); ok {
		t.Fatalf("received event %d from an evicted subscription, want its queue discarded and closed", msg.ID)
	}
	if n := b.byClient["ip:1.2.3.4"]; n != 0 {
		t.Fatalf("client key still counts %d subscriptions after eviction", n)
	}
	b.Broadcast(maxSizeEvent(5)) // must not send on the closed channel
}

// readAt40KBps plays a client that reads 40 KB/s, the way the SSE handler
// reads: it takes one event, releases it from the budget, then spends
// len(Data)/rate writing it before it takes the next. Events arrive every
// arrival. It runs on a virtual clock and returns how long the broker kept
// the subscription, or ok=false if it was still subscribed after all events.
func readAt40KBps(t *testing.T, b *SrvlogBroker, sub *SrvlogSubscription, events int, arrival time.Duration) (time.Duration, bool) {
	t.Helper()
	const rate = 40 << 10 // bytes per second
	var writing int64     // bytes of the event being written still to go
	sent := 0
	for now := time.Duration(0); now < time.Duration(events)*arrival; now += time.Second {
		for sent < events && time.Duration(sent)*arrival <= now {
			sent++
			b.Broadcast(maxSizeEvent(int64(sent)))
			if q := sub.queued.Load(); q > b.byteBudget {
				t.Fatalf("at %v queued = %d bytes, over the %d budget", now, q, b.byteBudget)
			}
			if b.Len() == 0 {
				return now, true
			}
		}
		for credit := int64(rate); credit > 0; {
			if writing == 0 {
				select {
				case msg := <-sub.Chan():
					sub.Consumed(msg)
					writing = int64(len(msg.Data))
				default:
					credit = 0 // nothing queued: the reader idles
					continue
				}
			}
			n := min(credit, writing)
			writing -= n
			credit -= n
		}
	}
	return 0, false
}

// A reader at 40 KB/s whose backlog grows is evicted once the backlog passes
// the byte budget, by the time the budget and the growth rate predict. The
// same reader stays subscribed when events arrive slower than it reads, even
// after many times the budget has passed through its queue.
func TestBroadcast_ByteBudgetEvictsSlowReaderWithGrowingBacklog(t *testing.T) {
	tests := []struct {
		name    string
		arrival time.Duration // one max-size event per arrival
		events  int
		evicted bool
	}{
		{"arrivals twice the read rate", 10 * time.Second, 15, true},
		{"arrivals slower than the read rate", 30 * time.Second, 15, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newTestBroker()
			size := marshalledSize(t, maxSizeEvent(1))
			b.byteBudget = 4 * size
			sub := mustSubscribe(t, b, model.SrvlogFilter{})
			defer b.Unsubscribe(sub)

			at, evicted := readAt40KBps(t, b, sub, tt.events, tt.arrival)
			if evicted != tt.evicted {
				t.Fatalf("evicted = %v (at %v), want %v", evicted, at, tt.evicted)
			}
			if !evicted {
				return
			}
			// The backlog grows by the arrival rate less the read rate, so it
			// reaches the budget after budget/growth, give or take the event
			// being written and one arrival interval.
			growth := float64(size)/tt.arrival.Seconds() - 40<<10
			limit := time.Duration(float64(b.byteBudget)/growth*float64(time.Second)) + 2*tt.arrival
			if at > limit {
				t.Fatalf("evicted at %v, want within %v", at, limit)
			}
			t.Logf("evicted at %v, limit %v", at, limit)
		})
	}
}

// An event dropped because the queue is full by count is released from the
// byte budget again, so dropping alone never leads to eviction.
func TestBroadcast_CountDropReleasesBudget(t *testing.T) {
	b := newTestBroker()
	sub := mustSubscribe(t, b, model.SrvlogFilter{})
	defer b.Unsubscribe(sub)

	for i := range subscriptionBufferSize + 1 {
		b.Broadcast(model.SrvlogEvent{ID: int64(i), Hostname: "router1"})
	}
	for range subscriptionBufferSize {
		sub.Consumed(<-sub.Chan())
	}
	if q := sub.queued.Load(); q != 0 {
		t.Fatalf("queued = %d bytes with an empty queue, want 0", q)
	}
}

func TestUnsubscribe_ClosesChannel(t *testing.T) {
	b := newTestBroker()

	sub := mustSubscribe(t, b, model.SrvlogFilter{})
	b.Unsubscribe(sub)

	// Channel should be closed.
	_, ok := <-sub.Chan()
	if ok {
		t.Error("expected channel to be closed after Unsubscribe")
	}
}

func TestSubscribe_MaxSubscribersPerClient(t *testing.T) {
	b := newTestBroker()

	// One client key fills its per-client quota.
	subs := make([]*SrvlogSubscription, 0, maxSubscribersPerClient)
	for range maxSubscribersPerClient {
		sub, err := b.Subscribe(model.SrvlogFilter{}, "ip:1.2.3.4")
		if err != nil {
			t.Fatalf("Subscribe() error = %v", err)
		}
		subs = append(subs, sub)
	}

	// The next connection from the same client is refused...
	if _, err := b.Subscribe(model.SrvlogFilter{}, "ip:1.2.3.4"); !errors.Is(err, ErrTooManyClientSubscribers) {
		t.Fatalf("Subscribe() error = %v, want ErrTooManyClientSubscribers", err)
	}

	// ...but a different client is unaffected.
	other, err := b.Subscribe(model.SrvlogFilter{}, "ip:5.6.7.8")
	if err != nil {
		t.Fatalf("Subscribe() other client error = %v", err)
	}
	b.Unsubscribe(other)

	// Freeing one slot lets the original client reconnect.
	b.Unsubscribe(subs[0])
	sub, err := b.Subscribe(model.SrvlogFilter{}, "ip:1.2.3.4")
	if err != nil {
		t.Fatalf("Subscribe() after unsubscribe error = %v", err)
	}
	b.Unsubscribe(sub)
	for _, s := range subs[1:] {
		b.Unsubscribe(s)
	}
}

func TestSubscribe_MaxSubscribers(t *testing.T) {
	b := newTestBroker()

	// Fill to max.
	subs := make([]*SrvlogSubscription, 0, maxSubscribers)
	for range maxSubscribers {
		sub := mustSubscribe(t, b, model.SrvlogFilter{})
		subs = append(subs, sub)
	}

	// Next subscribe should fail.
	_, err := b.Subscribe(model.SrvlogFilter{}, "")
	if !errors.Is(err, ErrTooManySubscribers) {
		t.Fatalf("Subscribe() error = %v, want ErrTooManySubscribers", err)
	}

	// After unsubscribing one, subscribe should work again.
	b.Unsubscribe(subs[0])
	sub, err := b.Subscribe(model.SrvlogFilter{}, "")
	if err != nil {
		t.Fatalf("Subscribe() after unsubscribe error = %v", err)
	}
	b.Unsubscribe(sub)

	// Clean up.
	for _, s := range subs[1:] {
		b.Unsubscribe(s)
	}
}
