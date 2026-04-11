package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/client"
	"github.com/lasseh/taillight/internal/tui/component"
	"github.com/lasseh/taillight/internal/tui/theme"
	"github.com/lasseh/taillight/internal/tui/view/applog"
	"github.com/lasseh/taillight/internal/tui/view/dashboard"
	"github.com/lasseh/taillight/internal/tui/view/hosts"
	"github.com/lasseh/taillight/internal/tui/view/netlog"
	"github.com/lasseh/taillight/internal/tui/view/notification"
	"github.com/lasseh/taillight/internal/tui/view/settings"
	"github.com/lasseh/taillight/internal/tui/view/srvlog"
)

// Config holds display configuration for the App.
type Config struct {
	BufferSize    int
	BatchInterval time.Duration
	AutoScroll    bool
	TimeFormat    string
}

// App is the root bubbletea model that owns all views and routes messages.
type App struct {
	cfg    Config
	client *client.Client
	keys   KeyMap

	width  int
	height int

	activeTab TabID
	focus     FocusTarget

	// Sub-models.
	srvlog       srvlog.Model
	applog       applog.Model
	netlog       netlog.Model
	dashboard    dashboard.Model
	hosts        hosts.Model
	notification notification.Model
	settings     settings.Model
	tabBar       component.TabBar
	statusBar    component.StatusBar
	helpModel    help.Model
	showHelp     bool

	// Track which views have been initialized.
	dashboardInit    bool
	hostsInit        bool
	notificationInit bool

	// Per-tab SSE streams. nil when not active.
	srvlogStream *client.SSEStream
	applogStream *client.SSEStream
	netlogStream *client.SSEStream

	// Notification toasts.
	toasts component.ToastQueue

	// State.
	lastError string
}

// NewApp creates the root App model. This constructor is wish-compatible: it
// takes config and client, not stdin/stdout.
func NewApp(cfg Config, c *client.Client) *App {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 10000
	}
	if cfg.BatchInterval <= 0 {
		cfg.BatchInterval = 50 * time.Millisecond
	}
	if cfg.TimeFormat == "" {
		cfg.TimeFormat = "15:04:05"
	}

	tabs := []component.Tab{
		// Primary tabs (left side, matching web GUI nav order).
		{ID: int(TabDashboard), Label: "DASHBOARD", Color: theme.ColorBlue},
		{ID: int(TabNetlog), Label: "NETLOG", Color: theme.ColorFuchsia},
		{ID: int(TabSrvlog), Label: "SRVLOG", Color: theme.ColorTeal},
		{ID: int(TabApplog), Label: "APPLOG", Color: theme.ColorPink},
		// Secondary tabs (right side, like the web GUI's dropdown menu).
		{ID: int(TabHosts), Label: "HOSTS", Color: theme.ColorGreen, Right: true},
		{ID: int(TabNotifications), Label: "ALERTS", Color: theme.ColorYellow, Right: true},
		{ID: int(TabSettings), Label: "SETTINGS", Color: theme.ColorComment, Right: true},
	}

	return &App{
		cfg:          cfg,
		client:       c,
		keys:         DefaultKeyMap(),
		activeTab:    TabDashboard,
		focus:        FocusTable,
		srvlog:       srvlog.New(cfg.BufferSize, cfg.TimeFormat),
		applog:       applog.New(cfg.BufferSize, cfg.TimeFormat),
		netlog:       netlog.New(cfg.BufferSize, cfg.TimeFormat),
		dashboard:    dashboard.New(c),
		hosts:        hosts.New(c),
		notification: notification.New(c),
		settings:     settings.New(c.BaseURL(), ""),
		toasts:       component.NewToastQueue(),
		tabBar:       component.NewTabBar(tabs),
		statusBar:    component.NewStatusBar(),
		helpModel:    help.New(),
	}
}

// Init starts background tasks.
func (a *App) Init() tea.Cmd {
	a.dashboardInit = true
	return tea.Batch(
		a.dashboard.Init(),
		a.sseTick(),
	)
}

