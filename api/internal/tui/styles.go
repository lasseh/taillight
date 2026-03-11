package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Tokyo Night color palette.
var (
	colorRed      = lipgloss.Color("#f7768e")
	colorYellow   = lipgloss.Color("#e0af68")
	colorGreen    = lipgloss.Color("#9ece6a")
	colorCyan     = lipgloss.Color("#7dcfff")
	colorBlue     = lipgloss.Color("#7aa2f7")
	colorDim      = lipgloss.Color("#565f89")
	colorFg       = lipgloss.Color("#c0caf5")
	colorBg       = lipgloss.Color("#1a1b26")
	colorBarBg    = lipgloss.Color("#24283b")
	colorSelBg    = lipgloss.Color("#33467c")
	colorFilterBg = lipgloss.Color("#292e42")
)

// Header style.
var headerStyle = lipgloss.NewStyle().
	Background(colorBarBg).
	Foreground(colorFg).
	Bold(true).
	Padding(0, 1)

// Tab styles.
var (
	activeTabStyle = lipgloss.NewStyle().
			Background(colorBlue).
			Foreground(colorBg).
			Bold(true).
			Padding(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(colorDim).
				Padding(0, 1)
)

// Status bar style.
var statusBarStyle = lipgloss.NewStyle().
	Background(colorBarBg).
	Foreground(colorDim).
	Padding(0, 1)

// Selected row style.
var selectedRowStyle = lipgloss.NewStyle().
	Background(colorSelBg)

// Detail box style.
var detailStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	Padding(0, 1)

// Filter bar style.
var filterBarStyle = lipgloss.NewStyle().
	Background(colorFilterBg).
	Padding(0, 1)

// Dim style for secondary text.
var dimStyle = lipgloss.NewStyle().
	Foreground(colorDim)

// Help popup style.
var helpPopupStyle = lipgloss.NewStyle().
	Background(colorBarBg).
	Foreground(colorFg).
	Border(lipgloss.RoundedBorder()).
	BorderForeground(colorBlue).
	Padding(1, 2)

var helpKeyStyle = lipgloss.NewStyle().
	Foreground(colorBlue).
	Bold(true).
	Width(14)

var helpDescStyle = lipgloss.NewStyle().
	Foreground(colorFg)

var helpSectionStyle = lipgloss.NewStyle().
	Foreground(colorCyan).
	Bold(true)

// helpText is the rendered help popup content.
var helpText = func() string {
	title := lipgloss.NewStyle().
		Foreground(colorBlue).
		Bold(true).
		Render("Keyboard Shortcuts")

	sections := []struct {
		name string
		keys []struct{ key, desc string }
	}{
		{"Navigation", []struct{ key, desc string }{
			{"Tab", "Switch between Syslog / AppLog"},
			{"j / ↓", "Move cursor down"},
			{"k / ↑", "Move cursor up"},
			{"PgDn / PgUp", "Scroll by page"},
			{"g / Home", "Jump to top"},
			{"G / End", "Jump to bottom (re-pin)"},
		}},
		{"Actions", []struct{ key, desc string }{
			{"Enter", "Toggle detail panel"},
			{"/", "Open filter bar"},
			{"Esc", "Close detail / filter"},
		}},
		{"Filter Syntax", []struct{ key, desc string }{
			{"key:value", "Filter by field"},
			{"bare text", "Full-text search"},
		}},
		{"Syslog Filters", []struct{ key, desc string }{
			{"hostname:", "Filter by hostname"},
			{"programname:", "Filter by program"},
			{"severity:", "Filter by severity (0-7)"},
			{"search:", "Search in message"},
		}},
		{"AppLog Filters", []struct{ key, desc string }{
			{"service:", "Filter by service"},
			{"component:", "Filter by component"},
			{"host:", "Filter by host"},
			{"level:", "Filter by level"},
		}},
		{"General", []struct{ key, desc string }{
			{"?", "Toggle this help"},
			{"q / Ctrl-C", "Quit"},
		}},
	}

	var b strings.Builder
	b.WriteString(title + "\n\n")

	for i, sec := range sections {
		b.WriteString(helpSectionStyle.Render(sec.name) + "\n")
		for _, kv := range sec.keys {
			b.WriteString(helpKeyStyle.Render(kv.key) + helpDescStyle.Render(kv.desc) + "\n")
		}
		if i < len(sections)-1 {
			b.WriteByte('\n')
		}
	}

	b.WriteString("\n" + dimStyle.Render("Press any key to close"))
	return b.String()
}()

// Connected/disconnected indicators.
var (
	connectedStyle    = lipgloss.NewStyle().Foreground(colorGreen)
	disconnectedStyle = lipgloss.NewStyle().Foreground(colorRed)
)

// SeverityStyle returns a style colored by syslog severity code.
func SeverityStyle(code int) lipgloss.Style {
	switch {
	case code <= 2:
		return lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	case code == 3:
		return lipgloss.NewStyle().Foreground(colorRed)
	case code == 4:
		return lipgloss.NewStyle().Foreground(colorYellow)
	case code == 5:
		return lipgloss.NewStyle().Foreground(colorCyan)
	case code == 6:
		return lipgloss.NewStyle().Foreground(colorGreen)
	default:
		return lipgloss.NewStyle().Foreground(colorDim)
	}
}

// LevelStyle returns a style colored by applog level string.
func LevelStyle(level string) lipgloss.Style {
	switch strings.ToUpper(level) {
	case "FATAL":
		return lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	case "ERROR":
		return lipgloss.NewStyle().Foreground(colorRed)
	case "WARN":
		return lipgloss.NewStyle().Foreground(colorYellow)
	case "INFO":
		return lipgloss.NewStyle().Foreground(colorGreen)
	default:
		return lipgloss.NewStyle().Foreground(colorDim)
	}
}

// SeverityBorderColor returns a border color for detail panels.
func SeverityBorderColor(code int) color.Color {
	switch {
	case code <= 2:
		return colorRed
	case code == 3:
		return colorRed
	case code == 4:
		return colorYellow
	case code == 5:
		return colorCyan
	case code == 6:
		return colorGreen
	default:
		return colorDim
	}
}

// LevelBorderColor returns a border color for applog detail panels.
func LevelBorderColor(level string) color.Color {
	switch strings.ToUpper(level) {
	case "FATAL", "ERROR":
		return colorRed
	case "WARN":
		return colorYellow
	case "INFO":
		return colorGreen
	default:
		return colorDim
	}
}
