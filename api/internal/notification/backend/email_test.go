package backend

import (
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/notification"
)

// discardLogger returns a logger that writes to io.Discard.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestEmailValidate(t *testing.T) {
	e := NewEmail(EmailGlobalConfig{}, nil, discardLogger())

	tests := []struct {
		name    string
		config  emailConfig
		wantErr string
	}{
		{
			name:    "valid single address",
			config:  emailConfig{To: []string{"user@example.com"}},
			wantErr: "",
		},
		{
			name:    "valid multiple addresses",
			config:  emailConfig{To: []string{"a@example.com", "b@example.com"}},
			wantErr: "",
		},
		{
			name:    "empty to list",
			config:  emailConfig{To: []string{}},
			wantErr: "to is required",
		},
		{
			name:    "nil to list",
			config:  emailConfig{},
			wantErr: "to is required",
		},
		{
			name:    "invalid address",
			config:  emailConfig{To: []string{"not-an-email"}},
			wantErr: "invalid email address",
		},
		{
			name:    "mixed valid and invalid",
			config:  emailConfig{To: []string{"ok@example.com", "bad"}},
			wantErr: "invalid email address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, _ := json.Marshal(tt.config)
			ch := notification.Channel{Config: raw}
			err := e.Validate(ch)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestEmailValidateInvalidJSON(t *testing.T) {
	e := NewEmail(EmailGlobalConfig{}, nil, discardLogger())
	ch := notification.Channel{Config: json.RawMessage(`{invalid`)}
	err := e.Validate(ch)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid email config") {
		t.Fatalf("expected 'invalid email config' error, got %q", err.Error())
	}
}

func TestBuildEmailSubject(t *testing.T) {
	tests := []struct {
		name     string
		tmpl     string
		payload  notification.Payload
		expected string
	}{
		{
			name: "custom template literal",
			tmpl: "Custom Alert",
			payload: notification.Payload{
				SrvlogEvent: &model.SrvlogEvent{Hostname: "web01", Severity: 3},
			},
			expected: "Custom Alert",
		},
		{
			name: "custom template interpolated",
			tmpl: "Alert: {{.RuleName}} on {{.SrvlogEvent.Hostname}}",
			payload: notification.Payload{
				RuleName:    "disk-full",
				SrvlogEvent: &model.SrvlogEvent{Hostname: "web01", Severity: 3},
			},
			expected: "Alert: disk-full on web01",
		},
		{
			name: "srvlog event",
			tmpl: "",
			payload: notification.Payload{
				SrvlogEvent: &model.SrvlogEvent{Hostname: "web01", Severity: 3},
			},
			expected: "[Taillight] web01 - ERR",
		},
		{
			name: "applog event",
			tmpl: "",
			payload: notification.Payload{
				AppLogEvent: &model.AppLogEvent{Host: "api01", Level: "WARN"},
			},
			expected: "[Taillight] api01 - WARN",
		},
		{
			name:     "no event",
			tmpl:     "",
			payload:  notification.Payload{},
			expected: "[Taillight] Notification",
		},
		{
			name: "analysis report",
			tmpl: "",
			payload: notification.Payload{
				AnalysisReport: &model.AnalysisReport{
					PromptMode: "daily",
					PeriodEnd:  time.Date(2026, 5, 22, 13, 15, 0, 0, time.UTC),
				},
			},
			expected: "[Taillight] Daily Operations Briefing — 2026-05-22",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildEmailSubject(tt.tmpl, tt.payload)
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestBuildEmailBody(t *testing.T) {
	p := notification.Payload{
		Kind:     notification.EventKindSrvlog,
		RuleName: "test-rule",
		SrvlogEvent: &model.SrvlogEvent{
			Hostname: "web01",
			Severity: 3,
			Message:  "something went wrong",
		},
		EventCount: 1,
		Timestamp:  time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	body := buildEmailBody(p)

	// Check that the body contains expected elements.
	checks := []string{
		"web01 - ERR",
		"something went wrong",
		"test-rule",
		"#E67E22", // orange for severity 3
		"2025-01-15T10:30:00Z",
	}
	for _, check := range checks {
		if !strings.Contains(body, check) {
			t.Errorf("expected body to contain %q", check)
		}
	}
}

func TestBuildEmailBodyDigest(t *testing.T) {
	p := notification.Payload{
		Kind:     notification.EventKindSrvlog,
		RuleName: "test-rule",
		IsDigest: true,
		SrvlogEvent: &model.SrvlogEvent{
			Hostname: "web01",
			Severity: 2,
			Message:  "critical error",
		},
		EventCount: 42,
		Window:     5 * time.Minute,
		Timestamp:  time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	body := buildEmailBody(p)

	checks := []string{
		"web01 - CRIT (digest)",
		"42 more events",
		"5 minutes",
		"#E74C3C", // red for severity 2
	}
	for _, check := range checks {
		if !strings.Contains(body, check) {
			t.Errorf("expected body to contain %q", check)
		}
	}
}

func TestBuildEmailAnalysisReport(t *testing.T) {
	completed := time.Date(2026, 5, 22, 13, 16, 0, 0, time.UTC)
	r := &model.AnalysisReport{
		Slug:        "netlog-incident-2026-05-22-1315",
		Feed:        "netlog",
		PromptMode:  "incident",
		Hosts:       []string{"s-vts-ep-1", "s-vts-ep-2"},
		Model:       "gpt-oss:20b",
		PeriodStart: time.Date(2026, 5, 22, 10, 15, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 5, 22, 13, 15, 0, 0, time.UTC),
		Report:      "# Incident Briefing — 2026-05-22 → 2026-05-22\n_Period: ..._\n\nCONTAIN — RTPERF_CPU_THRESHOLD_EXCEEDED spike on s-vts-ep-1 at 12:05 and 12:35.\n",
		CompletedAt: &completed,
		CreatedAt:   time.Date(2026, 5, 22, 13, 15, 30, 0, time.UTC),
	}
	body := buildEmailAnalysisReport(r)

	// Body must surface the title, period, scope, model, excerpt, and slug
	// pointer so the recipient can act on the email even without the PDF.
	checks := []string{
		"Incident Briefing",
		"2026-05-22 10:15 – 2026-05-22 13:15 UTC",
		"s-vts-ep-1, s-vts-ep-2",
		"gpt-oss:20b",
		"CONTAIN — RTPERF_CPU_THRESHOLD_EXCEEDED",
		"netlog-incident-2026-05-22-1315",
	}
	for _, check := range checks {
		if !strings.Contains(body, check) {
			t.Errorf("expected body to contain %q\ngot: %s", check, body)
		}
	}
}

func TestBuildMIMEMessageWithAttachment(t *testing.T) {
	pdf := []byte("%PDF-1.4 fake pdf bytes for testing\n")
	msg := buildMIMEMessageWithAttachment(
		"from@example.com",
		[]string{"to@example.com"},
		"[Taillight] Daily — 2026-05-22",
		"<p>body</p>",
		pdf,
		"netlog-daily-2026-05-22-1018.pdf",
	)
	s := string(msg)

	checks := []string{
		"From: from@example.com\r\n",
		"To: to@example.com\r\n",
		`Content-Type: multipart/mixed; boundary="taillight_`,
		"Content-Type: text/html; charset=UTF-8\r\n",
		"<p>body</p>",
		`Content-Type: application/pdf; name="netlog-daily-2026-05-22-1018.pdf"`,
		`Content-Disposition: attachment; filename="netlog-daily-2026-05-22-1018.pdf"`,
		"Content-Transfer-Encoding: base64\r\n",
	}
	for _, check := range checks {
		if !strings.Contains(s, check) {
			t.Errorf("expected MIME message to contain %q", check)
		}
	}

	// Final boundary marker must terminate the message.
	if !strings.HasSuffix(s, "--\r\n") {
		t.Errorf("expected message to end with terminating boundary, got tail %q", s[max(0, len(s)-40):])
	}
}

func TestPDFFilename(t *testing.T) {
	tests := []struct {
		name string
		in   *model.AnalysisReport
		want string
	}{
		{name: "slug present", in: &model.AnalysisReport{Slug: "netlog-daily-2026-05-22"}, want: "netlog-daily-2026-05-22.pdf"},
		{name: "nil falls back to timestamped", in: nil, want: "taillight-report-"},
		{name: "empty slug falls back to timestamped", in: &model.AnalysisReport{}, want: "taillight-report-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pdfFilename(tt.in)
			if !strings.HasPrefix(got, tt.want) {
				t.Errorf("expected filename to start with %q, got %q", tt.want, got)
			}
			if !strings.HasSuffix(got, ".pdf") {
				t.Errorf("expected filename to end with .pdf, got %q", got)
			}
		})
	}
}

func TestBuildMIMEMessage(t *testing.T) {
	msg := buildMIMEMessage("from@example.com", []string{"to@example.com"}, "Test Subject", "<p>body</p>")
	s := string(msg)

	checks := []string{
		"From: from@example.com\r\n",
		"To: to@example.com\r\n",
		"Subject: Test Subject\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: text/html; charset=UTF-8\r\n",
		"<p>body</p>",
	}
	for _, check := range checks {
		if !strings.Contains(s, check) {
			t.Errorf("expected MIME message to contain %q", check)
		}
	}
}

func TestSmtpAuth(t *testing.T) {
	tests := []struct {
		name     string
		authType string
		wantNil  bool
		wantErr  bool
	}{
		{name: "plain", authType: "plain", wantNil: false},
		{name: "crammd5", authType: "crammd5", wantNil: false},
		{name: "empty", authType: "", wantNil: true},
		{name: "none", authType: "none", wantNil: true},
		{name: "unsupported", authType: "oauth2", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Email{cfg: EmailGlobalConfig{
				AuthType: tt.authType,
				Host:     "smtp.example.com",
				Username: "user",
				Password: "pass",
			}}
			auth, err := e.smtpAuth()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil && auth != nil {
				t.Fatal("expected nil auth")
			}
			if !tt.wantNil && auth == nil {
				t.Fatal("expected non-nil auth")
			}
		})
	}
}
