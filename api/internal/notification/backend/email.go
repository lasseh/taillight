package backend

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"text/template"
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
		return fmt.Errorf("invalid email config: malformed JSON")
	}
	if len(cfg.To) == 0 {
		return fmt.Errorf("to is required")
	}
	for i, addr := range cfg.To {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("invalid email address at position %d", i+1)
		}
	}
	if cfg.SubjectTemplate != "" {
		if _, err := template.New("validate").Parse(cfg.SubjectTemplate); err != nil {
			return fmt.Errorf("invalid subject_template syntax: %w", err)
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
// All header values are sanitized to prevent CRLF injection.
func buildMIMEMessage(from string, to []string, subject, htmlBody string) []byte {
	var b strings.Builder
	b.WriteString("From: " + sanitizeHeaderValue(from) + "\r\n")
	b.WriteString("To: " + sanitizeHeaderValue(strings.Join(to, ", ")) + "\r\n")
	b.WriteString("Subject: " + sanitizeHeaderValue(subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(htmlBody)
	return []byte(b.String())
}

// sanitizeHeaderValue strips CR and LF characters to prevent header injection.
func sanitizeHeaderValue(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

// buildEmailSubject creates the email subject line.
// If a subject_template is configured, it is executed as a Go text/template.
func buildEmailSubject(tmpl string, p notification.Payload) string {
	if tmpl != "" {
		t, err := template.New("email-subject").Parse(tmpl)
		if err != nil {
			return sanitizeHeaderValue(tmpl) // Fallback to literal if invalid.
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, p); err != nil {
			return sanitizeHeaderValue(tmpl) // Fallback to literal on execution error.
		}
		return sanitizeHeaderValue(buf.String())
	}

	prefix := "[Taillight]"
	if p.SummaryReport != nil {
		r := p.SummaryReport
		freq := r.Schedule.Frequency
		if freq != "" {
			freq = strings.ToUpper(freq[:1]) + freq[1:]
		}
		return fmt.Sprintf("%s %s Summary — %s to %s",
			prefix,
			freq,
			r.From.Format("Jan 2"),
			r.To.Format("Jan 2, 2006"),
		)
	}
	if p.SrvlogEvent != nil {
		return fmt.Sprintf("%s %s - %s", prefix, p.SrvlogEvent.Hostname, strings.ToUpper(model.SeverityLabel(p.SrvlogEvent.Severity)))
	}
	if p.NetlogEvent != nil {
		return fmt.Sprintf("%s %s - %s", prefix, p.NetlogEvent.Hostname, strings.ToUpper(model.SeverityLabel(p.NetlogEvent.Severity)))
	}
	if p.AppLogEvent != nil {
		return fmt.Sprintf("%s %s - %s", prefix, p.AppLogEvent.Host, p.AppLogEvent.Level)
	}
	return prefix + " Notification"
}

// buildEmailBody creates an HTML email body with severity color coding.
func buildEmailBody(p notification.Payload) string {
	if p.SummaryReport != nil {
		return buildEmailSummary(p.SummaryReport)
	}

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
</html>`, color, html.EscapeString(summary), html.EscapeString(truncate(message, 5000)), html.EscapeString(p.RuleName), p.Timestamp.Format(time.RFC3339))
}

// buildEmailInitial formats the initial (non-digest) notification parts.
func buildEmailInitial(p notification.Payload) (summary, message string) {
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
	return summary, message
}

// Summary email color constants.
const (
	colorGray   = "#6b7280"
	colorRed    = "#ef4444"
	colorGreen  = "#22c55e"
	trendStable = "—"
)

// buildEmailSummary renders a full HTML summary report email.
func buildEmailSummary(r *notification.SummaryReport) string {
	var b strings.Builder

	b.WriteString(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; margin: 0; padding: 20px; background: #f5f5f5;">
  <div style="max-width: 700px; margin: 0 auto; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,0.1);">`)

	// Header.
	freq := r.Schedule.Frequency
	if freq != "" {
		freq = strings.ToUpper(freq[:1]) + freq[1:]
	}
	fmt.Fprintf(&b, `<div style="background: #2563eb; padding: 16px 20px; color: #fff;">
      <div style="font-size: 18px; font-weight: bold;">Taillight %s Summary</div>
      <div style="font-size: 13px; opacity: 0.85; margin-top: 4px;">%s — %s (%s)</div>
    </div>`,
		html.EscapeString(freq),
		r.From.Format("Jan 2 15:04 UTC"),
		r.To.Format("Jan 2 15:04 UTC"),
		html.EscapeString(r.PeriodLabel),
	)

	b.WriteString(`<div style="padding: 20px;">`)

	// Per-kind overview sections.
	if r.Srvlog != nil {
		writeSyslogSection(&b, "Srvlog", r.Srvlog)
	}
	if r.Netlog != nil {
		writeSyslogSection(&b, "Netlog", r.Netlog)
	}
	if r.AppLog != nil {
		writeAppLogSection(&b, r.AppLog)
	}

	// Top issues table.
	if len(r.TopIssues) > 0 {
		b.WriteString(`<h3 style="font-size: 14px; color: #374151; margin: 20px 0 8px;">Top Issues</h3>`)
		b.WriteString(`<table style="width: 100%; border-collapse: collapse; font-size: 12px;">`)
		b.WriteString(`<tr style="background: #f8f9fa; text-align: left;">
        <th style="padding: 6px 8px; border-bottom: 1px solid #e5e7eb;">Severity</th>
        <th style="padding: 6px 8px; border-bottom: 1px solid #e5e7eb;">Source</th>
        <th style="padding: 6px 8px; border-bottom: 1px solid #e5e7eb;">Program</th>
        <th style="padding: 6px 8px; border-bottom: 1px solid #e5e7eb;">Message</th>
        <th style="padding: 6px 8px; border-bottom: 1px solid #e5e7eb; text-align: right;">Count</th>
      </tr>`)
		for _, issue := range r.TopIssues {
			color := issueSeverityColor(issue.Severity)
			fmt.Fprintf(&b, `<tr>
          <td style="padding: 4px 8px; border-bottom: 1px solid #f3f4f6; color: %s; font-weight: bold;">%s</td>
          <td style="padding: 4px 8px; border-bottom: 1px solid #f3f4f6;">%s</td>
          <td style="padding: 4px 8px; border-bottom: 1px solid #f3f4f6;">%s</td>
          <td style="padding: 4px 8px; border-bottom: 1px solid #f3f4f6; max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">%s</td>
          <td style="padding: 4px 8px; border-bottom: 1px solid #f3f4f6; text-align: right; font-weight: bold;">%d</td>
        </tr>`,
				color,
				html.EscapeString(strings.ToUpper(issue.Label)),
				html.EscapeString(issue.Source),
				html.EscapeString(issue.Program),
				html.EscapeString(truncate(issue.Message, 120)),
				issue.Count,
			)
		}
		b.WriteString(`</table>`)
	}

	b.WriteString(`</div>`)

	// Footer.
	fmt.Fprintf(&b, `<div style="padding: 12px 20px; background: #f8f9fa; color: #888; font-size: 12px;">
      Schedule: %s | Generated: %s
    </div>`,
		html.EscapeString(r.Schedule.Name),
		r.To.Format(time.RFC3339),
	)

	b.WriteString(`</div></body></html>`)
	return b.String()
}

func writeSyslogSection(b *strings.Builder, kind string, s *model.SyslogSummary) {
	trendArrow := trendStable
	trendColor := colorGray
	if s.Trend > 0 {
		trendArrow = fmt.Sprintf("↑ %.0f%%", s.Trend)
		trendColor = colorRed
	} else if s.Trend < 0 {
		trendArrow = fmt.Sprintf("↓ %.0f%%", -s.Trend)
		trendColor = colorGreen
	}

	fmt.Fprintf(b, `<h3 style="font-size: 14px; color: #374151; margin: 16px 0 8px;">%s</h3>
    <table style="font-size: 13px; margin-bottom: 8px;">
      <tr><td style="padding: 2px 12px 2px 0; color: #6b7280;">Total</td><td style="font-weight: bold;">%d</td><td style="padding-left: 12px; color: %s; font-size: 12px;">%s</td></tr>
      <tr><td style="padding: 2px 12px 2px 0; color: #6b7280;">Errors</td><td style="font-weight: bold; color: #ef4444;">%d</td></tr>
      <tr><td style="padding: 2px 12px 2px 0; color: #6b7280;">Warnings</td><td style="font-weight: bold; color: #f59e0b;">%d</td></tr>
    </table>`,
		html.EscapeString(kind),
		s.Total, trendColor, trendArrow,
		s.Errors, s.Warnings,
	)

	if len(s.TopHosts) > 0 {
		b.WriteString(`<div style="font-size: 12px; color: #6b7280; margin-bottom: 12px;">Top hosts: `)
		for i, h := range s.TopHosts {
			if i > 4 {
				break
			}
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(b, "%s (%d)", html.EscapeString(h.Name), h.Count)
		}
		b.WriteString(`</div>`)
	}
}

func writeAppLogSection(b *strings.Builder, s *model.AppLogSummary) {
	trendArrow := trendStable
	trendColor := colorGray
	if s.Trend > 0 {
		trendArrow = fmt.Sprintf("↑ %.0f%%", s.Trend)
		trendColor = colorRed
	} else if s.Trend < 0 {
		trendArrow = fmt.Sprintf("↓ %.0f%%", -s.Trend)
		trendColor = colorGreen
	}

	fmt.Fprintf(b, `<h3 style="font-size: 14px; color: #374151; margin: 16px 0 8px;">AppLog</h3>
    <table style="font-size: 13px; margin-bottom: 8px;">
      <tr><td style="padding: 2px 12px 2px 0; color: #6b7280;">Total</td><td style="font-weight: bold;">%d</td><td style="padding-left: 12px; color: %s; font-size: 12px;">%s</td></tr>
      <tr><td style="padding: 2px 12px 2px 0; color: #6b7280;">Errors</td><td style="font-weight: bold; color: #ef4444;">%d</td></tr>
      <tr><td style="padding: 2px 12px 2px 0; color: #6b7280;">Warnings</td><td style="font-weight: bold; color: #f59e0b;">%d</td></tr>
    </table>`,
		s.Total, trendColor, trendArrow,
		s.Errors, s.Warnings,
	)

	if len(s.TopServices) > 0 {
		b.WriteString(`<div style="font-size: 12px; color: #6b7280; margin-bottom: 12px;">Top services: `)
		for i, svc := range s.TopServices {
			if i > 4 {
				break
			}
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(b, "%s (%d)", html.EscapeString(svc.Name), svc.Count)
		}
		b.WriteString(`</div>`)
	}
}

func issueSeverityColor(severity int) string {
	switch {
	case severity <= 2:
		return "#dc2626" // Critical/alert/emergency — red.
	case severity <= 3:
		return "#ef4444" // Error — light red.
	case severity <= 4:
		return "#f59e0b" // Warning — amber.
	default:
		return "#6b7280" // Info/debug — gray.
	}
}

// buildEmailDigest formats a digest summary notification parts.
func buildEmailDigest(p notification.Payload) (summary, message string) {
	windowMin := int(p.Window.Minutes())
	windowLabel := fmt.Sprintf("%d minutes", windowMin)
	if windowMin < 1 {
		windowLabel = fmt.Sprintf("%d seconds", int(p.Window.Seconds()))
	}

	if p.SrvlogEvent != nil {
		e := p.SrvlogEvent
		summary = fmt.Sprintf("%s - %s (digest)", e.Hostname, strings.ToUpper(model.SeverityLabel(e.Severity)))
		message = e.Message
	}
	if p.NetlogEvent != nil {
		e := p.NetlogEvent
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
