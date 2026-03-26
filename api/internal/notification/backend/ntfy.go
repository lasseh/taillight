package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/notification"
)

// Applog level strings used for priority and tag mapping.
const (
	levelFatal = "FATAL"
	levelError = "ERROR"
	levelWarn  = "WARN"
)

// ntfyConfig is the per-channel config schema for ntfy.
type ntfyConfig struct {
	ServerURL string `json:"server_url"` // e.g. "https://ntfy.sh" or self-hosted URL.
	Topic     string `json:"topic"`
	Token     string `json:"token,omitempty"`    // Bearer token for authentication.
	Priority  int    `json:"priority,omitempty"` // Fixed priority 1-5; 0 = auto-map from severity.
}

// Ntfy implements the Notifier interface for ntfy push notifications.
type Ntfy struct {
	client *http.Client
	logger *slog.Logger
}

// NewNtfy creates a new ntfy backend.
func NewNtfy(logger *slog.Logger) *Ntfy {
	return &Ntfy{
		client: newSSRFSafeClient(10 * time.Second),
		logger: logger.With("backend", "ntfy"),
	}
}

// Validate checks that the channel config contains a valid ntfy server URL and topic.
func (n *Ntfy) Validate(ch notification.Channel) error {
	var cfg ntfyConfig
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		return fmt.Errorf("invalid ntfy config: malformed JSON")
	}
	if cfg.ServerURL == "" {
		return fmt.Errorf("server_url is required")
	}
	if !strings.HasPrefix(cfg.ServerURL, "http://") && !strings.HasPrefix(cfg.ServerURL, "https://") {
		return fmt.Errorf("server_url must use HTTP or HTTPS")
	}
	if cfg.Topic == "" {
		return fmt.Errorf("topic is required")
	}
	if cfg.Priority < 0 || cfg.Priority > 5 {
		return fmt.Errorf("priority must be between 0 and 5")
	}
	if err := validateExternalURL(context.Background(), cfg.ServerURL); err != nil {
		return fmt.Errorf("server_url rejected: %w", err)
	}
	return nil
}

// Send delivers a notification via ntfy.
func (n *Ntfy) Send(ctx context.Context, ch notification.Channel, payload notification.Payload) notification.SendResult {
	start := time.Now()

	var cfg ntfyConfig
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		return notification.SendResult{Error: fmt.Errorf("parse ntfy config: %w", err), Duration: time.Since(start)}
	}

	title, body := buildNtfyMessage(payload)
	priority := cfg.Priority
	if priority == 0 {
		priority = ntfyPriority(payload)
	}
	tags := ntfyTags(payload)

	targetURL := strings.TrimRight(cfg.ServerURL, "/") + "/" + cfg.Topic

	if err := validateExternalURL(ctx, targetURL); err != nil {
		return notification.SendResult{Error: fmt.Errorf("server_url rejected: %w", err), Duration: time.Since(start)}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewBufferString(body))
	if err != nil {
		return notification.SendResult{Error: fmt.Errorf("create request: %w", err), Duration: time.Since(start)}
	}
	req.Header.Set("Title", sanitizeHeaderValue(title))
	req.Header.Set("Priority", strconv.Itoa(priority))
	if tags != "" {
		req.Header.Set("Tags", sanitizeHeaderValue(tags))
	}
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return notification.SendResult{Error: fmt.Errorf("send ntfy request: %w", err), Duration: time.Since(start)}
	}
	defer resp.Body.Close()               //nolint:errcheck // Response body close error is not actionable.
	_, _ = io.Copy(io.Discard, resp.Body) // Drain body to allow connection reuse.

	result := notification.SendResult{
		StatusCode: resp.StatusCode,
		Duration:   time.Since(start),
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		result.Error = fmt.Errorf("ntfy rate limited (429)")
		return result
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Success = true
	} else {
		result.Error = fmt.Errorf("ntfy returned status %d", resp.StatusCode)
	}

	return result
}

// buildNtfyMessage returns a title and plain-text body for the notification.
func buildNtfyMessage(p notification.Payload) (title, body string) {
	if p.IsDigest {
		return buildNtfyDigest(p)
	}
	return buildNtfyInitial(p)
}

