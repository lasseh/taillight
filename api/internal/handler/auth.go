package handler

import (
	"context"
	"crypto/md5" //nolint:gosec
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/auth"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/postgres"
)

const (
	sessionCookieName  = "tl_session"
	sessionDuration    = 30 * 24 * time.Hour // 30 days.
	maxSessionsPerUser = 10
	maxAuthBody        = 4096 // 4 KB — generous for auth JSON payloads.

	// loginRateLimit is the per-IP rate limit for login attempts (per second).
	loginRateLimit = rate.Limit(5.0 / 60) // 5 per minute.
	// loginRateBurst allows brief bursts above the sustained rate.
	loginRateBurst = 10
	// loginLimiterTTL is how long an idle per-IP limiter is kept.
	loginLimiterTTL = 15 * time.Minute
)

// loginRateLimiter is a per-IP rate limiter for the login endpoint.
var loginRL = newIPRateLimiter(loginRateLimit, loginRateBurst, loginLimiterTTL)

type ipRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*ipLimiterEntry
	rate     rate.Limit
	burst    int
	ttl      time.Duration
}

type ipLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newIPRateLimiter(r rate.Limit, burst int, ttl time.Duration) *ipRateLimiter {
	rl := &ipRateLimiter{
		limiters: make(map[string]*ipLimiterEntry),
		rate:     r,
		burst:    burst,
		ttl:      ttl,
	}
	go rl.evictLoop()
	return rl
}

