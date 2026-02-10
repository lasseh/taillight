// Package backend provides notification backend implementations.
package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/notification"
)

// slackConfig is the channel config schema for Slack.
type slackConfig struct {
	WebhookURL string `json:"webhook_url"`
	Channel    string `json:"channel,omitempty"`
	Username   string `json:"username,omitempty"`
	IconEmoji  string `json:"icon_emoji,omitempty"`
	IconURL    string `json:"icon_url,omitempty"`
}

// Slack implements the Notifier interface for Slack Incoming Webhooks.
type Slack struct {
	client *http.Client
	logger *slog.Logger
}

// NewSlack creates a new Slack backend.
func NewSlack(logger *slog.Logger) *Slack {
	return &Slack{
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger.With("backend", "slack"),
	}
}

// Type returns the backend type identifier.
func (s *Slack) Type() string { return "slack" }

// Validate checks that the channel config contains a valid webhook URL.
func (s *Slack) Validate(ch notification.Channel) error {
	var cfg slackConfig
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		return fmt.Errorf("invalid slack config: %w", err)
	}
	if cfg.WebhookURL == "" {
		return fmt.Errorf("webhook_url is required")
	}
	if !strings.HasPrefix(cfg.WebhookURL, "https://") {
		return fmt.Errorf("webhook_url must use HTTPS")
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

	msg := buildSlackMessage(cfg, payload)
	body, err := json.Marshal(msg)
	if err != nil {
		return notification.SendResult{Error: fmt.Errorf("marshal slack message: %w", err), Duration: time.Since(start)}
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
	defer resp.Body.Close() //nolint:errcheck // Response body close error is not actionable.

	result := notification.SendResult{
		StatusCode: resp.StatusCode,
		Duration:   time.Since(start),
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		if v := resp.Header.Get("Retry-After"); v != "" {
			if secs, err := strconv.Atoi(v); err == nil {
				result.RetryAfter = time.Duration(secs) * time.Second
			}
		}
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

// buildSlackMessage creates a Block Kit message from the payload.
func buildSlackMessage(cfg slackConfig, p notification.Payload) map[string]any {
	color := severityColor(p)
	header := fmt.Sprintf("Taillight Alert: %s", p.RuleName)

	var msgText string
	if p.EventCount > 1 {
		msgText = fmt.Sprintf("*%d events* matched rule *%s*", p.EventCount, p.RuleName)
	}

	var fields []map[string]any

	if p.SyslogEvent != nil {
		e := p.SyslogEvent
		fields = append(fields,
			slackField("Host", e.Hostname),
			slackField("Program", e.Programname),
			slackField("Severity", model.SeverityLabel(e.Severity)),
			slackField("Facility", model.FacilityLabel(e.Facility)),
		)
		if msgText == "" {
			msgText = e.Message
		} else {
			msgText += "\n\n" + e.Message
		}
	}

	if p.AppLogEvent != nil {
		e := p.AppLogEvent
		fields = append(fields,
			slackField("Service", e.Service),
			slackField("Level", e.Level),
			slackField("Host", e.Host),
			slackField("Component", e.Component),
		)
		if msgText == "" {
			msgText = e.Msg
		} else {
			msgText += "\n\n" + e.Msg
		}
	}

	blocks := []map[string]any{
		{
			"type": "header",
			"text": map[string]any{
				"type": "plain_text",
				"text": header,
			},
		},
		{
			"type":   "section",
			"fields": fields,
		},
		{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": "```\n" + truncate(msgText, 2900) + "\n```",
			},
		},
		{
			"type": "context",
			"elements": []map[string]any{
				{
					"type": "mrkdwn",
					"text": fmt.Sprintf("_%s | %d event(s)_", p.Timestamp.Format(time.RFC3339), p.EventCount),
				},
			},
		},
	}

	msg := map[string]any{
		"attachments": []map[string]any{
			{
				"color":  color,
				"blocks": blocks,
			},
		},
	}

	if cfg.Channel != "" {
		msg["channel"] = cfg.Channel
	}
	if cfg.Username != "" {
		msg["username"] = cfg.Username
	}
	if cfg.IconEmoji != "" {
		msg["icon_emoji"] = cfg.IconEmoji
	}
	if cfg.IconURL != "" {
		msg["icon_url"] = cfg.IconURL
	}

	return msg
}

func slackField(label, value string) map[string]any {
	if value == "" {
		value = "-"
	}
	return map[string]any{
		"type": "mrkdwn",
		"text": fmt.Sprintf("*%s:*\n%s", label, value),
	}
}

func severityColor(p notification.Payload) string {
	if p.SyslogEvent != nil {
		switch {
		case p.SyslogEvent.Severity <= 2:
			return "#E74C3C" // red
		case p.SyslogEvent.Severity == 3:
			return "#E67E22" // orange
		case p.SyslogEvent.Severity == 4:
			return "#F1C40F" // yellow
		case p.SyslogEvent.Severity <= 6:
			return "#2ECC71" // green
		default:
			return "#95A5A6" // gray
		}
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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
