package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/lasseh/taillight/internal/tui/component"
	"github.com/lasseh/taillight/internal/tui/view/applog"
	"github.com/lasseh/taillight/internal/tui/view/netlog"
	"github.com/lasseh/taillight/internal/tui/view/srvlog"
)

// handleKey routes a key press through help overlay → filter popup → filter
// input → global keys → active view, in that priority order.
func (a *App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Help overlay takes priority — any key dismisses it.
	if a.showHelp {
		if key.Matches(msg, a.keys.Help) || key.Matches(msg, a.keys.Escape) {
			a.showHelp = false
		}
		return a, nil
	}

	// Filter popup absorbs all keys while open. The popup's own Update
	// handles Esc while editing a field (cancel the edit); Esc in navigation
	// mode closes the popup entirely.
	if a.focus == FocusPopup && a.popup != nil {
		editing := a.popup.Editing()
		if key.Matches(msg, a.keys.Escape) && !editing {
			a.closePopup()
			return a, nil
		}
		updated, cmd := a.popup.Update(msg)
		a.popup = &updated
		return a, cmd
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
	case key.Matches(msg, a.keys.Filter):
		a.openPopup()
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

// openPopup initialises a filter popup for the active view. Dashboard has no
// filter, so the call is a no-op on that tab.
func (a *App) openPopup() {
	fields := a.activeFilterFields()
	if fields == nil {
		return
	}
	p := component.NewFilterPopup(fields)
	p.SetSize(a.width, a.height)
	a.popup = &p
	a.focus = FocusPopup
}

// closePopup dismisses the popup without applying filter values.
func (a *App) closePopup() {
	a.popup = nil
	a.focus = FocusTable
}

// activeFilterFields builds the popup fields for the active tab's filter.
// Returns nil when the tab has no associated filter (dashboard).
func (a *App) activeFilterFields() []component.Field {
	switch a.activeTab {
	case TabSrvlog:
		if f := srvlog.Filter(&a.srvlog); f != nil {
			return f.PopupFields()
		}
	case TabNetlog:
		if f := netlog.Filter(&a.netlog); f != nil {
			return f.PopupFields()
		}
	case TabApplog:
		if f := applog.Filter(&a.applog); f != nil {
			return f.PopupFields()
		}
	case TabDashboard:
		return nil
	}
	return nil
}

// applyPopupFilter commits values to the active view's filter and restarts
// that tab's SSE stream so the server-side filters take effect immediately.
func (a *App) applyPopupFilter(values map[string]string) tea.Cmd {
	a.closePopup()
	switch a.activeTab {
	case TabSrvlog:
		if f := srvlog.Filter(&a.srvlog); f != nil {
			f.ApplyPopupValues(values)
		}
		return a.restartStream(TabSrvlog)
	case TabNetlog:
		if f := netlog.Filter(&a.netlog); f != nil {
			f.ApplyPopupValues(values)
		}
		return a.restartStream(TabNetlog)
	case TabApplog:
		if f := applog.Filter(&a.applog); f != nil {
			f.ApplyPopupValues(values)
		}
		return a.restartStream(TabApplog)
	case TabDashboard:
	}
	return nil
}

// restartStream closes the active stream for the tab (if any) and starts a
// fresh one with the current filter parameters.
func (a *App) restartStream(tab TabID) tea.Cmd {
	switch tab {
	case TabSrvlog:
		if a.srvlogStream != nil {
			a.srvlogStream.Close()
			a.srvlogStream = nil
		}
	case TabApplog:
		if a.applogStream != nil {
			a.applogStream.Close()
			a.applogStream = nil
		}
	case TabNetlog:
		if a.netlogStream != nil {
			a.netlogStream.Close()
			a.netlogStream = nil
		}
	case TabDashboard:
		return nil
	}
	return a.startStream(tab)
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
	case TabDashboard:
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
	case TabDashboard:
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
	case TabDashboard:
	}
}
