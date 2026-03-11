package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Tab identifies the active view.
type Tab int

const (
	TabSyslog Tab = iota
	TabAppLog
)

// Model is the top-level Bubble Tea model.
type Model struct {
	activeTab Tab
	syslog    SyslogView
	applog    AppLogView
	filter    FilterBar
	client    *SSEClient
	width     int
	height    int
	showHelp  bool

	syslogConnected bool
	applogConnected bool

	// SSE channels for each stream.
	syslogCh     <-chan SSEMessage
	syslogCancel func()
	applogCh     <-chan SSEMessage
	applogCancel func()

	// Filter params for reconnection.
	syslogParams map[string]string
	applogParams map[string]string
}

// New creates a new top-level model.
func New(client *SSEClient) Model {
	return Model{
		activeTab:    TabSyslog,
		syslog:       NewSyslogView(),
		applog:       NewAppLogView(),
		filter:       NewFilterBar(),
		client:       client,
		syslogParams: make(map[string]string),
		applogParams: make(map[string]string),
	}
}

// Init starts both SSE streams.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		listenSSE(m.client, StreamSyslog, "/api/v1/syslog/stream", m.syslogParams),
		listenSSE(m.client, StreamAppLog, "/api/v1/applog/stream", m.applogParams),
	)
}

// Update handles all messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		viewHeight := m.height - 2 // header + status bar
		if m.filter.IsActive() || m.filter.HasFilter() {
			viewHeight--
		}
		m.syslog.SetSize(m.width, viewHeight)
		m.applog.SetSize(m.width, viewHeight)
		return m, nil

	case sseSubscription:
		// SSE connection established — store channel and start reading.
		switch msg.stream {
		case StreamSyslog:
			if m.syslogCancel != nil {
				m.syslogCancel()
			}
			m.syslogCh = msg.ch
			m.syslogCancel = msg.cancel
			m.syslogConnected = true
			return m, waitForSSE(StreamSyslog, msg.ch)
		case StreamAppLog:
			if m.applogCancel != nil {
				m.applogCancel()
			}
			m.applogCh = msg.ch
			m.applogCancel = msg.cancel
			m.applogConnected = true
			return m, waitForSSE(StreamAppLog, msg.ch)
		}

	case sseHeartbeat:
		return m, waitForSSE(msg.stream, msg.ch)

	case sseEventReceived:
		// Dispatch the parsed event to the appropriate view, then continue reading.
		var cmd tea.Cmd
		switch msg.msg.(type) {
		case SyslogEventMsg:
			m.syslog, cmd = m.syslog.Update(msg.msg)
		case AppLogEventMsg:
			m.applog, cmd = m.applog.Update(msg.msg)
		}
		return m, tea.Batch(cmd, waitForSSE(msg.stream, msg.ch))

	case SSEStatusMsg:
		switch msg.Stream {
		case StreamSyslog:
			m.syslogConnected = msg.Connected
		case StreamAppLog:
			m.applogConnected = msg.Connected
		}
		return m, nil

	case FilterAppliedMsg:
		return m.applyFilter(msg)

	case FilterClearedMsg:
		return m.clearFilter(msg)

	case tea.KeyMsg:
		// Help popup intercepts all keys.
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}

		// If filter is active, all keys go to the filter.
		if m.filter.IsActive() {
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "?":
			m.showHelp = true
			return m, nil
		case "q", "ctrl+c":
			if m.syslogCancel != nil {
				m.syslogCancel()
			}
			if m.applogCancel != nil {
				m.applogCancel()
			}
			return m, tea.Quit
		case "tab":
			if m.activeTab == TabSyslog {
				m.activeTab = TabAppLog
			} else {
				m.activeTab = TabSyslog
			}
			return m, nil
		case "/":
			cmd := m.filter.Open(Stream(m.activeTab))
			return m, cmd
		case "esc":
			// Close expanded detail.
			if m.activeTab == TabSyslog && m.syslog.expanded {
				m.syslog.expanded = false
				return m, nil
			}
			if m.activeTab == TabAppLog && m.applog.expanded {
				m.applog.expanded = false
				return m, nil
			}
			return m, nil
		}

		// Delegate to active view.
		var cmd tea.Cmd
		switch m.activeTab {
		case TabSyslog:
			m.syslog, cmd = m.syslog.Update(msg)
		case TabAppLog:
			m.applog, cmd = m.applog.Update(msg)
		}
		return m, cmd
	}

	return m, nil
}

func (m Model) applyFilter(msg FilterAppliedMsg) (tea.Model, tea.Cmd) {
	switch msg.Stream {
	case StreamSyslog:
		m.syslogParams = msg.Params
		m.syslog.Clear()
		if m.syslogCancel != nil {
			m.syslogCancel()
		}
		return m, listenSSE(m.client, StreamSyslog, "/api/v1/syslog/stream", msg.Params)
	case StreamAppLog:
		m.applogParams = msg.Params
		m.applog.Clear()
		if m.applogCancel != nil {
			m.applogCancel()
		}
		return m, listenSSE(m.client, StreamAppLog, "/api/v1/applog/stream", msg.Params)
	}
	return m, nil
}

