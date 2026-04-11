// Package settings implements the settings/connection info view.
package settings

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/theme"
)

// Model is the settings view.
type Model struct {
	serverURL string
	username  string
	width     int
	height    int
}

// New creates a new settings model.
func New(serverURL, username string) Model {
	return Model{
		serverURL: serverURL,
		username:  username,
	}
}

// SetSize updates dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Update handles messages (currently no-op).
func (m Model) Update(_ tea.Msg) (Model, tea.Cmd) {
	return m, nil
}

// View renders the settings display.
func (m *Model) View() string {
	var lines []string

	lines = append(lines, theme.TableHeader.Render("  Connection"))
	lines = append(lines, "")

	kv := func(k, v string) string {
		return fmt.Sprintf("  %s  %s",
			theme.DetailKey.Render(k),
			theme.DetailValue.Render(v),
		)
	}

	lines = append(lines, kv("Server URL:", m.serverURL))
	if m.username != "" {
		lines = append(lines, kv("User:      ", m.username))
	} else {
		lines = append(lines, kv("Auth:      ", "API Key"))
	}

	lines = append(lines, "")
	lines = append(lines, theme.TableHeader.Render("  Keyboard Shortcuts"))
	lines = append(lines, "")
	lines = append(lines, kv("1-5       ", "Switch tabs"))
	lines = append(lines, kv("/         ", "Focus search filter"))
	lines = append(lines, kv("j/k       ", "Navigate up/down"))
	lines = append(lines, kv("Enter     ", "Open detail panel"))
	lines = append(lines, kv("Esc       ", "Close detail / unfocus"))
	lines = append(lines, kv("?         ", "Toggle help"))
	lines = append(lines, kv("q         ", "Quit"))

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return theme.Base.Width(m.width).Render(content)
}
