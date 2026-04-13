package srvlog

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/buffer"
	"github.com/lasseh/taillight/internal/tui/client"
	"github.com/lasseh/taillight/internal/tui/theme"
)

// Detail sidebar width as a fraction of terminal width.
const detailWidthPct = 40

// srvlogEventAdapter wraps SrvlogEvent to satisfy the eventLike interface.
type srvlogEventAdapter struct {
	e client.SrvlogEvent
}

func (a srvlogEventAdapter) hostname() string    { return a.e.Hostname }
func (a srvlogEventAdapter) programname() string { return a.e.Programname }
func (a srvlogEventAdapter) severity() int       { return a.e.Severity }

// Model is the srvlog stream view.
type Model struct {
	buf        *buffer.Ring[client.SrvlogEvent]
	events     []client.SrvlogEvent // filtered view matching current filter
	table      table.Model
	filter     FilterModel
	detail     *viewport.Model     // nil when sidebar is closed
	detailEvt  *client.SrvlogEvent // event shown in sidebar
	timeFormat string
	width      int
	height     int
	autoScroll bool
}

// New creates a new srvlog view model.
func New(bufferSize int, timeFormat string) Model {
	cols := columns(80)
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(20),
	)
	s := table.DefaultStyles()
	s.Header = theme.TableHeader
	s.Selected = theme.TableSelected
	s.Cell = theme.TableCell
	t.SetStyles(s)

	return Model{
		buf:        buffer.New[client.SrvlogEvent](bufferSize),
		table:      t,
		filter:     newFilter(),
		timeFormat: timeFormat,
		autoScroll: true,
	}
}

// SetSize updates the view dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height

	tableWidth := width
	tableHeight := height - 1 // minus filter bar

	if m.detail != nil {
		// Sidebar takes detailWidthPct of width; table gets the rest.
		detailW := max(30, width*detailWidthPct/100)
		tableWidth = width - detailW - 1 // -1 for the │ border
		m.detail.SetWidth(detailW - 2)   // padding
		m.detail.SetHeight(max(3, tableHeight))
	}

	m.table.SetWidth(tableWidth)
	m.table.SetColumns(columns(tableWidth))
	m.table.SetHeight(max(3, tableHeight))
}

// Filter returns the current filter for SSE stream parameters.
func (m *Model) Filter() *FilterModel {
	return &m.filter
}

// SetMeta updates the filter's metadata (hosts, programs).
func (m *Model) SetMeta(hosts, programs []string) {
	m.filter.SetMeta(hosts, programs)
}

// PushEvents adds a batch of events to the buffer and updates the table.
func (m *Model) PushEvents(events []client.SrvlogEvent) {
	for i := range events {
		m.buf.Push(events[i])
	}
	m.rebuildTable()
}

// EventCount returns the number of filtered events visible in the table.
func (m *Model) EventCount() int {
	return len(m.events)
}

// FocusFilter activates the filter input.
func (m *Model) FocusFilter() {
	m.filter.Focus()
}

// BlurFilter deactivates the filter input.
func (m *Model) BlurFilter() {
	m.filter.Blur()
}

// DetailOpen reports whether the detail sidebar is showing.
func (m *Model) DetailOpen() bool {
	return m.detail != nil
}

// CloseDetail closes the detail sidebar.
func (m *Model) CloseDetail() {
	m.detail = nil
	m.detailEvt = nil
	m.SetSize(m.width, m.height)
}

// UpdateFilter handles input when the filter bar is focused.
func (m Model) UpdateFilter(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Dirty() {
		m.rebuildTable()
		m.filter.AckDirty()
	}
	return m, cmd
}

