// Package logview provides a generic streaming log table view for srvlog,
// applog, and netlog. The view handles the bubbletea lifecycle (table,
// filter bar, detail sidebar, ring buffer, auto-scroll) generically and
// delegates type-specific rendering to per-feed Adapter callbacks.
//
// Usage:
//
//	var srvlogAdapter = logview.Adapter[client.SrvlogEvent]{
//	    Columns: srvlogColumns,
//	    Row:     srvlogRow,
//	    Detail:  srvlogDetail,
//	    ID:      func(e client.SrvlogEvent) int64 { return e.ID },
//	}
//
//	model := logview.New(bufferSize, timeFormat, srvlogAdapter, srvlogFilter)
package logview

import (
	"net/url"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/buffer"
	"github.com/lasseh/taillight/internal/tui/theme"
)

// detailWidthPct is the detail sidebar width as a fraction of terminal width.
const detailWidthPct = 40

// Adapter parameterizes Model[T] for a specific event type. Each per-feed
// package supplies its own adapter with the type-specific rendering and
// identity functions.
type Adapter[T any] struct {
	// Columns returns the table column definitions for the given width.
	Columns func(width int) []table.Column

	// Row converts an event to a table row (one cell per column).
	Row func(e T, timeFormat string) table.Row

	// Detail renders the expanded detail panel for an event at the given width.
	Detail func(e T, width int) string

	// ID returns a stable identifier for an event used to detect cursor
	// movement (so the detail sidebar updates when the user navigates).
	ID func(e T) int64
}

// Filter is the contract a per-feed filter must satisfy. Each feed's filter
// owns its own metadata fields (hostnames, programs, services, etc.) but
// exposes the same lifecycle to the generic Model.
type Filter interface {
	// Update processes a key/text-input message. Returns the updated
	// filter and any command from the underlying input.
	Update(msg tea.Msg) (Filter, tea.Cmd)

	// Focus activates the filter input (returns a focus command).
	Focus() tea.Cmd

	// Blur deactivates the filter input.
	Blur()

	// Dirty reports whether the filter changed since the last AckDirty.
	Dirty() bool

	// AckDirty clears the dirty flag (called after the model rebuilds).
	AckDirty()

	// HasActiveFilter reports whether any filter narrows the result set
	// (used to choose between "Waiting for events" and "No events match").
	HasActiveFilter() bool

	// View renders the filter bar at the given width.
	View(width int) string

	// Params returns the filter as URL query parameters for the SSE stream.
	Params() url.Values
}

// Model is a generic streaming log view: filter bar + table + optional
// detail sidebar, fed by a ring buffer.
type Model[T any] struct {
	buf        *buffer.Ring[T]
	events     []T // filtered view of the buffer
	table      table.Model
	filter     Filter
	detail     *viewport.Model
	detailEvt  *T
	timeFormat string
	width      int
	height     int
	autoScroll bool
	adapter    Adapter[T]
}

// New creates a new generic log view model.
func New[T any](bufferSize int, timeFormat string, adapter Adapter[T], filter Filter) Model[T] {
	t := table.New(
		table.WithColumns(adapter.Columns(80)),
		table.WithFocused(true),
		table.WithHeight(20),
	)
	s := table.DefaultStyles()
	s.Header = theme.TableHeader
	s.Selected = theme.TableSelected
	s.Cell = theme.TableCell
	t.SetStyles(s)

	return Model[T]{
		buf:        buffer.New[T](bufferSize),
		table:      t,
		filter:     filter,
		timeFormat: timeFormat,
		autoScroll: true,
		adapter:    adapter,
	}
}

// SetSize updates the view dimensions.
func (m *Model[T]) SetSize(width, height int) {
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
	m.table.SetColumns(m.adapter.Columns(tableWidth))
	m.table.SetHeight(max(3, tableHeight))
}

// Filter returns the filter for SSE stream parameter inspection.
func (m *Model[T]) Filter() Filter {
	return m.filter
}

