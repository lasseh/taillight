package component

import (
	"fmt"

	"github.com/lasseh/taillight/internal/tui/theme"
)

// SeverityBadge renders a colored severity label.
func SeverityBadge(severity int, label string) string {
	return theme.SeverityStyle(severity).Render(fmt.Sprintf("%-8s", label))
}

// AppLogLevelBadge renders a colored applog level label.
func AppLogLevelBadge(level string) string {
	return theme.AppLogLevelStyle(level).Render(fmt.Sprintf("%-6s", level))
}
