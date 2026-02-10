package notification

import (
	"sync"

	"golang.org/x/time/rate"
)

// channelDefaults maps channel types to their default rate limits.
var channelDefaults = map[ChannelType]struct {
	rate  rate.Limit
	burst int
}{
	ChannelTypeSlack:   {rate: 1, burst: 3},
	ChannelTypeWebhook: {rate: 5, burst: 10},
}

// PerKeyLimiter provides per-channel token bucket rate limiting.
type PerKeyLimiter struct {
	mu       sync.RWMutex
	limiters map[int64]*rate.Limiter
}

// NewPerKeyLimiter creates a new PerKeyLimiter.
func NewPerKeyLimiter() *PerKeyLimiter {
	return &PerKeyLimiter{
		limiters: make(map[int64]*rate.Limiter),
	}
}

// Allow reports whether a notification to the given channel is allowed
// by the rate limiter.
func (l *PerKeyLimiter) Allow(channelID int64, channelType ChannelType) bool {
	l.mu.RLock()
	lim, ok := l.limiters[channelID]
	l.mu.RUnlock()

	if !ok {
		l.mu.Lock()
		// Double-check after acquiring write lock.
		lim, ok = l.limiters[channelID]
		if !ok {
			d := channelDefaults[channelType]
			if d.rate == 0 {
				d.rate = 5
				d.burst = 10
			}
			lim = rate.NewLimiter(d.rate, d.burst)
			l.limiters[channelID] = lim
		}
		l.mu.Unlock()
	}

	return lim.Allow()
}
