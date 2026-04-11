package tui

import (
	"github.com/lasseh/taillight/internal/tui/client"
)

// SSE event messages delivered from the stream goroutines to bubbletea.

// SrvlogEventMsg carries a batch of srvlog events from the SSE stream.
type SrvlogEventMsg struct {
	Events []client.SrvlogEvent
}

// ApplogEventMsg carries a batch of applog events from the SSE stream.
type ApplogEventMsg struct {
	Events []client.AppLogEvent
}

// NetlogEventMsg carries a batch of netlog events from the SSE stream.
type NetlogEventMsg struct {
	Events []client.SrvlogEvent
}

// SSEConnectedMsg indicates the SSE stream connected/reconnected.
type SSEConnectedMsg struct {
	Feed string // "srvlog", "applog", "netlog"
}

// SSEDisconnectedMsg indicates the SSE stream lost connection.
type SSEDisconnectedMsg struct {
	Feed string
}

// API response messages.

// MetaLoadedMsg carries metadata (hosts, programs, etc.) loaded from the API.
type MetaLoadedMsg struct {
	Feed     string
	Hosts    []string
	Programs []string
}

// ErrorMsg carries an error to display in the status bar.
type ErrorMsg struct {
	Err error
}

// ClearErrorMsg clears the error display.
type ClearErrorMsg struct{}

// SSETickMsg triggers draining the SSE channel for batched event delivery.
type SSETickMsg struct{}