// PushEvents adds a batch of events to the buffer and updates the table.
func (m *Model[T]) PushEvents(events []T) {
	for i := range events {
		m.buf.Push(events[i])
	}
	m.rebuildTable()
}

// EventCount returns the number of filtered events visible in the table.
func (m *Model[T]) EventCount() int {
	return len(m.events)
}

// FocusFilter activates the filter input. Returns the textinput's focus
// command (cursor blink) which the caller MUST forward to bubbletea via
// the Update return value — otherwise the cursor won't blink and the
// input may not respond properly.
func (m *Model[T]) FocusFilter() tea.Cmd {
	return m.filter.Focus()
}

// BlurFilter deactivates the filter input.
func (m *Model[T]) BlurFilter() {
	m.filter.Blur()
}

// DetailOpen reports whether the detail sidebar is showing.
func (m *Model[T]) DetailOpen() bool {
	return m.detail != nil
}

// CloseDetail closes the detail sidebar.
func (m *Model[T]) CloseDetail() {
	m.detail = nil
	m.detailEvt = nil
	m.SetSize(m.width, m.height)
}

// UpdateFilter handles input when the filter bar is focused.
func (m Model[T]) UpdateFilter(msg tea.Msg) (Model[T], tea.Cmd) {
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Dirty() {
		m.rebuildTable()
		m.filter.AckDirty()
	}
	return m, cmd
}

// UpdateTable handles input when the table is focused.
func (m Model[T]) UpdateTable(msg tea.Msg) (Model[T], tea.Cmd) {
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
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)

	// Update detail content when cursor moves.
	if m.detail != nil {
		cursor := m.table.Cursor()
		if cursor >= 0 && cursor < len(m.events) {
			evt := m.events[cursor]
			if m.detailEvt == nil || m.adapter.ID(*m.detailEvt) != m.adapter.ID(evt) {
				m.detailEvt = &evt
				detailW := max(30, m.width*detailWidthPct/100) - 2
				m.detail.SetContent(m.adapter.Detail(evt, detailW))
			}
		}
	}

	return m, cmd
}

// View renders the log view with optional detail sidebar.
func (m *Model[T]) View() string {
	filterBar := m.filter.View(m.width)
	tableView := m.table.View()

	// Empty state — distinguish "no data yet" from "filter excluded everything".
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
		borderChars := make([]string, 0, m.height-1)
		for range m.height - 1 {
			borderChars = append(borderChars, "│")
		}
		border := lipgloss.NewStyle().
			Foreground(theme.ColorBorder).
			Render(lipgloss.JoinVertical(lipgloss.Left, borderChars...))

		sidebarStyle := lipgloss.NewStyle().
			Padding(0, 1).
			Width(max(30, m.width*detailWidthPct/100) - 2)
		sidebar := sidebarStyle.Render(m.detail.View())

		mainContent := lipgloss.JoinHorizontal(lipgloss.Top, tableView, border, sidebar)
		return lipgloss.JoinVertical(lipgloss.Left, filterBar, mainContent)
	}

	return lipgloss.JoinVertical(lipgloss.Left, filterBar, tableView)
}

// rebuildTable re-filters the buffer and updates table rows. Filtering is
// done server-side via SSE query params, so this just copies the buffer
// contents into the table without further filtering.
func (m *Model[T]) rebuildTable() {
	all := m.buf.Slice() // oldest first
	m.events = all       // server filters; client just displays

	rows := make([]table.Row, len(all))
	for i := range all {
		rows[i] = m.adapter.Row(all[i], m.timeFormat)
	}
	m.table.SetRows(rows)

	if m.autoScroll && len(rows) > 0 {
		m.table.GotoBottom()
	}
}

// openDetail opens the detail sidebar for the currently selected table row.
func (m *Model[T]) openDetail() {
	cursor := m.table.Cursor()
	if cursor < 0 || cursor >= len(m.events) {
		return
	}
	evt := m.events[cursor]
	m.detailEvt = &evt

	detailW := max(30, m.width*detailWidthPct/100) - 2
	content := m.adapter.Detail(evt, detailW)

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
