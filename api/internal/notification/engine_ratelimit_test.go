package notification

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func newRateLimitTestEngine(store Store, backend Notifier) *Engine {
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	e := NewEngine(store, Config{
		DispatchWorkers: 1,
		DispatchBuffer:  4,
		SendTimeout:     500 * time.Millisecond,
		DefaultSilence:  time.Second,
	}, logger)
	// Email is the low-burst channel (rate 1/60s, burst 2) that exposed the bug.
	e.RegisterBackend(ChannelTypeEmail, backend)
	return e
}

// TestSendWithRetry_RetriesBypassRateLimiter verifies that retry attempts do
// NOT each consume a rate-limit token. A sustained outage on a low-burst
// channel must keep its full retry budget rather than being abandoned after
// two attempts (audit M3).
func TestSendWithRetry_RetriesBypassRateLimiter(t *testing.T) {
	defer withTestRetrySchedule([]time.Duration{5 * time.Millisecond, 5 * time.Millisecond, 5 * time.Millisecond})()

	store := &fakeStore{}
	backend := &failingBackend{successAfter: 1000} // never succeeds
	e := newRateLimitTestEngine(store, backend)
	defer e.rateLimiter.Stop()

	rule := Rule{ID: 1, Name: "r1"}
	ch := Channel{ID: 10, Name: "email1", Type: ChannelTypeEmail, Enabled: true}
	payload := Payload{Kind: EventKindSrvlog, EventCount: 1}

	e.sendWithRetry(context.Background(), rule, ch, payload)

	expected := len(retrySchedule) + 1
	if got := int(atomic.LoadInt64(&backend.attempts)); got != expected {
		t.Fatalf("backend attempts = %d, want %d (retries must bypass the rate limiter)", got, expected)
	}
	failed := 0
	for _, en := range store.snapshot() {
		if en.Status == statusFailed {
			failed++
		}
	}
	if failed != expected {
		t.Errorf("failed log rows = %d, want %d", failed, expected)
	}
}

// TestSendWithRetry_RateLimitStillGatesPerNotification verifies the limiter is
// still enforced once per notification: with burst 2, the third successful
// send is suppressed rather than delivered.
func TestSendWithRetry_RateLimitStillGatesPerNotification(t *testing.T) {
	store := &fakeStore{}
	backend := &failingBackend{successAfter: 1} // always succeeds first try
	e := newRateLimitTestEngine(store, backend)
	defer e.rateLimiter.Stop()

	rule := Rule{ID: 1, Name: "r1"}
	ch := Channel{ID: 11, Name: "email2", Type: ChannelTypeEmail, Enabled: true}
	payload := Payload{Kind: EventKindSrvlog, EventCount: 1}

	for range 3 {
		e.sendWithRetry(context.Background(), rule, ch, payload)
	}

	var sent, suppressed int
	for _, en := range store.snapshot() {
		switch en.Status {
		case statusSent:
			sent++
		case "suppressed":
			suppressed++
		}
	}
	if sent != 2 || suppressed != 1 {
		t.Errorf("with burst 2 expected 2 sent + 1 suppressed, got %d sent / %d suppressed", sent, suppressed)
	}
	// A suppressed notification must not have reached the backend.
	if got := int(atomic.LoadInt64(&backend.attempts)); got != 2 {
		t.Errorf("backend attempts = %d, want 2 (suppressed send must not hit the backend)", got)
	}
}
