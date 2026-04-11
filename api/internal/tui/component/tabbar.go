package component

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/theme"
)

// Tab represents a single tab with an ID, label, and accent color.
type Tab struct {
	ID    int
	Label string
	Color color.Color // accent color for this tab
}

// TabBar renders a horizontal tab bar with the Taillight logo and tabs.
type TabBar struct {
	tabs   []Tab
	active int
}

// NewTabBar creates a tab bar from the given tabs.
func NewTabBar(tabs []Tab) TabBar {
	return TabBar{tabs: tabs}
}

// SetActive sets the active tab by ID.
func (t *TabBar) SetActive(id int) {
	t.active = id
}

// View renders the tab bar at the given width.
func (t *TabBar) View(width int) string {
	// Logo.
	logo := theme.Logo.Render("[Taillight]")

	// Separator.
	sep := lipgloss.NewStyle().
		Foreground(theme.ColorGutter).
		Background(theme.ColorBGDark).
		Render("  ")

	// Tabs.
	var tabParts []string
	tabParts = append(tabParts, logo, sep)

	for _, tab := range t.tabs {
		if tab.ID == t.active {
			style := theme.ActiveTab.Foreground(tab.Color)
			tabParts = append(tabParts, style.Render(tab.Label))
		} else {
			// Inactive tabs: dimmed version of their accent color.
			style := theme.InactiveTab
			tabParts = append(tabParts, style.Render(tab.Label))
		}
	}

	bar := lipgloss.JoinHorizontal(lipgloss.Bottom, tabParts...)

	// Fill remaining width with dark background.
	barWidth := lipgloss.Width(bar)
	fill := theme.TabBarBG.Width(max(0, width-barWidth)).Render("")
	return lipgloss.JoinHorizontal(lipgloss.Bottom, bar, fill)
}
