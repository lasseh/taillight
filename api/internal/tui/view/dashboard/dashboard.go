// Package dashboard implements the dashboard view with summary stats.
package dashboard

import (
	"context"
	"fmt"
	"image/color"
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
	sections = append(sections, m.renderCards())
	sections = append(sections, "")

	// Severity distribution + top hosts.
	if m.srvlog != nil {
		left := m.renderSeverityBars(m.srvlog, "SEVERITY DISTRIBUTION")
		right := m.renderTopHosts(m.srvlog)
		bottom := lipgloss.JoinHorizontal(lipgloss.Top, left, "   ", right)
		sections = append(sections, bottom)
	}

	// Last updated.
	if !m.lastLoad.IsZero() {
		sections = append(sections, "")
		ts := theme.Comment.Render(fmt.Sprintf("  Updated %s  ·  r refresh", m.lastLoad.Format("15:04:05")))
		sections = append(sections, ts)
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m *Model) renderCards() string {
	cardWidth := max(18, (m.width-8)/3)

	renderCard := func(title string, titleColor color.Color, stats *client.StatsSummary) string {
		if stats == nil {
			return theme.Card.Width(cardWidth).Render(
				theme.Comment.Render(title + "\n  No data"),
			)
		}

		titleLine := lipgloss.NewStyle().Foreground(titleColor).Bold(true).Render(title)

		totalLine := fmt.Sprintf("  %s  %s",
			theme.Comment.Render("Total"),
			lipgloss.NewStyle().Foreground(theme.ColorTeal).Bold(true).Render(formatCount(stats.Total)))

		errLine := fmt.Sprintf("  %s  %s",
			theme.Comment.Render("Errors"),
			lipgloss.NewStyle().Foreground(theme.ColorOrange).Render(formatCount(stats.Errors)))

		warnLine := fmt.Sprintf("  %s %s",
			theme.Comment.Render("Warnings"),
			lipgloss.NewStyle().Foreground(theme.ColorYellow).Render(formatCount(stats.Warnings)))

		trendStr := fmt.Sprintf("%.1f%%", stats.Trend)
		var trendLine string
		switch {
		case stats.Trend > 0:
			trendLine = fmt.Sprintf("  %s  %s",
				theme.Comment.Render("Trend"),
				lipgloss.NewStyle().Foreground(theme.ColorRed).Render("▲ +"+trendStr))
		case stats.Trend < 0:
			trendLine = fmt.Sprintf("  %s  %s",
				theme.Comment.Render("Trend"),
				lipgloss.NewStyle().Foreground(theme.ColorGreen).Render("▼ "+trendStr))
		default:
			trendLine = fmt.Sprintf("  %s  %s",
				theme.Comment.Render("Trend"),
				theme.Comment.Render("─ "+trendStr))
		}

		content := strings.Join([]string{titleLine, "", totalLine, errLine, warnLine, trendLine}, "\n")
		return theme.Card.Width(cardWidth).Render(content)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top,
		renderCard("SRVLOG", theme.ColorTeal, m.srvlog),
		" ",
		renderCard("APPLOG", theme.ColorPink, m.applog),
		" ",
		renderCard("NETLOG", theme.ColorFuchsia, m.netlog),
	)
}

func (m *Model) renderSeverityBars(stats *client.StatsSummary, title string) string {
	barWidth := max(20, m.width/3)
	var lines []string
	lines = append(lines, theme.CardLabel.Render("  "+title))
	lines = append(lines, "")

	maxCount := int64(1)
	for _, sc := range stats.SeverityBreakdown {
		if sc.Count > maxCount {
			maxCount = sc.Count
		}
	}

	for _, sc := range stats.SeverityBreakdown {
		if sc.Count == 0 {
			continue
		}
		label := padRight(strings.ToUpper(sc.Label), 8)
		barLen := int(float64(sc.Count) / float64(maxCount) * float64(barWidth-22))
		barLen = max(1, barLen)
		bar := strings.Repeat("█", barLen)
		count := formatCount(sc.Count)

		sevStyle := theme.SeverityStyle(sc.Severity)
		lines = append(lines, fmt.Sprintf("  %s %s %s",
			sevStyle.Render(label),
			sevStyle.Render(bar),
			theme.Comment.Render(count),
		))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *Model) renderTopHosts(stats *client.StatsSummary) string {
	var lines []string
	lines = append(lines, theme.CardLabel.Render("  TOP HOSTS"))
	lines = append(lines, "")

	for i, h := range stats.TopHosts {
		if i >= 10 {
			break
		}
		lines = append(lines, fmt.Sprintf("  %s  %s  %s",
			theme.Hostname.Render(padRight(h.Name, 20)),
			theme.Base.Render(padRight(formatCount(h.Count), 8)),
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
