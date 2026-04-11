package tui

import (
	"charm.land/bubbles/v2/key"
)

// TabID identifies a view tab. Order matches the web GUI navigation.
type TabID int

const (
	TabDashboard TabID = iota
	TabNetlog
	TabSrvlog
	TabApplog
	TabHosts
	TabNotifications
	TabSettings
)

// AllTabs lists tabs in display order.
var AllTabs = []TabID{
	TabDashboard, TabNetlog, TabSrvlog, TabApplog,
	TabHosts, TabNotifications, TabSettings,
}

// TabName returns the display name for a tab.
func TabName(id TabID) string {
	switch id {
	case TabDashboard:
		return "DASHBOARD"
	case TabNetlog:
		return "NETLOG"
	case TabSrvlog:
		return "SRVLOG"
	case TabApplog:
		return "APPLOG"
	case TabHosts:
		return "HOSTS"
	case TabNotifications:
		return "ALERTS"
	case TabSettings:
		return "SETTINGS"
	default:
		return ""
	}
}

// FocusTarget identifies which component receives keyboard input.
type FocusTarget int

const (
	FocusTable FocusTarget = iota
	FocusFilter
	FocusDetail
	FocusHelp
)

// KeyMap defines global key bindings.
type KeyMap struct {
	Quit        key.Binding
	Help        key.Binding
	Search      key.Binding
	Tab1        key.Binding
	Tab2        key.Binding
	Tab3        key.Binding
	Tab4        key.Binding
	Tab5        key.Binding
	Tab6        key.Binding
	Tab7        key.Binding
	TabNext     key.Binding
	TabPrev     key.Binding
	Escape      key.Binding
	ToggleFocus key.Binding
}

// DefaultKeyMap returns the default global key bindings.
// Numbers match the tab display order: 1=dashboard, 2=netlog, 3=srvlog, 4=applog.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		Tab1: key.NewBinding(
			key.WithKeys("1"),
			key.WithHelp("1", "dashboard"),
		),
		Tab2: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "netlog"),
		),
		Tab3: key.NewBinding(
			key.WithKeys("3"),
			key.WithHelp("3", "srvlog"),
		),
		Tab4: key.NewBinding(
			key.WithKeys("4"),
			key.WithHelp("4", "applog"),
		),
		Tab5: key.NewBinding(
			key.WithKeys("5"),
			key.WithHelp("5", "hosts"),
		),
		Tab6: key.NewBinding(
			key.WithKeys("6"),
			key.WithHelp("6", "alerts"),
		),
		Tab7: key.NewBinding(
			key.WithKeys("7"),
			key.WithHelp("7", "settings"),
		),
		TabNext: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next tab"),
		),
		TabPrev: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev tab"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		ToggleFocus: key.NewBinding(
			key.WithKeys("ctrl+f"),
			key.WithHelp("ctrl+f", "filter"),
		),
	}
}

// ShortHelp returns the key bindings for the short help view.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Quit, k.Help, k.Search, k.TabNext}
}

// FullHelp returns the key bindings for the full help view.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Quit, k.Help, k.Search, k.Escape},
		{k.Tab1, k.Tab2, k.Tab3, k.Tab4, k.Tab5, k.Tab6, k.Tab7},
		{k.TabNext, k.TabPrev, k.ToggleFocus},
	}
}