func (rl *ipRateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	entry, ok := rl.limiters[ip]
	if !ok {
		entry = &ipLimiterEntry{limiter: rate.NewLimiter(rl.rate, rl.burst)}
		rl.limiters[ip] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter.Allow()
}

func (rl *ipRateLimiter) evictLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for now := range ticker.C {
		rl.mu.Lock()
		for ip, entry := range rl.limiters {
			if now.Sub(entry.lastSeen) > rl.ttl {
				delete(rl.limiters, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// AuthStore defines the data access interface for auth operations.
type AuthStore interface {
	CreateUser(ctx context.Context, username, passwordHash string, isAdmin bool) (model.User, error)
	GetUserByUsername(ctx context.Context, username string) (model.User, error)
	GetUserByID(ctx context.Context, id [16]byte) (model.User, error)
	UpdateLastLogin(ctx context.Context, id [16]byte) error
	UpdateEmail(ctx context.Context, id [16]byte, email string) error
	ListUsers(ctx context.Context) ([]model.User, error)
	SetUserActive(ctx context.Context, id [16]byte, active bool) error
	UpdatePassword(ctx context.Context, id [16]byte, passwordHash string) error
	CreateSession(ctx context.Context, tokenHash string, userID [16]byte, expiresAt time.Time, ip, userAgent string) error
	GetSession(ctx context.Context, tokenHash string) (postgres.SessionWithUser, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteUserSessions(ctx context.Context, userID [16]byte) error
	PruneUserSessions(ctx context.Context, userID [16]byte, keep int) error
	CleanExpiredSessions(ctx context.Context) (int64, error)
	CreateAPIKey(ctx context.Context, userID [16]byte, name, keyHash, keyPrefix string, scopes []string, expiresAt *time.Time) (model.APIKeyRow, error)
	GetAPIKeyByHash(ctx context.Context, keyHash string) (postgres.APIKeyWithUser, error)
	ListAPIKeysByUser(ctx context.Context, userID [16]byte) ([]model.APIKeyRow, error)
	RevokeAPIKey(ctx context.Context, id [16]byte) error
	GetAPIKeyByID(ctx context.Context, id [16]byte) (model.APIKeyRow, error)
}

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	store        AuthStore
	cookieSecure bool // Force Secure flag on session cookies.
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(store AuthStore, cookieSecure bool) *AuthHandler {
	return &AuthHandler{store: store, cookieSecure: cookieSecure}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	User userInfo `json:"user"`
}

type userInfo struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	Email       *string `json:"email,omitempty"`
	IsAdmin     bool    `json:"is_admin"`
	GravatarURL string  `json:"gravatar_url"`
	CreatedAt   string  `json:"created_at"`
	LastLoginAt *string `json:"last_login_at,omitempty"`
}

// buildUserInfo converts a model.User into the userInfo response type.
func buildUserInfo(u model.User) userInfo {
	info := userInfo{
		ID:          formatUUID(u.ID.Bytes),
		Username:    u.Username,
		Email:       u.Email,
		IsAdmin:     u.IsAdmin,
		GravatarURL: gravatarURL(u.Email),
		CreatedAt:   u.CreatedAt.Format(time.RFC3339),
	}
	// Only set if the timestamp is valid (user has logged in at least once).
	if u.LastLoginAt.Valid {
		s := u.LastLoginAt.Time.Format(time.RFC3339)
		info.LastLoginAt = &s
	}
	return info
}

// gravatarURL returns a Gravatar image URL for the given email.
// If email is nil or empty, the default mystery-person avatar is returned.
func gravatarURL(email *string) string {
	var input string
	if email != nil {
		input = strings.ToLower(strings.TrimSpace(*email))
	}
	h := md5.Sum([]byte(input)) //nolint:gosec
	return fmt.Sprintf("https://www.gravatar.com/avatar/%x?d=mp&s=160", h)
}

// requireAdmin returns false and writes a 403 response if the user is not an admin.
func requireAdmin(w http.ResponseWriter, user *model.User) bool {
	if !user.IsAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "admin access required")
		return false
	}
	return true
}

// Login handles POST /api/v1/auth/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	ip := stripPort(r.RemoteAddr)
	if !loginRL.allow(ip) {
		LoggerFromContext(r.Context()).Warn("login rate limited", "ip", ip)
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many login attempts, try again later")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBody)

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "username and password are required")
		return
	}

	logger := LoggerFromContext(r.Context())

	user, err := h.store.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Burn the same CPU time as a real bcrypt check to prevent
			// timing-based username enumeration.
			auth.DummyCheckPassword(req.Password)
			logger.Warn("login failed: unknown user", "username", req.Username, "ip", ip)
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
			return
		}
		logger.Error("login: get user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "login failed")
		return
	}

	// Always run bcrypt before checking active status so the response
	// time is identical for active vs inactive accounts.
	if err := auth.CheckPassword(req.Password, user.PasswordHash); err != nil {
		logger.Warn("login failed: wrong password", "username", req.Username, "ip", ip)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}

	if !user.IsActive {
		logger.Warn("login failed: inactive account", "username", req.Username, "ip", ip)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}

	// Create session.
	rawToken, tokenHash, err := auth.GenerateSessionToken()
	if err != nil {
		LoggerFromContext(r.Context()).Error("login: generate session token", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "login failed")
		return
	}

	expiresAt := time.Now().Add(sessionDuration)
	ua := r.UserAgent()

	if err := h.store.CreateSession(r.Context(), tokenHash, user.ID.Bytes, expiresAt, ip, ua); err != nil {
		LoggerFromContext(r.Context()).Error("login: create session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "login failed")
		return
	}

	// Cap sessions per user to prevent unbounded growth.
	if err := h.store.PruneUserSessions(r.Context(), user.ID.Bytes, maxSessionsPerUser); err != nil {
		LoggerFromContext(r.Context()).Warn("login: prune sessions", "err", err)
	}

	// Update last login timestamp.
	if err := h.store.UpdateLastLogin(r.Context(), user.ID.Bytes); err != nil {
		logger.Warn("login: update last login", "err", err)
	}
	logger.Info("login succeeded", "username", user.Username, "ip", ip)

	secure := h.cookieSecure || isSecureRequest(r)
	if !secure {
		LoggerFromContext(r.Context()).Warn("setting session cookie without Secure flag")
	}
	// SameSite=Lax is used intentionally instead of adding a separate CSRF
	// token. Lax prevents cross-site POST requests from sending the cookie,
	// which covers the primary CSRF vector. If this application ever needs
	// to support cross-origin POST requests with credentials (e.g. embedded
	// forms from partner sites), a CSRF token should be added at that point.
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})

	writeJSON(w, loginResponse{
		User: buildUserInfo(user),
	})
}

