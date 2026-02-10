package notification

import (
	"sync"
	"testing"
	"time"
)

func TestBurstWatcher_SingleEvent(t *testing.T) {
	var mu sync.Mutex
	var flushed []struct {
		ruleID int64
		count  int
	}

	bw := NewBurstWatcher(50*time.Millisecond, func(ruleID int64, _ Payload, count int) {
		mu.Lock()
		flushed = append(flushed, struct {
			ruleID int64
			count  int
		}{ruleID, count})
		mu.Unlock()
	})
	defer bw.Stop()

	bw.Add(1, 50*time.Millisecond, Payload{RuleName: "test"})

	// Wait for burst window to fire.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(flushed) != 1 {
		t.Fatalf("expected 1 flush, got %d", len(flushed))
	}
	if flushed[0].ruleID != 1 {
		t.Errorf("ruleID = %d, want 1", flushed[0].ruleID)
	}
	if flushed[0].count != 1 {
		t.Errorf("count = %d, want 1", flushed[0].count)
	}
}

func TestBurstWatcher_Accumulation(t *testing.T) {
	var mu sync.Mutex
	var flushed []struct {
		ruleID int64
		count  int
	}

	bw := NewBurstWatcher(100*time.Millisecond, func(ruleID int64, _ Payload, count int) {
		mu.Lock()
		flushed = append(flushed, struct {
			ruleID int64
			count  int
		}{ruleID, count})
		mu.Unlock()
	})
	defer bw.Stop()

	// Add multiple events within the burst window.
	for i := 0; i < 5; i++ {
		bw.Add(1, 100*time.Millisecond, Payload{RuleName: "test"})
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for burst window to fire.
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(flushed) != 1 {
		t.Fatalf("expected 1 flush, got %d", len(flushed))
	}
	if flushed[0].count != 5 {
		t.Errorf("count = %d, want 5", flushed[0].count)
	}
}

func TestBurstWatcher_MultipleRules(t *testing.T) {
	var mu sync.Mutex
	results := make(map[int64]int)

	bw := NewBurstWatcher(50*time.Millisecond, func(ruleID int64, _ Payload, count int) {
		mu.Lock()
		results[ruleID] = count
		mu.Unlock()
	})
	defer bw.Stop()

	bw.Add(1, 50*time.Millisecond, Payload{RuleName: "rule-1"})
	bw.Add(2, 50*time.Millisecond, Payload{RuleName: "rule-2"})
	bw.Add(2, 50*time.Millisecond, Payload{RuleName: "rule-2"})
	bw.Add(2, 50*time.Millisecond, Payload{RuleName: "rule-2"})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if results[1] != 1 {
		t.Errorf("rule 1 count = %d, want 1", results[1])
	}
	if results[2] != 3 {
		t.Errorf("rule 2 count = %d, want 3", results[2])
	}
}

func TestBurstWatcher_Stop(t *testing.T) {
	flushed := false

	bw := NewBurstWatcher(100*time.Millisecond, func(_ int64, _ Payload, _ int) {
		flushed = true
	})

	bw.Add(1, 100*time.Millisecond, Payload{RuleName: "test"})
	bw.Stop()

	time.Sleep(150 * time.Millisecond)

	if flushed {
		t.Error("expected no flush after Stop()")
	}
}
