package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/model"
)

// Level string constants.
const (
	levelFatal = "FATAL"
	levelError = "ERROR"
)

// Column widths for applog view.
const (
	colTimeApplog   = 8
	colLevel        = 8
	colHost         = 16
	colService      = 14
	colComponent    = 12
	colApplogMinMsg = 10
	detailHeightApp = 9
)

// AppLogView displays applog events in a scrollable table.
type AppLogView struct {
	events        *EventList[model.AppLogEvent]
	cursor        int
	offset        int
	pinned        bool
	expanded      bool
	width         int
	height        int
	newSincePause int
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
		} else {
			v.newSincePause++
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

func (v AppLogView) visibleRows() int {
	rows := v.height - 1 // subtract column header row
	if v.expanded {
		rows -= detailHeightApp
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

	// Column headers.
	b.WriteString(v.renderColumnHeader())
	b.WriteByte('\n')

	visible := v.visibleRows()
	end := min(v.offset+visible, v.events.Len())

	for i := v.offset; i < end; i++ {
		evt := v.events.Get(i)
		row := v.renderRow(evt)

		upperLevel := strings.ToUpper(evt.Level)
		switch {
		case i == v.cursor:
			row = selectedRowStyle.Width(v.width).Render(row)
		case upperLevel == levelFatal:
			row = rowTintEmerg.Width(v.width).Render(row)
		case upperLevel == levelError:
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

func (v AppLogView) renderColumnHeader() string {
	ts := columnHeaderStyle.Width(colTimeApplog).Render("TIME")
	lvl := columnHeaderStyle.Width(colLevel).Render("LEVEL")
	host := columnHeaderStyle.Width(colHost).Render("HOST")
	svc := columnHeaderStyle.Width(colService).Render("SERVICE")
	comp := columnHeaderStyle.Width(colComponent).Render("COMPONENT")
	msg := columnHeaderStyle.Render("MESSAGE")

	header := fmt.Sprintf(" %s %s %s %s %s %s", ts, lvl, host, svc, comp, msg)
	return dimStyle.Width(v.width).Render(header)
}

func (v AppLogView) renderRow(evt model.AppLogEvent) string {
	ts := evt.Timestamp.Local().Format("15:04:05")
	lvl := LevelStyle(evt.Level).Width(colLevel).Render(strings.ToUpper(evt.Level))

	host := hostnameStyle.Render(truncate(evt.Host, colHost))
	svc := serviceStyle.Render(truncate(evt.Service, colService))
	comp := componentStyle.Render(truncate(evt.Component, colComponent))

	// Calculate remaining width for message + inline attrs.
	fixedWidth := 1 + colTimeApplog + 1 + colLevel + 1 + colHost + 1 + colService + 1 + colComponent + 1
	remaining := max(v.width-fixedWidth, colApplogMinMsg)

	// Render inline attrs if present.
	attrsStr := renderInlineAttrs(evt.Attrs, remaining/3) // reserve 1/3 of remaining for attrs
	if attrsStr != "" {
		msgWidth := max(remaining-lipgloss.Width(attrsStr)-1, colApplogMinMsg)
		msg := truncate(evt.Msg, msgWidth)
		return fmt.Sprintf(" %s %s %-16s %-14s %-12s %s %s",
			dimStyle.Render(ts), lvl, host, svc, comp, msg, attrsStr)
	}

	msg := truncate(evt.Msg, remaining)
	return fmt.Sprintf(" %s %s %-16s %-14s %-12s %s",
		dimStyle.Render(ts), lvl, host, svc, comp, msg)
}

// renderInlineAttrs renders attrs as "— key=val key=val" in orange/dim style.
func renderInlineAttrs(attrs json.RawMessage, maxWidth int) string {
	if len(attrs) == 0 || string(attrs) == "null" || string(attrs) == "{}" {
		return ""
	}

	var m map[string]any
	if json.Unmarshal(attrs, &m) != nil || len(m) == 0 {
		return ""
	}

	var parts []string
	totalWidth := 2 // "— " prefix
	for k, val := range m {
		var vs string
		switch v := val.(type) {
		case string:
			vs = v
		case float64:
			vs = fmt.Sprintf("%g", v)
		case bool:
			vs = fmt.Sprintf("%t", v)
		default:
			continue
		}
		part := k + "=" + vs
		if totalWidth+len(part)+1 > maxWidth {
			break
		}
		parts = append(parts, part)
		totalWidth += len(part) + 1
	}

	if len(parts) == 0 {
		return ""
	}

	return attrsDashStyle.Render("—") + " " + attrsKVStyle.Render(strings.Join(parts, " "))
}

func (v AppLogView) renderDetail(evt model.AppLogEvent) string {
	border := detailStyle.
		BorderForeground(LevelBorderColor(evt.Level)).
		Width(v.width - 4)

	var content strings.Builder

	// Row 1: Time, Level, Host.
	fmt.Fprintf(&content, "%s %s  %s %s  %s %s\n",
		detailLabelStyle.Render("Time:"), evt.Timestamp.Local().Format("2006-01-02 15:04:05"),
		detailLabelStyle.Render("Level:"), LevelStyle(evt.Level).Render(strings.ToUpper(evt.Level)),
		detailLabelStyle.Render("Host:"), hostnameStyle.Render(evt.Host),
	)

	// Row 2: Service, Component, Source.
	fmt.Fprintf(&content, "%s %s  %s %s",
		detailLabelStyle.Render("Service:"), serviceStyle.Render(evt.Service),
		detailLabelStyle.Render("Component:"), componentStyle.Render(evt.Component),
	)
	if evt.Source != "" {
		fmt.Fprintf(&content, "  %s %s", detailLabelStyle.Render("Source:"), detailValueStyle.Render(evt.Source))
	}

	// Row 3: Message.
	fmt.Fprintf(&content, "\n%s %s", detailLabelStyle.Render("Message:"), evt.Msg)

	// Attrs block.
	if len(evt.Attrs) > 0 && string(evt.Attrs) != "null" && string(evt.Attrs) != "{}" {
		var pretty json.RawMessage
		if json.Unmarshal(evt.Attrs, &pretty) == nil {
			formatted, err := json.MarshalIndent(pretty, "  ", "  ")
			if err == nil {
				fmt.Fprintf(&content, "\n%s\n  %s", detailLabelStyle.Render("Attrs:"), string(formatted))
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

// NewSincePause returns how many events arrived while scrolled away.
func (v AppLogView) NewSincePause() int {
	return v.newSincePause
}

// ScrollPercent returns a 0-100 scroll position.
func (v AppLogView) ScrollPercent() int {
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
func (v *AppLogView) Clear() {
	v.events.Clear()
	v.cursor = 0
	v.offset = 0
	v.pinned = true
	v.expanded = false
	v.newSincePause = 0
}
