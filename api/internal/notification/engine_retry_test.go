package notification

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	statusFailed = "failed"
	statusSent   = "sent"
)

// fakeStore captures notification_log inserts.
type fakeStore struct {
	mu      sync.Mutex
	entries []LogEntry
}

func (f *fakeStore) ListNotificationRules(context.Context) ([]Rule, error)       { return nil, nil }
func (f *fakeStore) ListNotificationChannels(context.Context) ([]Channel, error) { return nil, nil }
func (f *fakeStore) InsertNotificationLog(_ context.Context, entry LogEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, entry)
	return nil
}
func (f *fakeStore) snapshot() []LogEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]LogEntry, len(f.entries))
	copy(out, f.entries)
	return out
}

// failingBackend returns failure until atomic counter reaches successAfter.
type failingBackend struct {
	attempts     int64
	successAfter int64
}

func (b *failingBackend) Send(_ context.Context, _ Channel, _ Payload) SendResult {
	n := atomic.AddInt64(&b.attempts, 1)
	if n >= b.successAfter {
		return SendResult{Success: true, StatusCode: 200, Duration: time.Millisecond}
	}
	return SendResult{
		Success:    false,
		StatusCode: 503,
		Error:      errors.New("simulated failure"),
		Duration:   time.Millisecond,
	}
}

func (b *failingBackend) Validate(Channel) error { return nil }

// withTestRetrySchedule replaces the package retrySchedule for the duration
// of a test. Returns a restore function to defer.
func withTestRetrySchedule(schedule []time.Duration) func() {
	prev := retrySchedule
	retrySchedule = schedule
	return func() { retrySchedule = prev }
}

func newTestEngine(store Store, backend Notifier) *Engine {
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	e := NewEngine(store, Config{
		DispatchWorkers: 1,
		DispatchBuffer:  4,
		SendTimeout:     500 * time.Millisecond,
		DefaultSilence:  time.Second,
	}, logger)
	e.RegisterBackend(ChannelTypeWebhook, backend)
	return e
}

// TestSendWithRetry_SucceedsAfterFailures verifies a backend that fails twice
// then succeeds produces three log rows and counts as retry_success.
func TestSendWithRetry_SucceedsAfterFailures(t *testing.T) {
	defer withTestRetrySchedule([]time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond})()

	store := &fakeStore{}
	backend := &failingBackend{successAfter: 3}
	e := newTestEngine(store, backend)

	rule := Rule{ID: 1, Name: "r1"}
	ch := Channel{ID: 10, Name: "w1", Type: ChannelTypeWebhook, Enabled: true}
	payload := Payload{Kind: EventKindSrvlog, RuleName: rule.Name, EventCount: 1}

	done := make(chan struct{})
	go func() {
		e.sendWithRetry(context.Background(), rule, ch, payload)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("sendWithRetry did not return in time")
	}

	entries := store.snapshot()
	if len(entries) != 3 {
		t.Fatalf("expected 3 notification_log rows, got %d", len(entries))
	}
	wantStatuses := []string{statusFailed, statusFailed, statusSent}
	for i, want := range wantStatuses {
		if entries[i].Status != want {
			t.Errorf("entry[%d].status = %q, want %q", i, entries[i].Status, want)
		}
	}
	if got := atomic.LoadInt64(&backend.attempts); got != 3 {
		t.Errorf("backend attempts = %d, want 3", got)
	}
}

// TestSendWithRetry_ExhaustsAndGivesUp verifies that a backend that always
// fails produces maxAttempts rows, all with status=failed.
func TestSendWithRetry_ExhaustsAndGivesUp(t *testing.T) {
	defer withTestRetrySchedule([]time.Duration{5 * time.Millisecond, 5 * time.Millisecond, 5 * time.Millisecond})()

	store := &fakeStore{}
	backend := &failingBackend{successAfter: 1000} // never succeeds
	e := newTestEngine(store, backend)

	rule := Rule{ID: 2}
	ch := Channel{ID: 20, Name: "w2", Type: ChannelTypeWebhook, Enabled: true}
	payload := Payload{Kind: EventKindSrvlog, EventCount: 1}

	e.sendWithRetry(context.Background(), rule, ch, payload)

	entries := store.snapshot()
	expected := len(retrySchedule) + 1
	if len(entries) != expected {
		t.Fatalf("expected %d notification_log rows, got %d", expected, len(entries))
	}
	for i, entry := range entries {
		if entry.Status != statusFailed {
			t.Errorf("entry[%d].status = %q, want %q", i, entry.Status, statusFailed)
		}
	}
}

// TestSendWithRetry_FirstTrySuccess verifies a backend that succeeds on the
// first attempt produces exactly one log row with status=sent.
func TestSendWithRetry_FirstTrySuccess(t *testing.T) {
	store := &fakeStore{}
	backend := &failingBackend{successAfter: 1}
	e := newTestEngine(store, backend)

	rule := Rule{ID: 3}
	ch := Channel{ID: 30, Name: "w3", Type: ChannelTypeWebhook, Enabled: true}
	payload := Payload{Kind: EventKindSrvlog, EventCount: 1}

	e.sendWithRetry(context.Background(), rule, ch, payload)

	entries := store.snapshot()
	if len(entries) != 1 {
		t.Fatalf("expected 1 notification_log row, got %d", len(entries))
	}
	if entries[0].Status != statusSent {
		t.Errorf("status = %q, want %q", entries[0].Status, statusSent)
	}
}
