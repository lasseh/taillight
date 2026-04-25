package netbox

import (
	"sync"
	"time"
)

// cacheEntry is a TTL-bounded result.
// data may be nil to record a negative cache (Netbox returned no match).
type cacheEntry struct {
	data    any
	expires time.Time
}

// cache is a TTL-bounded map keyed by "<entity-type>:<canonical-value>".
// Negative results (Netbox 404s) are cached to avoid repeatedly hammering
// the API for unknown entities.
type cache struct {
	mu      sync.Mutex
	entries map[string]*cacheEntry
	ttl     time.Duration
	stopCh  chan struct{}
}

func newCache(ttl time.Duration) *cache {
	c := &cache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
		stopCh:  make(chan struct{}),
	}
	go c.evictLoop()
	return c
}

// get returns (data, true) on cache hit. data may be nil for cached "not found".
// Returns (nil, false) on cache miss or expired entry.
func (c *cache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expires) {
		delete(c.entries, key)
		return nil, false
	}
	return e.data, true
}

// set stores data under key with the cache's TTL. data may be nil to cache a
// negative result.
func (c *cache) set(key string, data any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = &cacheEntry{
		data:    data,
		expires: time.Now().Add(c.ttl),
	}
}

func (c *cache) evictLoop() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-t.C:
			c.evict()
		}
	}
}

func (c *cache) evict() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if now.After(e.expires) {
			delete(c.entries, k)
		}
	}
}

// stop ends the eviction goroutine. Used by tests.
func (c *cache) stop() {
	select {
	case <-c.stopCh: // already closed
	default:
		close(c.stopCh)
	}
}
