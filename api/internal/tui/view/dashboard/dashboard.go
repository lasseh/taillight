// Package dashboard implements the dashboard view with summary stats,
// severity distribution, top hosts, and recent critical events — matching
// the web GUI's HomeView layout.
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
	"github.com/lasseh/taillight/internal/tui/highlight"
	"github.com/lasseh/taillight/internal/tui/theme"
)

const refreshInterval = 30 * time.Second

// StatsLoadedMsg carries loaded dashboard statistics.
type StatsLoadedMsg struct {
	Srvlog       *client.StatsSummary
	Applog       *client.AppLogStatsSummary
	CriticalLogs []client.SrvlogEvent
	RecentErrors []client.AppLogEvent
	Err          error
}

// Model is the dashboard view.
type Model struct {
	client       *client.Client
	srvlog       *client.StatsSummary
	applog       *client.AppLogStatsSummary
	criticalLogs []client.SrvlogEvent
	recentErrors []client.AppLogEvent
	loading      bool
	lastLoad     time.Time
	width        int
	height       int
}

// New creates a new dashboard model.
func New(c *client.Client) Model {
	return Model{client: c, loading: true}
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
		m.criticalLogs = msg.CriticalLogs
		m.recentErrors = msg.RecentErrors
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
		return lipgloss.NewStyle().
			Width(m.width).Height(m.height).
			Foreground(theme.ColorComment).
			Align(lipgloss.Center, lipgloss.Center).
			Render("Loading dashboard...")
	}

	var sections []string

	// Section header.
	sections = append(sections, sectionHeader("SYSLOG", theme.ColorTeal))

	// Summary cards.
	if m.srvlog != nil {
		sections = append(sections, m.renderSummaryCards())
		sections = append(sections, "")

		// Severity distribution (left) + Top hosts (right).
		leftW := max(30, m.width*55/100)
		rightW := max(20, m.width-leftW-3)
		left := m.renderSeverityBars(leftW)
		right := m.renderTopHosts(rightW)
		sections = append(sections, lipgloss.JoinHorizontal(lipgloss.Top, left, "   ", right))
		sections = append(sections, "")
	}

	// Recent high-severity events.
	if len(m.criticalLogs) > 0 {
		sections = append(sections, m.renderRecentCritical())
		sections = append(sections, "")
	}

	// Applog section.
	if m.applog != nil {
		sections = append(sections, sectionHeader("APPLOG", theme.ColorPink))
		sections = append(sections, m.renderApplogCards())
	}

	// Recent applog errors.
	if len(m.recentErrors) > 0 {
		sections = append(sections, "")
		sections = append(sections, m.renderRecentErrors())
	}

	// Footer.
	if !m.lastLoad.IsZero() {
		sections = append(sections, "")
		sections = append(sections, theme.Comment.Render(
			fmt.Sprintf("  Updated %s  ·  r refresh  ·  24h range", m.lastLoad.Format("15:04:05"))))
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func sectionHeader(title string, c color.Color) string {
	label := lipgloss.NewStyle().Foreground(c).Bold(true).Render("  " + title)
	return label
}

// --- Summary cards matching the web GUI: Total, Emerg & Alert, Criticals, Errors ---

func (m *Model) renderSummaryCards() string {
	s := m.srvlog
	cardW := max(15, (m.width-10)/4)

	// Find specific severity counts.
	var emergCnt, alertCnt, critCnt, errCnt int64
	for _, sc := range s.SeverityBreakdown {
		switch sc.Severity {
		case 0:
			emergCnt = sc.Count
		case 1:
			alertCnt = sc.Count
		case 2:
			critCnt = sc.Count
		case 3:
			errCnt = sc.Count
		}
	}

	total := miniCard(cardW, "Total", formatCount(s.Total), theme.ColorTeal,
		trendStr(s.Trend))
	emergAlert := miniCard(cardW, "Emerg & Alert",
		fmt.Sprintf("%s / %s", formatCount(emergCnt), formatCount(alertCnt)),
		theme.ColorPink, "")
	crits := miniCard(cardW, "Criticals", formatCount(critCnt), theme.ColorRed,
		pctStr(critCnt, s.Total))
	errs := miniCard(cardW, "Errors", formatCount(errCnt), theme.ColorOrange,
		pctStr(errCnt, s.Total))

	return lipgloss.JoinHorizontal(lipgloss.Top, total, " ", emergAlert, " ", crits, " ", errs)
}

func miniCard(width int, label, value string, valueColor color.Color, subtitle string) string {
	labelLine := theme.Comment.Render("  " + label)
	valueLine := lipgloss.NewStyle().Foreground(valueColor).Bold(true).Render("  " + value)
	var lines []string
	lines = append(lines, labelLine, valueLine)
	if subtitle != "" {
		lines = append(lines, theme.Comment.Render("  "+subtitle))
	}
	return theme.Card.Width(width).Render(strings.Join(lines, "\n"))
}

// --- Severity distribution bars ---

func (m *Model) renderSeverityBars(width int) string {
	var lines []string
	lines = append(lines, theme.CardLabel.Render("  SEVERITY DISTRIBUTION"))
	lines = append(lines, "")

	s := m.srvlog
	maxCount := int64(1)
	for _, sc := range s.SeverityBreakdown {
		if sc.Count > maxCount {
			maxCount = sc.Count
		}
	}

	barMaxW := width - 25
	for _, sc := range s.SeverityBreakdown {
		if sc.Count == 0 {
			continue
		}
		label := padRight(strings.ToUpper(sc.Label), 8)
		barLen := max(1, int(float64(sc.Count)/float64(maxCount)*float64(barMaxW)))
		bar := strings.Repeat("█", barLen)
		pct := fmt.Sprintf("%5.1f%%", sc.Pct)
		cnt := formatCount(sc.Count)

		sevStyle := theme.SeverityStyle(sc.Severity)
		lines = append(lines, fmt.Sprintf("  %s %s %s %s",
			sevStyle.Render(label),
			sevStyle.Render(bar),
			theme.Comment.Render(pct),
			theme.Comment.Render(cnt),
		))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// --- Top hosts ---

func (m *Model) renderTopHosts(width int) string {
	var lines []string
	lines = append(lines, theme.CardLabel.Render("  TOP HOSTS"))
	lines = append(lines, "")

	s := m.srvlog
	hostW := max(12, width-20)
	for i, h := range s.TopHosts {
		if i >= 10 {
			break
		}
		rank := theme.Comment.Render(fmt.Sprintf("  %2d.", i+1))
		name := theme.Hostname.Render(padRight(truncate(h.Name, hostW), hostW))
		count := theme.Base.Render(padRight(formatCount(h.Count), 7))
		pct := theme.Comment.Render(fmt.Sprintf("%5.1f%%", h.Pct))
		lines = append(lines, fmt.Sprintf("%s %s %s %s", rank, name, count, pct))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// --- Recent critical events ---

func (m *Model) renderRecentCritical() string {
	lines := make([]string, 0, 2+len(m.criticalLogs))
	lines = append(lines, theme.CardLabel.Render("  RECENT HIGH-SEVERITY"))
	lines = append(lines, "")

	for _, e := range m.criticalLogs {
		ts := theme.Timestamp.Render(e.ReceivedAt.Local().Format("15:04"))
		sev := theme.SeverityStyle(e.Severity).Render(padRight(strings.ToUpper(e.SeverityLabel), 8))
		host := theme.Hostname.Render(padRight(truncate(e.Hostname, 18), 18))
		prog := theme.Program.Render(padRight(truncate(e.Programname, 12), 12))

		msgW := max(20, m.width-55)
		msg := highlight.Message(truncate(e.Message, msgW))

		lines = append(lines, fmt.Sprintf("  %s  %s %s %s %s", ts, sev, host, prog, msg))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// --- Recent applog errors ---

func (m *Model) renderRecentErrors() string {
	lines := make([]string, 0, 2+len(m.recentErrors))
	lines = append(lines, theme.CardLabel.Render("  RECENT ERRORS"))
	lines = append(lines, "")

	for _, e := range m.recentErrors {
		ts := theme.Timestamp.Render(e.Timestamp.Local().Format("15:04"))
		lvl := theme.AppLogLevelStyle(e.Level).Render(padRight(e.Level, 6))
		svc := theme.Program.Render(padRight(truncate(e.Service, 20), 20))
		host := theme.Hostname.Render(padRight(truncate(e.Host, 14), 14))

		msgW := max(20, m.width-52)
		msg := highlight.Message(truncate(e.Msg, msgW))

		lines = append(lines, fmt.Sprintf("  %s  %s %s %s %s", ts, lvl, svc, host, msg))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// --- Applog summary cards ---

func (m *Model) renderApplogCards() string {
	s := m.applog
	cardW := max(15, (m.width-10)/4)

	var fatalCnt, errCnt, warnCnt, infoCnt int64
	for _, lc := range s.LevelBreakdown {
		switch lc.Level {
		case "FATAL":
			fatalCnt = lc.Count
		case "ERROR":
			errCnt = lc.Count
		case "WARN":
			warnCnt = lc.Count
		case "INFO":
			infoCnt = lc.Count
		}
	}

	total := miniCard(cardW, "Total", formatCount(s.Total), theme.ColorTeal,
		trendStr(s.Trend))
	fatalErr := miniCard(cardW, "Fatal & Errors",
		fmt.Sprintf("%s / %s", formatCount(fatalCnt), formatCount(errCnt)),
		theme.ColorPink, "")
	warns := miniCard(cardW, "Warnings", formatCount(warnCnt), theme.ColorYellow, "")
	infos := miniCard(cardW, "Info", formatCount(infoCnt), theme.ColorTeal, "")

	return lipgloss.JoinHorizontal(lipgloss.Top, total, " ", fatalErr, " ", warns, " ", infos)
}

// --- Data loading ---

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
		// Fetch recent critical srvlog events (severity ≤ 2).
		critResp, err := c.ListSrvlogs(ctx, client.SrvlogFilter{
			SeverityMax: 2,
			Facility:    -1,
		}, "", 10)
		if err != nil {
			return StatsLoadedMsg{Err: err}
		}
		// Fetch recent applog errors (WARN and above).
		errResp, err := c.ListAppLogs(ctx, client.AppLogFilter{
			Level: "WARN",
		}, "", 10)
		if err != nil {
			return StatsLoadedMsg{Err: err}
		}
		return StatsLoadedMsg{
			Srvlog:       srvlog,
			Applog:       applog,
			CriticalLogs: critResp.Data,
			RecentErrors: errResp.Data,
		}
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

// --- Helpers ---

func formatCount(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func trendStr(trend float64) string {
	if trend > 0 {
		return fmt.Sprintf("▲ +%.0f%%", trend)
	}
	if trend < 0 {
		return fmt.Sprintf("▼ %.0f%%", trend)
	}
	return "─ 0%"
}

func pctStr(count, total int64) string {
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("%.1f%% of total", float64(count)/float64(total)*100)
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
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
