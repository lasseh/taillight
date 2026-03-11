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

// Column widths for syslog view.
const (
	colTimeSyslog    = 8
	colSeverity      = 8
	colHostname      = 20
	colProgramname   = 14
	colSyslogMinMsg  = 10
	detailHeightSlog = 8
)

// SyslogView displays syslog events in a scrollable table.
type SyslogView struct {
	events        *EventList[model.SyslogEvent]
	cursor        int
	offset        int
	pinned        bool
	expanded      bool
	width         int
	height        int
	newSincePause int // events received while scrolled away
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
		} else {
			v.newSincePause++
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
			v.newSincePause = 0
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
			v.newSincePause = 0
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
		v.newSincePause = 0
		v.ensureVisible()
	case keyEnter:
		v.expanded = !v.expanded
		v.ensureVisible()
	}

	return v, nil
}

func (v SyslogView) visibleRows() int {
	rows := v.height - 1 // subtract column header row
	if v.expanded {
		rows -= detailHeightSlog
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

	// Column headers.
	b.WriteString(v.renderColumnHeader())
	b.WriteByte('\n')

	visible := v.visibleRows()
	end := min(v.offset+visible, v.events.Len())

	for i := v.offset; i < end; i++ {
		evt := v.events.Get(i)
		row := v.renderRow(evt, i)

		switch {
		case i == v.cursor:
			row = selectedRowStyle.Width(v.width).Render(row)
		case evt.Severity <= 1:
			row = rowTintEmerg.Width(v.width).Render(row)
		case evt.Severity <= 2:
			row = rowTintCrit.Width(v.width).Render(row)
		case i%2 == 0:
			row = zebraStyle.Width(v.width).Render(row)
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

func (v SyslogView) renderColumnHeader() string {
	ts := columnHeaderStyle.Width(colTimeSyslog).Render("TIME")
	sev := columnHeaderStyle.Width(colSeverity).Render("SEVERITY")
	host := columnHeaderStyle.Width(colHostname).Render("HOSTNAME")
	prog := columnHeaderStyle.Width(colProgramname).Render("PROGRAM")
	msg := columnHeaderStyle.Render("MESSAGE")

	header := fmt.Sprintf(" %s  %s  %s  %s  %s", ts, sev, host, prog, msg)
	return dimStyle.Width(v.width).Render(header)
}

func (v SyslogView) renderRow(evt model.SyslogEvent, _ int) string {
	ts := evt.ReceivedAt.Local().Format("15:04:05")
	sev := model.SeverityLabel(evt.Severity)
	sevStyled := SeverityStyle(evt.Severity).Width(colSeverity).Render(sev)

	host := hostnameStyle.Render(truncate(evt.Hostname, colHostname))
	prog := programStyle.Render(truncate(evt.Programname, colProgramname))

	msgWidth := max(v.width-colTimeSyslog-2-colSeverity-2-colHostname-2-colProgramname-2, colSyslogMinMsg)
	msg := highlightMessage(evt.Message, msgWidth)

	return fmt.Sprintf(" %s  %s  %-20s  %-14s  %s",
		dimStyle.Render(ts), sevStyled, host, prog, msg)
}

func (v SyslogView) renderDetail(evt model.SyslogEvent) string {
	border := detailStyle.
		BorderForeground(SeverityBorderColor(evt.Severity)).
		Width(v.width - 4)

	var content strings.Builder

	// Row 1: Time, Severity, Facility.
	fmt.Fprintf(&content, "%s %s  %s %s  %s %s\n",
		detailLabelStyle.Render("Time:"), evt.ReceivedAt.Local().Format("2006-01-02 15:04:05"),
		detailLabelStyle.Render("Severity:"), SeverityStyle(evt.Severity).Render(strings.ToUpper(model.SeverityLabel(evt.Severity))),
		detailLabelStyle.Render("Facility:"), detailValueStyle.Render(model.FacilityLabel(evt.Facility)),
	)

	// Row 2: Hostname, IP, Program, Tag.
	fmt.Fprintf(&content, "%s %s",
		detailLabelStyle.Render("Hostname:"), hostnameStyle.Render(evt.Hostname),
	)
	if evt.FromhostIP != "" {
		fmt.Fprintf(&content, "  %s %s",
			detailLabelStyle.Render("IP:"), detailValueStyle.Render(evt.FromhostIP),
		)
	}
	fmt.Fprintf(&content, "  %s %s",
		detailLabelStyle.Render("Program:"), programStyle.Render(evt.Programname),
	)
	if evt.SyslogTag != "" {
		fmt.Fprintf(&content, "  %s %s",
			detailLabelStyle.Render("Tag:"), detailValueStyle.Render(evt.SyslogTag),
		)
	}
	if evt.MsgID != "" {
		fmt.Fprintf(&content, "  %s %s",
			detailLabelStyle.Render("MsgID:"), detailValueStyle.Render(evt.MsgID),
		)
	}

	// Row 3: Message (highlighted).
	fmt.Fprintf(&content, "\n%s %s",
		detailLabelStyle.Render("Message:"), applyHighlights(evt.Message),
	)

	return border.Render(content.String())
}

// IsPinned returns whether the view is auto-scrolling.
func (v SyslogView) IsPinned() bool {
	return v.pinned
}

// NewSincePause returns how many events arrived while scrolled away.
func (v SyslogView) NewSincePause() int {
	return v.newSincePause
}

// ScrollPercent returns a 0-100 scroll position.
func (v SyslogView) ScrollPercent() int {
	total := v.events.Len()
	if total == 0 {
		return 100
	}
	visible := v.visibleRows()
	if total <= visible {
		return 100
	}
	maxOffset := total - visible
	if maxOffset <= 0 {
		return 100
	}
	return min(v.offset*100/maxOffset, 100)
}

// Clear removes all events.
func (v *SyslogView) Clear() {
	v.events.Clear()
	v.cursor = 0
	v.offset = 0
	v.pinned = true
	v.expanded = false
	v.newSincePause = 0
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
