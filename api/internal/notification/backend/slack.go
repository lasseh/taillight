// Package backend provides notification backend implementations.
package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/notification"
)

// slackConfig is the channel config schema for Slack.
type slackConfig struct {
	WebhookURL string `json:"webhook_url"`
}

// Slack implements the Notifier interface for Slack Incoming Webhooks.
type Slack struct {
	client *http.Client
	logger *slog.Logger
}

// NewSlack creates a new Slack backend.
func NewSlack(logger *slog.Logger) *Slack {
	return &Slack{
		client: newSSRFSafeClient(10 * time.Second),
		logger: logger.With("backend", "slack"),
	}
}

// Validate checks that the channel config contains a valid webhook URL.
func (s *Slack) Validate(ch notification.Channel) error {
	var cfg slackConfig
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		return fmt.Errorf("invalid slack config: malformed JSON")
	}
	if cfg.WebhookURL == "" {
		return fmt.Errorf("webhook_url is required")
	}
	if !strings.HasPrefix(cfg.WebhookURL, "https://") {
		return fmt.Errorf("webhook_url must use HTTPS")
	}
	if err := validateExternalURL(context.Background(), cfg.WebhookURL); err != nil {
		return fmt.Errorf("webhook_url rejected: %w", err)
	}
	return nil
}

// Send delivers a notification via Slack Incoming Webhook.
func (s *Slack) Send(ctx context.Context, ch notification.Channel, payload notification.Payload) notification.SendResult {
	start := time.Now()

	var cfg slackConfig
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		return notification.SendResult{Error: fmt.Errorf("parse slack config: %w", err), Duration: time.Since(start)}
	}

	msg := buildSlackMessage(payload)
	body, err := json.Marshal(msg)
	if err != nil {
		return notification.SendResult{Error: fmt.Errorf("marshal slack message: %w", err), Duration: time.Since(start)}
	}

	if err := validateExternalURL(ctx, cfg.WebhookURL); err != nil {
		return notification.SendResult{Error: fmt.Errorf("webhook_url rejected: %w", err), Duration: time.Since(start)}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return notification.SendResult{Error: fmt.Errorf("create request: %w", err), Duration: time.Since(start)}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return notification.SendResult{Error: fmt.Errorf("send slack webhook: %w", err), Duration: time.Since(start)}
	}
	defer resp.Body.Close()               //nolint:errcheck // Response body close error is not actionable.
	_, _ = io.Copy(io.Discard, resp.Body) // Drain body to allow connection reuse.

	result := notification.SendResult{
		StatusCode: resp.StatusCode,
		Duration:   time.Since(start),
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		result.Error = fmt.Errorf("slack rate limited (429)")
		return result
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Success = true
	} else {
		result.Error = fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}

	return result
}

// buildSlackMessage creates a compact Block Kit message from the payload.
func buildSlackMessage(p notification.Payload) map[string]any {
	color := severityColor(p)

	var text string
	if p.IsDigest {
		text = buildSlackDigest(p)
	} else {
		text = buildSlackInitial(p)
	}

	blocks := []map[string]any{
		{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": text,
			},
		},
	}

	return map[string]any{
		"attachments": []map[string]any{
			{
				"color":  color,
				"blocks": blocks,
			},
		},
	}
}

// buildSlackInitial formats the initial (non-digest) notification.
func buildSlackInitial(p notification.Payload) string {
	var summary, message string

	if p.SrvlogEvent != nil {
		e := p.SrvlogEvent
		summary = fmt.Sprintf("%s - %s", e.Hostname, strings.ToUpper(model.SeverityLabel(e.Severity)))
		message = e.Message
	}

	if p.NetlogEvent != nil {
		e := p.NetlogEvent
		summary = fmt.Sprintf("%s - %s", e.Hostname, strings.ToUpper(model.SeverityLabel(e.Severity)))
		message = e.Message
	}

	if p.AppLogEvent != nil {
		e := p.AppLogEvent
		summary = fmt.Sprintf("%s - %s", e.Host, e.Level)
		message = e.Msg
	}

	if p.EventCount > 1 {
		summary += fmt.Sprintf(" (%d events)", p.EventCount)
	}

	return summary + "\n```\n" + escapeSlackCodeBlock(truncate(message, 2900)) + "\n```"
}

// buildSlackDigest formats a digest summary notification.
func buildSlackDigest(p notification.Payload) string {
	var summary, lastMessage string
	windowMin := int(p.Window.Minutes())
	windowLabel := fmt.Sprintf("%d minutes", windowMin)
	if windowMin < 1 {
		windowLabel = fmt.Sprintf("%d seconds", int(p.Window.Seconds()))
	}

	if p.SrvlogEvent != nil {
		e := p.SrvlogEvent
		summary = fmt.Sprintf("%s - %s (digest)", e.Hostname, strings.ToUpper(model.SeverityLabel(e.Severity)))
		lastMessage = e.Message
	}

	if p.NetlogEvent != nil {
		e := p.NetlogEvent
		summary = fmt.Sprintf("%s - %s (digest)", e.Hostname, strings.ToUpper(model.SeverityLabel(e.Severity)))
		lastMessage = e.Message
	}

	if p.AppLogEvent != nil {
		e := p.AppLogEvent
		summary = fmt.Sprintf("%s - %s (digest)", e.Host, e.Level)
		lastMessage = e.Msg
	}

	return fmt.Sprintf("%s\n*%d more events* in the last %s\nLast seen: `%s`",
		summary, p.EventCount, windowLabel, truncate(lastMessage, 500))
}

func severityColor(p notification.Payload) string {
	if p.SrvlogEvent != nil {
		return syslogSeverityColor(p.SrvlogEvent.Severity)
	}
	if p.NetlogEvent != nil {
		return syslogSeverityColor(p.NetlogEvent.Severity)
	}
	if p.AppLogEvent != nil {
		switch p.AppLogEvent.Level {
		case "FATAL":
			return "#E74C3C"
		case "ERROR":
			return "#E67E22"
		case "WARN":
			return "#F1C40F"
		default:
			return "#2ECC71"
		}
	}
	return "#3498DB"
}

// syslogSeverityColor maps a syslog severity integer to a Slack attachment color.
func syslogSeverityColor(severity int) string {
	switch {
	case severity <= 2:
		return "#E74C3C" // red
	case severity == 3:
		return "#E67E22" // orange
	case severity == 4:
		return "#F1C40F" // yellow
	case severity <= 6:
		return "#2ECC71" // green
	default:
		return "#95A5A6" // gray
	}
}

// escapeSlackCodeBlock replaces triple-backtick sequences that would break
// out of a Slack mrkdwn code block.
func escapeSlackCodeBlock(s string) string {
	return strings.ReplaceAll(s, "```", "` ` `")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
