// Package theme defines the Tokyo Night color palette and pre-built lipgloss
// styles for the Taillight TUI.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Tokyo Night color palette — exact values from the web frontend CSS.
var (
	ColorBG          = lipgloss.Color("#1a1b26")
	ColorBGDark      = lipgloss.Color("#16161e")
	ColorBGHighlight = lipgloss.Color("#292e42")
	ColorBGHover     = lipgloss.Color("#24283b")
	ColorFG          = lipgloss.Color("#c0caf5")
	ColorFGDark      = lipgloss.Color("#a9b1d6")
	ColorComment     = lipgloss.Color("#565f89")
	ColorBorder      = lipgloss.Color("#292e42")
	ColorGutter      = lipgloss.Color("#3b4261")
	ColorRed         = lipgloss.Color("#f7768e")
	ColorOrange      = lipgloss.Color("#ff9e64")
	ColorYellow      = lipgloss.Color("#e0af68")
	ColorGreen       = lipgloss.Color("#9ece6a")
	ColorTeal        = lipgloss.Color("#2ac3de")
	ColorCyan        = lipgloss.Color("#7dcfff")
	ColorBlue        = lipgloss.Color("#7aa2f7")
	ColorMagenta     = lipgloss.Color("#bb9af7")
	ColorPurple      = lipgloss.Color("#9d7cd8")
	ColorFuchsia     = lipgloss.Color("#d946ef")
	ColorPink        = lipgloss.Color("#ff007c")
)

// Severity colors — matching the web frontend's CSS severity classes exactly.
var severityColors = [8]color.Color{
	lipgloss.Color("#ff007c"), // 0: emerg  — magenta/pink
	lipgloss.Color("#c026d3"), // 1: alert  — purple
	lipgloss.Color("#f7768e"), // 2: crit   — red
	lipgloss.Color("#ff9e64"), // 3: err    — orange
	lipgloss.Color("#e0af68"), // 4: warning — yellow
	lipgloss.Color("#9ece6a"), // 5: notice — green
	lipgloss.Color("#2ac3de"), // 6: info   — teal
	lipgloss.Color("#565f89"), // 7: debug  — comment/dim
}

// SeverityColor returns the color for a syslog severity code.
func SeverityColor(severity int) color.Color {
	if severity < 0 || severity > 7 {
		return ColorComment
	}
	return severityColors[severity]
}

// AppLogLevelColor returns the color for an applog level string.
func AppLogLevelColor(level string) color.Color {
	switch level {
	case "FATAL":
		return severityColors[0] // emerg pink
	case "ERROR":
		return severityColors[3] // orange
	case "WARN":
		return severityColors[4] // yellow
	case "INFO":
		return severityColors[6] // teal
	case "DEBUG":
		return severityColors[7] // comment
	default:
		return ColorComment
	}
}

// SeverityBar returns a left-border indicator character colored by severity.
func SeverityBar(severity int) string {
	return lipgloss.NewStyle().Foreground(SeverityColor(severity)).Render("▎")
}

// TabAccentColors maps tab feeds to their accent colors.
var TabAccentColors = map[string]color.Color{
	"srvlog":    ColorTeal,
	"applog":    ColorPink,
	"netlog":    ColorFuchsia,
	"dashboard": ColorBlue,
	"hosts":     ColorGreen,
	"alerts":    ColorYellow,
	"settings":  ColorComment,
}

// Pre-built styles.
var (
	Base = lipgloss.NewStyle().
		Foreground(ColorFG)

	TabBarBG = lipgloss.NewStyle().
			Background(ColorBGDark)

	ActiveTab = lipgloss.NewStyle().
			Background(ColorBGHighlight).
			Bold(true).
			Padding(0, 1)

	InactiveTab = lipgloss.NewStyle().
			Foreground(ColorComment).
			Background(ColorBGDark).
			Padding(0, 1)

	StatusBar = lipgloss.NewStyle().
			Foreground(ColorFGDark).
			Background(ColorBGDark).
			Padding(0, 1)

	StatusConnected = lipgloss.NewStyle().
			Foreground(ColorGreen)

	StatusDisconnected = lipgloss.NewStyle().
				Foreground(ColorRed)

	TableHeader = lipgloss.NewStyle().
			Foreground(ColorComment).
			Background(ColorBG).
			Bold(true)

	TableSelected = lipgloss.NewStyle().
			Foreground(ColorFG).
			Background(ColorBGHover)

	TableCell = lipgloss.NewStyle().
			Foreground(ColorFG)

	FilterBar = lipgloss.NewStyle().
			Foreground(ColorFG).
			Background(ColorBGDark).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(ColorBorder)

	FilterLabel = lipgloss.NewStyle().
			Foreground(ColorComment)

	FilterInput = lipgloss.NewStyle().
			Foreground(ColorFG).
			Background(ColorBGDark)

	DetailKey = lipgloss.NewStyle().
			Foreground(ColorTeal).
			Bold(true).
			Width(14)

	DetailValue = lipgloss.NewStyle().
			Foreground(ColorFG)

	Help = lipgloss.NewStyle().
		Foreground(ColorComment)

	Card = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		Background(ColorBGDark).
		Padding(1, 2)

	CardLabel = lipgloss.NewStyle().
			Foreground(ColorComment).
			Bold(true)

	CardValue = lipgloss.NewStyle().
			Bold(true)

	Border = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder)

	Hostname = lipgloss.NewStyle().
			Foreground(ColorTeal)

	Program = lipgloss.NewStyle().
		Foreground(ColorMagenta)

	Timestamp = lipgloss.NewStyle().
			Foreground(ColorComment)

	Message = lipgloss.NewStyle().
		Foreground(ColorFGDark)

	Comment = lipgloss.NewStyle().
		Foreground(ColorComment)

	// Feed badges (like the web's S/N/A badges).
	BadgeSrvlog = lipgloss.NewStyle().
			Foreground(ColorTeal).
			Background(lipgloss.Color("#2ac3de20"))

	BadgeNetlog = lipgloss.NewStyle().
			Foreground(ColorFuchsia).
			Background(lipgloss.Color("#d946ef20"))

	BadgeApplog = lipgloss.NewStyle().
			Foreground(ColorPink).
			Background(lipgloss.Color("#ff007c20"))

	// Logo style.
	Logo = lipgloss.NewStyle().
		Foreground(ColorPink).
		Bold(true)
)

// SeverityStyle returns a style with the foreground set to the severity color.
func SeverityStyle(severity int) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(SeverityColor(severity))
}

// AppLogLevelStyle returns a style with the foreground set to the level color.
func AppLogLevelStyle(level string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(AppLogLevelColor(level))
}

// IsCriticalSeverity returns true for severity levels that should get
// tinted row backgrounds (emerg, alert, crit).
func IsCriticalSeverity(severity int) bool {
	return severity <= 2
}
