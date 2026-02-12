// Package auth provides API key authentication middleware.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

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

			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		})
	}
}

// RequireScope returns a middleware that checks whether the request has the
// required scope. Session-based auth (nil scopes) is always allowed. The
// "admin" scope grants access to everything.
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			writeJSONError(w, http.StatusForbidden, "forbidden",
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

// extractBearer returns the token from an "Authorization: Bearer <token>" header.
func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	token := strings.TrimPrefix(h, "Bearer ")
	if token == h {
		return ""
	}
	return token
}

type authErrorBody struct {
	Error authErrorDetail `json:"error"`
}

type authErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSONError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(authErrorBody{ //nolint:errcheck
		Error: authErrorDetail{Code: code, Message: msg},
	})
}
