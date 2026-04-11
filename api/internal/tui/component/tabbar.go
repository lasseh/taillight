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
	Right bool        // render on the right side of the bar
}

// TabBar renders a horizontal tab bar with the Taillight logo, primary tabs
// on the left, and secondary tabs on the right — matching the web GUI header.
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
	bg := lipgloss.NewStyle().Background(theme.ColorBGDark)

	// Logo.
	logo := theme.Logo.Background(theme.ColorBGDark).Render(" [Taillight] ")

	// Split tabs into left (primary) and right (secondary).
	var leftParts, rightParts []string
	for _, tab := range t.tabs {
		rendered := t.renderTab(tab)
		if tab.Right {
			rightParts = append(rightParts, rendered)
		} else {
			leftParts = append(leftParts, rendered)
		}
	}

	left := lipgloss.JoinHorizontal(lipgloss.Bottom,
		append([]string{logo}, leftParts...)...)
	right := lipgloss.JoinHorizontal(lipgloss.Bottom, rightParts...)

	// Fill the gap between left and right.
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gapW := max(1, width-leftW-rightW)
	gap := bg.Width(gapW).Render("")

	tabLine := lipgloss.JoinHorizontal(lipgloss.Bottom, left, gap, right)

	// Thin separator line below.
	separator := lipgloss.NewStyle().
		Foreground(theme.ColorBorder).
		Render(strings.Repeat("─", width))

	return lipgloss.JoinVertical(lipgloss.Left, tabLine, separator)
}

func (t *TabBar) renderTab(tab Tab) string {
	if tab.ID == t.active {
		return lipgloss.NewStyle().
			Foreground(tab.Color).
			Background(theme.ColorBGHighlight).
			Bold(true).
			Padding(0, 1).
			Render(tab.Label)
	}
	return lipgloss.NewStyle().
		Foreground(theme.ColorComment).
		Background(theme.ColorBGDark).
		Padding(0, 1).
		Render(tab.Label)
}
