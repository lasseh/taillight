package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	oidcauth "github.com/lasseh/taillight/internal/oidc"
)

const (
	// oidcStateCookieName holds the signed in-flight login state between the
	// /oidc/login redirect and the /oidc/callback return. Path-scoped to the
	// OIDC endpoints so it never travels with ordinary API requests.
	oidcStateCookieName = "tl_oidc_state"
	oidcStateCookiePath = "/api/v1/auth/oidc"
	// oidcStateTTL bounds how long an in-flight login may take. Short-lived:
	// an expired flow just restarts with another click.
	oidcStateTTL = 10 * time.Minute
)

// Error codes surfaced to the login page as /login?error=<code>. Detailed
// reasons go to the server log only.
const (
	oidcErrFailed    = "sso_failed"    // Protocol/infrastructure failure.
	oidcErrDenied    = "sso_denied"    // The provider reported an error (user cancelled, IdP policy).
	oidcErrForbidden = "sso_forbidden" // Authenticated but not authorized (gating, inactive account).
	oidcErrExpired   = "sso_expired"   // Missing/stale/mismatched login state — restart the flow.
)

// oidcLoginState is the payload sealed into the state cookie: the per-login
// OIDC secrets plus the post-login redirect target and an expiry.
type oidcLoginState struct {
	oidcauth.LoginState
	Redirect string `json:"redirect"`
	Expires  int64  `json:"expires"` // Unix seconds.
}

// OIDCLogin handles GET /api/v1/auth/oidc/login. It generates the per-login
// secrets, seals them into a signed short-TTL cookie, and redirects the
// browser to the provider's authorization endpoint.
func (h *AuthHandler) OIDCLogin(w http.ResponseWriter, r *http.Request) {
	logger := LoggerFromContext(r.Context())

	ip := middleware.GetClientIP(r.Context())
	if !loginRL.allow(ip) {
		logger.Warn("oidc login rate limited", "ip", ip)
		redirectLoginError(w, r, oidcErrFailed)
		return
	}

	authURL, ls, err := h.oidc.BeginLogin()
	if err != nil {
		logger.Error("oidc login: begin", "err", err)
		redirectLoginError(w, r, oidcErrFailed)
		return
	}

	sealed, err := h.sealOIDCState(oidcLoginState{
		LoginState: ls,
		Redirect:   safeRedirectPath(r.URL.Query().Get("redirect")),
		Expires:    time.Now().Add(oidcStateTTL).Unix(),
	})
	if err != nil {
		logger.Error("oidc login: seal state", "err", err)
		redirectLoginError(w, r, oidcErrFailed)
		return
	}

	// SameSite=Lax: the callback arrives as a top-level cross-site navigation
	// from the IdP, which Lax permits; Strict would strip the cookie there.
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    sealed,
		Path:     oidcStateCookiePath,
		HttpOnly: true,
		Secure:   h.cookieSecure || isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(oidcStateTTL.Seconds()),
	})
	http.Redirect(w, r, authURL, http.StatusFound)
}