// Update handles all messages.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.helpModel.SetWidth(msg.Width)
		ch := a.contentHeight()
		a.srvlog.SetSize(msg.Width, ch)
		a.applog.SetSize(msg.Width, ch)
		a.netlog.SetSize(msg.Width, ch)
		a.dashboard.SetSize(msg.Width, ch)
		a.hosts.SetSize(msg.Width, ch)
		a.notification.SetSize(msg.Width, ch)
		a.settings.SetSize(msg.Width, ch)
		return a, nil

	case StreamStartedMsg:
		switch msg.Feed {
		case "srvlog":
			a.srvlogStream = msg.Stream
		case "applog":
			a.applogStream = msg.Stream
		case "netlog":
			a.netlogStream = msg.Stream
		}
		return a, nil

	case tea.KeyPressMsg:
		return a.handleKey(msg)

	case SSETickMsg:
		a.drainAllStreams()
		cmds = append(cmds, a.sseTick())
		return a, tea.Batch(cmds...)

	case MetaLoadedMsg:
		switch msg.Feed {
		case "srvlog":
			a.srvlog.SetMeta(msg.Hosts, msg.Programs)
		case "applog":
			a.applog.SetMeta(msg.Services, msg.Components, msg.Hosts)
		case "netlog":
			a.netlog.SetMeta(msg.Hosts, msg.Programs)
		}
		return a, nil

	case dashboard.StatsLoadedMsg:
		a.dashboard, _ = a.dashboard.Update(msg)
		return a, nil

	case dashboard.StreamsStartedMsg:
		var cmd tea.Cmd
		a.dashboard, cmd = a.dashboard.Update(msg)
		return a, cmd

	case dashboard.StreamTickMsg:
		var cmd tea.Cmd
		a.dashboard, cmd = a.dashboard.Update(msg)
		return a, cmd

	case hosts.HostsLoadedMsg:
		a.hosts, _ = a.hosts.Update(msg)
		return a, nil

	case dashboard.RefreshTickMsg:
		if a.activeTab == TabDashboard {
			var cmd tea.Cmd
			a.dashboard, cmd = a.dashboard.Update(msg)
			return a, cmd
		}
		return a, nil

	case hosts.RefreshTickMsg:
		if a.activeTab == TabHosts {
			var cmd tea.Cmd
			a.hosts, cmd = a.hosts.Update(msg)
			return a, cmd
		}
		return a, nil

	case notification.DataLoadedMsg:
		a.notification, _ = a.notification.Update(msg)
		return a, nil

	case ErrorMsg:
		a.lastError = msg.Err.Error()
		a.statusBar.SetError(a.lastError)
		cmds = append(cmds, tea.Tick(5*time.Second, func(time.Time) tea.Msg {
			return ClearErrorMsg{}
		}))
		return a, tea.Batch(cmds...)

	case ClearErrorMsg:
		a.lastError = ""
		a.statusBar.SetError("")
		return a, nil
	}

	return a, nil
}

func (a *App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Help overlay takes priority.
	if a.showHelp {
		if key.Matches(msg, a.keys.Help) || key.Matches(msg, a.keys.Escape) {
			a.showHelp = false
		}
		return a, nil
	}

	// Filter input takes priority when focused.
	if a.focus == FocusFilter {
		if key.Matches(msg, a.keys.Escape) || msg.String() == "enter" {
			a.focus = FocusTable
			a.blurActiveFilter()
			return a, nil
		}
		cmd := a.updateActiveFilter(msg)
		return a, cmd
	}

	// Global keys.
	switch {
	case key.Matches(msg, a.keys.Quit):
		return a, tea.Quit
	case key.Matches(msg, a.keys.Help):
		a.showHelp = true
		return a, nil
	case key.Matches(msg, a.keys.Search), key.Matches(msg, a.keys.ToggleFocus):
		a.focus = FocusFilter
		a.focusActiveFilter()
		return a, nil
	case key.Matches(msg, a.keys.Tab1):
		cmd := a.switchTab(TabDashboard)
		return a, cmd
	case key.Matches(msg, a.keys.Tab2):
		cmd := a.switchTab(TabNetlog)
		return a, cmd
	case key.Matches(msg, a.keys.Tab3):
		cmd := a.switchTab(TabSrvlog)
		return a, cmd
	case key.Matches(msg, a.keys.Tab4):
		cmd := a.switchTab(TabApplog)
		return a, cmd
	case key.Matches(msg, a.keys.Tab5):
		cmd := a.switchTab(TabHosts)
		return a, cmd
	case key.Matches(msg, a.keys.Tab6):
		cmd := a.switchTab(TabNotifications)
		return a, cmd
	case key.Matches(msg, a.keys.Tab7):
		cmd := a.switchTab(TabSettings)
		return a, cmd
	case key.Matches(msg, a.keys.Escape):
		if a.focus == FocusDetail {
			a.focus = FocusTable
			a.closeActiveDetail()
			return a, nil
		}
		return a, nil
	}

	// Delegate to active view.
	cmd := a.updateActiveTable(msg)
	return a, cmd
}

