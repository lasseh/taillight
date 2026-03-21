package notification

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// limiterTTL is how long an idle limiter is kept before eviction.
	limiterTTL = 1 * time.Hour
	// limiterEvictInterval is how often the eviction loop runs.
	limiterEvictInterval = 10 * time.Minute
)

// channelDefaults maps channel types to their default rate limits.
var channelDefaults = map[ChannelType]struct {
	rate  rate.Limit
	burst int
}{
	ChannelTypeSlack:   {rate: 1, burst: 3},
	ChannelTypeWebhook: {rate: 5, burst: 10},
	ChannelTypeEmail:   {rate: rate.Limit(1.0 / 60.0), burst: 2},
	ChannelTypeNtfy:    {rate: 5, burst: 10},
}

// limiterEntry wraps a rate.Limiter with a last-used timestamp for TTL eviction.
type limiterEntry struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// PerKeyLimiter provides per-channel token bucket rate limiting with TTL eviction.
type PerKeyLimiter struct {
	mu       sync.Mutex
	limiters map[int64]*limiterEntry
	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewPerKeyLimiter creates a new PerKeyLimiter and starts the eviction loop.
func NewPerKeyLimiter() *PerKeyLimiter {
	l := &PerKeyLimiter{
		limiters: make(map[int64]*limiterEntry),
		stopCh:   make(chan struct{}),
	}
	go l.evictLoop()
	return l
}

// Stop halts the eviction loop.
func (l *PerKeyLimiter) Stop() {
	l.stopOnce.Do(func() { close(l.stopCh) })
}

// Allow reports whether a notification to the given channel is allowed
// by the rate limiter.
func (l *PerKeyLimiter) Allow(channelID int64, channelType ChannelType) bool {
	now := time.Now()

	l.mu.Lock()
	entry, ok := l.limiters[channelID]
	if !ok {
		d := channelDefaults[channelType]
		if d.rate == 0 {
			d.rate = 5
			d.burst = 10
		}
		entry = &limiterEntry{
			limiter:  rate.NewLimiter(d.rate, d.burst),
			lastUsed: now,
		}
		l.limiters[channelID] = entry
	} else {
		entry.lastUsed = now
	}
	l.mu.Unlock()

	return entry.limiter.Allow()
}

// evictLoop periodically removes limiters that haven't been used recently.
func (l *PerKeyLimiter) evictLoop() {
	ticker := time.NewTicker(limiterEvictInterval)
	defer ticker.Stop()
	for {
		select {
		case <-l.stopCh:
			return
		case now := <-ticker.C:
			l.mu.Lock()
			for id, entry := range l.limiters {
				if now.Sub(entry.lastUsed) > limiterTTL {
					delete(l.limiters, id)
				}
			}
			l.mu.Unlock()
		}
	}
}
