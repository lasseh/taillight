package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lasseh/taillight/internal/tui/client"
)

// metaLoadTimeout is the budget for fetching meta (hosts/programs/services).
// Bounded so hung API calls don't leak goroutines forever.
const metaLoadTimeout = 10 * time.Second

// startStream begins an SSE connection for the given feed. The stream is
// returned via StreamStartedMsg so it can be safely assigned in Update (Cmds
// must not mutate the model).
func (a *App) startStream(tab TabID) tea.Cmd {
	c := a.client
	switch tab {
	case TabSrvlog:
		params := a.srvlog.Filter().Params()
		return func() tea.Msg {
			stream := client.NewSSEStream(c, "/api/v1/srvlog/stream", params, 0)
			return StreamStartedMsg{Feed: "srvlog", Stream: stream}
		}
	case TabApplog:
		params := a.applog.Filter().Params()
		return func() tea.Msg {
			stream := client.NewSSEStream(c, "/api/v1/applog/stream", params, 0)
			return StreamStartedMsg{Feed: "applog", Stream: stream}
		}
	case TabNetlog:
		params := a.netlog.Filter().Params()
		return func() tea.Msg {
			stream := client.NewSSEStream(c, "/api/v1/netlog/stream", params, 0)
			return StreamStartedMsg{Feed: "netlog", Stream: stream}
		}
	default:
		return nil
	}
}

// sseTick returns a command that fires an SSETickMsg after the batch interval.
func (a *App) sseTick() tea.Cmd {
	return tea.Tick(a.cfg.BatchInterval, func(time.Time) tea.Msg {
		return SSETickMsg{}
	})
}

// drainAllStreams reads events from all active SSE streams and pushes them to
// the corresponding views. Critical events also trigger toast notifications.
// Tracks JSON parse failures so the user is alerted via the status bar.
func (a *App) drainAllStreams() {
	if a.srvlogStream != nil {
		events, parseErrs := drainSrvlogSSE(a.srvlogStream, 100)
		a.parseErrors += parseErrs
		if len(events) > 0 {
			a.srvlog.PushEvents(events)
			for i := range events {
				if events[i].Severity <= notifySeverityMax {
					a.pushSrvlogToast(events[i], "srvlog")
				}
			}
		}
	}

	if a.applogStream != nil {
		events, parseErrs := drainApplogSSE(a.applogStream, 100)
		a.parseErrors += parseErrs
		if len(events) > 0 {
			a.applog.PushEvents(events)
			for i := range events {
				if events[i].Level == "FATAL" || events[i].Level == "ERROR" {
					a.pushApplogToast(events[i])
				}
			}
		}
	}

	if a.netlogStream != nil {
		events, parseErrs := drainNetlogSSE(a.netlogStream, 100)
		a.parseErrors += parseErrs
		if len(events) > 0 {
			a.netlog.PushEvents(events)
			for i := range events {
				if events[i].Severity <= notifySeverityMax {
					a.pushSrvlogToast(events[i], "netlog")
				}
			}
		}
	}

	a.statusBar.SetParseErrors(a.parseErrors)

	// Prune expired toasts.
	a.toasts.Prune()
	a.statusBar.SetConnected(a.isConnected())
}

// isConnected returns true if ANY SSE stream is currently connected.
// Checks all log-view streams AND the dashboard's own streams.
func (a *App) isConnected() bool {
	if a.srvlogStream != nil && a.srvlogStream.Connected() {
		return true
	}
	if a.applogStream != nil && a.applogStream.Connected() {
		return true
	}
	if a.netlogStream != nil && a.netlogStream.Connected() {
		return true
	}
	if a.dashboard.Connected() {
		return true
	}
	return false
}

// Metadata loaders.

func (a *App) loadSrvlogMeta() tea.Cmd {
	c := a.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), metaLoadTimeout)
		defer cancel()
		hostList, err := c.SrvlogHosts(ctx)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("srvlog hosts: %w", err)}
		}
		progList, err := c.SrvlogPrograms(ctx)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("srvlog programs: %w", err)}
		}
		return MetaLoadedMsg{Feed: "srvlog", Hosts: hostList, Programs: progList}
	}
}

func (a *App) loadApplogMeta() tea.Cmd {
	c := a.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), metaLoadTimeout)
		defer cancel()
		svcList, err := c.AppLogServices(ctx)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("applog services: %w", err)}
		}
		compList, err := c.AppLogComponents(ctx)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("applog components: %w", err)}
		}
		hostList, err := c.AppLogHosts(ctx)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("applog hosts: %w", err)}
		}
		return MetaLoadedMsg{Feed: "applog", Services: svcList, Components: compList, Hosts: hostList}
	}
}

func (a *App) loadNetlogMeta() tea.Cmd {
	c := a.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), metaLoadTimeout)
		defer cancel()
		hostList, err := c.NetlogHosts(ctx)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("netlog hosts: %w", err)}
		}
		progList, err := c.NetlogPrograms(ctx)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("netlog programs: %w", err)}
		}
		return MetaLoadedMsg{Feed: "netlog", Hosts: hostList, Programs: progList}
	}
}

// SSE drain helpers per event type. Returns the parsed events and the count
// of unmarshal failures encountered (so callers can surface parse errors
// instead of silently dropping them).

func drainSrvlogSSE(stream *client.SSEStream, maxEvents int) (events []client.SrvlogEvent, parseErrors int) {
	for range maxEvents {
		select {
		case evt, ok := <-stream.Events():
			if !ok {
				return events, parseErrors
			}
			var e client.SrvlogEvent
			if err := json.Unmarshal(evt.Data, &e); err != nil {
				parseErrors++
				continue
			}
			events = append(events, e)
		default:
			return events, parseErrors
		}
	}
	return events, parseErrors
}

func drainApplogSSE(stream *client.SSEStream, maxEvents int) (events []client.AppLogEvent, parseErrors int) {
	for range maxEvents {
		select {
		case evt, ok := <-stream.Events():
			if !ok {
				return events, parseErrors
			}
			var e client.AppLogEvent
			if err := json.Unmarshal(evt.Data, &e); err != nil {
				parseErrors++
				continue
			}
			events = append(events, e)
		default:
			return events, parseErrors
		}
	}
	return events, parseErrors
}

func drainNetlogSSE(stream *client.SSEStream, maxEvents int) (events []client.SrvlogEvent, parseErrors int) {
	for range maxEvents {
		select {
		case evt, ok := <-stream.Events():
			if !ok {
				return events, parseErrors
			}
			var e client.SrvlogEvent
			if err := json.Unmarshal(evt.Data, &e); err != nil {
				parseErrors++
				continue
			}
			events = append(events, e)
		default:
			return events, parseErrors
		}
	}
	return events, parseErrors
}
