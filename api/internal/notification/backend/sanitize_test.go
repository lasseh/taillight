package backend

import (
	"strings"
	"testing"
)

// TestSanitizeHeaderValue covers the CRLF/header-injection guard used for email
// and ntfy headers — the only defense against a newline-bearing subject or
// title splicing in an extra header (audit S5).
func TestSanitizeHeaderValue(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Weekly report", "Weekly report"},
		{"strips CR", "a\rb", "ab"},
		{"strips LF", "a\nb", "ab"},
		{"strips CRLF", "a\r\nb", "ab"},
		{"header injection", "Subject\r\nBcc: evil@example.com", "SubjectBcc: evil@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeHeaderValue(tt.in)
			if got != tt.want {
				t.Errorf("sanitizeHeaderValue(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if strings.ContainsAny(got, "\r\n") {
				t.Errorf("result still contains CR/LF: %q", got)
			}
		})
	}
}

// TestBuildMIMEMessage_RejectsHeaderInjection asserts a newline-bearing subject
// cannot introduce a second header line in the assembled message.
func TestBuildMIMEMessage_RejectsHeaderInjection(t *testing.T) {
	subject := "Hi\r\nBcc: evil@example.com"
	msg := string(buildMIMEMessage("from@example.com", []string{"to@example.com"}, subject, "body text"))

	// Split headers from body on the first blank line.
	headers := msg
	if idx := strings.Index(msg, "\r\n\r\n"); idx >= 0 {
		headers = msg[:idx]
	} else if idx := strings.Index(msg, "\n\n"); idx >= 0 {
		headers = msg[:idx]
	}
	// The injection is prevented when no header *line* begins with Bcc: — the
	// sanitized subject keeping the literal text "Bcc:" inline is fine.
	for _, line := range strings.Split(headers, "\n") {
		if strings.HasPrefix(strings.TrimRight(line, "\r"), "Bcc:") {
			t.Errorf("header injection succeeded; a header line starts with Bcc:\n%s", headers)
		}
	}
}
