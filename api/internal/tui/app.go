package tui

import (
	"context"
	"encoding/json"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/client"
	"github.com/lasseh/taillight/internal/tui/component"
	"github.com/lasseh/taillight/internal/tui/theme"
	"github.com/lasseh/taillight/internal/tui/view/applog"
	"github.com/lasseh/taillight/internal/tui/view/netlog"
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
	srvlog    srvlog.Model
	applog    applog.Model
	netlog    netlog.Model
	tabBar    component.TabBar
	statusBar component.StatusBar
	helpModel help.Model
	showHelp  bool

	// Per-tab SSE streams. nil when not active.
	srvlogStream *client.SSEStream
	applogStream *client.SSEStream
	netlogStream *client.SSEStream

	// State.
	connected bool
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
		{ID: int(TabSrvlog), Label: "SRVLOG"},
		{ID: int(TabApplog), Label: "APPLOG"},
		{ID: int(TabNetlog), Label: "NETLOG"},
		{ID: int(TabDashboard), Label: "DASHBOARD"},
		{ID: int(TabHosts), Label: "HOSTS"},
	}

	return &App{
		cfg:       cfg,
		client:    c,
		keys:      DefaultKeyMap(),
		activeTab: TabSrvlog,
		focus:     FocusTable,
		srvlog:    srvlog.New(cfg.BufferSize, cfg.TimeFormat),
		applog:    applog.New(cfg.BufferSize, cfg.TimeFormat),
		netlog:    netlog.New(cfg.BufferSize, cfg.TimeFormat),
		tabBar:    component.NewTabBar(tabs),
		statusBar: component.NewStatusBar(),
		helpModel: help.New(),
	}
}

// Init starts background tasks: SSE stream for the initial tab, metadata loading.
func (a *App) Init() tea.Cmd {
	return tea.Batch(
		a.startStream(TabSrvlog),
		a.loadSrvlogMeta(),
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
		if key.Matches(msg, a.keys.Escape) {
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
		cmd := a.switchTab(TabSrvlog)
		return a, cmd
	case key.Matches(msg, a.keys.Tab2):
		cmd := a.switchTab(TabApplog)
		return a, cmd
	case key.Matches(msg, a.keys.Tab3):
		cmd := a.switchTab(TabNetlog)
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

// View renders the full TUI.
func (a *App) View() tea.View {
	if a.width == 0 || a.height == 0 {
		return tea.NewView("Initializing...")
	}

	var sections []string

	// Tab bar.
	sections = append(sections, a.tabBar.View(a.width))

	// Main content.
	switch a.activeTab {
	case TabSrvlog:
		sections = append(sections, a.srvlog.View())
	case TabApplog:
		sections = append(sections, a.applog.View())
	case TabNetlog:
		sections = append(sections, a.netlog.View())
	case TabDashboard, TabHosts:
		sections = append(sections, theme.Base.
			Width(a.width).
			Height(a.contentHeight()).
			Render("Coming soon..."))
	}

	// Help overlay.
	if a.showHelp {
		helpView := a.helpModel.FullHelpView(a.keys.FullHelp())
		sections = append(sections, theme.Help.Render(helpView))
	}

	// Status bar.
	sections = append(sections, a.statusBar.View(a.width))

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (a *App) contentHeight() int {
	return max(a.height-2, 1)
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
	case TabDashboard, TabHosts:
		// No stream needed for these views yet.
	}

	return tea.Batch(cmds...)
}

// startStream begins an SSE connection for the given feed.
func (a *App) startStream(tab TabID) tea.Cmd {
	return func() tea.Msg {
		switch tab {
		case TabSrvlog:
			a.srvlogStream = client.NewSSEStream(a.client, "/api/v1/srvlog/stream", a.srvlog.Filter().Params(), 0)
			return SSEConnectedMsg{Feed: "srvlog"}
		case TabApplog:
			a.applogStream = client.NewSSEStream(a.client, "/api/v1/applog/stream", a.applog.Filter().Params(), 0)
			return SSEConnectedMsg{Feed: "applog"}
		case TabNetlog:
			a.netlogStream = client.NewSSEStream(a.client, "/api/v1/netlog/stream", a.netlog.Filter().Params(), 0)
			return SSEConnectedMsg{Feed: "netlog"}
		default:
			return nil
		}
	}
}

// sseTick returns a command that fires an SSETickMsg after the batch interval.
func (a *App) sseTick() tea.Cmd {
	return tea.Tick(a.cfg.BatchInterval, func(time.Time) tea.Msg {
		return SSETickMsg{}
	})
}

// drainAllStreams reads events from all active SSE streams and pushes them to
// the corresponding views.
func (a *App) drainAllStreams() {
	anyConnected := false

	if a.srvlogStream != nil {
		if a.srvlogStream.Connected() {
			anyConnected = true
		}
		events := drainSrvlogSSE(a.srvlogStream, 100)
		if len(events) > 0 {
			a.srvlog.PushEvents(events)
			a.statusBar.AddEvents(len(events))
		}
	}

	if a.applogStream != nil {
		if a.applogStream.Connected() {
			anyConnected = true
		}
		events := drainApplogSSE(a.applogStream, 100)
		if len(events) > 0 {
			a.applog.PushEvents(events)
			a.statusBar.AddEvents(len(events))
		}
	}

	if a.netlogStream != nil {
		if a.netlogStream.Connected() {
			anyConnected = true
		}
		events := drainNetlogSSE(a.netlogStream, 100)
		if len(events) > 0 {
			a.netlog.PushEvents(events)
			a.statusBar.AddEvents(len(events))
		}
	}

	a.connected = anyConnected
	a.statusBar.SetConnected(a.connected)
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
	case TabDashboard, TabHosts:
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
	case TabDashboard, TabHosts:
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
	case TabDashboard, TabHosts:
	}
}

// Metadata loaders.

func (a *App) loadSrvlogMeta() tea.Cmd {
	c := a.client
	return func() tea.Msg {
		ctx := context.Background()
		hosts, err := c.SrvlogHosts(ctx)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		programs, err := c.SrvlogPrograms(ctx)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return MetaLoadedMsg{Feed: "srvlog", Hosts: hosts, Programs: programs}
	}
}

func (a *App) loadApplogMeta() tea.Cmd {
	c := a.client
	return func() tea.Msg {
		ctx := context.Background()
		services, err := c.AppLogServices(ctx)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		components, err := c.AppLogComponents(ctx)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		hosts, err := c.AppLogHosts(ctx)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return MetaLoadedMsg{Feed: "applog", Services: services, Components: components, Hosts: hosts}
	}
}

func (a *App) loadNetlogMeta() tea.Cmd {
	c := a.client
	return func() tea.Msg {
		ctx := context.Background()
		hosts, err := c.NetlogHosts(ctx)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		programs, err := c.NetlogPrograms(ctx)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return MetaLoadedMsg{Feed: "netlog", Hosts: hosts, Programs: programs}
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