// Logout handles POST /api/v1/auth/logout.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "no session")
		return
	}

	tokenHash := auth.HashToken(cookie.Value)
	if err := h.store.DeleteSession(r.Context(), tokenHash); err != nil {
		LoggerFromContext(r.Context()).Error("logout: delete session", "err", err)
	}

	// Clear the cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookieSecure || isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	writeJSON(w, map[string]string{"status": "ok"})
}

// LogoutAll handles POST /api/v1/auth/sessions/revoke-all.
// Revokes all sessions for the authenticated user, including the current one.
func (h *AuthHandler) LogoutAll(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}

	if err := h.store.DeleteUserSessions(r.Context(), user.ID.Bytes); err != nil {
		LoggerFromContext(r.Context()).Error("logout all: delete sessions", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to revoke sessions")
		return
	}

	// Clear the current cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookieSecure || isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	writeJSON(w, map[string]string{"status": "ok"})
}

// RevokeUserSessions handles POST /api/v1/auth/users/{id}/revoke-sessions.
// Admin-only: revokes all sessions for the specified user.
func (h *AuthHandler) RevokeUserSessions(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}

	if !requireAdmin(w, user) {
		return
	}

	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid user ID")
		return
	}

	if err := h.store.DeleteUserSessions(r.Context(), id); err != nil {
		LoggerFromContext(r.Context()).Error("revoke user sessions", "user_id", formatUUID(id), "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to revoke sessions")
		return
	}

	writeJSON(w, map[string]string{"status": "ok"})
}

// Me handles GET /api/v1/auth/me.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}

	writeJSON(w, loginResponse{
		User: buildUserInfo(*user),
	})
}

// formatUUID formats a [16]byte as a standard UUID string.
func formatUUID(b [16]byte) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// parseUUID parses a UUID string (with or without hyphens) into a [16]byte.
func parseUUID(s string) ([16]byte, error) {
	var id [16]byte
	clean := strings.ReplaceAll(s, "-", "")
	if len(clean) != 32 {
		return id, fmt.Errorf("invalid UUID: %s", s)
	}
	b, err := hex.DecodeString(clean)
	if err != nil {
		return id, fmt.Errorf("invalid UUID: %w", err)
	}
	copy(id[:], b)
	return id, nil
}

// --- API Key endpoints ---

type createKeyRequest struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt *string  `json:"expires_at"`
}

// validScopes is the set of allowed scope values.
var validScopes = map[string]bool{
	"ingest": true,
	"read":   true,
	"admin":  true,
}

type createKeyResponse struct {
	Key     string          `json:"key"`
	KeyInfo model.APIKeyRow `json:"key_info"`
}

// CreateKey handles POST /api/v1/auth/keys.
func (h *AuthHandler) CreateKey(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBody)

	var req createKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}

	if len(req.Name) > 255 {
		writeError(w, http.StatusBadRequest, "invalid_request", "name must be 255 characters or less")
		return
	}

	if len(req.Scopes) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "at least one scope is required (ingest, read, admin)")
		return
	}
	for _, s := range req.Scopes {
		if !validScopes[s] {
			writeError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("invalid scope: %s", s))
			return
		}
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "expires_at must be RFC3339 format")
			return
		}
		expiresAt = &t
	}

	fullKey, hash, prefix, err := auth.GenerateAPIKey()
	if err != nil {
		LoggerFromContext(r.Context()).Error("create key: generate", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to generate key")
		return
	}

	keyRow, err := h.store.CreateAPIKey(r.Context(), user.ID.Bytes, req.Name, hash, prefix, req.Scopes, expiresAt)
	if err != nil {
		LoggerFromContext(r.Context()).Error("create key: store", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create key")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(createKeyResponse{ //nolint:errcheck
		Key:     fullKey,
		KeyInfo: keyRow,
	})
}

// ListKeys handles GET /api/v1/auth/keys.
func (h *AuthHandler) ListKeys(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}

	keys, err := h.store.ListAPIKeysByUser(r.Context(), user.ID.Bytes)
	if err != nil {
		LoggerFromContext(r.Context()).Error("list keys", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list keys")
		return
	}

	writeJSON(w, listResponse{Data: emptySlice(keys)})
}

