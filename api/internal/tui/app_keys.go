package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// handleKey routes a key press through help overlay → filter input →
// global keys → active view, in that priority order.
func (a *App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Help overlay takes priority — any key dismisses it.
	if a.showHelp {
		if key.Matches(msg, a.keys.Help) || key.Matches(msg, a.keys.Escape) {
			a.showHelp = false
		}
		return a, nil
	}

	// Filter input takes priority when focused so number keys (1-7) and
	// vim navigation don't get hijacked while the user is typing a search.
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
		// Release SSE goroutines and TCP connections before quitting.
		// Critical for the wish SSH server where each session would
		// otherwise leak streams until the parent process exits.
		a.Cleanup()
		return a, tea.Quit
	case key.Matches(msg, a.keys.Help):
		a.showHelp = true
		return a, nil
	case key.Matches(msg, a.keys.Search), key.Matches(msg, a.keys.ToggleFocus):
		a.focus = FocusFilter
		// IMPORTANT: forward the textinput's focus cmd (cursor blink)
		// to bubbletea — without it the input won't respond properly.
		cmd := a.focusActiveFilter()
		return a, cmd
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

// Active view delegation helpers.

// focusActiveFilter activates the active view's filter input and returns
// any tea.Cmd from the underlying textinput (e.g., cursor blink) which the
// caller must forward to bubbletea.
func (a *App) focusActiveFilter() tea.Cmd {
	switch a.activeTab {
	case TabSrvlog:
		return a.srvlog.FocusFilter()
	case TabApplog:
		return a.applog.FocusFilter()
	case TabNetlog:
		return a.netlog.FocusFilter()
	case TabDashboard, TabHosts, TabNotifications, TabSettings:
	}
	return nil
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
