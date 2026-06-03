package notification

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func newStartedEngine(t *testing.T, store Store, register func(*Engine)) *Engine {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	e := NewEngine(store, Config{
		DispatchWorkers:     1,
		DispatchBuffer:      8,
		SendTimeout:         500 * time.Millisecond,
		DefaultSilence:      time.Second,
		RuleRefreshInterval: time.Minute, // must be > 0: Start() tickers on it
	}, logger)
	register(e)
	return e
}

// TestEngine_ShutdownDrainsInFlightRetry verifies that Shutdown cancels an
// in-flight retry backoff and returns promptly, rather than waiting out the
// (here 1h) backoff or leaking the send goroutine — the failure mode a retry
// rework can easily introduce (audit S6).
func TestEngine_ShutdownDrainsInFlightRetry(t *testing.T) {
	defer withTestRetrySchedule([]time.Duration{time.Hour})()

	backend := &failingBackend{successAfter: 1000} // always fails → enters backoff
	e := newStartedEngine(t, &fakeStore{}, func(e *Engine) {
		e.RegisterBackend(ChannelTypeWebhook, backend)
	})
	e.Start(context.Background())

	e.dispatchCh <- dispatchJob{
		rule:     Rule{ID: 1, Name: "r1"},
		channels: []Channel{{ID: 10, Name: "w1", Type: ChannelTypeWebhook, Enabled: true}},
		payload:  Payload{Kind: EventKindSrvlog, EventCount: 1},
	}

	// Wait until the first attempt has failed (the send goroutine is now parked
	// in the 1h backoff).
	waitFor(t, func() bool { return atomic.LoadInt64(&backend.attempts) >= 1 }, 2*time.Second,
		"send was never attempted")

	start := time.Now()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := e.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown did not drain in-flight retry: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Shutdown took %v; in-flight retry backoff was not cancelled", elapsed)
	}
}

// TestEngine_FailingChannelDoesNotBlockHealthy verifies a failing channel stuck
// in a long retry backoff does not delay a healthy sibling channel in the same
// job — the core fix of moving per-channel delivery off the worker (audit S6).
func TestEngine_FailingChannelDoesNotBlockHealthy(t *testing.T) {
	defer withTestRetrySchedule([]time.Duration{time.Hour})()

	failing := &failingBackend{successAfter: 1000} // webhook: always fails, long backoff
	healthy := &failingBackend{successAfter: 1}    // email: succeeds first try
	e := newStartedEngine(t, &fakeStore{}, func(e *Engine) {
		e.RegisterBackend(ChannelTypeWebhook, failing)
		e.RegisterBackend(ChannelTypeEmail, healthy)
	})
	e.Start(context.Background())
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = e.Shutdown(ctx)
	}()

	// Failing channel is listed first; with the old serial dispatch the healthy
	// channel would wait out the failing channel's 1h backoff.
	e.dispatchCh <- dispatchJob{
		rule: Rule{ID: 1, Name: "r1"},
		channels: []Channel{
			{ID: 1, Name: "wh", Type: ChannelTypeWebhook, Enabled: true},
			{ID: 2, Name: "em", Type: ChannelTypeEmail, Enabled: true},
		},
		payload: Payload{Kind: EventKindSrvlog, EventCount: 1},
	}

	waitFor(t, func() bool { return atomic.LoadInt64(&healthy.attempts) >= 1 }, 2*time.Second,
		"healthy channel was blocked by the failing channel's retry backoff")
}

func waitFor(t *testing.T, cond func() bool, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(time.Millisecond)
	}
}
