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
	"text/template"
	"time"

	"github.com/lasseh/taillight/internal/notification"
)

// webhookConfig is the channel config schema for generic webhooks.
type webhookConfig struct {
	URL      string            `json:"url"`
	Method   string            `json:"method,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Template string            `json:"template,omitempty"`
}

// defaultWebhookTemplate is used when no custom template is configured.
// All string fields use the marshal helper to produce valid, escaped JSON.
var defaultWebhookTemplate = `{
  "source": "taillight",
  "rule": {{marshal .RuleName}},
  "kind": {{marshal .Kind}},
  "event_count": {{.EventCount}},
  "is_digest": {{.IsDigest}},
  "group_key": {{marshal .GroupKey}},
  "window_seconds": {{.Window.Seconds}},
  "timestamp": "{{.Timestamp.Format "2006-01-02T15:04:05Z07:00"}}",
  {{- if .SrvlogEvent}}
  "hostname": {{marshal .SrvlogEvent.Hostname}},
  "program": {{marshal .SrvlogEvent.Programname}},
  "severity": {{.SrvlogEvent.Severity}},
  "severity_label": {{marshal .SrvlogEvent.SeverityLabel}},
  "message": {{marshal .SrvlogEvent.Message}}
  {{- else if .AppLogEvent}}
  "service": {{marshal .AppLogEvent.Service}},
  "level": {{marshal .AppLogEvent.Level}},
  "host": {{marshal .AppLogEvent.Host}},
  "message": {{marshal .AppLogEvent.Msg}}
  {{- end}}
}`

var templateFuncs = template.FuncMap{
	"marshal": func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	},
}

// Webhook implements the Notifier interface for generic HTTP webhooks.
type Webhook struct {
	client *http.Client
	logger *slog.Logger
}

// NewWebhook creates a new Webhook backend.
func NewWebhook(logger *slog.Logger) *Webhook {
	return &Webhook{
		client: newSSRFSafeClient(10 * time.Second),
		logger: logger.With("backend", "webhook"),
	}
}

// Validate checks that the channel config contains a valid URL and optional template.
func (w *Webhook) Validate(ch notification.Channel) error {
	var cfg webhookConfig
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		return fmt.Errorf("invalid webhook config: malformed JSON")
	}
	if cfg.URL == "" {
		return fmt.Errorf("url is required")
	}
	if err := validateExternalURL(context.Background(), cfg.URL); err != nil {
		return fmt.Errorf("url rejected: %w", err)
	}
	if cfg.Method != "" {
		allowed := map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true}
		if !allowed[cfg.Method] {
			return fmt.Errorf("method must be one of GET, POST, PUT, PATCH")
		}
	}
	if cfg.Template != "" {
		if _, err := template.New("validate").Funcs(templateFuncs).Parse(cfg.Template); err != nil {
			return fmt.Errorf("invalid template syntax")
		}
	}
	// Block sensitive headers that could be used for request smuggling.
	for k := range cfg.Headers {
		lower := strings.ToLower(k)
		if lower == "host" || lower == "transfer-encoding" || lower == "content-length" {
			return fmt.Errorf("header %q is not allowed", k)
		}
	}
	return nil
}

// Send delivers a notification via HTTP webhook.
func (w *Webhook) Send(ctx context.Context, ch notification.Channel, payload notification.Payload) notification.SendResult {
	start := time.Now()

	var cfg webhookConfig
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		return notification.SendResult{Error: fmt.Errorf("parse webhook config: %w", err), Duration: time.Since(start)}
	}

	method := cfg.Method
	if method == "" {
		method = http.MethodPost
	}

	tmplStr := cfg.Template
	if tmplStr == "" {
		tmplStr = defaultWebhookTemplate
	}

	tmpl, err := template.New("webhook").Funcs(templateFuncs).Parse(tmplStr)
	if err != nil {
		return notification.SendResult{Error: fmt.Errorf("parse template: %w", err), Duration: time.Since(start)}
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, payload); err != nil {
		return notification.SendResult{Error: fmt.Errorf("execute template: %w", err), Duration: time.Since(start)}
	}

	if err := validateExternalURL(ctx, cfg.URL); err != nil {
		return notification.SendResult{Error: fmt.Errorf("url rejected: %w", err), Duration: time.Since(start)}
	}

	req, err := http.NewRequestWithContext(ctx, method, cfg.URL, &buf)
	if err != nil {
		return notification.SendResult{Error: fmt.Errorf("create request: %w", err), Duration: time.Since(start)}
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return notification.SendResult{Error: fmt.Errorf("send webhook: %w", err), Duration: time.Since(start)}
	}
	defer resp.Body.Close()               //nolint:errcheck // Response body close error is not actionable.
	_, _ = io.Copy(io.Discard, resp.Body) // Drain body to allow connection reuse.

	result := notification.SendResult{
		StatusCode: resp.StatusCode,
		Duration:   time.Since(start),
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Success = true
	} else {
		result.Error = fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return result
}
