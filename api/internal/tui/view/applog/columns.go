package applog

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"

	"github.com/lasseh/taillight/internal/tui/client"
	"github.com/lasseh/taillight/internal/tui/theme"
)

const (
	fixedTimeWidth  = 8 // HH:MM:SS
	fixedLevelWidth = 6 // "FATAL " (longest label)
)

// columns returns the table column definitions for the given terminal width.
func columns(width int) []table.Column {
	remaining := max(width-fixedTimeWidth-fixedLevelWidth-6, 20)

	serviceWidth := max(8, remaining*20/100)
	hostWidth := max(8, remaining*15/100)
	messageWidth := max(10, remaining-serviceWidth-hostWidth)

	return []table.Column{
		{Title: "TIME", Width: fixedTimeWidth},
		{Title: "LEVEL", Width: fixedLevelWidth},
		{Title: "SERVICE", Width: serviceWidth},
		{Title: "HOST", Width: hostWidth},
		{Title: "MESSAGE", Width: messageWidth},
	}
}

// eventToRow converts an AppLogEvent to a table row.
func eventToRow(e client.AppLogEvent, timeFormat string) table.Row {
	return table.Row{
		e.Timestamp.Local().Format(timeFormat),
		padRight(e.Level, fixedLevelWidth),
		truncate(e.Service, 20),
		truncate(e.Host, 16),
		strings.ReplaceAll(e.Msg, "\n", " "),
	}
}

// renderDetailPanel renders the expanded detail view for an applog event.
func renderDetailPanel(e client.AppLogEvent, width int) string {
	var b strings.Builder

	kv := func(key, value string) {
		k := theme.DetailKey.Render(key)
		v := theme.DetailValue.Render(value)
		fmt.Fprintf(&b, "%s %s\n", k, v)
	}

	lvlStyle := theme.AppLogLevelStyle(e.Level)

	kv("ID", fmt.Sprintf("%d", e.ID))
	kv("Received", e.ReceivedAt.Local().Format("2006-01-02 15:04:05.000"))
	kv("Timestamp", e.Timestamp.Local().Format("2006-01-02 15:04:05.000"))
	fmt.Fprintf(&b, "%s %s\n", theme.DetailKey.Render("Level"), lvlStyle.Render(e.Level))
	kv("Service", theme.Program.Render(e.Service))
	if e.Component != "" {
		kv("Component", e.Component)
	}
	kv("Host", theme.Hostname.Render(e.Host))
	if e.Source != "" {
		kv("Source", e.Source)
	}

	b.WriteString("\n")
	kv("Message", "")
	b.WriteString(theme.Message.Width(max(20, width-4)).Render(e.Msg))
	b.WriteString("\n")

	if len(e.Attrs) > 0 && string(e.Attrs) != "{}" && string(e.Attrs) != "null" {
		b.WriteString("\n")
		kv("Attributes", "")
		// Pretty-print JSON attrs.
		var pretty json.RawMessage
		if err := json.Unmarshal(e.Attrs, &pretty); err == nil {
			formatted, fErr := json.MarshalIndent(pretty, "", "  ")
			if fErr == nil {
				b.WriteString(theme.Comment.Width(max(20, width-4)).Render(string(formatted)))
			} else {
				b.WriteString(theme.Comment.Render(string(e.Attrs)))
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}
