package component

import (
	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/theme"
)

// Tab represents a single tab with an ID and label.
type Tab struct {
	ID    int
	Label string
}

// TabBar renders a horizontal tab bar with one active tab.
type TabBar struct {
	tabs   []Tab
	active int
}

// NewTabBar creates a tab bar from the given tabs. The first tab is active by
// default.
func NewTabBar(tabs []Tab) TabBar {
	return TabBar{tabs: tabs}
}

// SetActive sets the active tab by ID.
func (t *TabBar) SetActive(id int) {
	t.active = id
}

// View renders the tab bar at the given width.
func (t *TabBar) View(width int) string {
	rendered := make([]string, 0, len(t.tabs))
	for _, tab := range t.tabs {
		style := theme.InactiveTab
		if tab.ID == t.active {
			style = theme.ActiveTab
		}
		rendered = append(rendered, style.Render(tab.Label))
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Bottom, rendered...)
	// Fill remaining width.
	fill := theme.TabBar.Width(max(0, width-lipgloss.Width(bar))).Render("")
	return lipgloss.JoinHorizontal(lipgloss.Bottom, bar, fill)
}
