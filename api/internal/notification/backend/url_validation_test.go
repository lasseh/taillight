package backend

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
	if got := redactURLError(plain); !errors.Is(got, plain) {
		t.Errorf("non-url.Error should pass through unchanged, got %v", got)
	}
}

func TestValidateExternalURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"public ipv4", "http://93.184.216.34/hook", false},
		{"public ipv6", "https://[2606:4700:4700::1111]/hook", false},
		{"loopback", "http://127.0.0.1:8080/", true},
		{"mapped loopback with zone", "http://[::ffff:127.0.0.1%25x]:8080/", true},
		{"ipv6 loopback with zone", "http://[::1%25lo]/", true},
		{"link-local with zone", "http://[fe80::1%25eth0]/", true},
		{"public ipv6 with zone", "http://[2606:4700:4700::1111%25eth0]/", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExternalURL(context.Background(), tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateExternalURL(%q) = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestCheckResolved(t *testing.T) {
	tests := []struct {
		name    string
		ips     []string
		wantErr bool
	}{
		{"public addresses", []string{"93.184.216.34", "2606:4700:4700::1111"}, false},
		{"one blocked among public", []string{"93.184.216.34", "10.0.0.1"}, true},
		{"mapped blocked", []string{"::ffff:169.254.169.254"}, true},
		{"zoned", []string{"::ffff:127.0.0.1%x"}, true},
		{"unparseable", []string{"not-an-address"}, true},
		{"empty string", []string{""}, true},
		{"no addresses", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkResolved("h", tt.ips)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkResolved(%q) = %v, wantErr %v", tt.ips, err, tt.wantErr)
			}
		})
	}
}

// redirectFirstHop answers requests for host with a 307 to location and hands
// every other request to next, so a test can drive the client's redirect
// handling without reaching a public host.
type redirectFirstHop struct {
	host     string
	location string
	next     http.RoundTripper
}

func (rt redirectFirstHop) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != rt.host {
		return rt.next.RoundTrip(req)
	}
	if req.Body != nil {
		req.Body.Close() //nolint:errcheck // Test stub never reads the body.
	}
	return &http.Response{
		StatusCode: http.StatusTemporaryRedirect,
		Header:     http.Header{"Location": {rt.location}},
		Body:       http.NoBody,
		Request:    req,
	}, nil
}

// TestSSRFSafeClientRefusesRedirectToZonedAddress follows a redirect from an
// allowed first hop to a zoned literal and checks the dial guard refuses it
// before any connection is made.
func TestSSRFSafeClientRefusesRedirectToZonedAddress(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer target.Close()
	_, port, err := net.SplitHostPort(target.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	for _, host := range []string{"[::ffff:127.0.0.1%25x]", "[::1%25lo]", "[fe80::1%25eth0]"} {
		t.Run(host, func(t *testing.T) {
			client := newSSRFSafeClient(5 * time.Second)
			client.Transport = redirectFirstHop{
				host:     "198.51.100.20",
				location: "http://" + host + ":" + port + "/internal",
				next:     client.Transport,
			}
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://198.51.100.20/hook", strings.NewReader(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close() //nolint:errcheck // Test cleanup.
				t.Fatalf("redirect to %s was followed, status %d", host, resp.StatusCode)
			}
			if !strings.Contains(err.Error(), "unusable address") {
				t.Errorf("redirect to %s failed for another reason than the guard: %v", host, err)
			}
		})
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("internal target received %d requests, want 0", n)
	}
}

func TestValidateExternalURL_ParseErrorRedacted(t *testing.T) {
	// A control character makes url.Parse fail; the wrapped parse error would
	// otherwise embed the full secret URL verbatim into notification_log/slog.
	secret := "SUPERSECRETTOKEN"
	raw := "https://hooks.slack.com/services/T0/B0/" + secret + "\x7f"
	err := validateExternalURL(context.Background(), raw)
	if err == nil {
		t.Fatal("expected validateExternalURL to reject a malformed URL")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("parse error leaks secret URL: %s", err)
	}
}
