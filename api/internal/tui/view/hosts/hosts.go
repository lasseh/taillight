// Package hosts implements the hosts inventory view.
package hosts

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/client"
	"github.com/lasseh/taillight/internal/tui/theme"
)

const refreshInterval = 30 * time.Second

// HostsLoadedMsg carries loaded host data.
type HostsLoadedMsg struct {
	Hosts []client.HostEntry
	Err   error
}

// Model is the hosts inventory view.
type Model struct {
	client   *client.Client
	hosts    []client.HostEntry
	cursor   int
	loading  bool
	lastLoad time.Time
	width    int
	height   int
}

// New creates a new hosts model.
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

// Init returns the initial load command.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadHosts(), m.refreshTick())
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case HostsLoadedMsg:
		m.loading = false
		m.lastLoad = time.Now()
		if msg.Err == nil {
			m.hosts = msg.Hosts
		}
		return m, nil

	case RefreshTickMsg:
		return m, tea.Batch(m.loadHosts(), m.refreshTick())

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, refreshKey):
			m.loading = true
			cmd := m.loadHosts()
			return m, cmd
		case key.Matches(msg, downKey):
			if m.cursor < len(m.hosts)-1 {
				m.cursor++
			}
		case key.Matches(msg, upKey):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, topKey):
			m.cursor = 0
		case key.Matches(msg, bottomKey):
			if len(m.hosts) > 0 {
				m.cursor = len(m.hosts) - 1
			}
		}
	}
	return m, nil
}

// View renders the hosts list.
func (m *Model) View() string {
	if m.loading && len(m.hosts) == 0 {
		return theme.Base.Width(m.width).Height(m.height).
			Align(lipgloss.Center, lipgloss.Center).
			Render("Loading hosts...")
	}

	var lines []string

	// Header.
	header := fmt.Sprintf("  %-20s  %-6s  %8s  %8s  %6s  %s",
		"HOSTNAME", "FEED", "TOTAL", "ERRORS", "ERR%", "LAST SEEN")
	lines = append(lines, theme.TableHeader.Width(m.width).Render(header))

	// Rows.
	visibleRows := max(1, m.height-3) // header + footer
	startIdx := 0
	if m.cursor >= visibleRows {
		startIdx = m.cursor - visibleRows + 1
	}

	for i := startIdx; i < len(m.hosts) && i < startIdx+visibleRows; i++ {
		h := m.hosts[i]

		// Status dot.
		dot := statusDot(h.LastSeenAt)

		// Trend arrow.
		var trendStr string
		switch {
		case h.Trend > 0:
			trendStr = lipgloss.NewStyle().Foreground(theme.ColorYellow).Render("▲")
		case h.Trend < 0:
			trendStr = lipgloss.NewStyle().Foreground(theme.ColorGreen).Render("▼")
		default:
			trendStr = theme.Comment.Render("─")
		}

		// Last seen.
		lastSeen := "never"
		if h.LastSeenAt != nil {
			lastSeen = relativeTime(*h.LastSeenAt)
		}

		row := fmt.Sprintf("%s %-20s  %-6s  %8s  %8s  %5.1f%%  %s  %s",
			dot,
			truncate(h.Hostname, 20),
			h.Feed,
			formatCount(h.TotalCount),
			formatCount(h.ErrorCount),
			h.ErrorRatio*100,
			trendStr,
			lastSeen,
		)

		style := theme.TableCell
		if i == m.cursor {
			style = theme.TableSelected
		}
		lines = append(lines, style.Width(m.width).Render(row))
	}

	// Footer.
	footer := theme.Comment.Render(fmt.Sprintf("  %d hosts  |  r to refresh", len(m.hosts)))
	lines = append(lines, footer)

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func statusDot(lastSeen *time.Time) string {
	if lastSeen == nil {
		return theme.Comment.Render("●")
	}
	age := time.Since(*lastSeen)
	switch {
	case age < 5*time.Minute:
		return lipgloss.NewStyle().Foreground(theme.ColorGreen).Render("●")
	case age < 1*time.Hour:
		return lipgloss.NewStyle().Foreground(theme.ColorYellow).Render("●")
	default:
		return lipgloss.NewStyle().Foreground(theme.ColorRed).Render("●")
	}
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func (m *Model) loadHosts() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		hosts, err := c.Hosts(ctx, "24h")
		if err != nil {
			return HostsLoadedMsg{Err: err}
		}
		return HostsLoadedMsg{Hosts: hosts}
	}
}

// RefreshTickMsg triggers a hosts refresh.
type RefreshTickMsg struct{}

func (m *Model) refreshTick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg {
		return RefreshTickMsg{}
	})
}

var (
	refreshKey = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh"))
	downKey    = key.NewBinding(key.WithKeys("j", "down"))
	upKey      = key.NewBinding(key.WithKeys("k", "up"))
	topKey     = key.NewBinding(key.WithKeys("g"))
	bottomKey  = key.NewBinding(key.WithKeys("G"))
)

func formatCount(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
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