// RevokeKey handles DELETE /api/v1/auth/keys/{id}.
func (h *AuthHandler) RevokeKey(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}

	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid key ID")
		return
	}

	key, err := h.store.GetAPIKeyByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "key not found")
		return
	}
	if key.UserID.Bytes != user.ID.Bytes && !user.IsAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "cannot revoke another user's key")
		return
	}

	if err := h.store.RevokeAPIKey(r.Context(), id); err != nil {
		LoggerFromContext(r.Context()).Error("revoke key", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to revoke key")
		return
	}

	writeJSON(w, map[string]string{"status": "ok"})
}

// --- User management endpoints ---

// ListUsers handles GET /api/v1/auth/users.
func (h *AuthHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}

	if !requireAdmin(w, user) {
		return
	}

	users, err := h.store.ListUsers(r.Context())
	if err != nil {
		LoggerFromContext(r.Context()).Error("list users", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list users")
		return
	}

	writeJSON(w, listResponse{Data: emptySlice(users)})
}

type setUserActiveRequest struct {
	Active bool `json:"active"`
}

// SetUserActive handles PATCH /api/v1/auth/users/{id}/active.
func (h *AuthHandler) SetUserActive(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}

	if !requireAdmin(w, user) {
		return
	}

	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid user ID")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBody)

	var req setUserActiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if err := h.store.SetUserActive(r.Context(), id, req.Active); err != nil {
		LoggerFromContext(r.Context()).Error("set user active", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update user")
		return
	}

	// Force logout if deactivating.
	if !req.Active {
		if err := h.store.DeleteUserSessions(r.Context(), id); err != nil {
			LoggerFromContext(r.Context()).Error("delete user sessions on deactivate", "err", err)
		}
	}

	writeJSON(w, map[string]string{"status": "ok"})
}

type updatePasswordRequest struct {
	Password        string `json:"password"`
	CurrentPassword string `json:"current_password"`
}

// UpdateUserPassword handles PATCH /api/v1/auth/users/{id}/password.
func (h *AuthHandler) UpdateUserPassword(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}

	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid user ID")
		return
	}

	if id != user.ID.Bytes {
		writeError(w, http.StatusForbidden, "forbidden", "can only change your own password")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBody)

	var req updatePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if req.CurrentPassword == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "current password is required")
		return
	}

	if err := auth.CheckPassword(req.CurrentPassword, user.PasswordHash); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "current password is incorrect")
		return
	}

	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "invalid_request", "password must be at least 8 characters")
		return
	}
	if len(req.Password) > 72 {
		writeError(w, http.StatusBadRequest, "invalid_request", "password must be at most 72 characters (bcrypt limit)")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		LoggerFromContext(r.Context()).Error("update password: hash", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update password")
		return
	}

	if err := h.store.UpdatePassword(r.Context(), id, hash); err != nil {
		LoggerFromContext(r.Context()).Error("update password: store", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update password")
		return
	}

	// Invalidate all existing sessions for this user.
	if err := h.store.DeleteUserSessions(r.Context(), id); err != nil {
		LoggerFromContext(r.Context()).Error("delete user sessions on password change", "err", err)
	}

	writeJSON(w, map[string]string{"status": "ok"})
}

type updateEmailRequest struct {
	Email string `json:"email"`
}

// UpdateEmail handles PATCH /api/v1/auth/me/email.
func (h *AuthHandler) UpdateEmail(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBody)

	var req updateEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "email is required")
		return
	}

	if err := h.store.UpdateEmail(r.Context(), user.ID.Bytes, req.Email); err != nil {
		LoggerFromContext(r.Context()).Error("update email: store", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update email")
		return
	}

	// Re-fetch user to return updated info.
	updated, err := h.store.GetUserByID(r.Context(), user.ID.Bytes)
	if err != nil {
		LoggerFromContext(r.Context()).Error("update email: re-fetch", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update email")
		return
	}

	writeJSON(w, loginResponse{
		User: buildUserInfo(updated),
	})
}

// isSecureRequest returns true when the request arrived over TLS or via a
// trusted reverse proxy that set X-Forwarded-Proto: https.
func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// stripPort removes the port suffix from an address string so it can be
// stored as a bare IP in a Postgres INET column.
func stripPort(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr // already bare IP or unparseable — use as-is.
	}
	return host
}
