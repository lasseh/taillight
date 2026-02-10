package notification

import (
	"sync"
	"time"
)

// burst tracks accumulated events for a single rule during a burst window.
type burst struct {
	first  Payload
	count  int
	timer  *time.Timer
	window time.Duration
}

// BurstWatcher collects events per rule into burst windows. When the window
// expires, it calls onFlush with the first event and total count.
type BurstWatcher struct {
	mu            sync.Mutex
	bursts        map[int64]*burst
	onFlush       func(ruleID int64, first Payload, count int)
	defaultWindow time.Duration
}

// NewBurstWatcher creates a new BurstWatcher.
func NewBurstWatcher(defaultWindow time.Duration, onFlush func(ruleID int64, first Payload, count int)) *BurstWatcher {
	return &BurstWatcher{
		bursts:        make(map[int64]*burst),
		onFlush:       onFlush,
		defaultWindow: defaultWindow,
	}
}

// Add records an event for the given rule. If no burst window is active, one
// is started. If a burst is already active, the count is incremented.
func (bw *BurstWatcher) Add(ruleID int64, window time.Duration, payload Payload) {
	if window <= 0 {
		window = bw.defaultWindow
	}

	bw.mu.Lock()
	defer bw.mu.Unlock()

	if b, ok := bw.bursts[ruleID]; ok {
		b.count++
		return
	}

	b := &burst{
		first:  payload,
		count:  1,
		window: window,
	}
	b.timer = time.AfterFunc(window, func() {
		bw.flush(ruleID)
	})
	bw.bursts[ruleID] = b
}

// flush fires the onFlush callback and removes the burst entry.
func (bw *BurstWatcher) flush(ruleID int64) {
	bw.mu.Lock()
	b, ok := bw.bursts[ruleID]
	if !ok {
		bw.mu.Unlock()
		return
	}
	first := b.first
	count := b.count
	delete(bw.bursts, ruleID)
	bw.mu.Unlock()

	bw.onFlush(ruleID, first, count)
}

// Stop cancels all pending burst timers.
func (bw *BurstWatcher) Stop() {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	for id, b := range bw.bursts {
		b.timer.Stop()
		delete(bw.bursts, id)
	}
}
