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

// Model is the netlog stream view.
type Model struct {
	buf        *buffer.Ring[client.NetlogEvent]
	events     []client.NetlogEvent // filtered view
	table      table.Model
	filter     FilterModel
	detail     *viewport.Model     // nil when closed
	detailEvt  *client.NetlogEvent // event shown in detail
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
	m.table.SetWidth(width)
	m.table.SetColumns(columns(width))

	tableHeight := height - 1
	if m.detail != nil {
		tableHeight = height * 60 / 100
	}
	m.table.SetHeight(max(3, tableHeight))

	if m.detail != nil {
		detailHeight := height - tableHeight - 1
		m.detail.SetWidth(width)
		m.detail.SetHeight(max(3, detailHeight))
	}
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

// DetailOpen reports whether the detail panel is showing.
func (m *Model) DetailOpen() bool {
	return m.detail != nil
}

// CloseDetail closes the detail panel.
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

		if m.detail != nil {
			d := *m.detail
			d, _ = d.Update(msg)
			m.detail = &d
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// View renders the netlog view.
func (m *Model) View() string {
	var sections []string

	sections = append(sections, m.filter.View(m.width))
	sections = append(sections, m.table.View())

	if m.detail != nil && m.detailEvt != nil {
		border := theme.Border.Width(m.width - 2)
		sections = append(sections, border.Render(m.detail.View()))
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
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

	content := renderDetailPanel(evt, m.width)
	vp := viewport.New(viewport.WithWidth(m.width-4), viewport.WithHeight(max(3, m.height*40/100)))
	vp.SetContent(content)
	m.detail = &vp
	m.SetSize(m.width, m.height)
}

var (
	enterKey = key.NewBinding(key.WithKeys("enter"))
	escKey   = key.NewBinding(key.WithKeys("escape"))
)
