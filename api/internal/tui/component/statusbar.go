// Package component provides shared TUI components.
package component

import (
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/theme"
)

// StatusBar renders a multi-segment status bar pinned to the terminal bottom.
type StatusBar struct {
	connected bool
	newEvents int // events arrived while scrolled up
	errMsg    string
	filters   []string // active filter pills
}

// NewStatusBar creates a new status bar.
func NewStatusBar() StatusBar {
	return StatusBar{}
}

// SetConnected updates the connection state.
func (s *StatusBar) SetConnected(v bool) {
	s.connected = v
}

// SetNewEvents sets the count of events arrived while user is scrolled up.
func (s *StatusBar) SetNewEvents(n int) {
	s.newEvents = n
}

// SetError sets or clears the error message.
func (s *StatusBar) SetError(msg string) {
	s.errMsg = msg
}

// SetFilters sets the active filter pill labels.
func (s *StatusBar) SetFilters(filters []string) {
	s.filters = filters
}

// Segment styles.
var (
	segConn = lipgloss.NewStyle().
		Padding(0, 1)

	segFilter = lipgloss.NewStyle().
			Foreground(theme.ColorTeal).
			Background(lipgloss.Color("#1e2030")).
			Padding(0, 1)

	segNew = lipgloss.NewStyle().
		Foreground(theme.ColorBGDark).
		Background(theme.ColorYellow).
		Bold(true).
		Padding(0, 1)

	segErr = lipgloss.NewStyle().
		Foreground(theme.ColorBGDark).
		Background(theme.ColorRed).
		Bold(true).
		Padding(0, 1)

	segHelp = lipgloss.NewStyle().
		Foreground(theme.ColorComment).
		Background(lipgloss.Color("#16161e")).
		Padding(0, 1)

	segFill = lipgloss.NewStyle().
		Background(lipgloss.Color("#16161e"))
)

// View renders the multi-segment status bar at the given width.
func (s *StatusBar) View(width int) string {
	var segments []string

	// Connection segment.
	if s.connected {
		segments = append(segments, segConn.
			Foreground(theme.ColorBGDark).
			Background(theme.ColorGreen).
			Render("● LIVE"))
	} else {
		segments = append(segments, segConn.
			Foreground(theme.ColorBGDark).
			Background(theme.ColorRed).
			Render("● OFFLINE"))
	}

	// Error segment.
	if s.errMsg != "" {
		segments = append(segments, segErr.Render(s.errMsg))
	}

	// New events indicator (when scrolled up).
	if s.newEvents > 0 {
		segments = append(segments, segNew.Render(
			fmt.Sprintf("▼ %d new", s.newEvents)))
	}

	// Active filter pills.
	for _, f := range s.filters {
		segments = append(segments, segFilter.Render(f))
	}

	// Right side: help.
	helpSeg := segHelp.Render("/ search  ? help  q quit")

	// Calculate fill.
	leftWidth := 0
	for _, seg := range segments {
		leftWidth += lipgloss.Width(seg)
	}
	rightWidth := lipgloss.Width(helpSeg)
	fillWidth := max(0, width-leftWidth-rightWidth)

	fill := segFill.Width(fillWidth).Render("")

	segments = append(segments, fill, helpSeg)
	return lipgloss.JoinHorizontal(lipgloss.Bottom, segments...)
}
