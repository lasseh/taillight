package notification

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestPerKeyLimiter_EvictOlderThan(t *testing.T) {
	l := NewPerKeyLimiter()
	t.Cleanup(l.Stop)

	// Create two limiters via Allow.
	l.Allow(1, ChannelTypeSlack)
	l.Allow(2, ChannelTypeSlack)

	now := time.Now()
	l.mu.Lock()
	l.limiters[1].lastUsed = now.Add(-2 * time.Hour) // idle
	l.limiters[2].lastUsed = now.Add(-1 * time.Minute)
	l.mu.Unlock()

	l.evictOlderThan(now, time.Hour)

	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.limiters[1]; ok {
		t.Error("idle limiter (2h) should have been evicted")
	}
	if _, ok := l.limiters[2]; !ok {
		t.Error("fresh limiter (1m) should have been kept")
	}
}

func TestEngine_EvictStaleBreakersAsOf(t *testing.T) {
	e := NewEngine(&fakeStore{}, Config{DispatchBuffer: 1}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(e.rateLimiter.Stop)

	now := time.Now()
	e.breakerMu.Lock()
	e.breakers[1] = &breakerEntry{lastUsed: now.Add(-2 * time.Hour)} // stale
	e.breakers[2] = &breakerEntry{lastUsed: now}                     // fresh
	e.breakerMu.Unlock()

	e.evictStaleBreakersAsOf(now, time.Hour)

	e.breakerMu.Lock()
	defer e.breakerMu.Unlock()
	if _, ok := e.breakers[1]; ok {
		t.Error("stale breaker (2h) should have been evicted")
	}
	if _, ok := e.breakers[2]; !ok {
		t.Error("fresh breaker should have been kept")
	}
}
