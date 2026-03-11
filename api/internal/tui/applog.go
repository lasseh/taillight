package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/lasseh/taillight/internal/model"
)

// AppLogView displays applog events in a scrollable table.
type AppLogView struct {
	events   *EventList[model.AppLogEvent]
	cursor   int
	offset   int
	pinned   bool
	expanded bool
	width    int
	height   int
}

// NewAppLogView creates a new applog view.
func NewAppLogView() AppLogView {
	return AppLogView{
		events: NewEventList[model.AppLogEvent](maxEvents),
		pinned: true,
	}
}

// Update handles messages for the applog view.
func (v AppLogView) Update(msg tea.Msg) (AppLogView, tea.Cmd) {
	switch msg := msg.(type) {
	case AppLogEventMsg:
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

func (v AppLogView) handleKey(msg tea.KeyMsg) (AppLogView, tea.Cmd) {
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

func (v AppLogView) visibleRows() int {
	rows := v.height
	if v.expanded {
		rows -= 8
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (v *AppLogView) ensureVisible() {
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
func (v *AppLogView) SetSize(w, h int) {
	v.width = w
	v.height = h
}

// View renders the applog view.
func (v AppLogView) View() string {
	if v.events.Len() == 0 {
		return dimStyle.Render("  Waiting for applog events...")
	}

	var b strings.Builder
	visible := v.visibleRows()
	end := min(v.offset+visible, v.events.Len())

	for i := v.offset; i < end; i++ {
		evt := v.events.Get(i)
		row := v.renderRow(evt)
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

func (v AppLogView) renderRow(evt model.AppLogEvent) string {
	ts := evt.Timestamp.Local().Format("15:04:05")
	lvl := LevelStyle(evt.Level).Width(8).Render(strings.ToUpper(evt.Level))

	host := truncate(evt.Host, 16)
	service := truncate(evt.Service, 14)
	component := truncate(evt.Component, 12)

	msgWidth := max(v.width-8-1-8-1-16-1-14-1-12-1, 10)
	msg := truncate(evt.Msg, msgWidth)

	return fmt.Sprintf("%s %s %-16s %-14s %-12s %s",
		dimStyle.Render(ts), lvl, host, service, component, msg)
}

func (v AppLogView) renderDetail(evt model.AppLogEvent) string {
	border := detailStyle.
		BorderForeground(LevelBorderColor(evt.Level)).
		Width(v.width - 4)

	var content strings.Builder
	fmt.Fprintf(&content, "%s %s  %s %s  %s %s\n",
		dimStyle.Render("Time:"), evt.Timestamp.Local().Format("2006-01-02 15:04:05"),
		dimStyle.Render("Level:"), LevelStyle(evt.Level).Render(strings.ToUpper(evt.Level)),
		dimStyle.Render("Host:"), evt.Host,
	)
	fmt.Fprintf(&content, "%s %s  %s %s",
		dimStyle.Render("Service:"), evt.Service,
		dimStyle.Render("Component:"), evt.Component,
	)
	if evt.Source != "" {
		fmt.Fprintf(&content, "  %s %s", dimStyle.Render("Source:"), evt.Source)
	}
	fmt.Fprintf(&content, "\n%s %s", dimStyle.Render("Message:"), evt.Msg)

	if len(evt.Attrs) > 0 && string(evt.Attrs) != "null" && string(evt.Attrs) != "{}" {
		var pretty json.RawMessage
		if json.Unmarshal(evt.Attrs, &pretty) == nil {
			formatted, err := json.MarshalIndent(pretty, "  ", "  ")
			if err == nil {
				fmt.Fprintf(&content, "\n%s\n  %s", dimStyle.Render("Attrs:"), string(formatted))
			}
		}
	}

	return border.Render(content.String())
}

// EventCount returns the number of events.
func (v AppLogView) EventCount() int {
	return v.events.Len()
}

// IsPinned returns whether the view is auto-scrolling.
func (v AppLogView) IsPinned() bool {
	return v.pinned
}

// Clear removes all events.
func (v *AppLogView) Clear() {
	v.events.Clear()
	v.cursor = 0
	v.offset = 0
	v.pinned = true
	v.expanded = false
}
