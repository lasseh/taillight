package component

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/theme"
)

// Toast is a notification popup that appears in the top-right corner.
type Toast struct {
	Title   string
	Message string
	Feed    string // "srvlog", "netlog", "applog"
	Level   int    // severity (0-7) for srvlog/netlog, or mapped level for applog
	Expires time.Time
}

// ToastQueue manages a queue of notification toasts.
type ToastQueue struct {
	toasts  []Toast
	maxAge  time.Duration
	maxShow int
}

// NewToastQueue creates a toast queue.
func NewToastQueue() ToastQueue {
	return ToastQueue{
		maxAge:  5 * time.Second,
		maxShow: 3,
	}
}

// Push adds a new toast notification.
func (q *ToastQueue) Push(t Toast) {
	t.Expires = time.Now().Add(q.maxAge)
	// Prepend (newest first).
	q.toasts = append([]Toast{t}, q.toasts...)
	// Cap the queue.
	if len(q.toasts) > 10 {
		q.toasts = q.toasts[:10]
	}
}

// Prune removes expired toasts. Returns true if any were removed.
func (q *ToastQueue) Prune() bool {
	now := time.Now()
	before := len(q.toasts)
	filtered := q.toasts[:0]
	for _, t := range q.toasts {
		if now.Before(t.Expires) {
			filtered = append(filtered, t)
		}
	}
	q.toasts = filtered
	return len(q.toasts) != before
}

// HasToasts returns true if there are visible toasts.
func (q *ToastQueue) HasToasts() bool {
	return len(q.toasts) > 0
}

// Render renders the toast overlay box for the top-right corner.
// Returns the rendered string and its width/height, or empty if no toasts.
func (q *ToastQueue) Render(maxWidth int) string {
	if len(q.toasts) == 0 {
		return ""
	}

	toastW := min(50, maxWidth-4)
	var lines []string

	shown := min(q.maxShow, len(q.toasts))
	for i := range shown {
		t := q.toasts[i]
		lines = append(lines, renderToast(t, toastW))
		if i < shown-1 {
			lines = append(lines, "")
		}
	}

	// Show count of hidden toasts.
	if len(q.toasts) > q.maxShow {
		extra := len(q.toasts) - q.maxShow
		lines = append(lines, theme.Comment.Render(
			fmt.Sprintf("  +%d more", extra)))
	}

	content := strings.Join(lines, "\n")

	return lipgloss.NewStyle().
		Padding(0, 1).
		Render(content)
}

func renderToast(t Toast, width int) string {
	// Border color based on severity.
	borderColor := theme.SeverityColor(t.Level)

	// Feed badge.
	var badge string
	switch t.Feed {
	case "srvlog":
		badge = theme.BadgeSrvlog.Render(" S ")
	case "netlog":
		badge = theme.BadgeNetlog.Render(" N ")
	case "applog":
		badge = theme.BadgeApplog.Render(" A ")
	}

	// Title line.
	sevStyle := theme.SeverityStyle(t.Level)
	title := fmt.Sprintf("%s %s %s",
		badge,
		sevStyle.Bold(true).Render(t.Title),
		theme.Timestamp.Render(time.Now().Format("15:04:05")))

	// Message (truncated).
	msgW := max(10, width-4)
	msg := t.Message
	if len(msg) > msgW {
		msg = msg[:msgW-3] + "..."
	}
	msgLine := theme.Message.Render(msg)

	content := title + "\n" + msgLine

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(theme.ColorBGDark).
		Width(width).
		Padding(0, 1).
		Render(content)
}

// OverlayToasts places the toast overlay on top of the screen content
// in the top-right corner.
func OverlayToasts(screen, toasts string, screenW, screenH int) string {
	if toasts == "" {
		return screen
	}

	toastW := lipgloss.Width(toasts)
	toastH := lipgloss.Height(toasts)

	// Position in top-right with 1-char margin.
	x := max(0, screenW-toastW-2)
	y := 3 // below tab bar + separator

	return placeOverlay(x, y, toasts, screen, screenW, screenH, toastH)
}

// placeOverlay places fg on top of bg at position (x, y).
func placeOverlay(x, y int, fg, bg string, bgW, bgH, fgH int) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")

	// Pad bg if needed.
	for len(bgLines) < bgH {
		bgLines = append(bgLines, strings.Repeat(" ", bgW))
	}

	// Overlay fg onto bg.
	for i, fgLine := range fgLines {
		bgIdx := y + i
		if bgIdx < 0 || bgIdx >= len(bgLines) {
			continue
		}
		bgLine := bgLines[bgIdx]
		bgRunes := []rune(bgLine)

		// Pad bg line if shorter than x.
		for len(bgRunes) < x {
			bgRunes = append(bgRunes, ' ')
		}

		// Replace portion of bg line with fg line.
		fgW := lipgloss.Width(fgLine)
		_ = fgW // We just splice the string at the rune level
		_ = fgH

		// Simple approach: take bg up to x, then fg, then remaining bg.
		prefix := string(bgRunes[:min(x, len(bgRunes))])
		bgLines[bgIdx] = prefix + fgLine
	}

	return strings.Join(bgLines, "\n")
}
