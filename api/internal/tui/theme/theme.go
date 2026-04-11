// Package theme defines the Tokyo Night color palette and pre-built lipgloss
// styles for the Taillight TUI.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Tokyo Night color palette.
var (
	ColorBG          = lipgloss.Color("#1a1b26")
	ColorBGDark      = lipgloss.Color("#16161e")
	ColorBGHighlight = lipgloss.Color("#292e42")
	ColorFG          = lipgloss.Color("#c0caf5")
	ColorFGDark      = lipgloss.Color("#a9b1d6")
	ColorComment     = lipgloss.Color("#565f89")
	ColorBorder      = lipgloss.Color("#3b4261")
	ColorRed         = lipgloss.Color("#f7768e")
	ColorOrange      = lipgloss.Color("#ff9e64")
	ColorYellow      = lipgloss.Color("#e0af68")
	ColorGreen       = lipgloss.Color("#9ece6a")
	ColorCyan        = lipgloss.Color("#7dcfff")
	ColorBlue        = lipgloss.Color("#7aa2f7")
	ColorMagenta     = lipgloss.Color("#bb9af7")
	ColorPurple      = lipgloss.Color("#9d7cd8")
)

// Severity colors map syslog severity codes (0-7) to Tokyo Night colors.
var severityColors = [8]color.Color{
	ColorRed,     // 0: emergency
	ColorRed,     // 1: alert
	ColorOrange,  // 2: critical
	ColorOrange,  // 3: error
	ColorYellow,  // 4: warning
	ColorBlue,    // 5: notice
	ColorGreen,   // 6: info
	ColorComment, // 7: debug
}

// SeverityColor returns the Tokyo Night color for a syslog severity code.
func SeverityColor(severity int) color.Color {
	if severity < 0 || severity > 7 {
		return ColorComment
	}
	return severityColors[severity]
}

// AppLogLevelColor returns the Tokyo Night color for an applog level string.
func AppLogLevelColor(level string) color.Color {
	switch level {
	case "FATAL":
		return ColorRed
	case "ERROR":
		return ColorOrange
	case "WARN":
		return ColorYellow
	case "INFO":
		return ColorGreen
	case "DEBUG":
		return ColorComment
	default:
		return ColorComment
	}
}

// Pre-built styles.
var (
	Base = lipgloss.NewStyle().
		Foreground(ColorFG).
		Background(ColorBG)

	TabBar = lipgloss.NewStyle().
		Background(ColorBGDark).
		Padding(0, 1)

	ActiveTab = lipgloss.NewStyle().
			Foreground(ColorBG).
			Background(ColorBlue).
			Bold(true).
			Padding(0, 1)

	InactiveTab = lipgloss.NewStyle().
			Foreground(ColorComment).
			Background(ColorBGDark).
			Padding(0, 1)

	StatusBar = lipgloss.NewStyle().
			Foreground(ColorFG).
			Background(ColorBGDark).
			Padding(0, 1)

	StatusConnected = lipgloss.NewStyle().
			Foreground(ColorGreen).
			Bold(true)

	StatusDisconnected = lipgloss.NewStyle().
				Foreground(ColorRed).
				Bold(true)

	TableHeader = lipgloss.NewStyle().
			Foreground(ColorBlue).
			Background(ColorBGHighlight).
			Bold(true)

	TableSelected = lipgloss.NewStyle().
			Foreground(ColorFG).
			Background(ColorBGHighlight).
			Bold(true)

	TableCell = lipgloss.NewStyle().
			Foreground(ColorFG)

	FilterLabel = lipgloss.NewStyle().
			Foreground(ColorComment)

	FilterInput = lipgloss.NewStyle().
			Foreground(ColorFG).
			Background(ColorBGDark)

	DetailKey = lipgloss.NewStyle().
			Foreground(ColorCyan).
			Bold(true).
			Width(14)

	DetailValue = lipgloss.NewStyle().
			Foreground(ColorFG)

	Help = lipgloss.NewStyle().
		Foreground(ColorComment)

	Border = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder)

	Hostname = lipgloss.NewStyle().
			Foreground(ColorCyan)

	Program = lipgloss.NewStyle().
		Foreground(ColorPurple)

	Timestamp = lipgloss.NewStyle().
			Foreground(ColorComment)

	Message = lipgloss.NewStyle().
		Foreground(ColorFG)

	Comment = lipgloss.NewStyle().
		Foreground(ColorComment)
)

// SeverityStyle returns a lipgloss style with the foreground set to the
// severity color.
func SeverityStyle(severity int) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(SeverityColor(severity))
}

// AppLogLevelStyle returns a lipgloss style with the foreground set to the
// applog level color.
func AppLogLevelStyle(level string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(AppLogLevelColor(level))
}
