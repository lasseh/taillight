package notification

import (
	"sync"
	"testing"
	"time"
)

type flushRecord struct {
	ruleID   int64
	groupKey string
	fp       FlushPayload
}

func collectFlushes(t *testing.T) (*GroupTracker, *sync.Mutex, *[]flushRecord) {
	t.Helper()
	var mu sync.Mutex
	var records []flushRecord

	gt := NewGroupTracker(func(ruleID int64, groupKey string, fp FlushPayload) {
		mu.Lock()
		records = append(records, flushRecord{ruleID, groupKey, fp})
		mu.Unlock()
	})

	return gt, &mu, &records
}

func TestGroupTracker_SingleEvent(t *testing.T) {
	gt, mu, records := collectFlushes(t)
	defer gt.Stop()

	gt.Add(1, "host1", 50*time.Millisecond, 100*time.Millisecond, time.Second, Payload{RuleName: "test"})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(*records) != 1 {
		t.Fatalf("expected 1 flush, got %d", len(*records))
	}
	r := (*records)[0]
	if r.ruleID != 1 {
		t.Errorf("ruleID = %d, want 1", r.ruleID)
	}
	if r.groupKey != "host1" {
		t.Errorf("groupKey = %q, want %q", r.groupKey, "host1")
	}
	if r.fp.IsDigest {
		t.Error("expected initial notification, not digest")
	}
	if r.fp.Count != 1 {
		t.Errorf("count = %d, want 1", r.fp.Count)
	}
}

