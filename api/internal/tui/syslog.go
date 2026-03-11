package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/model"
)

const (
	maxEvents = 2000
	keyEnter  = "enter"
)

// SyslogView displays syslog events in a scrollable table.
type SyslogView struct {
	events   *EventList[model.SyslogEvent]
	cursor   int  // selected row index
	offset   int  // first visible row
	pinned   bool // auto-scroll to bottom
	expanded bool // detail panel visible
	width    int
	height   int
}

// NewSyslogView creates a new syslog view.
func NewSyslogView() SyslogView {
	return SyslogView{
		events: NewEventList[model.SyslogEvent](maxEvents),
		pinned: true,
	}
}

// Update handles messages for the syslog view.
func (v SyslogView) Update(msg tea.Msg) (SyslogView, tea.Cmd) {
	switch msg := msg.(type) {
	case SyslogEventMsg:
		v.events.Push(msg.Event)
		if v.pinned {
			v.cursor = v.events.Len() - 1
			v.ensureVisible()
		}
	case tea.KeyMsg:
		return v.handleKey(msg)
	}
	return v, nil
}

func (v SyslogView) handleKey(msg tea.KeyMsg) (SyslogView, tea.Cmd) {
	visibleRows := v.visibleRows()

	switch msg.String() {
	case "up", "k":
		if v.cursor > 0 {
			v.cursor--
			v.pinned = false
		}
		v.ensureVisible()
	case "down", "j":
		if v.cursor < v.events.Len()-1 {
			v.cursor++
		}
		if v.cursor >= v.events.Len()-1 {
			v.pinned = true
		}
		v.ensureVisible()
	case "pgup":
		v.cursor -= visibleRows
		if v.cursor < 0 {
			v.cursor = 0
		}
		v.pinned = false
		v.ensureVisible()
	case "pgdown":
		v.cursor += visibleRows
		if v.cursor >= v.events.Len() {
			v.cursor = v.events.Len() - 1
		}
		if v.cursor >= v.events.Len()-1 {
			v.pinned = true
		}
		v.ensureVisible()
	case "home", "g":
		v.cursor = 0
		v.pinned = false
		v.ensureVisible()
	case "end", "G":
		if v.events.Len() > 0 {
			v.cursor = v.events.Len() - 1
		}
		v.pinned = true
		v.ensureVisible()
	case keyEnter:
		v.expanded = !v.expanded
		v.ensureVisible()
	}

	return v, nil
}

func (v SyslogView) visibleRows() int {
	rows := v.height
	if v.expanded {
		rows -= 7 // detail panel height
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (v *SyslogView) ensureVisible() {
	visible := v.visibleRows()
	if v.cursor < v.offset {
		v.offset = v.cursor
	}
	if v.cursor >= v.offset+visible {
		v.offset = v.cursor - visible + 1
	}
	if v.offset < 0 {
		v.offset = 0
	}
}

// SetSize updates the view dimensions.
func (v *SyslogView) SetSize(w, h int) {
	v.width = w
	v.height = h
}

// View renders the syslog view.
func (v SyslogView) View() string {
	if v.events.Len() == 0 {
		return dimStyle.Render("  Waiting for syslog events...")
	}

	var b strings.Builder
	visible := v.visibleRows()
	end := min(v.offset+visible, v.events.Len())

	for i := v.offset; i < end; i++ {
		evt := v.events.Get(i)
		row := v.renderRow(evt, i)
		if i == v.cursor {
			row = selectedRowStyle.Width(v.width).Render(row)
		}
		b.WriteString(row)
		if i < end-1 || v.expanded {
			b.WriteByte('\n')
		}
	}

	if v.expanded && v.cursor >= 0 && v.cursor < v.events.Len() {
		b.WriteString(v.renderDetail(v.events.Get(v.cursor)))
	}

	return b.String()
}

func (v SyslogView) renderRow(evt model.SyslogEvent, _ int) string {
	ts := evt.ReceivedAt.Local().Format("15:04:05")
	sev := model.SeverityLabel(evt.Severity)
	sevStyled := SeverityStyle(evt.Severity).Width(8).Render(sev)

	hostname := truncate(evt.Hostname, 20)
	program := truncate(evt.Programname, 14)

	msgWidth := max(v.width-8-1-8-1-20-1-14-1, 10)
	msg := truncate(evt.Message, msgWidth)

	return fmt.Sprintf("%s %s %-20s %-14s %s",
		dimStyle.Render(ts), sevStyled, hostname, program, msg)
}

func (v SyslogView) renderDetail(evt model.SyslogEvent) string {
	border := detailStyle.
		BorderForeground(SeverityBorderColor(evt.Severity)).
		Width(v.width - 4)

	content := fmt.Sprintf(
		"%s %s  %s %s  %s %s  %s %s\n%s %s  %s %s\n%s %s",
		dimStyle.Render("Time:"), evt.ReceivedAt.Local().Format("2006-01-02 15:04:05"),
		dimStyle.Render("Hostname:"), evt.Hostname,
		dimStyle.Render("Severity:"), SeverityStyle(evt.Severity).Render(model.SeverityLabel(evt.Severity)),
		dimStyle.Render("Facility:"), model.FacilityLabel(evt.Facility),
		dimStyle.Render("Program:"), evt.Programname,
		dimStyle.Render("Tag:"), evt.SyslogTag,
		dimStyle.Render("Message:"), evt.Message,
	)

	return border.Render(content)
}

// EventCount returns the number of events.
func (v SyslogView) EventCount() int {
	return v.events.Len()
}

// IsPinned returns whether the view is auto-scrolling.
func (v SyslogView) IsPinned() bool {
	return v.pinned
}

// Clear removes all events.
func (v *SyslogView) Clear() {
	v.events.Clear()
	v.cursor = 0
	v.offset = 0
	v.pinned = true
	v.expanded = false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + lipgloss.NewStyle().Foreground(colorDim).Render("…")
}