// View renders the full TUI. The layout is pinned: tab bar at top, status bar
// at bottom, content fills the exact space in between.
func (a *App) View() tea.View {
	if a.width == 0 || a.height == 0 {
		return tea.NewView("Initializing...")
	}

	// Tab bar (2 lines: tabs + separator).
	tabBar := a.tabBar.View(a.width)

	// Main content — rendered into a fixed-height box.
	var content string
	switch a.activeTab {
	case TabSrvlog:
		content = a.srvlog.View()
	case TabApplog:
		content = a.applog.View()
	case TabNetlog:
		content = a.netlog.View()
	case TabDashboard:
		content = a.dashboard.View()
	case TabHosts:
		content = a.hosts.View()
	case TabNotifications:
		content = a.notification.View()
	case TabSettings:
		content = a.settings.View()
	}

	// Help overlay replaces content.
	if a.showHelp {
		content = theme.Help.Render(a.helpModel.FullHelpView(a.keys.FullHelp()))
	}

	// Force content to exact height so status bar is always at the bottom.
	ch := a.contentHeight()
	contentBox := lipgloss.NewStyle().
		Width(a.width).
		Height(ch).
		MaxHeight(ch).
		Render(content)

	// Status bar (1 line, always at bottom).
	statusBar := a.statusBar.View(a.width)

	screen := lipgloss.JoinVertical(lipgloss.Left, tabBar, contentBox, statusBar)

	// Overlay toast notifications in the top-right corner.
	if a.toasts.HasToasts() {
		toastOverlay := a.toasts.Render(a.width)
		screen = component.OverlayToasts(screen, toastOverlay, a.width, a.height)
	}

	v := tea.NewView(screen)
	v.AltScreen = true
	return v
}

func (a *App) contentHeight() int {
	// Tab bar (1) + separator (1) + status bar (1) = 3 lines of chrome.
	return max(a.height-3, 1)
}

// switchTab changes the active tab, starts the stream lazily if needed, and
// loads metadata.
func (a *App) switchTab(tab TabID) tea.Cmd {
	if a.activeTab == tab {
		return nil
	}
	a.activeTab = tab
	a.focus = FocusTable
	a.tabBar.SetActive(int(tab))

	var cmds []tea.Cmd

	// Start stream for the new tab if not already running.
	switch tab {
	case TabSrvlog:
		if a.srvlogStream == nil {
			cmds = append(cmds, a.startStream(TabSrvlog))
			cmds = append(cmds, a.loadSrvlogMeta())
		}
	case TabApplog:
		if a.applogStream == nil {
			cmds = append(cmds, a.startStream(TabApplog))
			cmds = append(cmds, a.loadApplogMeta())
		}
	case TabNetlog:
		if a.netlogStream == nil {
			cmds = append(cmds, a.startStream(TabNetlog))
			cmds = append(cmds, a.loadNetlogMeta())
		}
	case TabDashboard:
		if !a.dashboardInit {
			a.dashboardInit = true
			cmds = append(cmds, a.dashboard.Init())
		}
	case TabHosts:
		if !a.hostsInit {
			a.hostsInit = true
			cmds = append(cmds, a.hosts.Init())
		}
	case TabNotifications:
		if !a.notificationInit {
			a.notificationInit = true
			cmds = append(cmds, a.notification.Init())
		}
	case TabSettings:
		// Settings is static, no init needed.
	}

	return tea.Batch(cmds...)
}

// startStream begins an SSE connection for the given feed. The stream is
// returned via StreamStartedMsg so it can be safely assigned in Update (Cmds
// must not mutate the model).
func (a *App) startStream(tab TabID) tea.Cmd {
	c := a.client
	switch tab {
	case TabSrvlog:
		params := a.srvlog.Filter().Params()
		return func() tea.Msg {
			stream := client.NewSSEStream(c, "/api/v1/srvlog/stream", params, 0)
			return StreamStartedMsg{Feed: "srvlog", Stream: stream}
		}
	case TabApplog:
		params := a.applog.Filter().Params()
		return func() tea.Msg {
			stream := client.NewSSEStream(c, "/api/v1/applog/stream", params, 0)
			return StreamStartedMsg{Feed: "applog", Stream: stream}
		}
	case TabNetlog:
		params := a.netlog.Filter().Params()
		return func() tea.Msg {
			stream := client.NewSSEStream(c, "/api/v1/netlog/stream", params, 0)
			return StreamStartedMsg{Feed: "netlog", Stream: stream}
		}
	default:
		return nil
	}
}

