package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lasseh/taillight/internal/model"
)

// SSEMessage is a raw SSE message parsed from the stream.
type SSEMessage struct {
	ID    string
	Event string
	Data  string
}

// SSEClient connects to SSE endpoints and parses events.
type SSEClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewSSEClient creates an SSE client.
func NewSSEClient(baseURL, apiKey string) *SSEClient {
	return &SSEClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 0, // no timeout for streaming
		},
	}
}

// Stream is the identifier for which SSE stream a message belongs to.
type Stream int

const (
	StreamSyslog Stream = iota
	StreamAppLog
)

// SSE Bubble Tea message types.

// SyslogEventMsg is a new syslog event from SSE.
type SyslogEventMsg struct {
	Event model.SyslogEvent
}

// AppLogEventMsg is a new applog event from SSE.
type AppLogEventMsg struct {
	Event model.AppLogEvent
}

// SSEStatusMsg reports connection status changes.
type SSEStatusMsg struct {
	Stream    Stream
	Connected bool
	Err       error
}

const (
	heartbeatTimeout = 45 * time.Second
	maxBackoff       = 30 * time.Second
)

// Connect opens an SSE connection and sends parsed messages to the channel.
// It blocks until ctx is cancelled. Handles reconnection with backoff.
func (c *SSEClient) Connect(ctx context.Context, path string, params map[string]string, lastEventID string, ch chan<- SSEMessage) {
	backoff := time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		url := c.baseURL + path
		if len(params) > 0 {
			parts := make([]string, 0, len(params))
			for k, v := range params {
				parts = append(parts, fmt.Sprintf("%s=%s", k, v))
			}
			url += "?" + strings.Join(parts, "&")
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				backoff = min(backoff*2, maxBackoff)
				continue
			}
		}

		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Cache-Control", "no-cache")
		if lastEventID != "" {
			req.Header.Set("Last-Event-ID", lastEventID)
		}
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				backoff = min(backoff*2, maxBackoff)
				continue
			}
		}

		// Reset backoff on successful connect.
		backoff = time.Second

		lastEventID = c.readStream(ctx, resp, ch, lastEventID)
		_ = resp.Body.Close() //nolint:errcheck // best-effort close on reconnect

		// Brief pause before reconnect.
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// readStream reads SSE events from the response body. Returns the last event ID seen.
func (c *SSEClient) readStream(ctx context.Context, resp *http.Response, ch chan<- SSEMessage, lastID string) string {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var msg SSEMessage
	timer := time.NewTimer(heartbeatTimeout)
	defer timer.Stop()

	lineCh := make(chan string)
	errCh := make(chan error, 1)

	go func() {
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
		errCh <- scanner.Err()
	}()

	for {
		select {
		case <-ctx.Done():
			return lastID
		case <-timer.C:
			// Heartbeat timeout — reconnect.
			return lastID
		case line := <-lineCh:
			timer.Reset(heartbeatTimeout)

			if line == "" {
				// Blank line = dispatch event.
				if msg.Data != "" || msg.Event != "" {
					if msg.ID != "" {
						lastID = msg.ID
					}
					select {
					case ch <- msg:
					case <-ctx.Done():
						return lastID
					}
				}
				msg = SSEMessage{}
				continue
			}

			if after, ok := strings.CutPrefix(line, "id: "); ok {
				msg.ID = after
			} else if after, ok := strings.CutPrefix(line, "event: "); ok {
				msg.Event = after
			} else if after, ok := strings.CutPrefix(line, "data: "); ok {
				msg.Data = after
			} else if line == "data:" {
				msg.Data = ""
			}
		case <-errCh:
			return lastID
		}
	}
}

// listenSSE returns a tea.Cmd that subscribes to an SSE stream and dispatches
// parsed events as Bubble Tea messages.
func listenSSE(client *SSEClient, stream Stream, path string, params map[string]string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		ch := make(chan SSEMessage, 64)

		go client.Connect(ctx, path, params, "", ch)

		// Return a subscription that will keep sending messages.
		_ = cancel // keep cancel available for cleanup
		return sseSubscription{
			stream: stream,
			ch:     ch,
			cancel: cancel,
		}
	}
}

// sseSubscription is an internal message carrying the SSE channel.
type sseSubscription struct {
	stream Stream
	ch     <-chan SSEMessage
	cancel context.CancelFunc
}

// waitForSSE reads the next message from an SSE channel and returns it as a tea.Msg.
func waitForSSE(stream Stream, ch <-chan SSEMessage) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return SSEStatusMsg{Stream: stream, Connected: false}
		}

		if msg.Event == "heartbeat" {
			return sseHeartbeat{stream: stream, ch: ch}
		}

		switch msg.Event {
		case "syslog":
			var evt model.SyslogEvent
			if err := json.Unmarshal([]byte(msg.Data), &evt); err != nil {
				return sseHeartbeat{stream: stream, ch: ch} // skip bad message
			}
			return sseEventReceived{
				stream: stream,
				ch:     ch,
				msg:    SyslogEventMsg{Event: evt},
			}
		case "applog":
			var evt model.AppLogEvent
			if err := json.Unmarshal([]byte(msg.Data), &evt); err != nil {
				return sseHeartbeat{stream: stream, ch: ch}
			}
			return sseEventReceived{
				stream: stream,
				ch:     ch,
				msg:    AppLogEventMsg{Event: evt},
			}
		default:
			return sseHeartbeat{stream: stream, ch: ch}
		}
	}
}

// sseHeartbeat is an internal message to continue reading the SSE channel.
type sseHeartbeat struct {
	stream Stream
	ch     <-chan SSEMessage
}

// sseEventReceived wraps a parsed event and the channel for continued reading.
type sseEventReceived struct {
	stream Stream
	ch     <-chan SSEMessage
	msg    tea.Msg
}
