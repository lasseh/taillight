package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// SSEEvent is a parsed Server-Sent Event.
type SSEEvent struct {
	ID    int64
	Event string          // "srvlog", "applog", "netlog", "heartbeat"
	Data  json.RawMessage // raw JSON payload
}

// SSEStream manages a long-lived SSE connection with automatic reconnection.
type SSEStream struct {
	eventCh   chan SSEEvent
	cancel    context.CancelFunc
	connected atomic.Bool
	lastID    atomic.Int64
}

// NewSSEStream starts an SSE stream to the given path with filter parameters.
// Events are delivered on the returned stream's channel. The stream reconnects
// automatically on disconnection with exponential backoff.
func NewSSEStream(c *Client, path string, params url.Values, lastEventID int64) *SSEStream {
	ctx, cancel := context.WithCancel(context.Background())
	s := &SSEStream{
		eventCh: make(chan SSEEvent, 256),
		cancel:  cancel,
	}
	s.lastID.Store(lastEventID)

	go s.run(ctx, c, path, params)
	return s
}

// Events returns the channel on which parsed SSE events are delivered.
func (s *SSEStream) Events() <-chan SSEEvent {
	return s.eventCh
}

// Connected reports whether the stream is currently connected.
func (s *SSEStream) Connected() bool {
	return s.connected.Load()
}

// LastID returns the ID of the most recently received event.
func (s *SSEStream) LastID() int64 {
	return s.lastID.Load()
}

// Close stops the stream and closes the event channel.
func (s *SSEStream) Close() {
	s.cancel()
}

func (s *SSEStream) run(ctx context.Context, c *Client, path string, params url.Values) {
	defer close(s.eventCh)

	httpClient := c.SSEClient()
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if attempt > 0 {
			delay := backoff(attempt)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}

		err := s.connect(ctx, httpClient, c, path, params)
		if ctx.Err() != nil {
			return // cancelled
		}
		s.connected.Store(false)
		attempt++
		_ = err // reconnect silently
	}
}

func (s *SSEStream) connect(ctx context.Context, httpClient *http.Client, c *Client, path string, params url.Values) error {
	u, err := url.JoinPath(c.BaseURL(), path)
	if err != nil {
		return fmt.Errorf("build URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.URL.RawQuery = params.Encode()
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if c.APIKey() != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey())
	}
	if lastID := s.lastID.Load(); lastID > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatInt(lastID, 10))
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("SSE connect: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SSE status %d", resp.StatusCode)
	}

	s.connected.Store(true)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	var (
		eventID    int64
		eventType  string
		dataBuffer strings.Builder
	)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()

		switch {
		case line == "":
			// Empty line = event dispatch.
			if dataBuffer.Len() > 0 {
				evt := SSEEvent{
					ID:    eventID,
					Event: eventType,
					Data:  json.RawMessage(dataBuffer.String()),
				}
				if eventType != "heartbeat" {
					select {
					case s.eventCh <- evt:
					default:
						// Channel full — drop event to avoid blocking.
					}
				}
				if eventID > 0 {
					s.lastID.Store(eventID)
				}
			}
			// Reset for next event.
			eventID = 0
			eventType = ""
			dataBuffer.Reset()

		case strings.HasPrefix(line, "id: "):
			eventID, _ = strconv.ParseInt(strings.TrimPrefix(line, "id: "), 10, 64)

		case strings.HasPrefix(line, "event: "):
			eventType = strings.TrimPrefix(line, "event: ")

		case strings.HasPrefix(line, "data: "):
			if dataBuffer.Len() > 0 {
				dataBuffer.WriteByte('\n')
			}
			dataBuffer.WriteString(strings.TrimPrefix(line, "data: "))

		default:
			// SSE comment or unknown field — ignore.
		}
	}

	return scanner.Err()
}

// backoff returns a delay with exponential backoff and jitter.
// attempt 1 = ~1s, attempt 2 = ~2s, ..., capped at 30s.
func backoff(attempt int) time.Duration {
	base := math.Min(float64(attempt)*1000, 30000)
	jitter := rand.Float64() * base * 0.3 //nolint:gosec // non-crypto jitter
	return time.Duration(base+jitter) * time.Millisecond
}