// sseTick returns a command that fires an SSETickMsg after the batch interval.
func (a *App) sseTick() tea.Cmd {
	return tea.Tick(a.cfg.BatchInterval, func(time.Time) tea.Msg {
		return SSETickMsg{}
	})
}

// notifySeverityMax is the max severity that triggers a toast notification.
// Events with severity <= this value will show a toast (0=emerg, 3=err).
const notifySeverityMax = 3

// drainAllStreams reads events from all active SSE streams and pushes them to
// the corresponding views. Critical events also trigger toast notifications.
func (a *App) drainAllStreams() {
	if a.srvlogStream != nil {
		events := drainSrvlogSSE(a.srvlogStream, 100)
		if len(events) > 0 {
			a.srvlog.PushEvents(events)
			for i := range events {
				if events[i].Severity <= notifySeverityMax {
					a.pushSrvlogToast(events[i], "srvlog")
				}
			}
		}
	}

	if a.applogStream != nil {
		events := drainApplogSSE(a.applogStream, 100)
		if len(events) > 0 {
			a.applog.PushEvents(events)
			for i := range events {
				if events[i].Level == "FATAL" || events[i].Level == "ERROR" {
					a.pushApplogToast(events[i])
				}
			}
		}
	}

	if a.netlogStream != nil {
		events := drainNetlogSSE(a.netlogStream, 100)
		if len(events) > 0 {
			a.netlog.PushEvents(events)
			for i := range events {
				if events[i].Severity <= notifySeverityMax {
					a.pushSrvlogToast(events[i], "netlog")
				}
			}
		}
	}

	// Prune expired toasts.
	a.toasts.Prune()
	a.statusBar.SetConnected(a.isConnected())
}

func (a *App) pushSrvlogToast(e client.SrvlogEvent, feed string) {
	// Skip old events (backfill) — only notify for events from the last 30s.
	if time.Since(e.ReceivedAt) > 30*time.Second {
		return
	}
	a.toasts.Push(component.Toast{
		Title:   fmt.Sprintf("[%s] %s", strings.ToUpper(e.SeverityLabel), e.Hostname),
		Message: e.Message,
		Feed:    feed,
		Level:   e.Severity,
		Time:    e.ReceivedAt,
	})
}

func (a *App) pushApplogToast(e client.AppLogEvent) {
	if time.Since(e.ReceivedAt) > 30*time.Second {
		return
	}
	level := 3 // default to "err" level for toast color
	if e.Level == "FATAL" {
		level = 0
	}
	a.toasts.Push(component.Toast{
		Title:   fmt.Sprintf("[%s] %s", e.Level, e.Service),
		Message: e.Msg,
		Feed:    "applog",
		Level:   level,
		Time:    e.ReceivedAt,
	})
}

// isConnected returns true if ANY SSE stream is currently connected.
// Checks all log-view streams AND the dashboard's own streams.
func (a *App) isConnected() bool {
	if a.srvlogStream != nil && a.srvlogStream.Connected() {
		return true
	}
	if a.applogStream != nil && a.applogStream.Connected() {
		return true
	}
	if a.netlogStream != nil && a.netlogStream.Connected() {
		return true
	}
	if a.dashboard.Connected() {
		return true
	}
	return false
}

// Active view delegation helpers.

func (a *App) focusActiveFilter() {
	switch a.activeTab {
	case TabSrvlog:
		a.srvlog.FocusFilter()
	case TabApplog:
		a.applog.FocusFilter()
	case TabNetlog:
		a.netlog.FocusFilter()
	case TabDashboard, TabHosts, TabNotifications, TabSettings:
	}
}

func (a *App) blurActiveFilter() {
	switch a.activeTab {
	case TabSrvlog:
		a.srvlog.BlurFilter()
	case TabApplog:
		a.applog.BlurFilter()
	case TabNetlog:
		a.netlog.BlurFilter()
	case TabDashboard, TabHosts, TabNotifications, TabSettings:
	}
}

func (a *App) updateActiveFilter(msg tea.Msg) tea.Cmd {
	switch a.activeTab {
	case TabSrvlog:
		var cmd tea.Cmd
		a.srvlog, cmd = a.srvlog.UpdateFilter(msg)
		return cmd
	case TabApplog:
		var cmd tea.Cmd
		a.applog, cmd = a.applog.UpdateFilter(msg)
		return cmd
	case TabNetlog:
		var cmd tea.Cmd
		a.netlog, cmd = a.netlog.UpdateFilter(msg)
		return cmd
	default:
		return nil
	}
}

