package notification

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sony/gobreaker/v2"
)

// rejectingBackend refuses every payload with ErrPayloadRejected until
// accept is set, then delivers.
type rejectingBackend struct {
	attempts atomic.Int64
	accept   atomic.Bool
}

func (b *rejectingBackend) Send(context.Context, Channel, Payload) SendResult {
	b.attempts.Add(1)
	if b.accept.Load() {
		return SendResult{Success: true, StatusCode: 200, Duration: time.Millisecond}
	}
	return SendResult{
		StatusCode: 400,
		Error:      fmt.Errorf("%w: status 400", ErrPayloadRejected),
		Duration:   time.Millisecond,
	}
}

func (b *rejectingBackend) Validate(Channel) error { return nil }

// TestSendWithRetry_PayloadRejectionLeavesBreakerClosed verifies that sends
// the destination refuses for their content are not retried and do not count
// toward the channel's circuit breaker, so a later alert is still delivered.
func TestSendWithRetry_PayloadRejectionLeavesBreakerClosed(t *testing.T) {
	// Short delays so a wrongly retried rejection shows up as extra attempts
	// instead of a slow test.
	defer withTestRetrySchedule([]time.Duration{time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond})()

	store := &fakeStore{}
	backend := &rejectingBackend{}
	e := newTestEngine(store, backend)
	defer e.rateLimiter.Stop()

	rule := Rule{ID: 1, Name: "r1"}
	ch := Channel{ID: 60, Name: "w-reject", Type: ChannelTypeWebhook, Enabled: true}
	payload := Payload{Kind: EventKindAppLog, RuleName: rule.Name, EventCount: 1}

	const rejected = 5
	for range rejected {
		e.sendWithRetry(context.Background(), rule, ch, payload)
	}

	if got := backend.attempts.Load(); got != rejected {
		t.Errorf("backend attempts = %d, want %d (rejections must not be retried)", got, rejected)
	}
	entries := store.snapshot()
	if len(entries) != rejected {
		t.Fatalf("expected %d notification_log rows, got %d", rejected, len(entries))
	}
	for i, entry := range entries {
		if entry.Status != statusFailed {
			t.Errorf("entry[%d].status = %q, want %q", i, entry.Status, statusFailed)
		}
	}
	if st := e.getOrCreateBreaker(ch.ID, ch.Name).State(); st != gobreaker.StateClosed {
		t.Fatalf("breaker state after %d rejections = %v, want closed", rejected, st)
	}

	// A genuine alert on the same channel is delivered.
	backend.accept.Store(true)
	e.sendWithRetry(context.Background(), rule, ch, Payload{Kind: EventKindNetlog, RuleName: rule.Name, EventCount: 1})

	entries = store.snapshot()
	if last := entries[len(entries)-1]; last.Status != statusSent {
		t.Errorf("genuine alert status = %q, want %q", last.Status, statusSent)
	}
}
