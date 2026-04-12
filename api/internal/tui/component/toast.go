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
	Feed    string    // "srvlog", "netlog", "applog"
	Level   int       // severity (0-7) for srvlog/netlog, or mapped level for applog
	Time    time.Time // when the event occurred (captured once at push)
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
	if t.Time.IsZero() {
		t.Time = time.Now()
	}
	t.Expires = time.Now().Add(q.maxAge)
	q.toasts = append([]Toast{t}, q.toasts...)
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

// Render renders the toast stack as a single string block.
func (q *ToastQueue) Render(maxWidth int) string {
	if len(q.toasts) == 0 {
		return ""
	}

	toastW := min(80, maxWidth*45/100)
	var parts []string

	shown := min(q.maxShow, len(q.toasts))
	for i := range shown {
		parts = append(parts, renderToast(q.toasts[i], toastW))
	}

	if len(q.toasts) > q.maxShow {
		extra := len(q.toasts) - q.maxShow
		parts = append(parts, theme.Comment.Render(
			fmt.Sprintf("  +%d more", extra)))
	}

	return strings.Join(parts, "\n")
}

func renderToast(t Toast, width int) string {
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
		theme.Timestamp.Render(t.Time.Local().Format("15:04:05")))

	// Message — allow up to 3 lines of wrapping for visibility.
	msgW := max(20, width-4)
	msg := t.Message
	maxLen := msgW * 3
	if len(msg) > maxLen {
		msg = msg[:maxLen-3] + "..."
	}
	msgLine := lipgloss.NewStyle().
		Foreground(theme.ColorFGDark).
		Width(msgW).
		Render(msg)

	content := title + "\n" + msgLine

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(theme.ColorBGDark).
		Width(width).
		Padding(0, 1).
		Render(content)
}

// OverlayToasts composites the toast block on top of the screen content
// in the top-right corner using lipgloss's cell-based Canvas/Layer system.
func OverlayToasts(screen, toasts string, screenW, screenH int) string {
	toastW := lipgloss.Width(toasts)

	// Position: top-right, below tab bar (y=3), 1 char right margin.
	x := max(0, screenW-toastW-1)
	y := 3

	base := lipgloss.NewLayer(screen)
	overlay := lipgloss.NewLayer(toasts).X(x).Y(y).Z(1)
	c := lipgloss.NewCompositor(base, overlay)
	return c.Render()
}