func (a *App) updateActiveTable(msg tea.Msg) tea.Cmd {
	switch a.activeTab {
	case TabSrvlog:
		var cmd tea.Cmd
		a.srvlog, cmd = a.srvlog.UpdateTable(msg)
		if a.srvlog.DetailOpen() {
			a.focus = FocusDetail
		}
		return cmd
	case TabApplog:
		var cmd tea.Cmd
		a.applog, cmd = a.applog.UpdateTable(msg)
		if a.applog.DetailOpen() {
			a.focus = FocusDetail
		}
		return cmd
	case TabNetlog:
		var cmd tea.Cmd
		a.netlog, cmd = a.netlog.UpdateTable(msg)
		if a.netlog.DetailOpen() {
			a.focus = FocusDetail
		}
		return cmd
	case TabDashboard:
		var cmd tea.Cmd
		a.dashboard, cmd = a.dashboard.Update(msg)
		return cmd
	case TabHosts:
		var cmd tea.Cmd
		a.hosts, cmd = a.hosts.Update(msg)
		return cmd
	case TabNotifications:
		var cmd tea.Cmd
		a.notification, cmd = a.notification.Update(msg)
		return cmd
	case TabSettings:
		var cmd tea.Cmd
		a.settings, cmd = a.settings.Update(msg)
		return cmd
	default:
		return nil
	}
}

func (a *App) closeActiveDetail() {
	switch a.activeTab {
	case TabSrvlog:
		a.srvlog.CloseDetail()
	case TabApplog:
		a.applog.CloseDetail()
	case TabNetlog:
		a.netlog.CloseDetail()
	case TabDashboard, TabHosts, TabNotifications, TabSettings:
	}
}

// Metadata loaders.

func (a *App) loadSrvlogMeta() tea.Cmd {
	c := a.client
	return func() tea.Msg {
		ctx := context.Background()
		hostList, err := c.SrvlogHosts(ctx)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		progList, err := c.SrvlogPrograms(ctx)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return MetaLoadedMsg{Feed: "srvlog", Hosts: hostList, Programs: progList}
	}
}

func (a *App) loadApplogMeta() tea.Cmd {
	c := a.client
	return func() tea.Msg {
		ctx := context.Background()
		svcList, err := c.AppLogServices(ctx)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		compList, err := c.AppLogComponents(ctx)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		hostList, err := c.AppLogHosts(ctx)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return MetaLoadedMsg{Feed: "applog", Services: svcList, Components: compList, Hosts: hostList}
	}
}

func (a *App) loadNetlogMeta() tea.Cmd {
	c := a.client
	return func() tea.Msg {
		ctx := context.Background()
		hostList, err := c.NetlogHosts(ctx)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		progList, err := c.NetlogPrograms(ctx)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return MetaLoadedMsg{Feed: "netlog", Hosts: hostList, Programs: progList}
	}
}

// SSE drain helpers per event type.

func drainSrvlogSSE(stream *client.SSEStream, maxEvents int) []client.SrvlogEvent {
	var events []client.SrvlogEvent
	for range maxEvents {
		select {
		case evt, ok := <-stream.Events():
			if !ok {
				return events
			}
			var e client.SrvlogEvent
			if err := json.Unmarshal(evt.Data, &e); err == nil {
				events = append(events, e)
			}
		default:
			return events
		}
	}
	return events
}

func drainApplogSSE(stream *client.SSEStream, maxEvents int) []client.AppLogEvent {
	var events []client.AppLogEvent
	for range maxEvents {
		select {
		case evt, ok := <-stream.Events():
			if !ok {
				return events
			}
			var e client.AppLogEvent
			if err := json.Unmarshal(evt.Data, &e); err == nil {
				events = append(events, e)
			}
		default:
			return events
		}
	}
	return events
}

func drainNetlogSSE(stream *client.SSEStream, maxEvents int) []client.SrvlogEvent {
	var events []client.SrvlogEvent
	for range maxEvents {
		select {
		case evt, ok := <-stream.Events():
			if !ok {
				return events
			}
			var e client.SrvlogEvent
			if err := json.Unmarshal(evt.Data, &e); err == nil {
				events = append(events, e)
			}
		default:
			return events
		}
	}
	return events
}
