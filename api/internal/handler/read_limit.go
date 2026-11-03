package handler

import (
	"net/http"
	"sync"
	"time"
)

const (
	// readLimitPerPrincipal caps the log read requests (lists, stats,
	// exports) one principal runs at once, so one principal cannot hold most
	// of the read pool's connections.
	readLimitPerPrincipal = 4
	// readLimitWait is how long a request over the cap waits for a slot.
	// It stays under the SPA's 15s and the TUI's 10s client timeouts, so a
	// burst of page-load requests queues instead of failing.
	readLimitWait = 8 * time.Second
)

// ReadLimiter caps concurrent log read requests per principal: the
// authenticated user, or the client IP when the request is anonymous (the key
// SSE subscriptions are capped by, see sseClientKey). A request over the cap
// waits for a slot, then gets 429.
type ReadLimiter struct {
	max  int
	wait time.Duration

	mu    sync.Mutex
	slots map[string]*principalSlots
}

// principalSlots is one principal's semaphore. refs counts the requests that
// hold or wait for a slot, so the entry is dropped once the last one leaves.
type principalSlots struct {
	sem  chan struct{}
	refs int
}

// NewReadLimiter returns a ReadLimiter with the default per-principal cap.
func NewReadLimiter() *ReadLimiter {
	return &ReadLimiter{max: readLimitPerPrincipal, wait: readLimitWait, slots: make(map[string]*principalSlots)}
}

// Middleware runs next once the request's principal has a free slot.
func (l *ReadLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := sseClientKey(r)
		s := l.join(key)
		defer l.leave(key, s)

		timer := time.NewTimer(l.wait)
		defer timer.Stop()
		select {
		case s.sem <- struct{}{}:
		case <-timer.C:
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "too_many_requests", "too many concurrent requests, retry shortly")
			return
		case <-r.Context().Done():
			return
		}
		defer func() { <-s.sem }()
		next.ServeHTTP(w, r)
	})
}

func (l *ReadLimiter) join(key string) *principalSlots {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.slots[key]
	if s == nil {
		s = &principalSlots{sem: make(chan struct{}, l.max)}
		l.slots[key] = s
	}
	s.refs++
	return s
}

func (l *ReadLimiter) leave(key string, s *principalSlots) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s.refs--
	if s.refs == 0 {
		delete(l.slots, key)
	}
}
