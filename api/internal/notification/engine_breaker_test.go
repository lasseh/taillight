package notification

import (
	"context"
	"sync/atomic"
	"testing"
)

// TestAttemptSend_OpenBreakerSuppressesWithoutBackend drives a channel past the
// breaker trip threshold and asserts the next attempt short-circuits to
// attemptSuppressed without touching the backend — i.e. it does not retry
// through an open breaker (audit N9).
func TestAttemptSend_OpenBreakerSuppressesWithoutBackend(t *testing.T) {
	store := &fakeStore{}
	backend := &failingBackend{successAfter: 1000} // never succeeds
	e := newTestEngine(store, backend)
	defer e.rateLimiter.Stop()

	rule := Rule{ID: 1, Name: "r1"}
	ch := Channel{ID: 50, Name: "w-open", Type: ChannelTypeWebhook, Enabled: true}
	payload := Payload{Kind: EventKindSrvlog, EventCount: 1}

	// Drive failures until the breaker trips (it opens on the failure that
	// pushes ConsecutiveFailures to the threshold, so that call still hits the
	// backend and reports suppressed).
	tripped := false
	for i := 1; i <= 10; i++ {
		status, _ := e.attemptSend(context.Background(), rule, ch, payload)
		if status == attemptSuppressed {
			tripped = true
			break
		}
		if status != attemptFailed {
			t.Fatalf("attempt %d: expected attemptFailed while breaker closed, got %v", i, status)
		}
	}
	if !tripped {
		t.Fatal("breaker never opened after sustained failures")
	}

	// The breaker is now open. A further attempt must short-circuit without
	// touching the backend.
	attemptsBefore := atomic.LoadInt64(&backend.attempts)
	status, result := e.attemptSend(context.Background(), rule, ch, payload)
	if status != attemptSuppressed {
		t.Fatalf("expected attemptSuppressed with open breaker, got %v", status)
	}
	if got := atomic.LoadInt64(&backend.attempts); got != attemptsBefore {
		t.Errorf("backend was invoked while breaker open (attempts %d -> %d)", attemptsBefore, got)
	}
	if result.Error == nil {
		t.Error("expected a non-nil error describing the open breaker")
	}
}