func TestGroupTracker_BurstAccumulation(t *testing.T) {
	gt, mu, records := collectFlushes(t)
	defer gt.Stop()

	for range 5 {
		gt.Add(1, "host1", 100*time.Millisecond, 200*time.Millisecond, time.Second, Payload{RuleName: "test"})
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(*records) != 1 {
		t.Fatalf("expected 1 flush, got %d", len(*records))
	}
	if (*records)[0].fp.Count != 5 {
		t.Errorf("count = %d, want 5", (*records)[0].fp.Count)
	}
	if (*records)[0].fp.IsDigest {
		t.Error("expected initial notification, not digest")
	}
}

func TestGroupTracker_MultipleGroups(t *testing.T) {
	gt, mu, records := collectFlushes(t)
	defer gt.Stop()

	gt.Add(1, "host1", 50*time.Millisecond, 200*time.Millisecond, time.Second, Payload{RuleName: "rule-1"})
	gt.Add(1, "host2", 50*time.Millisecond, 200*time.Millisecond, time.Second, Payload{RuleName: "rule-1"})
	gt.Add(1, "host2", 50*time.Millisecond, 200*time.Millisecond, time.Second, Payload{RuleName: "rule-1"})
	gt.Add(2, "host1", 50*time.Millisecond, 200*time.Millisecond, time.Second, Payload{RuleName: "rule-2"})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(*records) != 3 {
		t.Fatalf("expected 3 flushes, got %d", len(*records))
	}

	counts := make(map[string]int)
	for _, r := range *records {
		key := groupKeyString(r.ruleID, r.groupKey)
		counts[key] = r.fp.Count
	}

	if counts["1:host1"] != 1 {
		t.Errorf("1:host1 count = %d, want 1", counts["1:host1"])
	}
	if counts["1:host2"] != 2 {
		t.Errorf("1:host2 count = %d, want 2", counts["1:host2"])
	}
	if counts["2:host1"] != 1 {
		t.Errorf("2:host1 count = %d, want 1", counts["2:host1"])
	}
}

func TestGroupTracker_DigestAfterCooldown(t *testing.T) {
	gt, mu, records := collectFlushes(t)
	defer gt.Stop()

	burst := 50 * time.Millisecond
	cooldown := 100 * time.Millisecond

	// Initial event triggers burst window.
	gt.Add(1, "host1", burst, cooldown, time.Second, Payload{RuleName: "test"})

	// Wait for burst to flush.
	time.Sleep(80 * time.Millisecond)

	// Add events during cooldown.
	gt.Add(1, "host1", burst, cooldown, time.Second, Payload{RuleName: "test", EventCount: 99})
	gt.Add(1, "host1", burst, cooldown, time.Second, Payload{RuleName: "test", EventCount: 100})

	// Wait for cooldown to expire and digest to fire.
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(*records) < 2 {
		t.Fatalf("expected at least 2 flushes (initial + digest), got %d", len(*records))
	}

	initial := (*records)[0]
	if initial.fp.IsDigest {
		t.Error("first flush should be initial, not digest")
	}
	if initial.fp.Count != 1 {
		t.Errorf("initial count = %d, want 1", initial.fp.Count)
	}

	digest := (*records)[1]
	if !digest.fp.IsDigest {
		t.Error("second flush should be digest")
	}
	if digest.fp.Count != 2 {
		t.Errorf("digest count = %d, want 2", digest.fp.Count)
	}
}

func TestGroupTracker_ExponentialBackoff(t *testing.T) {
	gt, mu, records := collectFlushes(t)
	defer gt.Stop()

	burst := 30 * time.Millisecond
	cooldown := 50 * time.Millisecond
	maxCooldown := 500 * time.Millisecond

	// First event.
	gt.Add(1, "host1", burst, cooldown, maxCooldown, Payload{RuleName: "test"})

	// Wait for burst flush.
	time.Sleep(50 * time.Millisecond)

	// Events during first cooldown (50ms).
	gt.Add(1, "host1", burst, cooldown, maxCooldown, Payload{RuleName: "test"})
	time.Sleep(80 * time.Millisecond) // Cooldown fires digest, doubles to 100ms.

	// Events during second cooldown (100ms).
	gt.Add(1, "host1", burst, cooldown, maxCooldown, Payload{RuleName: "test"})
	time.Sleep(130 * time.Millisecond) // Cooldown fires digest, doubles to 200ms.

	mu.Lock()
	defer mu.Unlock()

	// Should have: initial + digest1 + digest2.
	if len(*records) < 3 {
		t.Fatalf("expected at least 3 flushes, got %d", len(*records))
	}

	if (*records)[0].fp.IsDigest {
		t.Error("first flush should be initial")
	}
	if !(*records)[1].fp.IsDigest {
		t.Error("second flush should be digest")
	}
	if !(*records)[2].fp.IsDigest {
		t.Error("third flush should be digest")
	}
}

func TestGroupTracker_SilenceResetsToIdle(t *testing.T) {
	gt, mu, records := collectFlushes(t)
	defer gt.Stop()

	burst := 30 * time.Millisecond
	cooldown := 50 * time.Millisecond

	// First event.
	gt.Add(1, "host1", burst, cooldown, time.Second, Payload{RuleName: "test"})

	// Wait for burst flush + cooldown with no events.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	count := len(*records)
	mu.Unlock()

	// Should only have the initial notification, no digest (silence).
	if count != 1 {
		t.Fatalf("expected 1 flush (initial only, no digest), got %d", count)
	}

	// Verify group was cleaned up — adding new event should start fresh.
	gt.Add(1, "host1", burst, cooldown, time.Second, Payload{RuleName: "test-2"})
	time.Sleep(60 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(*records) != 2 {
		t.Fatalf("expected 2 flushes total, got %d", len(*records))
	}
	if (*records)[1].fp.IsDigest {
		t.Error("second flush should be a new initial, not digest")
	}
}

func TestGroupTracker_Stop(t *testing.T) {
	flushed := false

	gt := NewGroupTracker(func(_ int64, _ string, _ FlushPayload) {
		flushed = true
	})

	gt.Add(1, "host1", 100*time.Millisecond, 200*time.Millisecond, time.Second, Payload{RuleName: "test"})
	gt.Stop()

	time.Sleep(150 * time.Millisecond)

	if flushed {
		t.Error("expected no flush after Stop()")
	}
}
