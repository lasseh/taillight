// Package auth provides API key authentication middleware.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lasseh/taillight/internal/httputil"
	"github.com/lasseh/taillight/internal/model"
)

type ctxKeyUser struct{}
type ctxKeyScopes struct{}
type ctxKeyAPIKeyID struct{}

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

// WithAPIKeyID stores the authenticated API key's ID in the context.
func WithAPIKeyID(ctx context.Context, id pgtype.UUID) context.Context {
	return context.WithValue(ctx, ctxKeyAPIKeyID{}, id)
}

// APIKeyIDFromContext returns the authenticated API key's ID and true if the
// request was authenticated via an API key. Returns the zero value and false
// for session auth.
func APIKeyIDFromContext(ctx context.Context) (pgtype.UUID, bool) {
	id, ok := ctx.Value(ctxKeyAPIKeyID{}).(pgtype.UUID)
	return id, ok
}

// SessionLookup is the interface for looking up sessions by token hash.
// An error wrapping pgx.ErrNoRows means no live session matches.
type SessionLookup interface {
	GetSessionUser(ctx context.Context, tokenHash string) (*model.User, error)
}

// APIKeyLookup is the interface for looking up API keys by hash.
// Returns the owning user, the key's scopes, and the key's row ID. An error
// wrapping pgx.ErrNoRows means no live key matches.
type APIKeyLookup interface {
	GetAPIKeyUser(ctx context.Context, keyHash string) (*model.User, []string, pgtype.UUID, error)
}

// lookupTimeout bounds each credential lookup, so a request whose lookup
// cannot get a database connection fails fast instead of waiting in the
// pool's queue.
const lookupTimeout = 5 * time.Second

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
//
// Each lookup has its own short deadline. A lookup that fails for any reason
// other than "no matching credential" (a timeout, an unavailable database)
// answers 503 rather than 401, unless the other credential succeeds: the
// caller's credential may well be valid, so clients must not treat the
// failure as a logout.
func SessionOrAPIKey(sessions SessionLookup, apiKeys APIKeyLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			unavailable := false

			// 1. Try session cookie.
			if cookie, err := r.Cookie("tl_session"); err == nil && cookie.Value != "" {
				tokenHash := HashToken(cookie.Value)
				ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
				user, err := sessions.GetSessionUser(ctx, tokenHash)
				cancel()
				if err == nil && user != nil {
					next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
					return
				}
				unavailable = lookupFailed(err)
			}

			// 2. Try Bearer token (DB-backed API key).
			if bearer := extractBearer(r); bearer != "" {
				if strings.HasPrefix(bearer, "tl_") {
					keyHash := HashToken(bearer)
					lookupCtx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
					user, scopes, keyID, err := apiKeys.GetAPIKeyUser(lookupCtx, keyHash)
					cancel()
					if err == nil && user != nil {
						ctx := WithUser(r.Context(), user)
						ctx = WithScopes(ctx, scopes)
						ctx = WithAPIKeyID(ctx, keyID)
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
					unavailable = unavailable || lookupFailed(err)
				}
			}

			if unavailable {
				w.Header().Set("Retry-After", "1")
				httputil.WriteError(w, http.StatusServiceUnavailable, "auth_unavailable", "credential check unavailable, retry shortly")
				return
			}
			httputil.WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		})
	}
}

// lookupFailed reports whether a credential lookup error means the lookup
// could not finish, as opposed to finding no matching credential.
func lookupFailed(err error) bool {
	return err != nil && !errors.Is(err, pgx.ErrNoRows)
}

// ScopeAdmin is the privileged scope that grants access to write/admin routes.
const ScopeAdmin = "admin"

// HasGrant reports whether a principal is authorized for the given scope.
// user is the session user or the API key's owner, as loaded for this request.
//
// The "admin" scope always requires the user's current admin flag, for session
// and API-key auth alike: a session is not a blanket admin grant, and an
// admin-scope key stops granting admin as soon as its owner is no longer an
// admin. Beyond that, API-key auth (non-nil scopes) holds a scope if it is
// listed or the key holds "admin", and session auth (nil scopes) holds every
// non-admin scope.
func HasGrant(scopes []string, user *model.User, scope string) bool {
	if scope == ScopeAdmin && (user == nil || !user.IsAdmin) {
		return false
	}
	if scopes == nil {
		return user != nil
	}
	return hasScope(scopes, scope)
}

// RequireScope returns a middleware that checks whether the request is
// authorized for the required scope. Session users are authorized for non-admin
// scopes, and the "admin" scope requires the user's admin flag for sessions and
// API keys alike (see HasGrant).
// If no user is in the context (i.e. no auth middleware ran), the request is
// rejected.
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil {
				httputil.WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
				return
			}
			if HasGrant(ScopesFromContext(r.Context()), user, scope) {
				next.ServeHTTP(w, r)
				return
			}
			httputil.WriteError(w, http.StatusForbidden, "forbidden",
				fmt.Sprintf("missing required scope: %s", scope))
		})
	}
}

// RequireSession refuses requests authenticated by an API key with 403, so
// only a logged-in user reaches the route.
func RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, viaKey := APIKeyIDFromContext(r.Context()); viaKey {
			httputil.WriteError(w, http.StatusForbidden, "forbidden", "API keys cannot use this endpoint; sign in")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireSessionOrAdminKey lets every session through and refuses, with 403,
// an API key that does not hold the admin grant (see HasGrant). It guards the
// account writes a read or ingest key has no use for.
func RequireSessionOrAdminKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if _, viaKey := APIKeyIDFromContext(ctx); viaKey && !HasGrant(ScopesFromContext(ctx), UserFromContext(ctx), ScopeAdmin) {
			httputil.WriteError(w, http.StatusForbidden, "forbidden", "missing required scope: admin")
			return
		}
		next.ServeHTTP(w, r)
	})
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
// Both the resolved client IP (real_ip_header when behind a proxy, else the
// TCP peer) and the raw TCP peer must be private: the resolved IP alone is
// forgeable when a peer bypasses the proxy, and the raw peer alone is the
// proxy's (private) address for internet clients behind a containerized proxy.
func DenyWrites(exempt ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			for _, path := range exempt {
				if r.URL.Path == path &&
					isPrivateIP(middleware.GetClientIP(r.Context())) &&
					isPrivateIP(r.RemoteAddr) {
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
