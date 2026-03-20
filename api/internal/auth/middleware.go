// Package auth provides API key authentication middleware.
package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/lasseh/taillight/internal/httputil"
	"github.com/lasseh/taillight/internal/model"
)

type ctxKeyUser struct{}
type ctxKeyScopes struct{}

// WithUser stores a user in the context.
func WithUser(ctx context.Context, user *model.User) context.Context {
	return context.WithValue(ctx, ctxKeyUser{}, user)
}

// UserFromContext returns the authenticated user, or nil if not authenticated.
func UserFromContext(ctx context.Context) *model.User {
	u, _ := ctx.Value(ctxKeyUser{}).(*model.User)
	return u
}

// WithScopes stores API key scopes in the context.
func WithScopes(ctx context.Context, scopes []string) context.Context {
	return context.WithValue(ctx, ctxKeyScopes{}, scopes)
}

// ScopesFromContext returns the API key scopes, or nil for session-based auth.
func ScopesFromContext(ctx context.Context) []string {
	s, _ := ctx.Value(ctxKeyScopes{}).([]string)
	return s
}

// SessionLookup is the interface for looking up sessions by token hash.
type SessionLookup interface {
	GetSessionUser(ctx context.Context, tokenHash string) (*model.User, error)
}

// APIKeyLookup is the interface for looking up API keys by hash.
type APIKeyLookup interface {
	GetAPIKeyUser(ctx context.Context, keyHash string) (*model.User, []string, error)
}

// AllowAnonymous is a middleware that stores an anonymous user in the context.
// Used when authentication is disabled.
func AllowAnonymous(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := &model.User{Username: "anonymous", IsActive: true}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
	})
}

// SessionOrAPIKey returns a middleware that authenticates via session cookie
// or Bearer API key. On success, the authenticated user (and scopes for API
// keys) are stored in the context.
func SessionOrAPIKey(sessions SessionLookup, apiKeys APIKeyLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Try session cookie.
			if cookie, err := r.Cookie("tl_session"); err == nil && cookie.Value != "" {
				tokenHash := HashToken(cookie.Value)
				user, err := sessions.GetSessionUser(r.Context(), tokenHash)
				if err == nil && user != nil {
					next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
					return
				}
			}

			// 2. Try Bearer token (DB-backed API key).
			if bearer := extractBearer(r); bearer != "" {
				if strings.HasPrefix(bearer, "tl_") {
					keyHash := HashToken(bearer)
					user, scopes, err := apiKeys.GetAPIKeyUser(r.Context(), keyHash)
					if err == nil && user != nil {
						ctx := WithUser(r.Context(), user)
						ctx = WithScopes(ctx, scopes)
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
				}
			}

			httputil.WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		})
	}
}

// RequireScope returns a middleware that checks whether the request has the
// required scope. Session-based auth (user present but nil scopes) is always
// allowed. The "admin" scope grants access to everything. If no user is in the
// context (i.e. no auth middleware ran), the request is rejected.
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil {
				httputil.WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
				return
			}
			scopes := ScopesFromContext(r.Context())
			if scopes == nil {
				// Session-based auth — full access.
				next.ServeHTTP(w, r)
				return
			}
			if hasScope(scopes, scope) {
				next.ServeHTTP(w, r)
				return
			}
			httputil.WriteError(w, http.StatusForbidden, "forbidden",
				fmt.Sprintf("api key missing required scope: %s", scope))
		})
	}
}

// hasScope checks whether scopes contains the target scope or "admin".
func hasScope(scopes []string, target string) bool {
	for _, s := range scopes {
		if s == target || s == "admin" {
			return true
		}
	}
	return false
}

// DenyWrites returns a middleware that rejects all non-GET/HEAD/OPTIONS requests
// with 403 Forbidden. Used in demo mode to make the API read-only.
// Exempt paths (e.g. ingest endpoint for loadgen) are allowed only from
// private/loopback IPs (Docker containers, localhost), never from the internet.
// The source IP is taken from r.RemoteAddr after chi's RealIP middleware has
// resolved X-Forwarded-For / X-Real-IP, so internet clients get their real IP.
func DenyWrites(exempt ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			for _, path := range exempt {
				if r.URL.Path == path && isPrivateIP(r.RemoteAddr) {
					next.ServeHTTP(w, r)
					return
				}
			}
			httputil.WriteError(w, http.StatusForbidden, "demo_mode", "write operations are disabled in demo mode")
		})
	}
}

// isPrivateIP reports whether the host portion of addr (host:port or bare IP)
// belongs to a private or loopback range (RFC 1918, RFC 4193, loopback).
func isPrivateIP(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr // bare IP without port
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

// extractBearer returns the token from an "Authorization: Bearer <token>" header.
func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	token := strings.TrimPrefix(h, "Bearer ")
	if token == h {
		return ""
	}
	return token
}
