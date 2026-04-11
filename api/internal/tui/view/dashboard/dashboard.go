// Package dashboard implements the dashboard view with summary stats.
package dashboard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/client"
	"github.com/lasseh/taillight/internal/tui/theme"
)

// refreshInterval is how often the dashboard auto-refreshes stats.
const refreshInterval = 30 * time.Second

// StatsLoadedMsg carries loaded dashboard statistics.
type StatsLoadedMsg struct {
	Srvlog *client.StatsSummary
	Applog *client.StatsSummary
	Netlog *client.StatsSummary
	Err    error
}

// Model is the dashboard view.
type Model struct {
	client   *client.Client
	srvlog   *client.StatsSummary
	applog   *client.StatsSummary
	netlog   *client.StatsSummary
	loading  bool
	lastLoad time.Time
	width    int
	height   int
}

// New creates a new dashboard model.
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
	return tea.Batch(m.loadStats(), m.refreshTick())
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case StatsLoadedMsg:
		m.loading = false
		m.lastLoad = time.Now()
		if msg.Err != nil {
			return m, nil
		}
		m.srvlog = msg.Srvlog
		m.applog = msg.Applog
		m.netlog = msg.Netlog
		return m, nil

	case RefreshTickMsg:
		return m, tea.Batch(m.loadStats(), m.refreshTick())

	case tea.KeyPressMsg:
		if key.Matches(msg, refreshKey) {
			m.loading = true
			cmd := m.loadStats()
			return m, cmd
		}
	}
	return m, nil
}

// View renders the dashboard.
func (m *Model) View() string {
	if m.loading && m.srvlog == nil {
		return theme.Base.Width(m.width).Height(m.height).
			Align(lipgloss.Center, lipgloss.Center).
			Render("Loading dashboard...")
	}

	var sections []string

	// Summary cards row.
	cards := m.renderCards()
	sections = append(sections, cards)

	// Severity distribution + top hosts.
	if m.srvlog != nil {
		bottom := lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderSeverityBars(m.srvlog, "Srvlog Severity"),
			"  ",
			m.renderTopHosts(m.srvlog),
		)
		sections = append(sections, bottom)
	}

	// Last updated.
	if !m.lastLoad.IsZero() {
		ts := theme.Comment.Render(fmt.Sprintf("Last updated: %s  (r to refresh)", m.lastLoad.Format("15:04:05")))
		sections = append(sections, ts)
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m *Model) renderCards() string {
	halfWidth := max(20, (m.width-4)/3)

	renderCard := func(title string, stats *client.StatsSummary) string {
		if stats == nil {
			return theme.Border.Width(halfWidth).Render(
				theme.Comment.Render(title + "\n  No data"),
			)
		}
		trendStr := fmt.Sprintf("%.1f%%", stats.Trend)
		switch {
		case stats.Trend > 0:
			trendStr = theme.SeverityStyle(4).Render("+" + trendStr) // yellow for up
		case stats.Trend < 0:
			trendStr = lipgloss.NewStyle().Foreground(theme.ColorGreen).Render(trendStr)
		default:
			trendStr = theme.Comment.Render(trendStr)
		}

		content := strings.Join([]string{
			theme.ActiveTab.Render(title),
			fmt.Sprintf("Total:    %s", theme.DetailValue.Render(formatCount(stats.Total))),
			fmt.Sprintf("Errors:   %s", lipgloss.NewStyle().Foreground(theme.ColorOrange).Render(formatCount(stats.Errors))),
			fmt.Sprintf("Warnings: %s", lipgloss.NewStyle().Foreground(theme.ColorYellow).Render(formatCount(stats.Warnings))),
			fmt.Sprintf("Trend:    %s", trendStr),
		}, "\n")

		return theme.Border.Width(halfWidth).Render(content)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top,
		renderCard("SRVLOG", m.srvlog),
		" ",
		renderCard("APPLOG", m.applog),
		" ",
		renderCard("NETLOG", m.netlog),
	)
}

func (m *Model) renderSeverityBars(stats *client.StatsSummary, title string) string {
	barWidth := max(20, m.width/3)
	var lines []string
	lines = append(lines, theme.TableHeader.Render(title))
	lines = append(lines, "")

	for _, sc := range stats.SeverityBreakdown {
		if sc.Count == 0 {
			continue
		}
		label := padRight(sc.Label, 8)
		barLen := int(sc.Pct / 100 * float64(barWidth-20))
		barLen = max(1, barLen)
		bar := strings.Repeat("█", barLen)
		count := formatCount(sc.Count)

		sevStyle := theme.SeverityStyle(sc.Severity)
		lines = append(lines, fmt.Sprintf("%s %s %s",
			sevStyle.Render(label),
			sevStyle.Render(bar),
			theme.Comment.Render(count),
		))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *Model) renderTopHosts(stats *client.StatsSummary) string {
	var lines []string
	lines = append(lines, theme.TableHeader.Render("Top Hosts"))
	lines = append(lines, "")

	for i, h := range stats.TopHosts {
		if i >= 10 {
			break
		}
		lines = append(lines, fmt.Sprintf("%s  %s  %s",
			theme.Hostname.Render(padRight(h.Name, 20)),
			theme.DetailValue.Render(padRight(formatCount(h.Count), 8)),
			theme.Comment.Render(fmt.Sprintf("%.1f%%", h.Pct)),
		))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// loadStats fetches summary data for all feeds.
func (m *Model) loadStats() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		srvlog, err := c.SrvlogSummary(ctx, "24h")
		if err != nil {
			return StatsLoadedMsg{Err: err}
		}
		applog, err := c.AppLogSummary(ctx, "24h")
		if err != nil {
			return StatsLoadedMsg{Err: err}
		}
		netlog, err := c.NetlogSummary(ctx, "24h")
		if err != nil {
			return StatsLoadedMsg{Err: err}
		}
		return StatsLoadedMsg{Srvlog: srvlog, Applog: applog, Netlog: netlog}
	}
}

// RefreshTickMsg triggers a dashboard refresh.
type RefreshTickMsg struct{}

func (m *Model) refreshTick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg {
		return RefreshTickMsg{}
	})
}

var refreshKey = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh"))

func formatCount(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}
