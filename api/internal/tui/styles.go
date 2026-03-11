package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Tokyo Night color palette — matches the web GUI exactly.
var (
	colorMagenta  = lipgloss.Color("#ff007c") // EMERG, FATAL
	colorRed      = lipgloss.Color("#f7768e") // CRIT, ALERT, ERROR
	colorOrange   = lipgloss.Color("#ff9e64") // ERR, WARN (applog)
	colorYellow   = lipgloss.Color("#e0af68") // WARNING, components
	colorGreen    = lipgloss.Color("#9ece6a") // NOTICE, INFO
	colorTeal     = lipgloss.Color("#2ac3de") // INFO (syslog), hostnames
	colorCyan     = lipgloss.Color("#7dcfff") // secondary info
	colorBlue     = lipgloss.Color("#7aa2f7") // UI accent, links
	colorPurple   = lipgloss.Color("#bb9af7") // program names, services
	colorDim      = lipgloss.Color("#565f89") // dimmed text
	colorFg       = lipgloss.Color("#c0caf5") // primary text
	colorBg       = lipgloss.Color("#1a1b26") // main background
	colorBarBg    = lipgloss.Color("#24283b") // header/status bar bg
	colorSelBg    = lipgloss.Color("#33467c") // selected row bg
	colorFilterBg = lipgloss.Color("#292e42") // filter bar / highlight bg
	colorZebraBg  = lipgloss.Color("#1e2030") // zebra stripe (subtle)
	colorBorder   = lipgloss.Color("#3b4261") // subtle borders
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

// Column header style — dim text on slightly darker bg.
var columnHeaderStyle = lipgloss.NewStyle().
	Foreground(colorDim).
	Bold(true)

// Selected row style.
var selectedRowStyle = lipgloss.NewStyle().
	Background(colorSelBg)

// Zebra stripe style (even rows).
var zebraStyle = lipgloss.NewStyle().
	Background(colorZebraBg)

// Row tint styles for high-severity events (dark tinted backgrounds).
var (
	rowTintEmerg = lipgloss.NewStyle().Background(lipgloss.Color("#261a22")) // dark magenta tint
	rowTintCrit  = lipgloss.NewStyle().Background(lipgloss.Color("#241a1e")) // dark red tint
)

// Detail box style.
var detailStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(colorBorder).
	Padding(0, 1)

// Detail label style.
var detailLabelStyle = lipgloss.NewStyle().
	Foreground(colorDim)

// Detail value style.
var detailValueStyle = lipgloss.NewStyle().
	Foreground(colorFg)

// Filter bar style.
var filterBarStyle = lipgloss.NewStyle().
	Background(colorFilterBg).
	Padding(0, 1)

// Filter tag key style.
var filterTagKeyStyle = lipgloss.NewStyle().
	Foreground(colorDim)

// Filter tag value style.
var filterTagValueStyle = lipgloss.NewStyle().
	Foreground(colorTeal).
	Bold(true)

// Dim style for secondary text.
var dimStyle = lipgloss.NewStyle().
	Foreground(colorDim)

// Hostname style — teal like the web GUI.
var hostnameStyle = lipgloss.NewStyle().
	Foreground(colorTeal)

// Program name style — purple like the web GUI.
var programStyle = lipgloss.NewStyle().
	Foreground(colorPurple)

// Service name style — purple like the web GUI.
var serviceStyle = lipgloss.NewStyle().
	Foreground(colorPurple)

// Component style — yellow like the web GUI.
var componentStyle = lipgloss.NewStyle().
	Foreground(colorYellow)

// Attrs inline style — orange dash, dim key=value.
var (
	attrsDashStyle = lipgloss.NewStyle().Foreground(colorOrange)
	attrsKVStyle   = lipgloss.NewStyle().Foreground(colorDim)
)

// Jump-to-latest banner style — magenta like the web GUI.
var jumpToLatestStyle = lipgloss.NewStyle().
	Foreground(colorMagenta).
	Bold(true)

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
// Matches the web GUI's Tokyo Night severity color mapping.
func SeverityStyle(code int) lipgloss.Style {
	switch code {
	case 0: // EMERG
		return lipgloss.NewStyle().Foreground(colorMagenta).Bold(true)
	case 1: // ALERT
		return lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	case 2: // CRIT
		return lipgloss.NewStyle().Foreground(colorRed)
	case 3: // ERR
		return lipgloss.NewStyle().Foreground(colorOrange)
	case 4: // WARNING
		return lipgloss.NewStyle().Foreground(colorYellow)
	case 5: // NOTICE
		return lipgloss.NewStyle().Foreground(colorGreen)
	case 6: // INFO
		return lipgloss.NewStyle().Foreground(colorTeal)
	default: // DEBUG
		return lipgloss.NewStyle().Foreground(colorDim)
	}
}

// LevelStyle returns a style colored by applog level string.
// Matches the web GUI's Tokyo Night level color mapping.
func LevelStyle(level string) lipgloss.Style {
	switch strings.ToUpper(level) {
	case "FATAL":
		return lipgloss.NewStyle().Foreground(colorMagenta).Bold(true)
	case "ERROR":
		return lipgloss.NewStyle().Foreground(colorRed)
	case "WARN":
		return lipgloss.NewStyle().Foreground(colorOrange)
	case "INFO":
		return lipgloss.NewStyle().Foreground(colorGreen)
	default: // DEBUG
		return lipgloss.NewStyle().Foreground(colorDim)
	}
}

// SeverityBorderColor returns a border color for detail panels.
func SeverityBorderColor(code int) color.Color {
	switch code {
	case 0:
		return colorMagenta
	case 1, 2:
		return colorRed
	case 3:
		return colorOrange
	case 4:
		return colorYellow
	case 5:
		return colorGreen
	case 6:
		return colorTeal
	default:
		return colorDim
	}
}

// LevelBorderColor returns a border color for applog detail panels.
func LevelBorderColor(level string) color.Color {
	switch strings.ToUpper(level) {
	case "FATAL":
		return colorMagenta
	case "ERROR":
		return colorRed
	case "WARN":
		return colorOrange
	case "INFO":
		return colorGreen
	default:
		return colorDim
	}
}
