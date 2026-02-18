package backend

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/notification"
)

// emailConfig is the per-channel config schema for email notifications.
type emailConfig struct {
	To              []string `json:"to"`
	SubjectTemplate string   `json:"subject_template,omitempty"`
}

// EmailGlobalConfig holds SMTP connection settings from the app config.
type EmailGlobalConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLS      bool
	AuthType string // "plain", "login", "crammd5", or "" (no auth).
}

// Email implements the Notifier interface for SMTP email delivery.
type Email struct {
	cfg    EmailGlobalConfig
	logger *slog.Logger
}

// NewEmail creates a new Email backend.
func NewEmail(cfg EmailGlobalConfig, logger *slog.Logger) *Email {
	return &Email{
		cfg:    cfg,
		logger: logger.With("backend", "email"),
	}
}

// Validate checks that the channel config contains valid email addresses.
func (e *Email) Validate(ch notification.Channel) error {
	var cfg emailConfig
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		return fmt.Errorf("invalid email config: %w", err)
	}
	if len(cfg.To) == 0 {
		return fmt.Errorf("to is required")
	}
	for _, addr := range cfg.To {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("invalid email address %q: %w", addr, err)
		}
	}
	return nil
}

// Send delivers a notification via SMTP email.
func (e *Email) Send(ctx context.Context, ch notification.Channel, payload notification.Payload) notification.SendResult {
	start := time.Now()

	var cfg emailConfig
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		return notification.SendResult{Error: fmt.Errorf("parse email config: %w", err), Duration: time.Since(start)}
	}

	subject := buildEmailSubject(cfg.SubjectTemplate, payload)
	body := buildEmailBody(payload)

	msg := buildMIMEMessage(e.cfg.From, cfg.To, subject, body)

	if err := e.sendSMTP(ctx, cfg.To, msg); err != nil {
		return notification.SendResult{Error: fmt.Errorf("send email: %w", err), Duration: time.Since(start)}
	}

	return notification.SendResult{
		Success:  true,
		Duration: time.Since(start),
	}
}

// sendSMTP connects to the SMTP server and sends the message.
func (e *Email) sendSMTP(ctx context.Context, to []string, msg []byte) error {
	addr := net.JoinHostPort(e.cfg.Host, fmt.Sprintf("%d", e.cfg.Port))

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial SMTP server: %w", err)
	}

	c, err := smtp.NewClient(conn, e.cfg.Host)
	if err != nil {
		conn.Close() //nolint:errcheck // Best-effort cleanup on client creation failure.
		return fmt.Errorf("create SMTP client: %w", err)
	}
	defer c.Close() //nolint:errcheck // Best-effort cleanup.

	// STARTTLS if configured.
	if e.cfg.TLS {
		if err := c.StartTLS(&tls.Config{ServerName: e.cfg.Host}); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}

	// Authenticate if configured.
	auth, err := e.smtpAuth()
	if err != nil {
		return err
	}
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth: %w", err)
		}
	}

	if err := c.Mail(e.cfg.From); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, addr := range to {
		if err := c.Rcpt(addr); err != nil {
			return fmt.Errorf("RCPT TO %q: %w", addr, err)
		}
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close DATA: %w", err)
	}

	return c.Quit()
}

// smtpAuth returns the appropriate smtp.Auth based on the configured auth type.
func (e *Email) smtpAuth() (smtp.Auth, error) {
	switch strings.ToLower(e.cfg.AuthType) {
	case "plain":
		return smtp.PlainAuth("", e.cfg.Username, e.cfg.Password, e.cfg.Host), nil
	case "crammd5":
		return smtp.CRAMMD5Auth(e.cfg.Username, e.cfg.Password), nil
	case "", "none":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported SMTP auth type: %q", e.cfg.AuthType)
	}
}

// buildMIMEMessage constructs a raw MIME email message.
func buildMIMEMessage(from string, to []string, subject, htmlBody string) []byte {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(htmlBody)
	return []byte(b.String())
}

// buildEmailSubject creates the email subject line.
func buildEmailSubject(tmpl string, p notification.Payload) string {
	if tmpl != "" {
		return tmpl
	}

	prefix := "[Taillight]"
	if p.SyslogEvent != nil {
		return fmt.Sprintf("%s %s - %s", prefix, p.SyslogEvent.Hostname, strings.ToUpper(model.SeverityLabel(p.SyslogEvent.Severity)))
	}
	if p.AppLogEvent != nil {
		return fmt.Sprintf("%s %s - %s", prefix, p.AppLogEvent.Host, p.AppLogEvent.Level)
	}
	return prefix + " Notification"
}

// buildEmailBody creates an HTML email body with severity color coding.
func buildEmailBody(p notification.Payload) string {
	color := severityColor(p)

	var summary, message string
	if p.IsDigest {
		summary, message = buildEmailDigest(p)
	} else {
		summary, message = buildEmailInitial(p)
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; margin: 0; padding: 20px; background: #f5f5f5;">
  <div style="max-width: 600px; margin: 0 auto; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,0.1);">
    <div style="background: %s; padding: 12px 20px; color: #fff; font-weight: bold;">%s</div>
    <div style="padding: 20px;">
      <pre style="background: #f8f9fa; padding: 12px; border-radius: 4px; overflow-x: auto; font-size: 13px; line-height: 1.4;">%s</pre>
    </div>
    <div style="padding: 12px 20px; background: #f8f9fa; color: #888; font-size: 12px;">
      Rule: %s | %s
    </div>
  </div>
</body>
</html>`, color, summary, truncate(message, 5000), p.RuleName, p.Timestamp.Format(time.RFC3339))
}

// buildEmailInitial formats the initial (non-digest) notification parts.
func buildEmailInitial(p notification.Payload) (summary, message string) {
	if p.SyslogEvent != nil {
		e := p.SyslogEvent
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
	return summary, message
}

// buildEmailDigest formats a digest summary notification parts.
func buildEmailDigest(p notification.Payload) (summary, message string) {
	windowMin := int(p.Window.Minutes())
	windowLabel := fmt.Sprintf("%d minutes", windowMin)
	if windowMin < 1 {
		windowLabel = fmt.Sprintf("%d seconds", int(p.Window.Seconds()))
	}

	if p.SyslogEvent != nil {
		e := p.SyslogEvent
		summary = fmt.Sprintf("%s - %s (digest)", e.Hostname, strings.ToUpper(model.SeverityLabel(e.Severity)))
		message = e.Message
	}
	if p.AppLogEvent != nil {
		e := p.AppLogEvent
		summary = fmt.Sprintf("%s - %s (digest)", e.Host, e.Level)
		message = e.Msg
	}

	message = fmt.Sprintf("%d more events in the last %s\nLast seen: %s", p.EventCount, windowLabel, truncate(message, 500))
	return summary, message
}
