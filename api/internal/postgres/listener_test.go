package postgres

import (
	"testing"
	"time"
)

// TestParsePayload covers both NOTIFY payload forms: "<id> <received_at in
// unix microseconds>" from the current trigger, and "<id>" alone from the
// trigger before it or for a row whose received_at is not finite.
func TestParsePayload(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantID  int64
		wantAt  time.Time // zero: the payload carried no timestamp
		wantErr bool
	}{
		{name: "id only", payload: "42", wantID: 42},
		{
			name:    "id and received_at",
			payload: "42 1762162335123457",
			wantID:  42,
			wantAt:  time.Date(2025, 11, 3, 9, 32, 15, 123457000, time.UTC),
		},
		{
			name:    "received_at before 1970",
			payload: "7 -1000001",
			wantID:  7,
			wantAt:  time.Date(1969, 12, 31, 23, 59, 58, 999999000, time.UTC),
		},
		{name: "empty", payload: "", wantErr: true},
		{name: "not a number", payload: "abc", wantErr: true},
		{name: "malformed received_at", payload: "42 abc", wantErr: true},
		{name: "trailing space", payload: "42 ", wantErr: true},
		{name: "extra field", payload: "42 1762162335123457 9", wantErr: true},
		{name: "malformed id with received_at", payload: "x 1762162335123457", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, at, err := parsePayload(tt.payload)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parsePayload(%q) = %d, %v; want error", tt.payload, id, at)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePayload(%q): %v", tt.payload, err)
			}
			if id != tt.wantID {
				t.Errorf("id = %d, want %d", id, tt.wantID)
			}
			if !at.Equal(tt.wantAt) || at.IsZero() != tt.wantAt.IsZero() {
				t.Errorf("received_at = %v, want %v", at, tt.wantAt)
			}
		})
	}
}

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
