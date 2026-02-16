package broker

import (
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

func newTestAppLogBroker() *AppLogBroker {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewAppLogBroker(logger)
}

func mustAppLogSubscribe(t *testing.T, b *AppLogBroker, filter model.AppLogFilter) *AppLogSubscription {
	t.Helper()
	sub, err := b.Subscribe(filter)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	return sub
}

func TestAppLogSubscribeUnsubscribe(t *testing.T) {
	b := newTestAppLogBroker()

	if b.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", b.Len())
	}

	sub1 := mustAppLogSubscribe(t, b, model.AppLogFilter{})
	if b.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", b.Len())
	}

	sub2 := mustAppLogSubscribe(t, b, model.AppLogFilter{Service: "api"})
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

func TestAppLogBroadcast_NoSubscribers(_ *testing.T) {
	b := newTestAppLogBroker()

	// Should not panic with zero subscribers.
	b.Broadcast(model.AppLogEvent{ID: 1, Service: "api", Level: "INFO", Msg: "hello"})
}

func TestAppLogBroadcast_AllReceive(t *testing.T) {
	b := newTestAppLogBroker()

	sub := mustAppLogSubscribe(t, b, model.AppLogFilter{})
	defer b.Unsubscribe(sub)

	event := model.AppLogEvent{
		ID:      1,
		Service: "api",
		Level:   "INFO",
		Msg:     "test message",
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

func TestAppLogBroadcast_FilteredOut(t *testing.T) {
	b := newTestAppLogBroker()

	// Subscribe with service filter.
	sub := mustAppLogSubscribe(t, b, model.AppLogFilter{Service: "worker"})
	defer b.Unsubscribe(sub)

	// Broadcast event for different service — should not reach subscriber.
	b.Broadcast(model.AppLogEvent{ID: 1, Service: "api", Level: "INFO", Msg: "hello"})

	select {
	case <-sub.Chan():
		t.Fatal("received event that should have been filtered out")
	case <-time.After(50 * time.Millisecond):
		// Expected: no message received.
	}
}

func TestAppLogBroadcast_FilterMatch(t *testing.T) {
	b := newTestAppLogBroker()

	sub := mustAppLogSubscribe(t, b, model.AppLogFilter{Service: "api", Level: "WARN"})
	defer b.Unsubscribe(sub)

	// Matching event (ERROR >= WARN).
	b.Broadcast(model.AppLogEvent{ID: 1, Service: "api", Level: "ERROR", Msg: "oops"})

	select {
	case msg := <-sub.Chan():
		if msg.ID != 1 {
			t.Errorf("msg.ID = %d, want 1", msg.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for matching event")
	}

	// Non-matching level (INFO < WARN).
	b.Broadcast(model.AppLogEvent{ID: 2, Service: "api", Level: "INFO", Msg: "info"})

	select {
	case <-sub.Chan():
		t.Fatal("received event that should have been filtered by level")
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}
}

func TestAppLogBroadcast_SlowClient(t *testing.T) {
	b := newTestAppLogBroker()

	sub := mustAppLogSubscribe(t, b, model.AppLogFilter{})
	defer b.Unsubscribe(sub)

	// Fill the channel (capacity 64).
	for i := range 65 {
		b.Broadcast(model.AppLogEvent{ID: int64(i), Service: "api", Level: "INFO", Msg: "msg"})
	}

	// Drain and count — should get exactly 64 (channel capacity).
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
	if count != 64 {
		t.Errorf("received %d messages, want 64 (channel capacity)", count)
	}
}

func TestAppLogUnsubscribe_ClosesChannel(t *testing.T) {
	b := newTestAppLogBroker()

	sub := mustAppLogSubscribe(t, b, model.AppLogFilter{})
	b.Unsubscribe(sub)

	// Channel should be closed.
	_, ok := <-sub.Chan()
	if ok {
		t.Error("expected channel to be closed after Unsubscribe")
	}
}

func TestAppLogShutdown(t *testing.T) {
	b := newTestAppLogBroker()

	sub1 := mustAppLogSubscribe(t, b, model.AppLogFilter{})
	sub2 := mustAppLogSubscribe(t, b, model.AppLogFilter{Service: "worker"})

	b.Shutdown()

	if b.Len() != 0 {
		t.Errorf("Len() = %d, want 0 after shutdown", b.Len())
	}

	// Channels should be closed.
	if _, ok := <-sub1.Chan(); ok {
		t.Error("expected sub1 channel to be closed after shutdown")
	}
	if _, ok := <-sub2.Chan(); ok {
		t.Error("expected sub2 channel to be closed after shutdown")
	}
}

func TestAppLogUnsubscribe_DoubleUnsubscribe(t *testing.T) {
	b := newTestAppLogBroker()

	sub := mustAppLogSubscribe(t, b, model.AppLogFilter{})
	b.Unsubscribe(sub)
	// Second unsubscribe should be a no-op (not panic).
	b.Unsubscribe(sub)

	if b.Len() != 0 {
		t.Errorf("Len() = %d, want 0", b.Len())
	}
}

func TestAppLogSubscribe_MaxSubscribers(t *testing.T) {
	b := newTestAppLogBroker()

	// Fill to max.
	subs := make([]*AppLogSubscription, 0, maxSubscribers)
	for range maxSubscribers {
		sub := mustAppLogSubscribe(t, b, model.AppLogFilter{})
		subs = append(subs, sub)
	}

	// Next subscribe should fail.
	_, err := b.Subscribe(model.AppLogFilter{})
	if !errors.Is(err, ErrTooManySubscribers) {
		t.Fatalf("Subscribe() error = %v, want ErrTooManySubscribers", err)
	}

	// After unsubscribing one, subscribe should work again.
	b.Unsubscribe(subs[0])
	sub, err := b.Subscribe(model.AppLogFilter{})
	if err != nil {
		t.Fatalf("Subscribe() after unsubscribe error = %v", err)
	}
	b.Unsubscribe(sub)

	// Clean up.
	for _, s := range subs[1:] {
		b.Unsubscribe(s)
	}
}
