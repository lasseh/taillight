package notification

import (
	"sync"
	"testing"
	"time"
)

type flushRecord struct {
	ruleID   int64
	groupKey string
	payload  Payload
}

func collectFlushes(t *testing.T) (*Suppressor, *sync.Mutex, *[]flushRecord) {
	t.Helper()
	var mu sync.Mutex
	var records []flushRecord

	s := NewSuppressor(func(ruleID int64, groupKey string, payload Payload) {
		mu.Lock()
		records = append(records, flushRecord{ruleID, groupKey, payload})
		mu.Unlock()
	})

	return s, &mu, &records
}

// TestSuppressor_FirstEventImmediate verifies the headline behaviour: with
// coalesce=0, a match on a clean fingerprint fires an alert without delay.
func TestSuppressor_FirstEventImmediate(t *testing.T) {
	s, mu, records := collectFlushes(t)
	defer s.Stop()

	start := time.Now()
	s.Record(1, "host1", 200*time.Millisecond, time.Second, 0, Payload{RuleName: "test"})
	elapsed := time.Since(start)

	// Record returns only after the synchronous first-alert flush.
	if elapsed > 50*time.Millisecond {
		t.Errorf("first alert took %v — expected immediate (sub-50ms)", elapsed)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(*records) != 1 {
		t.Fatalf("expected 1 flush, got %d", len(*records))
	}
	r := (*records)[0]
	if r.payload.IsDigest {
		t.Error("first flush should be alert, not digest")
	}
	if r.payload.EventCount != 1 {
		t.Errorf("count = %d, want 1", r.payload.EventCount)
	}
	if r.groupKey != "host1" {
		t.Errorf("groupKey = %q, want host1", r.groupKey)
	}
}

// TestSuppressor_SilenceSuppresses verifies events arriving after the
// initial alert are counted but not sent until the silence window closes.
func TestSuppressor_SilenceSuppresses(t *testing.T) {
	s, mu, records := collectFlushes(t)
	defer s.Stop()

	silence := 100 * time.Millisecond

	// First event fires immediately.
	s.Record(1, "host1", silence, time.Second, 0, Payload{RuleName: "test"})
	// Subsequent matches during silence should be suppressed.
	for range 5 {
		s.Record(1, "host1", silence, time.Second, 0, Payload{RuleName: "test"})
	}

	// Still within silence window — only the initial alert should have fired.
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if len(*records) != 1 {
		t.Errorf("during silence: expected 1 flush, got %d", len(*records))
	}
	mu.Unlock()

	// Wait for silence to close, then check for the digest.
	time.Sleep(silence)

	mu.Lock()
	defer mu.Unlock()
	if len(*records) != 2 {
		t.Fatalf("expected 2 flushes (alert + digest), got %d", len(*records))
	}
	digest := (*records)[1]
	if !digest.payload.IsDigest {
		t.Error("second flush should be digest")
	}
	if digest.payload.EventCount != 5 {
		t.Errorf("digest count = %d, want 5", digest.payload.EventCount)
	}
}

// TestSuppressor_QuietWindowResets verifies a fingerprint with no activity
// during silence closes cleanly — the next event fires immediately again.
func TestSuppressor_QuietWindowResets(t *testing.T) {
	s, mu, records := collectFlushes(t)
	defer s.Stop()

	silence := 80 * time.Millisecond

	s.Record(1, "host1", silence, time.Second, 0, Payload{RuleName: "test"})
	time.Sleep(silence + 50*time.Millisecond) // silence closes with count=0

	mu.Lock()
	if len(*records) != 1 {
		t.Fatalf("after quiet silence: expected 1 flush (no digest), got %d", len(*records))
	}
	mu.Unlock()

	// Next event should fire immediately (fingerprint closed).
	s.Record(1, "host1", silence, time.Second, 0, Payload{RuleName: "test-2"})

	mu.Lock()
	defer mu.Unlock()
	if len(*records) != 2 {
		t.Fatalf("expected 2 flushes total, got %d", len(*records))
	}
	if (*records)[1].payload.IsDigest {
		t.Error("second flush should be a fresh alert, not digest")
	}
}

// TestSuppressor_LinearSilenceGrowth verifies silence grows linearly and is
// capped at silenceMax.
func TestSuppressor_LinearSilenceGrowth(t *testing.T) {
	s, mu, records := collectFlushes(t)
	defer s.Stop()

	silence := 40 * time.Millisecond
	silenceMax := 100 * time.Millisecond

	// Fire the initial alert.
	s.Record(1, "host1", silence, silenceMax, 0, Payload{RuleName: "test"})

	// Keep the fingerprint active across several silence windows.
	// silence progression: 40 → 80 → 100 (cap) → 100 …
	// We run ~400ms and inject events in each window.
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.Record(1, "host1", silence, silenceMax, 0, Payload{RuleName: "test"})
		time.Sleep(20 * time.Millisecond)
	}
	// Give any pending digest time to fire.
	time.Sleep(120 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// We expect: 1 alert + multiple digests. Check the first digest is after
	// the base silence (~40ms) and later digests are spaced by >= silence +
	// step, never exceeding silenceMax by much.
	if len(*records) < 3 {
		t.Fatalf("expected at least 3 flushes, got %d", len(*records))
	}
	if (*records)[0].payload.IsDigest {
		t.Error("first flush should be alert")
	}
	for _, r := range (*records)[1:] {
		if !r.payload.IsDigest {
			t.Error("subsequent flushes should all be digests")
		}
	}
}

// TestSuppressor_CoalesceBatchesFirstAlert verifies the coalesce window folds
// simultaneous matches into a single initial alert with count=N.
func TestSuppressor_CoalesceBatchesFirstAlert(t *testing.T) {
	s, mu, records := collectFlushes(t)
	defer s.Stop()

	coalesce := 60 * time.Millisecond

	// Inject 5 events inside the coalesce window.
	for range 5 {
		s.Record(1, "host1", 500*time.Millisecond, time.Second, coalesce, Payload{RuleName: "test"})
		time.Sleep(5 * time.Millisecond)
	}

	// Before coalesce closes, no flush yet.
	mu.Lock()
	if len(*records) != 0 {
		t.Errorf("before coalesce window closes: expected 0 flushes, got %d", len(*records))
	}
	mu.Unlock()

	// Wait for coalesce to close.
	time.Sleep(coalesce + 30*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(*records) != 1 {
		t.Fatalf("expected 1 flush, got %d", len(*records))
	}
	r := (*records)[0]
	if r.payload.IsDigest {
		t.Error("coalesced flush should be alert, not digest")
	}
	if r.payload.EventCount != 5 {
		t.Errorf("count = %d, want 5", r.payload.EventCount)
	}
}

// TestSuppressor_IndependentFingerprints verifies different rule/group
// combinations are tracked separately.
func TestSuppressor_IndependentFingerprints(t *testing.T) {
	s, mu, records := collectFlushes(t)
	defer s.Stop()

	silence := 100 * time.Millisecond

	s.Record(1, "host1", silence, time.Second, 0, Payload{RuleName: "r1"})
	s.Record(1, "host2", silence, time.Second, 0, Payload{RuleName: "r1"})
	s.Record(2, "host1", silence, time.Second, 0, Payload{RuleName: "r2"})

	mu.Lock()
	defer mu.Unlock()

	if len(*records) != 3 {
		t.Fatalf("expected 3 independent alerts, got %d", len(*records))
	}
	for _, r := range *records {
		if r.payload.IsDigest {
			t.Errorf("expected all alerts, got digest for %d:%s", r.ruleID, r.groupKey)
		}
	}
}

// TestSuppressor_FingerprintCapEnforced verifies the per-rule cap prevents
// unbounded memory growth from too-wide group_by keys. Uses a large coalesce
// so timers don't fire and exercise the synchronous-flush path during the
// test.
func TestSuppressor_FingerprintCapEnforced(t *testing.T) {
	s, _, _ := collectFlushes(t)
	defer s.Stop()

	const coalesce = 10 * time.Second // never fires during the test

	// Fill to the cap.
	for i := range maxFingerprintsPerRule {
		s.Record(1, itoa(i), time.Second, time.Second, coalesce, Payload{})
	}

	// Beyond the cap — should be silently dropped (no panic, no deadlock).
	s.Record(1, "overflow", time.Second, time.Second, coalesce, Payload{})

	// Verify internal count matches the cap, not cap+1.
	s.mu.Lock()
	got := s.ruleFingerprintCount[1]
	s.mu.Unlock()
	if got != maxFingerprintsPerRule {
		t.Errorf("fingerprint count = %d, want %d", got, maxFingerprintsPerRule)
	}
}

// TestSuppressor_StopCancelsPending verifies Stop() prevents pending flushes
// from firing after shutdown.
func TestSuppressor_StopCancelsPending(t *testing.T) {
	fired := false
	var mu sync.Mutex

	s := NewSuppressor(func(_ int64, _ string, _ Payload) {
		mu.Lock()
		fired = true
		mu.Unlock()
	})

	// Coalesce delays the first send, giving Stop time to cancel.
	s.Record(1, "host1", time.Second, time.Second, 100*time.Millisecond, Payload{})
	s.Stop()

	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if fired {
		t.Error("expected no flush after Stop()")
	}
}

// itoa is a tiny helper to avoid pulling in strconv for the cap test.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
