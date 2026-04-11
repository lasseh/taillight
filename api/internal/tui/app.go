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
	statusBar component.StatusBar
	helpModel help.Model
	showHelp  bool

	// SSE stream.
	sseStream *client.SSEStream

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

	return &App{
		cfg:       cfg,
		client:    c,
		keys:      DefaultKeyMap(),
		activeTab: TabSrvlog,
		focus:     FocusTable,
		srvlog:    srvlog.New(cfg.BufferSize, cfg.TimeFormat),
		statusBar: component.NewStatusBar(),
		helpModel: help.New(),
	}
}

// Init starts background tasks: SSE stream, metadata loading.
func (a *App) Init() tea.Cmd {
	return tea.Batch(
		a.startSSEStream(),
		a.loadMeta(),
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
		a.srvlog.SetSize(msg.Width, a.contentHeight())
		return a, nil

	case tea.KeyPressMsg:
		// Help overlay takes priority.
		if a.showHelp {
			if key.Matches(msg, a.keys.Help) || key.Matches(msg, a.keys.Escape) {
				a.showHelp = false
				return a, nil
			}
			return a, nil
		}

		// Filter input takes priority when focused.
		if a.focus == FocusFilter {
			switch {
			case key.Matches(msg, a.keys.Escape):
				a.focus = FocusTable
				a.srvlog.BlurFilter()
				return a, nil
			default:
				var cmd tea.Cmd
				a.srvlog, cmd = a.srvlog.UpdateFilter(msg)
				cmds = append(cmds, cmd)
				return a, tea.Batch(cmds...)
			}
		}

		// Global keys.
		switch {
		case key.Matches(msg, a.keys.Quit):
			return a, tea.Quit
		case key.Matches(msg, a.keys.Help):
			a.showHelp = !a.showHelp
			return a, nil
		case key.Matches(msg, a.keys.Search), key.Matches(msg, a.keys.ToggleFocus):
			a.focus = FocusFilter
			a.srvlog.FocusFilter()
			return a, nil
		case key.Matches(msg, a.keys.Tab1):
			a.switchTab(TabSrvlog)
			return a, nil
		case key.Matches(msg, a.keys.Escape):
			if a.focus == FocusDetail {
				a.focus = FocusTable
				a.srvlog.CloseDetail()
				return a, nil
			}
			return a, nil
		}

		// Delegate to active view.
		if a.activeTab == TabSrvlog {
			var cmd tea.Cmd
			a.srvlog, cmd = a.srvlog.UpdateTable(msg)
			cmds = append(cmds, cmd)

			// Check if detail was opened.
			if a.srvlog.DetailOpen() {
				a.focus = FocusDetail
			}
		}
		return a, tea.Batch(cmds...)

	case SSETickMsg:
		// Drain the SSE channel and deliver events in batch.
		if a.sseStream != nil {
			a.connected = a.sseStream.Connected()
			a.statusBar.SetConnected(a.connected)

			events := drainSSE(a.sseStream, 100)
			if len(events) > 0 {
				a.srvlog.PushEvents(events)
				a.statusBar.AddEvents(len(events))
			}
		}
		cmds = append(cmds, a.sseTick())
		return a, tea.Batch(cmds...)

	case MetaLoadedMsg:
		a.srvlog.SetMeta(msg.Hosts, msg.Programs)
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

// View renders the full TUI.
func (a *App) View() tea.View {
	if a.width == 0 || a.height == 0 {
		return tea.NewView("Initializing...")
	}

	var sections []string

	// Tab bar.
	sections = append(sections, a.renderTabBar())

	// Main content.
	switch a.activeTab {
	case TabSrvlog:
		sections = append(sections, a.srvlog.View())
	default:
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

func (a *App) renderTabBar() string {
	tabs := []TabID{TabSrvlog, TabApplog, TabNetlog, TabDashboard, TabHosts}
	rendered := make([]string, 0, len(tabs))
	for _, tab := range tabs {
		style := theme.InactiveTab
		if tab == a.activeTab {
			style = theme.ActiveTab
		}
		rendered = append(rendered, style.Render(TabName(tab)))
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Bottom, rendered...)
	// Fill remaining width with background.
	fill := theme.TabBar.Width(max(0, a.width-lipgloss.Width(bar))).Render("")
	return lipgloss.JoinHorizontal(lipgloss.Bottom, bar, fill)
}

func (a *App) contentHeight() int {
	// Total height minus tab bar (1) and status bar (1).
	h := max(a.height-2, 1)
	return h
}

func (a *App) switchTab(tab TabID) {
	a.activeTab = tab
	a.focus = FocusTable
}

// startSSEStream begins the SSE connection for the active feed.
func (a *App) startSSEStream() tea.Cmd {
	return func() tea.Msg {
		filter := a.srvlog.Filter()
		a.sseStream = client.NewSSEStream(a.client, "/api/v1/srvlog/stream", filter.Params(), 0)
		return SSEConnectedMsg{Feed: "srvlog"}
	}
}

// sseTick returns a command that fires an SSETickMsg after the batch interval.
func (a *App) sseTick() tea.Cmd {
	return tea.Tick(a.cfg.BatchInterval, func(time.Time) tea.Msg {
		return SSETickMsg{}
	})
}

// loadMeta fetches metadata (hosts, programs) from the API.
func (a *App) loadMeta() tea.Cmd {
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
		return MetaLoadedMsg{
			Feed:     "srvlog",
			Hosts:    hosts,
			Programs: programs,
		}
	}
}

// drainSSE reads up to maxEvents from the SSE stream channel without blocking.
func drainSSE(stream *client.SSEStream, maxEvents int) []client.SrvlogEvent {
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
