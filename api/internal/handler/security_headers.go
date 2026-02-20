package handler

import (
	"net/http"
	"slices"
	"strings"
)

// SecurityHeaders returns a chi middleware that sets standard security headers.
// The corsOrigins parameter is used to build the CSP connect-src directive so
// that cross-origin API requests from allowed origins are not blocked.
func SecurityHeaders(corsOrigins []string) func(http.Handler) http.Handler {
	var connectSrc string
	if slices.Contains(corsOrigins, "*") {
		connectSrc = "*"
	} else {
		connectSrc = "'self'"
		for _, o := range corsOrigins {
			if !strings.Contains(connectSrc, o) {
				connectSrc += " " + o
			}
		}
	}

	csp := "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://www.gravatar.com; connect-src " + connectSrc

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			w.Header().Set("Content-Security-Policy", csp)
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			next.ServeHTTP(w, r)
		})
	}
}
