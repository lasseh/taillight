package component

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/theme"
)

// Tab represents a single tab with an ID, label, and accent color.
type Tab struct {
	ID    int
	Label string
	Color color.Color // accent color for this tab
}

// TabBar renders a horizontal tab bar with the Taillight logo, tabs, and a
// thin separator line below — matching the web GUI header style.
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

// View renders the tab bar + separator line at the given width.
func (t *TabBar) View(width int) string {
	// Logo.
	logo := theme.Logo.Render("[Taillight]")

	// Separator between logo and tabs.
	sep := lipgloss.NewStyle().
		Foreground(theme.ColorGutter).
		Background(theme.ColorBGDark).
		Render("  ")

	// Render tabs.
	var tabParts []string
	tabParts = append(tabParts, logo, sep)

	for _, tab := range t.tabs {
		if tab.ID == t.active {
			// Active: highlighted bg + accent color text.
			style := lipgloss.NewStyle().
				Foreground(tab.Color).
				Background(theme.ColorBGHighlight).
				Bold(true).
				Padding(0, 1)
			tabParts = append(tabParts, style.Render(tab.Label))
		} else {
			// Inactive: dimmed accent color.
			style := lipgloss.NewStyle().
				Foreground(theme.ColorComment).
				Background(theme.ColorBGDark).
				Padding(0, 1)
			tabParts = append(tabParts, style.Render(tab.Label))
		}
	}

	bar := lipgloss.JoinHorizontal(lipgloss.Bottom, tabParts...)

	// Fill remaining width.
	barWidth := lipgloss.Width(bar)
	fill := lipgloss.NewStyle().
		Background(theme.ColorBGDark).
		Width(max(0, width-barWidth)).
		Render("")
	tabLine := lipgloss.JoinHorizontal(lipgloss.Bottom, bar, fill)

	// Thin separator line below the tab bar (like the web GUI's border-bottom).
	separator := lipgloss.NewStyle().
		Foreground(theme.ColorBorder).
		Render(strings.Repeat("─", width))

	return lipgloss.JoinVertical(lipgloss.Left, tabLine, separator)
}
