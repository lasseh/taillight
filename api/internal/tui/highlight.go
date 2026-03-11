package tui

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
)

// highlightMessage applies syntax highlighting to a syslog message string,
// matching the web GUI's PrismJS + Juniper/JunOS token coloring.
func highlightMessage(msg string, maxWidth int) string {
	if maxWidth > 0 && len(msg) > maxWidth {
		msg = msg[:maxWidth-1] + lipgloss.NewStyle().Foreground(colorDim).Render("…")
	}
	return applyHighlights(msg)
}

// token represents a highlighted region in the message.
type token struct {
	start int
	end   int
	style lipgloss.Style
}

// Compiled patterns ordered by priority (first match wins for overlapping regions).
// Matches the web GUI's highlighter.ts + prism-junos.css color mapping.
var highlightRules = func() []highlightRule {
	rules := []struct {
		pattern string
		style   lipgloss.Style
	}{
		// IPv6 addresses → teal.
		{`\[[0-9a-fA-F]{1,4}(?::[0-9a-fA-F]{0,4}){1,7}\](?::\d{1,5})?`, lipgloss.NewStyle().Foreground(colorTeal)},
		{`\b[0-9a-fA-F]{1,4}(?::[0-9a-fA-F]{0,4}){2,7}\b`, lipgloss.NewStyle().Foreground(colorTeal)},

		// JunOS interface names → teal bold.
		{`\b(?:(?:(?:[gx]e|et|so|fe|gr|ip|[lmuv]t|p[de]|pf[eh]|lc|lsq|sp)-\d+/\d+/\d+(?::\d+)?)|(?:ae|em|fxp|lo|me|vme|pp)\d{0,4}|(?:reth|irb|cbp|lsi|mtun|pimd|pime|tap|dsc|demux|st|vlan)\d*)(?:\.\d{1,5})?\b`, lipgloss.NewStyle().Foreground(colorTeal).Bold(true)},

		// JunOS event tags (RPD_BGP_NEIGHBOR_STATE_CHANGED) → yellow.
		{`\b[A-Z][A-Z0-9]+_[A-Z][A-Z0-9_]+\b`, lipgloss.NewStyle().Foreground(colorYellow)},

		// Routing protocols → blue bold.
		{`\b(?:BGP|OSPF|OSPFv[23]|IS-?IS|MPLS|LDP|RSVP|BFD|LACP|LLDP|VRRP|RIPng|RIP|PIM|IGMP|MLD|MSDP|STP|RSTP|MSTP|MVRP)\b`, lipgloss.NewStyle().Foreground(colorBlue).Bold(true)},

		// BGP states → green.
		{`\b(?:Idle|Connect|Active|OpenSent|OpenConfirm|Established)\b`, lipgloss.NewStyle().Foreground(colorGreen)},

		// Firewall actions → orange bold.
		{`\b(?:accept|permit|deny|discard|reject)\b`, lipgloss.NewStyle().Foreground(colorOrange).Bold(true)},

		// JunOS hardware → purple.
		{`\b(?:FPC|PIC|MIC|MPC|RE[01]?|SCB|SFB|SIB|CB|FEB|PEM|PSU)\b`, lipgloss.NewStyle().Foreground(colorPurple)},

		// JunOS daemons → purple.
		{`\b(?:rpd|chassisd|mgd|dcd|pfed|dfwd|snmpd|mib2d|alarmd|craftd|eventd|cosd|ppmd|vrrpd|bfdd|sampled|kmd|l2ald|eswd|lacpd|rmopd|lmpd|fsad|spd|authd|jdhcpd|l2cpd|sflowd|lfmd|ksyncd|xntpd|ntpd)\b`, lipgloss.NewStyle().Foreground(colorPurple)},

		// Routing tables → teal.
		{`\b(?:[\w-]+\.)?(?:inet6?|mpls|inetflow|iso|bgp\.l[23]vpn)\.\d+\b`, lipgloss.NewStyle().Foreground(colorTeal)},

		// ASN references → orange.
		{`\bAS\s?\d{1,10}\b`, lipgloss.NewStyle().Foreground(colorOrange)},

		// IPv4 addresses → teal.
		{`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(?:/\d{1,3})?\b`, lipgloss.NewStyle().Foreground(colorTeal)},

		// MAC addresses → purple.
		{`\b[0-9a-fA-F]{2}(?::[0-9a-fA-F]{2}){5}\b`, lipgloss.NewStyle().Foreground(colorPurple)},

		// URLs → blue.
		{`https?://\S+`, lipgloss.NewStyle().Foreground(colorBlue)},

		// File paths → blue.
		{`/(?:[\w.-]+/)+[\w.-]+`, lipgloss.NewStyle().Foreground(colorBlue)},

		// Quoted strings → green.
		{`"[^"]*"`, lipgloss.NewStyle().Foreground(colorGreen)},
		{`'[^']*'`, lipgloss.NewStyle().Foreground(colorGreen)},

		// Standalone numbers → orange.
		{`\b\d+(?:\.\d+)?\b`, lipgloss.NewStyle().Foreground(colorOrange)},
	}

	compiled := make([]highlightRule, len(rules))
	for i, r := range rules {
		compiled[i] = highlightRule{
			re:    regexp.MustCompile(r.pattern),
			style: r.style,
		}
	}
	return compiled
}()

type highlightRule struct {
	re    *regexp.Regexp
	style lipgloss.Style
}

// applyHighlights tokenizes msg and applies colored styles to matched regions.
func applyHighlights(msg string) string {
	if msg == "" {
		return msg
	}

	// Collect all tokens, respecting priority (first-rule-wins for overlaps).
	var tokens []token
	covered := make([]bool, len(msg))

	for _, rule := range highlightRules {
		matches := rule.re.FindAllStringIndex(msg, -1)
		for _, m := range matches {
			start, end := m[0], m[1]

			// Skip if any byte in this range is already claimed.
			overlap := false
			for i := start; i < end; i++ {
				if covered[i] {
					overlap = true
					break
				}
			}
			if overlap {
				continue
			}

			// Claim this range.
			for i := start; i < end; i++ {
				covered[i] = i >= start && i < end
			}
			tokens = append(tokens, token{start: start, end: end, style: rule.style})
		}
	}

	if len(tokens) == 0 {
		return msg
	}

	// Sort tokens by start position.
	sortTokens(tokens)

	// Build the highlighted string.
	var b strings.Builder
	b.Grow(len(msg) * 2) // rough estimate for ANSI overhead
	pos := 0
	for _, t := range tokens {
		if t.start > pos {
			b.WriteString(msg[pos:t.start])
		}
		b.WriteString(t.style.Render(msg[t.start:t.end]))
		pos = t.end
	}
	if pos < len(msg) {
		b.WriteString(msg[pos:])
	}

	return b.String()
}

// sortTokens sorts by start position using insertion sort (small slices).
func sortTokens(tokens []token) {
	for i := 1; i < len(tokens); i++ {
		key := tokens[i]
		j := i - 1
		for j >= 0 && tokens[j].start > key.start {
			tokens[j+1] = tokens[j]
			j--
		}
		tokens[j+1] = key
	}
}
