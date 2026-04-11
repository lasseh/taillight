package tui

import (
	"charm.land/bubbles/v2/key"
)

// TabID identifies a view tab.
type TabID int

const (
	TabSrvlog TabID = iota
	TabApplog
	TabNetlog
	TabDashboard
	TabHosts
	TabNotifications
	TabSettings
)

// TabName returns the display name for a tab.
func TabName(id TabID) string {
	switch id {
	case TabSrvlog:
		return "SRVLOG"
	case TabApplog:
		return "APPLOG"
	case TabNetlog:
		return "NETLOG"
	case TabDashboard:
		return "DASHBOARD"
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
			key.WithHelp("1", "srvlog"),
		),
		Tab2: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "applog"),
		),
		Tab3: key.NewBinding(
			key.WithKeys("3"),
			key.WithHelp("3", "netlog"),
		),
		Tab4: key.NewBinding(
			key.WithKeys("4"),
			key.WithHelp("4", "dashboard"),
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
