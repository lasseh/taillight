// Package highlight provides syntax highlighting for log messages using jink's
// lexer with Tokyo Night colors via lipgloss.
package highlight

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/lasseh/jink/lexer"
	"github.com/lasseh/taillight/internal/tui/theme"
)

// tokenColors maps jink token types to Tokyo Night colors.
var tokenColors = map[lexer.TokenType]color.Color{
	// Network tokens — teal (like hostnames in the web GUI).
	lexer.TokenIPv4:       theme.ColorTeal,
	lexer.TokenIPv4Prefix: theme.ColorTeal,
	lexer.TokenIPv6:       theme.ColorTeal,
	lexer.TokenIPv6Prefix: theme.ColorTeal,
	lexer.TokenMAC:        theme.ColorTeal,
	lexer.TokenInterface:  theme.ColorTeal,

	// Numbers and values — orange.
	lexer.TokenNumber:     theme.ColorOrange,
	lexer.TokenPercentage: theme.ColorOrange,
	lexer.TokenByteSize:   theme.ColorOrange,

	// Time — cyan.
	lexer.TokenTimeDuration: theme.ColorCyan,

	// Strings — green.
	lexer.TokenString: theme.ColorGreen,

	// States — semantic colors matching the web GUI.
	lexer.TokenStateGood:    theme.ColorGreen,
	lexer.TokenStateBad:     theme.ColorRed,
	lexer.TokenStateWarning: theme.ColorYellow,
	lexer.TokenStateNeutral: theme.ColorComment,

	// Structural — purple (like program names).
	lexer.TokenCommand:  theme.ColorMagenta,
	lexer.TokenKeyword:  theme.ColorBlue,
	lexer.TokenSection:  theme.ColorBlue,
	lexer.TokenProtocol: theme.ColorCyan,
	lexer.TokenAction:   theme.ColorYellow,

	// Routes and tables.
	lexer.TokenRouteProtocol: theme.ColorMagenta,
	lexer.TokenTableName:     theme.ColorCyan,
	lexer.TokenASN:           theme.ColorOrange,
	lexer.TokenCommunity:     theme.ColorOrange,

	// Comments — dim.
	lexer.TokenComment:    theme.ColorComment,
	lexer.TokenAnnotation: theme.ColorComment,
}

// Message applies syntax highlighting to a log message string using jink's
// lexer. Tokens like IPs, numbers, quoted strings, and state words get
// colored; plain text keeps the default message color.
func Message(msg string) string {
	// Single-line only for table cells.
	msg = strings.ReplaceAll(msg, "\n", " ")

	lex := lexer.New(msg)
	lex.SetParseMode(lexer.ParseModeShow) // show mode catches IPs, states, etc.
	tokens := lex.Tokenize()

	var b strings.Builder
	b.Grow(len(msg) * 2) // rough estimate with ANSI codes

	defaultStyle := theme.Message

	for _, tok := range tokens {
		if c, ok := tokenColors[tok.Type]; ok {
			b.WriteString(lipgloss.NewStyle().Foreground(c).Render(tok.Value))
		} else {
			b.WriteString(defaultStyle.Render(tok.Value))
		}
	}

	return b.String()
}
