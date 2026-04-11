// Package notification implements a read-only view of notification rules and channels.
package notification

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/client"
	"github.com/lasseh/taillight/internal/tui/theme"
)

// DataLoadedMsg carries notification data from the API.
type DataLoadedMsg struct {
	Channels []client.Channel
	Rules    []client.Rule
	Err      error
}

// Model is the notifications view.
type Model struct {
	client   *client.Client
	channels []client.Channel
	rules    []client.Rule
	tab      int // 0=rules, 1=channels
	cursor   int
	loading  bool
	width    int
	height   int
}

// New creates a new notification model.
func New(c *client.Client) Model {
	return Model{
		client:  c,
		loading: true,
	}
}

// SetSize updates dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Init loads notification data.
func (m *Model) Init() tea.Cmd {
	return m.loadData()
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case DataLoadedMsg:
		m.loading = false
		if msg.Err == nil {
			m.channels = msg.Channels
			m.rules = msg.Rules
		}
		return m, nil

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, tabKey):
			m.tab = (m.tab + 1) % 2
			m.cursor = 0
		case key.Matches(msg, downKey):
			m.cursor++
			m.clampCursor()
		case key.Matches(msg, upKey):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, refreshKey):
			m.loading = true
			cmd := m.loadData()
			return m, cmd
		}
	}
	return m, nil
}

// View renders the notification view.
func (m *Model) View() string {
	if m.loading && len(m.rules) == 0 {
		return theme.Base.Width(m.width).Height(m.height).
			Align(lipgloss.Center, lipgloss.Center).
			Render("Loading notifications...")
	}

	var sections []string

	// Sub-tabs.
	rulesTab := theme.InactiveTab.Render("RULES")
	channelsTab := theme.InactiveTab.Render("CHANNELS")
	if m.tab == 0 {
		rulesTab = theme.ActiveTab.Render("RULES")
	} else {
		channelsTab = theme.ActiveTab.Render("CHANNELS")
	}
	sections = append(sections, lipgloss.JoinHorizontal(lipgloss.Bottom, rulesTab, " ", channelsTab))
	sections = append(sections, "")

	if m.tab == 0 {
		sections = append(sections, m.renderRules()...)
	} else {
		sections = append(sections, m.renderChannels()...)
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m *Model) renderRules() []string {
	if len(m.rules) == 0 {
		return []string{theme.Comment.Render("  No notification rules configured")}
	}

	var lines []string
	header := fmt.Sprintf("  %-4s  %-20s  %-8s  %-8s  %-20s  %s",
		"ID", "NAME", "KIND", "ENABLED", "FILTER", "CHANNELS")
	lines = append(lines, theme.TableHeader.Width(m.width).Render(header))

	for i, r := range m.rules {
		enabled := lipgloss.NewStyle().Foreground(theme.ColorGreen).Render("yes")
		if !r.Enabled {
			enabled = lipgloss.NewStyle().Foreground(theme.ColorRed).Render("no ")
		}

		filter := buildFilterSummary(r)
		chIDs := fmt.Sprintf("%v", r.ChannelIDs)

		row := fmt.Sprintf("  %-4d  %-20s  %-8s  %s       %-20s  %s",
			r.ID,
			truncate(r.Name, 20),
			r.EventKind,
			enabled,
			truncate(filter, 20),
			chIDs,
		)

		style := theme.TableCell
		if i == m.cursor {
			style = theme.TableSelected
		}
		lines = append(lines, style.Width(m.width).Render(row))
	}
	return lines
}

func (m *Model) renderChannels() []string {
	if len(m.channels) == 0 {
		return []string{theme.Comment.Render("  No notification channels configured")}
	}

	var lines []string
	header := fmt.Sprintf("  %-4s  %-20s  %-10s  %-8s  %s",
		"ID", "NAME", "TYPE", "ENABLED", "CREATED")
	lines = append(lines, theme.TableHeader.Width(m.width).Render(header))

	for i, ch := range m.channels {
		enabled := lipgloss.NewStyle().Foreground(theme.ColorGreen).Render("yes")
		if !ch.Enabled {
			enabled = lipgloss.NewStyle().Foreground(theme.ColorRed).Render("no ")
		}

		row := fmt.Sprintf("  %-4d  %-20s  %-10s  %s       %s",
			ch.ID,
			truncate(ch.Name, 20),
			ch.Type,
			enabled,
			ch.CreatedAt.Format("2006-01-02"),
		)

		style := theme.TableCell
		if i == m.cursor {
			style = theme.TableSelected
		}
		lines = append(lines, style.Width(m.width).Render(row))
	}
	return lines
}

func (m *Model) clampCursor() {
	var maxIdx int
	if m.tab == 0 {
		maxIdx = max(0, len(m.rules)-1)
	} else {
		maxIdx = max(0, len(m.channels)-1)
	}
	if m.cursor > maxIdx {
		m.cursor = maxIdx
	}
}

func (m *Model) loadData() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		channels, err := c.ListNotificationChannels(ctx)
		if err != nil {
			return DataLoadedMsg{Err: err}
		}
		rules, err := c.ListNotificationRules(ctx)
		if err != nil {
			return DataLoadedMsg{Err: err}
		}
		return DataLoadedMsg{Channels: channels, Rules: rules}
	}
}

func buildFilterSummary(r client.Rule) string {
	var parts []string
	if r.Hostname != "" {
		parts = append(parts, "host:"+r.Hostname)
	}
	if r.Programname != "" {
		parts = append(parts, "prog:"+r.Programname)
	}
	if r.Service != "" {
		parts = append(parts, "svc:"+r.Service)
	}
	if r.SeverityMax != nil {
		parts = append(parts, fmt.Sprintf("sev<=%d", *r.SeverityMax))
	}
	if r.Search != "" {
		parts = append(parts, "search:"+r.Search)
	}
	if len(parts) == 0 {
		return "all"
	}
	return strings.Join(parts, " ")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

var (
	tabKey     = key.NewBinding(key.WithKeys("tab"))
	downKey    = key.NewBinding(key.WithKeys("j", "down"))
	upKey      = key.NewBinding(key.WithKeys("k", "up"))
	refreshKey = key.NewBinding(key.WithKeys("r"))
)
