package notification

import (
	"sync"
	"time"
)

// cooldownState tracks when a rule entered cooldown and how many events
// were suppressed during that period.
type cooldownState struct {
	until           time.Time
	suppressedCount int
}

// ExpiredCooldown holds information about a cooldown that has expired.
type ExpiredCooldown struct {
	RuleID          int64
	SuppressedCount int
}

// CooldownTracker manages per-rule post-send cooldown periods.
type CooldownTracker struct {
	mu    sync.Mutex
	rules map[int64]*cooldownState
}

// NewCooldownTracker creates a new CooldownTracker.
func NewCooldownTracker() *CooldownTracker {
	return &CooldownTracker{
		rules: make(map[int64]*cooldownState),
	}
}

// Check reports whether the given rule is currently in cooldown.
func (ct *CooldownTracker) Check(ruleID int64) bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	state, ok := ct.rules[ruleID]
	if !ok {
		return false
	}
	return time.Now().Before(state.until)
}

// Activate starts a cooldown period for the given rule.
func (ct *CooldownTracker) Activate(ruleID int64, duration time.Duration) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.rules[ruleID] = &cooldownState{
		until: time.Now().Add(duration),
	}
}

// Suppress increments the suppressed event count for a rule in cooldown.
func (ct *CooldownTracker) Suppress(ruleID int64) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if state, ok := ct.rules[ruleID]; ok {
		state.suppressedCount++
	}
}

// DrainExpired returns all rules whose cooldown has expired and that had
// suppressed events. It removes the expired entries from the tracker.
func (ct *CooldownTracker) DrainExpired() []ExpiredCooldown {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	now := time.Now()
	var expired []ExpiredCooldown

	for ruleID, state := range ct.rules {
		if now.After(state.until) {
			if state.suppressedCount > 0 {
				expired = append(expired, ExpiredCooldown{
					RuleID:          ruleID,
					SuppressedCount: state.suppressedCount,
				})
			}
			delete(ct.rules, ruleID)
		}
	}

	return expired
}
