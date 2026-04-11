// Package component provides shared TUI components.
package component

import (
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/theme"
)

// StatusBar renders the bottom status bar.
type StatusBar struct {
	connected  bool
	eventCount int
	errMsg     string
}

// NewStatusBar creates a new status bar.
func NewStatusBar() StatusBar {
	return StatusBar{}
}

// SetConnected updates the connection state.
func (s *StatusBar) SetConnected(v bool) {
	s.connected = v
}

// AddEvents increments the event counter.
func (s *StatusBar) AddEvents(n int) {
	s.eventCount += n
}

// SetError sets or clears the error message.
func (s *StatusBar) SetError(msg string) {
	s.errMsg = msg
}

// View renders the status bar at the given width.
func (s *StatusBar) View(width int) string {
	// Connection indicator.
	var connStr string
	if s.connected {
		connStr = theme.StatusConnected.Render("● Connected")
	} else {
		connStr = theme.StatusDisconnected.Render("● Disconnected")
	}

	// Event count.
	countStr := fmt.Sprintf("%d events", s.eventCount)

	// Error.
	var errStr string
	if s.errMsg != "" {
		errStr = lipgloss.NewStyle().Foreground(theme.ColorRed).Render(" | " + s.errMsg)
	}

	// Help hint.
	helpStr := theme.Help.Render("?help")

	left := connStr + " | " + countStr + errStr
	right := helpStr

	gap := max(0, width-lipgloss.Width(left)-lipgloss.Width(right)-2)
	fill := lipgloss.NewStyle().Width(gap).Render("")

	return theme.StatusBar.Width(width).Render(
		lipgloss.JoinHorizontal(lipgloss.Bottom, left, fill, right),
	)
}
