package broker

import (
	"errors"
	"log/slog"
	"os"
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
	sub, err := b.Subscribe(filter)
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

func TestSubscribe_MaxSubscribers(t *testing.T) {
	b := newTestBroker()

	// Fill to max.
	subs := make([]*SrvlogSubscription, 0, maxSubscribers)
	for range maxSubscribers {
		sub := mustSubscribe(t, b, model.SrvlogFilter{})
		subs = append(subs, sub)
	}

	// Next subscribe should fail.
	_, err := b.Subscribe(model.SrvlogFilter{})
	if !errors.Is(err, ErrTooManySubscribers) {
		t.Fatalf("Subscribe() error = %v, want ErrTooManySubscribers", err)
	}

	// After unsubscribing one, subscribe should work again.
	b.Unsubscribe(subs[0])
	sub, err := b.Subscribe(model.SrvlogFilter{})
	if err != nil {
		t.Fatalf("Subscribe() after unsubscribe error = %v", err)
	}
	b.Unsubscribe(sub)

	// Clean up.
	for _, s := range subs[1:] {
		b.Unsubscribe(s)
	}
}