func (m Model) clearFilter(msg FilterClearedMsg) (tea.Model, tea.Cmd) {
	switch msg.Stream {
	case StreamSyslog:
		m.syslogParams = make(map[string]string)
		m.syslog.Clear()
		if m.syslogCancel != nil {
			m.syslogCancel()
		}
		return m, listenSSE(m.client, StreamSyslog, "/api/v1/syslog/stream", nil)
	case StreamAppLog:
		m.applogParams = make(map[string]string)
		m.applog.Clear()
		if m.applogCancel != nil {
			m.applogCancel()
		}
		return m, listenSSE(m.client, StreamAppLog, "/api/v1/applog/stream", nil)
	}
	return m, nil
}

// View renders the full UI.
func (m Model) View() tea.View {
	var b strings.Builder

	// Header.
	b.WriteString(m.renderHeader())
	b.WriteByte('\n')

	// Filter bar.
	filterView := m.filter.View(m.width)
	if filterView != "" {
		b.WriteString(filterView)
		b.WriteByte('\n')
	}

	// Active view.
	viewHeight := m.height - 2 // header + status
	if filterView != "" {
		viewHeight--
	}
	m.syslog.SetSize(m.width, viewHeight)
	m.applog.SetSize(m.width, viewHeight)

	switch m.activeTab {
	case TabSyslog:
		content := m.syslog.View()
		b.WriteString(content)
	case TabAppLog:
		content := m.applog.View()
		b.WriteString(content)
	}

	// Pad to fill remaining space, then status bar at bottom.
	currentLines := strings.Count(b.String(), "\n")
	remaining := m.height - currentLines - 2 // -1 for status bar, -1 buffer
	for range max(remaining, 0) {
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString(m.renderStatusBar())

	output := b.String()
	if m.showHelp {
		output = m.overlayHelp(output)
	}

	v := tea.NewView(output)
	v.AltScreen = true
	return v
}

func (m Model) overlayHelp(bg string) string {
	help := helpPopupStyle.Render(helpText)
	return placeOverlay(m.width, m.height, help, bg)
}

// placeOverlay centers overlay on top of background text.
func placeOverlay(width, height int, overlay, bg string) string {
	bgLines := strings.Split(bg, "\n")
	for len(bgLines) < height {
		bgLines = append(bgLines, "")
	}

	overlayLines := strings.Split(overlay, "\n")
	oHeight := len(overlayLines)
	oWidth := lipgloss.Width(overlay)

	startY := max((height-oHeight)/2, 0)
	startX := max((width-oWidth)/2, 0)

	for i, line := range overlayLines {
		y := startY + i
		if y >= len(bgLines) {
			break
		}
		row := bgLines[y]
		// Pad row to width so overlay doesn't clip.
		rowWidth := lipgloss.Width(row)
		if rowWidth < startX+lipgloss.Width(line) {
			row += strings.Repeat(" ", startX+lipgloss.Width(line)-rowWidth)
		}
		// Replace the middle section with the overlay line.
		before := ansiTruncate(row, startX)
		bgLines[y] = before + line
	}

	return strings.Join(bgLines[:height], "\n")
}

// ansiTruncate returns the first n visible characters of s, preserving ANSI codes.
func ansiTruncate(s string, n int) string {
	visible := 0
	inEsc := false
	var out strings.Builder
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
		}
		if inEsc {
			out.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if visible >= n {
			break
		}
		out.WriteRune(r)
		visible++
	}
	// Pad if string was shorter than n.
	for visible < n {
		out.WriteByte(' ')
		visible++
	}
	return out.String()
}

func (m Model) renderHeader() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(colorBlue).Render("TAILLIGHT")

	syslogTab := inactiveTabStyle.Render("Syslog")
	applogTab := inactiveTabStyle.Render("AppLog")
	if m.activeTab == TabSyslog {
		syslogTab = activeTabStyle.Render("Syslog")
	} else {
		applogTab = activeTabStyle.Render("AppLog")
	}

	tabs := title + "  " + syslogTab + " " + applogTab

	// Connection status.
	var status string
	if m.syslogConnected && m.applogConnected {
		status = connectedStyle.Render("● connected")
	} else {
		status = disconnectedStyle.Render("● disconnected")
	}

	gap := max(m.width-lipgloss.Width(tabs)-lipgloss.Width(status)-2, 1)

	return headerStyle.Width(m.width).Render(tabs + strings.Repeat(" ", gap) + status)
}

func (m Model) renderStatusBar() string {
	var count int
	switch m.activeTab {
	case TabSyslog:
		count = m.syslog.EventCount()
	case TabAppLog:
		count = m.applog.EventCount()
	}

	pinIndicator := ""
	isPinned := false
	switch m.activeTab {
	case TabSyslog:
		isPinned = m.syslog.IsPinned()
	case TabAppLog:
		isPinned = m.applog.IsPinned()
	}
	if !isPinned {
		pinIndicator = "  " + dimStyle.Render("[scrolled]")
	}

	left := fmt.Sprintf(" %d events%s", count, pinIndicator)
	right := "[Tab] switch  [/] filter  [Enter] expand  [?] help  [q] quit "

	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)

	return statusBarStyle.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}