// UpdateTable handles input when the table is focused.
func (m Model) UpdateTable(msg tea.Msg) (Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(msg, enterKey):
			m.openDetail()
			return m, nil
		case key.Matches(msg, escKey):
			if m.detail != nil {
				m.CloseDetail()
				return m, nil
			}
		}

		// If detail sidebar is open, scrolling keys go to the detail viewport.
		if m.detail != nil {
			if msg.String() == "tab" {
				// Tab toggles focus between table and detail.
				d := *m.detail
				d, _ = d.Update(msg)
				m.detail = &d
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)

	// Update detail content when cursor moves.
	if m.detail != nil {
		cursor := m.table.Cursor()
		if cursor >= 0 && cursor < len(m.events) {
			evt := m.events[cursor]
			if m.detailEvt == nil || m.detailEvt.ID != evt.ID {
				m.detailEvt = &evt
				detailW := max(30, m.width*detailWidthPct/100) - 2
				m.detail.SetContent(renderDetailPanel(evt, detailW))
			}
		}
	}

	return m, cmd
}

// View renders the srvlog view with optional detail sidebar.
func (m *Model) View() string {
	filterBar := m.filter.View(m.width)

	tableView := m.table.View()

	// Show an empty-state message when there are no events to display so
	// users can tell the difference between "still loading" and "filter
	// excluded everything".
	if len(m.events) == 0 {
		var msg string
		if m.filter.HasActiveFilter() {
			msg = "No events match the current filter"
		} else {
			msg = "Waiting for events..."
		}
		empty := lipgloss.NewStyle().
			Foreground(theme.ColorComment).
			Width(m.width).
			Height(max(3, m.height-1)).
			Align(lipgloss.Center, lipgloss.Center).
			Render(msg)
		return lipgloss.JoinVertical(lipgloss.Left, filterBar, empty)
	}

	if m.detail != nil && m.detailEvt != nil {
		// Thin │ border between table and sidebar.
		borderStyle := lipgloss.NewStyle().
			Foreground(theme.ColorBorder).
			Height(m.height - 1)

		borderChars := make([]string, 0, m.height-1)
		for range m.height - 1 {
			borderChars = append(borderChars, "│")
		}
		border := borderStyle.Render(lipgloss.JoinVertical(lipgloss.Left, borderChars...))

		// Detail sidebar content.
		sidebarStyle := lipgloss.NewStyle().
			Padding(0, 1).
			Width(max(30, m.width*detailWidthPct/100) - 2)

		sidebar := sidebarStyle.Render(m.detail.View())

		// Combine table + border + sidebar horizontally.
		mainContent := lipgloss.JoinHorizontal(lipgloss.Top, tableView, border, sidebar)
		return lipgloss.JoinVertical(lipgloss.Left, filterBar, mainContent)
	}

	return lipgloss.JoinVertical(lipgloss.Left, filterBar, tableView)
}

// rebuildTable re-filters the buffer and updates table rows.
func (m *Model) rebuildTable() {
	all := m.buf.Slice() // oldest first
	m.events = m.events[:0]
	var rows []table.Row

	for i := range all {
		if m.filter.Matches(srvlogEventAdapter{all[i]}) {
			m.events = append(m.events, all[i])
			rows = append(rows, eventToRow(all[i], m.timeFormat))
		}
	}

	m.table.SetRows(rows)

	// Auto-scroll to bottom (newest event).
	if m.autoScroll && len(rows) > 0 {
		m.table.GotoBottom()
	}
}

// openDetail opens the detail sidebar for the currently selected table row.
func (m *Model) openDetail() {
	cursor := m.table.Cursor()
	if cursor < 0 || cursor >= len(m.events) {
		return
	}
	evt := m.events[cursor]
	m.detailEvt = &evt

	detailW := max(30, m.width*detailWidthPct/100) - 2
	content := renderDetailPanel(evt, detailW)

	vp := viewport.New(
		viewport.WithWidth(detailW),
		viewport.WithHeight(max(3, m.height-1)),
	)
	vp.SetContent(content)
	m.detail = &vp

	// Resize table to share width with sidebar.
	m.SetSize(m.width, m.height)
}

var (
	enterKey = key.NewBinding(key.WithKeys("enter"))
	escKey   = key.NewBinding(key.WithKeys("esc"))
)
