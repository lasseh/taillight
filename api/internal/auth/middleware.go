// Package auth provides API key authentication middleware.
package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lasseh/taillight/internal/model"
)

type ctxKeyUser struct{}

// WithUser stores a user in the context.
func WithUser(ctx context.Context, user *model.User) context.Context {
	return context.WithValue(ctx, ctxKeyUser{}, user)
}

// UserFromContext returns the authenticated user, or nil if not authenticated.
func UserFromContext(ctx context.Context) *model.User {
	u, _ := ctx.Value(ctxKeyUser{}).(*model.User)
	return u
}

// SessionLookup is the interface for looking up sessions by token hash.
type SessionLookup interface {
	GetSessionUser(ctx context.Context, tokenHash string) (*model.User, error)
}

// APIKeyLookup is the interface for looking up API keys by hash.
type APIKeyLookup interface {
	GetAPIKeyUser(ctx context.Context, keyHash string) (*model.User, error)
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
// or Bearer API key. If both fail, falls back to config-based API keys.
// On success, the authenticated user is stored in the context.
func SessionOrAPIKey(sessions SessionLookup, apiKeys APIKeyLookup, configKeys []string) func(http.Handler) http.Handler {
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
					user, err := apiKeys.GetAPIKeyUser(r.Context(), keyHash)
					if err == nil && user != nil {
						next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
						return
					}
					// Any error (not found, DB error) — fall through to config keys.
				}

				// 3. Fall back to config-based API keys.
				if constantTimeMatch(configKeys, bearer) {
					next.ServeHTTP(w, r)
					return
				}
			}

			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		})
	}
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

// constantTimeMatch checks whether token matches any of the valid keys
// using constant-time comparison to prevent timing attacks. All keys are
// checked (no early return) so the execution time is independent of which
// key matches or whether any key matches at all.
func constantTimeMatch(validKeys []string, token string) bool {
	match := 0
	for _, key := range validKeys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(token)) == 1 {
			match = 1
		}
	}
	return match == 1
}

type authErrorBody struct {
	Error authErrorDetail `json:"error"`
}

type authErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSONError(w http.ResponseWriter, status int, code, msg string) { //nolint:unparam
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(authErrorBody{ //nolint:errcheck
		Error: authErrorDetail{Code: code, Message: msg},
	})
}