// OIDCCallback handles GET /api/v1/auth/oidc/callback. It verifies the state
// cookie, completes the code exchange, provisions/refreshes the user, and
// establishes an ordinary session before redirecting to the requested page.
// All failures redirect to the login page with a coarse error code.
func (h *AuthHandler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	logger := LoggerFromContext(r.Context())

	ip := middleware.GetClientIP(r.Context())
	if !loginRL.allow(ip) {
		logger.Warn("oidc callback rate limited", "ip", ip)
		redirectLoginError(w, r, oidcErrFailed)
		return
	}

	cookie, err := r.Cookie(oidcStateCookieName)
	if err != nil {
		logger.Warn("oidc callback without state cookie", "ip", ip)
		redirectLoginError(w, r, oidcErrExpired)
		return
	}
	h.clearOIDCStateCookie(w, r)

	st, err := h.openOIDCState(cookie.Value)
	if err != nil {
		logger.Warn("oidc callback: invalid state cookie", "err", err, "ip", ip)
		redirectLoginError(w, r, oidcErrExpired)
		return
	}

	q := r.URL.Query()
	if errCode := q.Get("error"); errCode != "" {
		logger.Warn("oidc login denied by provider",
			"error", errCode, "description", q.Get("error_description"), "ip", ip)
		redirectLoginError(w, r, oidcErrDenied)
		return
	}
	if subtleCompare(q.Get("state"), st.State) != 1 {
		logger.Warn("oidc callback: state mismatch", "ip", ip)
		redirectLoginError(w, r, oidcErrExpired)
		return
	}
	code := q.Get("code")
	if code == "" {
		logger.Warn("oidc callback without code", "ip", ip)
		redirectLoginError(w, r, oidcErrFailed)
		return
	}

	ident, err := h.oidc.CompleteLogin(r.Context(), code, st.LoginState)
	if err != nil {
		if errors.Is(err, oidcauth.ErrNotAuthorized) {
			logger.Warn("oidc login not authorized", "err", err, "ip", ip)
			redirectLoginError(w, r, oidcErrForbidden)
			return
		}
		logger.Error("oidc login failed", "err", err, "ip", ip)
		redirectLoginError(w, r, oidcErrFailed)
		return
	}

	user, err := h.store.UpsertOIDCUser(r.Context(), ident.Issuer, ident.Subject, ident.Username, ident.Email, ident.IsAdmin)
	if err != nil {
		logger.Error("oidc login: upsert user", "err", err)
		redirectLoginError(w, r, oidcErrFailed)
		return
	}
	if !user.IsActive {
		logger.Warn("oidc login failed: inactive account", "username", user.Username, "ip", ip)
		redirectLoginError(w, r, oidcErrForbidden)
		return
	}

	if err := h.establishSession(w, r, user); err != nil {
		logger.Error("oidc login: establish session", "err", err)
		redirectLoginError(w, r, oidcErrFailed)
		return
	}

	logger.Info("login succeeded", "username", user.Username, "auth_source", user.AuthSource, "ip", ip)
	http.Redirect(w, r, st.Redirect, http.StatusFound)
}

// sealOIDCState serializes and HMAC-signs the login state for the cookie:
// base64url(payload) + "." + base64url(HMAC-SHA256(payload)).
func (h *AuthHandler) sealOIDCState(st oidcLoginState) (string, error) {
	payload, err := json.Marshal(st)
	if err != nil {
		return "", fmt.Errorf("marshal oidc state: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + h.signOIDCState(encoded), nil
}

// openOIDCState verifies the cookie signature and expiry and returns the
// login state.
func (h *AuthHandler) openOIDCState(sealed string) (oidcLoginState, error) {
	encoded, sig, ok := strings.Cut(sealed, ".")
	if !ok {
		return oidcLoginState{}, errors.New("malformed state cookie")
	}
	if subtleCompare(sig, h.signOIDCState(encoded)) != 1 {
		return oidcLoginState{}, errors.New("state cookie signature mismatch")
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return oidcLoginState{}, fmt.Errorf("decode state cookie: %w", err)
	}
	var st oidcLoginState
	if err := json.Unmarshal(payload, &st); err != nil {
		return oidcLoginState{}, fmt.Errorf("unmarshal state cookie: %w", err)
	}
	if time.Now().Unix() > st.Expires {
		return oidcLoginState{}, errors.New("state cookie expired")
	}
	return st, nil
}

// signOIDCState computes the base64url HMAC-SHA256 tag over an encoded payload.
func (h *AuthHandler) signOIDCState(encoded string) string {
	mac := hmac.New(sha256.New, h.oidcStateKey)
	mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// clearOIDCStateCookie expires the state cookie; it is single-use.
func (h *AuthHandler) clearOIDCStateCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    "",
		Path:     oidcStateCookiePath,
		HttpOnly: true,
		Secure:   h.cookieSecure || isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// redirectLoginError sends the browser back to the SPA login page with a
// coarse machine-readable error code.
func redirectLoginError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/login?error="+url.QueryEscape(code), http.StatusFound)
}

// safeRedirectPath returns p when it is a same-origin absolute path, else "/".
// Rejects scheme-relative ("//evil.example") and backslash variants so the
// post-login redirect can never leave the origin.
func safeRedirectPath(p string) string {
	if p == "" || p[0] != '/' {
		return "/"
	}
	if len(p) > 1 && (p[1] == '/' || p[1] == '\\') {
		return "/"
	}
	return p
}

// subtleCompare wraps constant-time string comparison (1 when equal).
func subtleCompare(a, b string) int {
	if len(a) != len(b) {
		return 0
	}
	if hmac.Equal([]byte(a), []byte(b)) {
		return 1
	}
	return 0
}
