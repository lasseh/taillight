package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/lasseh/taillight/internal/config"
)

// TestClientIPMiddleware verifies the trust boundary: the proxy header is only
// honored when real_ip_header is configured — and, when trusted_proxies is
// also set, only from a peer inside one of the listed CIDRs. Everything else
// resolves to the TCP peer, so neither a directly-exposed deployment nor a
// proxy-bypassing peer can spoof its IP.
func TestClientIPMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		realIPHeader   string
		trustedProxies []netip.Prefix
		remoteAddr     string
		xRealIP        string
		want           string
	}{
		{"default ignores spoofed header", "", nil, "127.0.0.1:5000", "9.9.9.9", "127.0.0.1"},
		{"default uses peer when no header", "", nil, "203.0.113.7:443", "", "203.0.113.7"},
		{"header mode trusts proxy header", "X-Real-IP", nil, "127.0.0.1:5000", "9.9.9.9", "9.9.9.9"},
		{"header mode fails closed without header", "X-Real-IP", nil, "127.0.0.1:5000", "", ""},
		{"trusted peer honors header", "X-Real-IP", prefixes("127.0.0.0/8"), "127.0.0.1:5000", "9.9.9.9", "9.9.9.9"},
		{"untrusted peer ignores spoofed header", "X-Real-IP", prefixes("10.0.0.0/8"), "203.0.113.7:443", "9.9.9.9", "203.0.113.7"},
		{"trusted peer fails closed without header", "X-Real-IP", prefixes("127.0.0.0/8"), "127.0.0.1:5000", "", ""},
		{"v4-mapped peer matches v4 cidr", "X-Real-IP", prefixes("127.0.0.0/8"), "[::ffff:127.0.0.1]:5000", "9.9.9.9", "9.9.9.9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = middleware.GetClientIP(r.Context())
			})
			cfg := config.Config{RealIPHeader: tt.realIPHeader, TrustedProxies: tt.trustedProxies}
			handler := clientIPMiddleware(cfg)(next)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}
			handler.ServeHTTP(httptest.NewRecorder(), req)

			if got != tt.want {
				t.Errorf("GetClientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestClientIPMiddlewareSpoofedHeaderStableKey encodes the residual-hardening
// property from June audit issue 14: an untrusted peer rotating spoofed
// real_ip_header values always resolves to its own TCP peer address, so the
// per-IP login rate limiter (keyed on GetClientIP) still trips after the burst.
func TestClientIPMiddlewareSpoofedHeaderStableKey(t *testing.T) {
	var got string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = middleware.GetClientIP(r.Context())
	})
	cfg := config.Config{RealIPHeader: "X-Real-IP", TrustedProxies: prefixes("10.0.0.0/8")}
	handler := clientIPMiddleware(cfg)(next)

	for _, spoofed := range []string{"9.9.9.9", "8.8.8.8", "1.2.3.4"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = "203.0.113.7:443"
		req.Header.Set("X-Real-IP", spoofed)
		handler.ServeHTTP(httptest.NewRecorder(), req)

		if got != "203.0.113.7" {
			t.Errorf("spoofed %q: GetClientIP = %q, want peer 203.0.113.7", spoofed, got)
		}
	}
}

// prefixes parses CIDRs into netip prefixes for test tables.
func prefixes(cidrs ...string) []netip.Prefix {
	ps := make([]netip.Prefix, len(cidrs))
	for i, c := range cidrs {
		ps[i] = netip.MustParsePrefix(c)
	}
	return ps
}
