package notification

import (
	"testing"
	"time"
)

func TestCooldownTracker_NotInCooldown(t *testing.T) {
	ct := NewCooldownTracker()

	if ct.Check(1) {
		t.Error("rule should not be in cooldown initially")
	}
}

func TestCooldownTracker_ActiveCooldown(t *testing.T) {
	ct := NewCooldownTracker()
	ct.Activate(1, 100*time.Millisecond)

	if !ct.Check(1) {
		t.Error("rule should be in cooldown after Activate")
	}

	time.Sleep(150 * time.Millisecond)

	if ct.Check(1) {
		t.Error("rule should not be in cooldown after expiry")
	}
}

func TestCooldownTracker_SuppressAndDrain(t *testing.T) {
	ct := NewCooldownTracker()
	ct.Activate(1, 50*time.Millisecond)

	ct.Suppress(1)
	ct.Suppress(1)
	ct.Suppress(1)

	// Not yet expired.
	expired := ct.DrainExpired()
	if len(expired) != 0 {
		t.Errorf("expected no expired entries yet, got %d", len(expired))
	}

	time.Sleep(100 * time.Millisecond)

	expired = ct.DrainExpired()
	if len(expired) != 1 {
		t.Fatalf("expected 1 expired entry, got %d", len(expired))
	}
	if expired[0].RuleID != 1 {
		t.Errorf("RuleID = %d, want 1", expired[0].RuleID)
	}
	if expired[0].SuppressedCount != 3 {
		t.Errorf("SuppressedCount = %d, want 3", expired[0].SuppressedCount)
	}

	// Should be cleaned up now.
	expired = ct.DrainExpired()
	if len(expired) != 0 {
		t.Errorf("expected no expired entries after drain, got %d", len(expired))
	}
}

func TestCooldownTracker_ExpiredWithoutSuppression(t *testing.T) {
	ct := NewCooldownTracker()
	ct.Activate(1, 50*time.Millisecond)

	time.Sleep(100 * time.Millisecond)

	// No events were suppressed — should be silently cleaned up.
	expired := ct.DrainExpired()
	if len(expired) != 0 {
		t.Errorf("expected no expired entries (no suppressed events), got %d", len(expired))
	}

	// Entry should be removed.
	if ct.Check(1) {
		t.Error("rule should not be in cooldown after drain")
	}
}