// buildNtfyInitial formats an initial (non-digest) notification.
func buildNtfyInitial(p notification.Payload) (title, body string) {
	if p.SrvlogEvent != nil {
		e := p.SrvlogEvent
		title = fmt.Sprintf("[%s] %s %s", strings.ToUpper(model.SeverityLabel(e.Severity)), e.Hostname, e.Programname)
		if p.EventCount > 1 {
			title += fmt.Sprintf(" (%d events)", p.EventCount)
		}
		body = truncate(e.Message, 2900)
	}
	if p.NetlogEvent != nil {
		e := p.NetlogEvent
		title = fmt.Sprintf("[%s] %s %s", strings.ToUpper(model.SeverityLabel(e.Severity)), e.Hostname, e.Programname)
		if p.EventCount > 1 {
			title += fmt.Sprintf(" (%d events)", p.EventCount)
		}
		body = truncate(e.Message, 2900)
	}
	if p.AppLogEvent != nil {
		e := p.AppLogEvent
		title = fmt.Sprintf("[%s] %s %s", e.Level, e.Service, e.Host)
		if p.EventCount > 1 {
			title += fmt.Sprintf(" (%d events)", p.EventCount)
		}
		body = truncate(e.Msg, 2900)
	}
	return title, body
}

// buildNtfyDigest formats a digest summary notification.
func buildNtfyDigest(p notification.Payload) (title, body string) {
	windowMin := int(p.Window.Minutes())
	windowLabel := fmt.Sprintf("%d minutes", windowMin)
	if windowMin < 1 {
		windowLabel = fmt.Sprintf("%d seconds", int(p.Window.Seconds()))
	}

	if p.SrvlogEvent != nil {
		e := p.SrvlogEvent
		title = fmt.Sprintf("[%s] %s (digest)", strings.ToUpper(model.SeverityLabel(e.Severity)), e.Hostname)
		body = fmt.Sprintf("%d more events in the last %s\nLast: %s", p.EventCount, windowLabel, truncate(e.Message, 500))
	}
	if p.NetlogEvent != nil {
		e := p.NetlogEvent
		title = fmt.Sprintf("[%s] %s (digest)", strings.ToUpper(model.SeverityLabel(e.Severity)), e.Hostname)
		body = fmt.Sprintf("%d more events in the last %s\nLast: %s", p.EventCount, windowLabel, truncate(e.Message, 500))
	}
	if p.AppLogEvent != nil {
		e := p.AppLogEvent
		title = fmt.Sprintf("[%s] %s (digest)", e.Level, e.Service)
		body = fmt.Sprintf("%d more events in the last %s\nLast: %s", p.EventCount, windowLabel, truncate(e.Msg, 500))
	}
	return title, body
}

// ntfyPriority maps event severity to ntfy priority (1-5).
func ntfyPriority(p notification.Payload) int {
	if p.SrvlogEvent != nil {
		return syslogNtfyPriority(p.SrvlogEvent.Severity)
	}
	if p.NetlogEvent != nil {
		return syslogNtfyPriority(p.NetlogEvent.Severity)
	}
	if p.AppLogEvent != nil {
		switch p.AppLogEvent.Level {
		case levelFatal:
			return 5
		case levelError:
			return 4
		case levelWarn:
			return 3
		default:
			return 2
		}
	}
	return 3
}

// ntfyTags returns comma-separated ntfy tags based on severity.
func ntfyTags(p notification.Payload) string {
	if p.SrvlogEvent != nil {
		return syslogNtfyTag(p.SrvlogEvent.Severity)
	}
	if p.NetlogEvent != nil {
		return syslogNtfyTag(p.NetlogEvent.Severity)
	}
	if p.AppLogEvent != nil {
		switch p.AppLogEvent.Level {
		case levelFatal:
			return "rotating_light"
		case levelError:
			return "x"
		case levelWarn:
			return "warning"
		default:
			return "information_source"
		}
	}
	return ""
}

// syslogNtfyPriority maps a syslog severity to ntfy priority (1-5).
func syslogNtfyPriority(severity int) int {
	switch {
	case severity <= 1:
		return 5 // urgent
	case severity <= 3:
		return 4 // high
	case severity == 4:
		return 3 // default
	default:
		return 2 // low
	}
}

// syslogNtfyTag maps a syslog severity to an ntfy tag.
func syslogNtfyTag(severity int) string {
	switch {
	case severity <= 2:
		return "rotating_light"
	case severity == 3:
		return "x"
	case severity == 4:
		return "warning"
	case severity <= 6:
		return "information_source"
	default:
		return "mag"
	}
}
