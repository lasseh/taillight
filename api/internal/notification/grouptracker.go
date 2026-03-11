package notification

import (
	"fmt"
	"sync"
	"time"
)

// groupPhase identifies where a group is in its lifecycle.
type groupPhase int

const (
	phaseAccumulating groupPhase = iota
	phaseCooldown

	// maxGroupsPerRule limits the number of distinct group keys tracked per rule
	// to prevent memory exhaustion from attacker-controlled unique hostnames.
	maxGroupsPerRule = 10000
)

// FlushPayload carries aggregated event data when a burst or cooldown window closes.
type FlushPayload struct {
	IsDigest bool
	First    Payload       // First event (used for initial notification).
	Last     Payload       // Most recent event (used for digest content).
	Count    int           // Events accumulated in the just-closed window.
	Window   time.Duration // Duration of the window that just closed.
}

// group tracks the full lifecycle of a single rule+groupKey combination.
type group struct {
	phase           groupPhase
	first           Payload
	last            Payload
	count           int
	timer           *time.Timer
	currentCooldown time.Duration
	baseCooldown    time.Duration
	maxCooldown     time.Duration
	burstWindow     time.Duration
}

// GroupTracker manages the immediate-first + digest lifecycle for notification
// groups keyed by "ruleID:groupKey".
type GroupTracker struct {
	mu             sync.Mutex
	groups         map[string]*group
	ruleGroupCount map[int64]int // tracks number of groups per rule for cap enforcement
	stopped        bool
	inflightWg     sync.WaitGroup
	onFlush        func(ruleID int64, groupKey string, fp FlushPayload)
}

// NewGroupTracker creates a new GroupTracker.
func NewGroupTracker(onFlush func(ruleID int64, groupKey string, fp FlushPayload)) *GroupTracker {
	return &GroupTracker{
		groups:         make(map[string]*group),
		ruleGroupCount: make(map[int64]int),
		onFlush:        onFlush,
	}
}

// Add records an event for the given rule and group key.
func (gt *GroupTracker) Add(ruleID int64, groupKey string, burstWindow, cooldown, maxCooldown time.Duration, payload Payload) {
	key := groupKeyString(ruleID, groupKey)

	gt.mu.Lock()
	defer gt.mu.Unlock()

	if gt.stopped {
		return
	}

	g, ok := gt.groups[key]
	if !ok {
		// Enforce per-rule group cap to prevent memory exhaustion.
		if gt.ruleGroupCount[ruleID] >= maxGroupsPerRule {
			return
		}
		// New group: start accumulating.
		g = &group{
			phase:           phaseAccumulating,
			first:           payload,
			last:            payload,
			count:           1,
			baseCooldown:    cooldown,
			currentCooldown: cooldown,
			maxCooldown:     maxCooldown,
			burstWindow:     burstWindow,
		}
		g.timer = time.AfterFunc(burstWindow, func() {
			gt.flushBurst(ruleID, groupKey, key)
		})
		gt.groups[key] = g
		gt.ruleGroupCount[ruleID]++
		return
	}

	// Existing group — accumulate regardless of phase.
	g.count++
	g.last = payload
}

// flushBurst fires when the burst window expires: sends initial notification,
// transitions to cooldown.
func (gt *GroupTracker) flushBurst(ruleID int64, groupKey, key string) {
	gt.mu.Lock()
	if gt.stopped {
		gt.mu.Unlock()
		return
	}
	g, ok := gt.groups[key]
	if !ok {
		gt.mu.Unlock()
		return
	}

	fp := FlushPayload{
		IsDigest: false,
		First:    g.first,
		Last:     g.last,
		Count:    g.count,
		Window:   g.burstWindow,
	}

	// Transition to cooldown phase.
	g.phase = phaseCooldown
	g.count = 0
	g.timer = time.AfterFunc(g.currentCooldown, func() {
		gt.flushCooldown(ruleID, groupKey, key)
	})

	gt.inflightWg.Add(1)
	gt.mu.Unlock()

	defer gt.inflightWg.Done()
	gt.onFlush(ruleID, groupKey, fp)
}

// flushCooldown fires when the cooldown expires. If events accumulated, sends
// a digest and doubles cooldown. If silent, deletes the group (back to idle).
func (gt *GroupTracker) flushCooldown(ruleID int64, groupKey, key string) {
	gt.mu.Lock()
	if gt.stopped {
		gt.mu.Unlock()
		return
	}
	g, ok := gt.groups[key]
	if !ok {
		gt.mu.Unlock()
		return
	}

	if g.count == 0 {
		// Silence — reset to idle.
		delete(gt.groups, key)
		if gt.ruleGroupCount[ruleID] > 0 {
			gt.ruleGroupCount[ruleID]--
		}
		gt.mu.Unlock()
		return
	}

	fp := FlushPayload{
		IsDigest: true,
		First:    g.first,
		Last:     g.last,
		Count:    g.count,
		Window:   g.currentCooldown,
	}

	// Double cooldown (capped at max).
	g.currentCooldown *= 2
	if g.currentCooldown > g.maxCooldown {
		g.currentCooldown = g.maxCooldown
	}

	// Reset count and restart cooldown.
	g.count = 0
	g.timer = time.AfterFunc(g.currentCooldown, func() {
		gt.flushCooldown(ruleID, groupKey, key)
	})

	gt.inflightWg.Add(1)
	gt.mu.Unlock()

	defer gt.inflightWg.Done()
	gt.onFlush(ruleID, groupKey, fp)
}

// Stop cancels all pending timers and waits for in-flight flush callbacks.
func (gt *GroupTracker) Stop() {
	gt.mu.Lock()
	gt.stopped = true
	for key, g := range gt.groups {
		g.timer.Stop()
		delete(gt.groups, key)
	}
	gt.ruleGroupCount = make(map[int64]int)
	gt.mu.Unlock()

	gt.inflightWg.Wait()
}

func groupKeyString(ruleID int64, groupKey string) string {
	return fmt.Sprintf("%d:%s", ruleID, groupKey)
}
