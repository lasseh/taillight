package backend

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
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
	// AttachPDF, when true on an analysis-report payload, renders the report
	// to PDF via the configured PDFRenderer and ships it as a multipart/mixed
	// attachment. Ignored on other payload kinds and silently skipped when
	// the backend has no renderer wired (logs a warning).
	AttachPDF bool `json:"attach_pdf,omitempty"`
}

// PDFRenderer is the interface the email backend uses to render an analysis
// report to PDF bytes. Implemented by internal/pdfrender — but injected as an
// interface so backend tests can substitute a fake. Nil renderer means
// PDF attachment is disabled and AttachPDF channel configs degrade to a
// plain text/html body with a warning logged.
type PDFRenderer interface {
	RenderAnalysisReport(ctx context.Context, r model.AnalysisReport) ([]byte, error)
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
	cfg      EmailGlobalConfig
	renderer PDFRenderer
	logger   *slog.Logger
}

// NewEmail creates a new Email backend. Pass nil for renderer to disable PDF
// attachment support — channels with attach_pdf=true will still send, but
// only the HTML body (a warning is logged).
func NewEmail(cfg EmailGlobalConfig, renderer PDFRenderer, logger *slog.Logger) *Email {
	return &Email{
		cfg:      cfg,
		renderer: renderer,
		logger:   logger.With("backend", "email"),
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

	if e.cfg.Host == "" {
		return notification.SendResult{
			Error:    fmt.Errorf("email backend is not configured: smtp.host is not set in the server config"),
			Duration: time.Since(start),
		}
	}

	var cfg emailConfig
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		return notification.SendResult{Error: fmt.Errorf("parse email config: %w", err), Duration: time.Since(start)}
	}

	subject := buildEmailSubject(cfg.SubjectTemplate, payload)
	body := buildEmailBody(payload)

	// PDF attachment path. Only kicks in for AnalysisReport payloads with
	// attach_pdf=true on the channel and a renderer wired on the backend.
	// Anything else falls through to the plain text/html message that the
	// existing summary / event flows already use.
	var pdf []byte
	if cfg.AttachPDF && payload.AnalysisReport != nil {
		if e.renderer == nil {
			e.logger.Warn("attach_pdf requested but no PDF renderer configured; sending without attachment",
				"slug", payload.AnalysisReport.Slug)
		} else {
			rendered, rerr := e.renderer.RenderAnalysisReport(ctx, *payload.AnalysisReport)
			if rerr != nil {
				// Failing the whole send because the PDF render failed would be
				// worse than delivering the HTML body — the recipient can still
				// open the report via the link in the body.
				e.logger.Warn("PDF render failed; sending email without attachment",
					"slug", payload.AnalysisReport.Slug, "err", rerr)
			} else {
				pdf = rendered
			}
		}
	}

	var msg []byte
	if len(pdf) > 0 {
		filename := pdfFilename(payload.AnalysisReport)
		msg = buildMIMEMessageWithAttachment(e.cfg.From, cfg.To, subject, body, pdf, filename)
	} else {
		msg = buildMIMEMessage(e.cfg.From, cfg.To, subject, body)
	}

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

// buildMIMEMessageWithAttachment constructs a multipart/mixed email carrying
// the HTML body plus a single binary attachment (the analysis-report PDF).
// The boundary is derived from a fixed prefix + a nanosecond timestamp; that
// combination is unique enough for one message and avoids pulling in
// crypto/rand for what is effectively a delimiter.
//
// Attachment is base64-encoded with 76-char line wrapping per RFC 2045 §6.8.
func buildMIMEMessageWithAttachment(from string, to []string, subject, htmlBody string, attachment []byte, filename string) []byte {
	boundary := fmt.Sprintf("taillight_%d", time.Now().UnixNano())
	filename = sanitizeHeaderValue(filename)

	var b strings.Builder
	b.WriteString("From: " + sanitizeHeaderValue(from) + "\r\n")
	b.WriteString("To: " + sanitizeHeaderValue(strings.Join(to, ", ")) + "\r\n")
	b.WriteString("Subject: " + sanitizeHeaderValue(subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n")
	b.WriteString("\r\n")

	// HTML body part.
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 7bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(htmlBody)
	b.WriteString("\r\n")

	// PDF attachment part.
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: application/pdf; name=\"" + filename + "\"\r\n")
	b.WriteString("Content-Disposition: attachment; filename=\"" + filename + "\"\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString("\r\n")
	encoded := base64.StdEncoding.EncodeToString(attachment)
	// Wrap to 76 chars per RFC 2045 §6.8.
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		b.WriteString(encoded[i:end])
		b.WriteString("\r\n")
	}

	// Final boundary.
	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String())
}

// pdfFilename derives a safe filename for the report PDF. Falls back to a
// timestamped default if the report has no slug (shouldn't happen post-store).
func pdfFilename(r *model.AnalysisReport) string {
	if r != nil && r.Slug != "" {
		return r.Slug + ".pdf"
	}
	return fmt.Sprintf("taillight-report-%d.pdf", time.Now().Unix())
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
	if p.AnalysisReport != nil {
		r := p.AnalysisReport
		return fmt.Sprintf("%s %s — %s",
			prefix,
			analysisBriefingTitle(r.PromptMode),
			r.PeriodEnd.UTC().Format("2006-01-02"),
		)
	}
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

// analysisBriefingTitle is the email-subject mapper for prompt_mode. Mirrors
// analyzer.briefingTitle (which is unexported and lives a layer below this
// package, so duplicating the short switch is cheaper than reaching across).
func analysisBriefingTitle(mode string) string {
	switch mode {
	case "daily":
		return "Daily Operations Briefing"
	case "weekly":
		return "Weekly Operations Briefing"
	case "incident":
		return "Incident Briefing"
	default:
		return "Operations Briefing"
	}
}

// buildEmailBody creates an HTML email body with severity color coding.
func buildEmailBody(p notification.Payload) string {
	if p.AnalysisReport != nil {
		return buildEmailAnalysisReport(p.AnalysisReport)
	}
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

// buildEmailAnalysisReport renders a short HTML body for a completed analysis
// report. The body is intentionally minimal — title, period, scope, a short
// excerpt, and a "open in Taillight" pointer — because the full report
// arrives as a PDF attachment when attach_pdf is configured. When no
// attachment is sent (renderer down or attach_pdf=false), the recipient
// still has enough context here to know what's in the report and where to
// open it. The full markdown is not inlined: rendering 30 KB of nested
// tables across mail clients is a known mess best left to the PDF path.
func buildEmailAnalysisReport(r *model.AnalysisReport) string {
	title := analysisBriefingTitle(r.PromptMode)
	period := fmt.Sprintf("%s – %s UTC",
		r.PeriodStart.UTC().Format("2006-01-02 15:04"),
		r.PeriodEnd.UTC().Format("2006-01-02 15:04"),
	)
	scope := "all hosts"
	if len(r.Hosts) > 0 {
		scope = strings.Join(r.Hosts, ", ")
	}
	excerpt := analysisExcerpt(r.Report, 320)

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; margin: 0; padding: 20px; background: #f5f5f5;">
  <div style="max-width: 640px; margin: 0 auto; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,0.1);">
    <div style="background: #1f2937; padding: 16px 20px; color: #fff;">
      <div style="font-size: 11px; letter-spacing: 0.08em; text-transform: uppercase; opacity: 0.7;">Taillight — Analysis Report</div>
      <div style="font-size: 18px; font-weight: bold; margin-top: 4px;">%s</div>
      <div style="font-size: 13px; opacity: 0.8; margin-top: 4px;">%s</div>
    </div>
    <div style="padding: 20px;">
      <table style="font-size: 13px; margin-bottom: 12px;">
        <tr><td style="padding: 2px 12px 2px 0; color: #6b7280;">Source</td><td style="font-weight: 600;">%s</td></tr>
        <tr><td style="padding: 2px 12px 2px 0; color: #6b7280;">Mode</td><td style="font-weight: 600;">%s</td></tr>
        <tr><td style="padding: 2px 12px 2px 0; color: #6b7280;">Scope</td><td style="font-weight: 600;">%s</td></tr>
        <tr><td style="padding: 2px 12px 2px 0; color: #6b7280;">Model</td><td style="font-weight: 600;">%s</td></tr>
      </table>
      <div style="background: #f8f9fa; border-left: 3px solid #2563eb; padding: 10px 12px; font-size: 13px; color: #374151; white-space: pre-wrap;">%s</div>
      <div style="margin-top: 16px; font-size: 12px; color: #6b7280;">The full report is attached as PDF. Open in Taillight: <code>/analysis/reports/%s</code></div>
    </div>
    <div style="padding: 12px 20px; background: #f8f9fa; color: #888; font-size: 12px;">
      Generated %s
    </div>
  </div>
</body>
</html>`,
		html.EscapeString(title),
		html.EscapeString(period),
		html.EscapeString(r.Feed),
		html.EscapeString(r.PromptMode),
		html.EscapeString(scope),
		html.EscapeString(r.Model),
		html.EscapeString(excerpt),
		html.EscapeString(r.Slug),
		analysisGeneratedAt(r),
	)
}

// analysisExcerpt returns the first paragraph of the rendered markdown body,
// truncated to n characters with an ellipsis. The analyzer prepends a level-1
// title and a "_Period: ..._" line that aren't interesting in an excerpt, so
// the helper skips lines starting with "#" or "_" until it finds prose.
func analysisExcerpt(body string, n int) string {
	if body == "" {
		return "(report body empty)"
	}
	for _, line := range strings.Split(body, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "_") {
			continue
		}
		return truncate(s, n)
	}
	return truncate(strings.TrimSpace(body), n)
}

// analysisGeneratedAt prefers completed_at over created_at so a finished
// report stamps the actual finish time, not the queue time.
func analysisGeneratedAt(r *model.AnalysisReport) string {
	if r.CompletedAt != nil {
		return r.CompletedAt.UTC().Format(time.RFC3339)
	}
	return r.CreatedAt.UTC().Format(time.RFC3339)
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
