package backend

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		blocked bool
	}{
		{"ipv4 unspecified this-host", "0.0.0.0", true},
		{"ipv4 this-host range", "0.1.2.3", true},
		{"ipv6 unspecified", "::", true},
		{"ipv4 loopback", "127.0.0.1", true},
		{"ipv6 loopback", "::1", true},
		{"ipv4-mapped loopback", "::ffff:127.0.0.1", true},
		{"rfc1918 10", "10.1.2.3", true},
		{"rfc1918 172", "172.16.5.5", true},
		{"rfc1918 192", "192.168.1.1", true},
		{"ipv4 link-local", "169.254.169.254", true}, // cloud metadata
		{"ipv6 link-local", "fe80::1", true},
		{"ipv6 ula", "fd00::1", true},
		{"public ipv4", "1.1.1.1", false},
		{"public ipv4 2", "93.184.216.34", false},
		{"public ipv6", "2606:4700:4700::1111", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("could not parse IP %q", tt.ip)
			}
			if got := isBlockedIP(ip); got != tt.blocked {
				t.Errorf("isBlockedIP(%q) = %v, want %v", tt.ip, got, tt.blocked)
			}
		})
	}
}

func TestRedactURLError(t *testing.T) {
	// Slack-style secret: the token lives in the URL path.
	inner := errors.New("dial tcp: connection refused")
	ue := &url.Error{
		Op:  "Post",
		URL: "https://hooks.slack.com/services/T00000000/B00000000/SUPERSECRETTOKEN?u=admin:pw@x",
		Err: inner,
	}
	// Wrap exactly as the backends do.
	wrapped := fmt.Errorf("send slack webhook: %w", redactURLError(ue))
	got := wrapped.Error()

	for _, secret := range []string{"SUPERSECRETTOKEN", "B00000000", "T00000000", "admin:pw"} {
		if strings.Contains(got, secret) {
			t.Errorf("redacted error leaks %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "hooks.slack.com") {
		t.Errorf("redacted error should keep the host for diagnosis, got: %s", got)
	}
	if !errors.Is(wrapped, inner) {
		t.Errorf("redaction broke the error chain; errors.Is(inner) failed")
	}
}

func TestRedactURLError_NonURLErrorUnchanged(t *testing.T) {
	plain := errors.New("marshal payload: boom")
	if got := redactURLError(plain); got != plain {
		t.Errorf("non-url.Error should pass through unchanged, got %v", got)
	}
}
