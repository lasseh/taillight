package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/lasseh/taillight/internal/tui/client"
	"github.com/lasseh/taillight/internal/tui/component"
)

// notifySeverityMax is the max severity that triggers a toast notification.
// Events with severity <= this value will show a toast (0=emerg, 3=err).
const notifySeverityMax = 3

// notifyMaxAge is the cutoff for triggering toasts on backfill events.
// Events older than this are silently skipped — otherwise reconnecting
// to a busy stream would flood the screen with old alerts.
const notifyMaxAge = 30 * time.Second

func (a *App) pushSrvlogToast(e client.SrvlogEvent, feed string) {
	if time.Since(e.ReceivedAt) > notifyMaxAge {
		return
	}
	a.toasts.Push(component.Toast{
		Title:   fmt.Sprintf("[%s] %s", strings.ToUpper(e.SeverityLabel), e.Hostname),
		Message: e.Message,
		Feed:    feed,
		Level:   e.Severity,
		Time:    e.ReceivedAt,
	})
}

func (a *App) pushApplogToast(e client.AppLogEvent) {
	if time.Since(e.ReceivedAt) > notifyMaxAge {
		return
	}
	level := 3 // default to "err" level for toast color
	if e.Level == "FATAL" {
		level = 0
	}
	a.toasts.Push(component.Toast{
		Title:   fmt.Sprintf("[%s] %s", e.Level, e.Service),
		Message: e.Msg,
		Feed:    "applog",
		Level:   level,
		Time:    e.ReceivedAt,
	})
}
