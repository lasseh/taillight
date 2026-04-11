// Package srvlog implements the srvlog stream view for the TUI.
package srvlog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"

	"github.com/lasseh/taillight/internal/tui/client"
	"github.com/lasseh/taillight/internal/tui/theme"
)

const (
	fixedTimeWidth     = 8 // HH:MM:SS
	fixedSeverityWidth = 8 // "warning " (longest label)
)

// columns returns the table column definitions for the given terminal width.
func columns(width int) []table.Column {
	// Remaining width after fixed columns and padding.
	remaining := max(
		// 6 for inter-column gaps
		width-fixedTimeWidth-fixedSeverityWidth-6, 20)

	hostnameWidth := max(8, remaining*20/100)
	programWidth := max(6, remaining*15/100)
	messageWidth := max(10, remaining-hostnameWidth-programWidth)

	return []table.Column{
		{Title: "TIME", Width: fixedTimeWidth},
		{Title: "SEVERITY", Width: fixedSeverityWidth},
		{Title: "HOSTNAME", Width: hostnameWidth},
		{Title: "PROGRAM", Width: programWidth},
		{Title: "MESSAGE", Width: messageWidth},
	}
}

// eventToRow converts a SrvlogEvent to a table row string slice.
func eventToRow(e client.SrvlogEvent, timeFormat string) table.Row {
	return table.Row{
		e.ReceivedAt.Local().Format(timeFormat),
		padRight(e.SeverityLabel, fixedSeverityWidth),
		truncate(e.Hostname, 20),
		truncate(e.Programname, 16),
		strings.ReplaceAll(e.Message, "\n", " "),
	}
}

// renderDetailPanel renders the expanded detail view for a srvlog event.
func renderDetailPanel(e client.SrvlogEvent, width int) string {
	var b strings.Builder

	kv := func(key, value string) {
		k := theme.DetailKey.Render(key)
		v := theme.DetailValue.Render(value)
		fmt.Fprintf(&b, "%s %s\n", k, v)
	}

	sevStyle := theme.SeverityStyle(e.Severity)
	sevStr := sevStyle.Render(fmt.Sprintf("%d (%s)", e.Severity, e.SeverityLabel))

	kv("ID", fmt.Sprintf("%d", e.ID))
	kv("Received", e.ReceivedAt.Local().Format("2006-01-02 15:04:05.000"))
	kv("Reported", e.ReportedAt.Local().Format("2006-01-02 15:04:05.000"))
	kv("Hostname", theme.Hostname.Render(e.Hostname))
	kv("From IP", e.FromhostIP)
	kv("Program", theme.Program.Render(e.Programname))
	fmt.Fprintf(&b, "%s %s\n", theme.DetailKey.Render("Severity"), sevStr)
	kv("Facility", fmt.Sprintf("%d (%s)", e.Facility, e.FacilityLabel))
	kv("Syslog Tag", e.SyslogTag)
	if e.MsgID != "" {
		kv("Msg ID", e.MsgID)
	}

	b.WriteString("\n")
	kv("Message", "")
	b.WriteString(theme.Message.Width(max(20, width-4)).Render(e.Message))
	b.WriteString("\n")

	if e.StructuredData != nil && *e.StructuredData != "" {
		b.WriteString("\n")
		kv("Structured Data", "")
		b.WriteString(theme.DetailValue.Render(*e.StructuredData))
		b.WriteString("\n")
	}

	if e.RawMessage != nil && *e.RawMessage != "" {
		b.WriteString("\n")
		kv("Raw Message", "")
		b.WriteString(theme.Comment.Width(max(20, width-4)).Render(*e.RawMessage))
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
