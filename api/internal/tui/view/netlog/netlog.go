package netlog

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

const detailWidthPct = 40

// Model is the netlog stream view.
type Model struct {
	buf        *buffer.Ring[client.NetlogEvent]
	events     []client.NetlogEvent // filtered view
	table      table.Model
	filter     FilterModel
	detail     *viewport.Model
	detailEvt  *client.NetlogEvent
	timeFormat string
	width      int
	height     int
	autoScroll bool
}

// New creates a new netlog view model.
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
		buf:        buffer.New[client.NetlogEvent](bufferSize),
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
	tableHeight := height - 1

	if m.detail != nil {
		detailW := max(30, width*detailWidthPct/100)
		tableWidth = width - detailW - 1
		m.detail.SetWidth(detailW - 2)
		m.detail.SetHeight(max(3, tableHeight))
	}

	m.table.SetWidth(tableWidth)
	m.table.SetColumns(columns(tableWidth))
	m.table.SetHeight(max(3, tableHeight))
}

// Filter returns the current filter.
func (m *Model) Filter() *FilterModel {
	return &m.filter
}

// SetMeta updates the filter's metadata.
func (m *Model) SetMeta(hosts, programs []string) {
	m.filter.SetMeta(hosts, programs)
}

// PushEvents adds a batch of events.
func (m *Model) PushEvents(events []client.NetlogEvent) {
	for i := range events {
		m.buf.Push(events[i])
	}
	m.rebuildTable()
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

// View renders the netlog view with optional detail sidebar.
func (m *Model) View() string {
	filterBar := m.filter.View(m.width)
	tableView := m.table.View()

	// Empty-state message — see srvlog.View for rationale.
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
		borderStyle := lipgloss.NewStyle().
			Foreground(theme.ColorBorder).
			Height(m.height - 1)
		borderChars := make([]string, 0, m.height-1)
		for range m.height - 1 {
			borderChars = append(borderChars, "│")
		}
		border := borderStyle.Render(lipgloss.JoinVertical(lipgloss.Left, borderChars...))

		sidebarStyle := lipgloss.NewStyle().Padding(0, 1).
			Width(max(30, m.width*detailWidthPct/100) - 2)
		sidebar := sidebarStyle.Render(m.detail.View())

		mainContent := lipgloss.JoinHorizontal(lipgloss.Top, tableView, border, sidebar)
		return lipgloss.JoinVertical(lipgloss.Left, filterBar, mainContent)
	}

	return lipgloss.JoinVertical(lipgloss.Left, filterBar, tableView)
}

func (m *Model) rebuildTable() {
	all := m.buf.Slice()
	m.events = m.events[:0]
	var rows []table.Row

	for i := range all {
		if m.filter.Matches(all[i].Hostname, all[i].Programname, all[i].Severity) {
			m.events = append(m.events, all[i])
			rows = append(rows, eventToRow(all[i], m.timeFormat))
		}
	}

	m.table.SetRows(rows)
	if m.autoScroll && len(rows) > 0 {
		m.table.GotoBottom()
	}
}

func (m *Model) openDetail() {
	cursor := m.table.Cursor()
	if cursor < 0 || cursor >= len(m.events) {
		return
	}
	evt := m.events[cursor]
	m.detailEvt = &evt

	detailW := max(30, m.width*detailWidthPct/100) - 2
	content := renderDetailPanel(evt, detailW)
	vp := viewport.New(viewport.WithWidth(detailW), viewport.WithHeight(max(3, m.height-1)))
	vp.SetContent(content)
	m.detail = &vp
	m.SetSize(m.width, m.height)
}

var (
	enterKey = key.NewBinding(key.WithKeys("enter"))
	escKey   = key.NewBinding(key.WithKeys("esc"))
)
