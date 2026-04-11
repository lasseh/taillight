package srvlog

import (
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/tui/client"
)

func testEvents(n int) []client.SrvlogEvent {
	events := make([]client.SrvlogEvent, n)
	for i := range n {
		events[i] = client.SrvlogEvent{
			ID:            int64(i + 1),
			ReceivedAt:    time.Now(),
			ReportedAt:    time.Now(),
			Hostname:      "host-01",
			Programname:   "sshd",
			Severity:      3,
			SeverityLabel: "err",
			FacilityLabel: "auth",
			Message:       "test event",
		}
	}
	return events
}

func TestNew(t *testing.T) {
	m := New(100, "15:04:05")
	if m.buf.Len() != 0 {
		t.Fatalf("new model should have empty buffer, got %d", m.buf.Len())
	}
	if m.DetailOpen() {
		t.Fatal("detail should not be open on new model")
	}
}

func TestPushEvents(t *testing.T) {
	m := New(100, "15:04:05")
	m.SetSize(120, 40)

	events := testEvents(5)
	m.PushEvents(events)

	if m.buf.Len() != 5 {
		t.Fatalf("buffer should have 5 events, got %d", m.buf.Len())
	}

	// Table should have rows (events match default empty filter).
	if len(m.events) != 5 {
		t.Fatalf("filtered events should be 5, got %d", len(m.events))
	}
}

func TestPushEventsOverflow(t *testing.T) {
	m := New(3, "15:04:05")
	m.SetSize(120, 40)

	events := testEvents(5)
	m.PushEvents(events)

	if m.buf.Len() != 3 {
		t.Fatalf("buffer should cap at 3, got %d", m.buf.Len())
	}
}

func TestOpenCloseDetail(t *testing.T) {
	m := New(100, "15:04:05")
	m.SetSize(120, 40)

	events := testEvents(3)
	m.PushEvents(events)

	// Open detail on the cursor row (auto-scroll puts cursor at the last row).
	m.openDetail()
	if !m.DetailOpen() {
		t.Fatal("detail should be open after openDetail()")
	}
	if m.detailEvt == nil {
		t.Fatal("detailEvt should be set")
	}
	// With auto-scroll, cursor is at the last (newest) event.
	lastEvt := events[len(events)-1]
	if m.detailEvt.ID != lastEvt.ID {
		t.Fatalf("detail event ID = %d, want %d (last event via auto-scroll)", m.detailEvt.ID, lastEvt.ID)
	}

	// Close detail.
	m.CloseDetail()
	if m.DetailOpen() {
		t.Fatal("detail should be closed after CloseDetail()")
	}
}

func TestFilterMeta(t *testing.T) {
	m := New(100, "15:04:05")
	m.SetMeta([]string{"host-01", "host-02"}, []string{"sshd", "nginx"})

	filter := m.Filter()
	if filter.Hostname() != "" {
		t.Fatalf("default hostname filter should be empty, got %q", filter.Hostname())
	}
}
