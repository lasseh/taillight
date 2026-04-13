// Package dashboard implements the dashboard view with summary stats,
// severity distribution, top hosts, and recent critical events — matching
// the web GUI's HomeView layout.
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"net/url"
	"slices"
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

const maxRecentEvents = 10

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

	// Live SSE streams for critical/error events.
	srvlogStream *client.SSEStream
	applogStream *client.SSEStream
}

// New creates a new dashboard model.
func New(c *client.Client) Model {
	return Model{client: c, loading: true}
}

// Connected reports whether any of the dashboard's SSE streams are connected.
func (m *Model) Connected() bool {
	if m.srvlogStream != nil && m.srvlogStream.Connected() {
		return true
	}
	if m.applogStream != nil && m.applogStream.Connected() {
		return true
	}
	return false
}

// SetSize updates dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Init returns the initial load command and starts live SSE streams.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadStats(), m.refreshTick(), m.startStreams())
}

// StreamTickMsg triggers draining the dashboard SSE streams.
type StreamTickMsg struct{}

func (m Model) streamTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return StreamTickMsg{}
	})
}

func (m *Model) startStreams() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		// Srvlog stream filtered to severity ≤ 2 (emerg, alert, crit).
		srvParams := url.Values{"severity_max": {"2"}}
		srvStream := client.NewSSEStream(c, "/api/v1/srvlog/stream", srvParams, 0)

		// Applog stream filtered to WARN and above.
		appParams := url.Values{"level": {"WARN"}}
		appStream := client.NewSSEStream(c, "/api/v1/applog/stream", appParams, 0)

		return StreamsStartedMsg{srvlog: srvStream, applog: appStream}
	}
}

type StreamsStartedMsg struct {
	srvlog *client.SSEStream
	applog *client.SSEStream
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

	case StreamsStartedMsg:
		m.srvlogStream = msg.srvlog
		m.applogStream = msg.applog
		return m, m.streamTick()

	case StreamTickMsg:
		m = m.drainStreams()
		return m, m.streamTick()

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
	subtitleLine := " " // always render a third line for consistent height
	if subtitle != "" {
		subtitleLine = theme.Comment.Render("  " + subtitle)
	}
	content := strings.Join([]string{labelLine, valueLine, subtitleLine}, "\n")
	return theme.Card.Width(width).Render(content)
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
		svc := theme.Program.Render(padRight(truncate(e.Service, 18), 18))
		comp := lipgloss.NewStyle().Foreground(theme.ColorYellow).Render(padRight(truncate(e.Component, 14), 14))

		msgW := max(20, m.width-56)
		msg := highlight.Message(truncate(e.Msg, msgW))

		// Inline attrs like the web GUI.
		if attrs := formatAttrsInline(e.Attrs); attrs != "" {
			msg += " " + lipgloss.NewStyle().Foreground(theme.ColorOrange).Render("-") +
				" " + theme.Comment.Render(truncate(attrs, max(10, msgW/2)))
		}

		lines = append(lines, fmt.Sprintf("  %s  %s %s %s %s", ts, lvl, svc, comp, msg))
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

// --- Live stream draining ---

func (m Model) drainStreams() Model {
	m.criticalLogs = drainSrvlogCritical(m.srvlogStream, m.criticalLogs)
	m.recentErrors = drainApplogErrors(m.applogStream, m.recentErrors)
	return m
}

// drainSrvlogCritical reads up to 50 events from the srvlog stream and
// prepends them to the critical logs list (newest first, capped).
func drainSrvlogCritical(stream *client.SSEStream, current []client.SrvlogEvent) []client.SrvlogEvent {
	if stream == nil {
		return current
	}
	for range 50 {
		select {
		case evt, ok := <-stream.Events():
			if !ok {
				return current
			}
			var e client.SrvlogEvent
			if err := json.Unmarshal(evt.Data, &e); err != nil {
				continue
			}
			current = append([]client.SrvlogEvent{e}, current...)
			if len(current) > maxRecentEvents {
				current = current[:maxRecentEvents]
			}
		default:
			return current
		}
	}
	return current
}

// drainApplogErrors reads up to 50 events from the applog stream and
// prepends them to the recent errors list (newest first, capped).
func drainApplogErrors(stream *client.SSEStream, current []client.AppLogEvent) []client.AppLogEvent {
	if stream == nil {
		return current
	}
	for range 50 {
		select {
		case evt, ok := <-stream.Events():
			if !ok {
				return current
			}
			var e client.AppLogEvent
			if err := json.Unmarshal(evt.Data, &e); err != nil {
				continue
			}
			current = append([]client.AppLogEvent{e}, current...)
			if len(current) > maxRecentEvents {
				current = current[:maxRecentEvents]
			}
		default:
			return current
		}
	}
	return current
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

// formatAttrsInline renders attrs as "key=val key=val" like the web GUI.
func formatAttrsInline(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "{}" || string(raw) == "null" {
		return ""
	}
	var attrs map[string]any
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return ""
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		switch val := attrs[k].(type) {
		case string:
			parts = append(parts, k+"="+val)
		default:
			b, _ := json.Marshal(val)
			parts = append(parts, k+"="+string(b))
		}
	}
	return strings.Join(parts, " ")
}

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
