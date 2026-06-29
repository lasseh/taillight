package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/lasseh/taillight/internal/config"
)

// TestClientIPMiddleware verifies the trust boundary: the proxy header is only
// honored when real_ip_header is configured; otherwise it is ignored in favor
// of the TCP peer, so a directly-exposed deployment cannot be spoofed.
func TestClientIPMiddleware(t *testing.T) {
	tests := []struct {
		name         string
		realIPHeader string
		remoteAddr   string
		xRealIP      string
		want         string
	}{
		{"default ignores spoofed header", "", "127.0.0.1:5000", "9.9.9.9", "127.0.0.1"},
		{"default uses peer when no header", "", "203.0.113.7:443", "", "203.0.113.7"},
		{"header mode trusts proxy header", "X-Real-IP", "127.0.0.1:5000", "9.9.9.9", "9.9.9.9"},
		{"header mode fails closed without header", "X-Real-IP", "127.0.0.1:5000", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = middleware.GetClientIP(r.Context())
			})
			handler := clientIPMiddleware(config.Config{RealIPHeader: tt.realIPHeader})(next)

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
