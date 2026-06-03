package postgres

import "testing"

// TestListener_LastSeenIDPerChannel verifies the per-channel last-seen tracking
// keeps srvlog and netlog baselines isolated, so a swapped case cannot
// cross-contaminate gap-fill baselines (audit S9). Uses a struct literal — the
// atomics are usable zero values, no DB needed.
func TestListener_LastSeenIDPerChannel(t *testing.T) {
	var l Listener

	// Unknown channels return 0 and storing to one is a no-op.
	if got := l.lastSeenIDForChannel("bogus"); got != 0 {
		t.Errorf("unknown channel = %d, want 0", got)
	}

	l.storeLastSeenID("srvlog_ingest", 42)
	if got := l.lastSeenIDForChannel("srvlog_ingest"); got != 42 {
		t.Errorf("srvlog last seen = %d, want 42", got)
	}
	if got := l.lastSeenIDForChannel("netlog_ingest"); got != 0 {
		t.Errorf("netlog must be unaffected by a srvlog store, got %d", got)
	}

	l.storeLastSeenID("netlog_ingest", 99)
	if got := l.lastSeenIDForChannel("netlog_ingest"); got != 99 {
		t.Errorf("netlog last seen = %d, want 99", got)
	}
	if got := l.lastSeenIDForChannel("srvlog_ingest"); got != 42 {
		t.Errorf("srvlog must be unaffected by a netlog store, got %d", got)
	}

	// Storing to an unknown channel must not touch either tracked baseline.
	l.storeLastSeenID("bogus", 7)
	if got := l.lastSeenIDForChannel("srvlog_ingest"); got != 42 {
		t.Errorf("srvlog changed by unknown-channel store: %d", got)
	}
	if got := l.lastSeenIDForChannel("netlog_ingest"); got != 99 {
		t.Errorf("netlog changed by unknown-channel store: %d", got)
	}
}
