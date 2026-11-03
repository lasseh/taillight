package handler

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lasseh/taillight/internal/auth"
	"github.com/lasseh/taillight/internal/model"
	oidcauth "github.com/lasseh/taillight/internal/oidc"
)

func TestSafeRedirectPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "/"},
		{name: "root", in: "/", want: "/"},
		{name: "path with query", in: "/netlog?host=a", want: "/netlog?host=a"},
		{name: "encoded backslash in query", in: "/netlog?severity=3&q=a%5Cb", want: "/netlog?severity=3&q=a%5Cb"},
		{name: "relative path", in: "netlog", want: "/"},
		{name: "absolute URL", in: "https://other.example/", want: "/"},
		{name: "scheme-relative", in: "//other.example/", want: "/"},
		{name: "backslash after first slash", in: "/\\other.example/", want: "/"},
		{name: "dot segment before backslash", in: "/./\\other.example/", want: "/"},
		{name: "parent segment before backslash", in: "/x/../\\other.example/", want: "/"},
		{name: "tab after first slash", in: "/\t/other.example/", want: "/"},
		{name: "tab before backslash", in: "/\t\\other.example/", want: "/"},
		{name: "carriage return", in: "/\r/other.example/", want: "/"},
		{name: "line feed", in: "/\n/other.example/", want: "/"},
		{name: "NUL", in: "/netlog\x00", want: "/"},
		{name: "DEL", in: "/netlog\x7f", want: "/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeRedirectPath(tt.in); got != tt.want {
				t.Errorf("safeRedirectPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestOIDCCallbackLocationStaysOnOrigin runs the whole flow, login then
// callback, against a loopback provider, and checks the Location header the
// callback emits after a successful login.
func TestOIDCCallbackLocationStaysOnOrigin(t *testing.T) {
	idp := newTestIdP(t)
	provider := oidcauth.New(oidcauth.Config{
		IssuerURL:    idp.srv.URL,
		ClientID:     testIdPClientID,
		ClientSecret: "test-secret",
		RedirectURL:  "http://taillight.test/api/v1/auth/oidc/callback",
	}, slog.New(slog.DiscardHandler))
	user := model.User{ID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true}, Username: "dana", AuthSource: auth.SourceOIDC, IsActive: true}
	h := NewAuthHandler(&mockAuthStore{user: user}, nil, provider, false)

	tests := []struct {
		name     string
		redirect string
		want     string
	}{
		{name: "path with query preserved", redirect: "/netlog?host=a", want: "/netlog?host=a"},
		{name: "dot segment before backslash", redirect: "/./\\other.example/", want: "/"},
		{name: "parent segment before backslash", redirect: "/x/../\\other.example/", want: "/"},
		{name: "tab after first slash", redirect: "/\t/other.example/", want: "/"},
		{name: "scheme-relative", redirect: "//other.example/", want: "/"},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// One client IP per case keeps the shared login rate limiter
			// from tripping here or in later tests.
			remote := fmt.Sprintf("192.0.2.%d:40000", 10+i)

			login := serveWithClientIP(h.OIDCLogin, "/api/v1/auth/oidc/login?"+url.Values{"redirect": {tt.redirect}}.Encode(), remote, nil)
			if login.Code != http.StatusFound {
				t.Fatalf("login status = %d, want %d", login.Code, http.StatusFound)
			}
			authURL, err := url.Parse(login.Header().Get("Location"))
			if err != nil {
				t.Fatalf("parse authorization URL: %v", err)
			}
			var state *http.Cookie
			for _, c := range login.Result().Cookies() {
				if c.Name == oidcStateCookieName {
					state = c
				}
			}
			if state == nil {
				t.Fatal("login set no state cookie")
			}
			idp.nonce = authURL.Query().Get("nonce")

			q := url.Values{"state": {authURL.Query().Get("state")}, "code": {"test-code"}}
			cb := serveWithClientIP(h.OIDCCallback, "/api/v1/auth/oidc/callback?"+q.Encode(), remote, state)
			if cb.Code != http.StatusFound {
				t.Fatalf("callback status = %d, want %d", cb.Code, http.StatusFound)
			}
			if got := cb.Header().Get("Location"); got != tt.want {
				t.Errorf("callback Location = %q, want %q", got, tt.want)
			}
		})
	}
}

// serveWithClientIP runs handler on a GET to target from the given remote
// address, resolved as the client IP the way the router does it.
func serveWithClientIP(handler http.HandlerFunc, target, remote string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	req.RemoteAddr = remote
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	middleware.ClientIPFromRemoteAddr(handler).ServeHTTP(rec, req)
	return rec
}

const testIdPClientID = "taillight-client"

// testIdP is a loopback OpenID provider: discovery, JWKS, and a token
// endpoint that returns an RS256 ID token carrying nonce.
type testIdP struct {
	srv   *httptest.Server
	key   *rsa.PrivateKey
	nonce string
}

func newTestIdP(t *testing.T) *testIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &testIdP{key: key}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, map[string]any{
			"issuer":                                idp.srv.URL,
			"authorization_endpoint":                idp.srv.URL + "/authorize",
			"token_endpoint":                        idp.srv.URL + "/token",
			"jwks_uri":                              idp.srv.URL + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, map[string]any{"keys": []map[string]any{{
			"kty": "RSA",
			"alg": "RS256",
			"use": "sig",
			"kid": "test-key",
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   "AQAB",
		}}})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, map[string]any{
			"access_token": "test-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"id_token":     idp.idToken(t),
		})
	})
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

// idToken runs on the server goroutine, so it reports failures with Errorf.
func (idp *testIdP) idToken(t *testing.T) string {
	t.Helper()
	now := time.Now()
	payload, err := json.Marshal(map[string]any{
		"iss":                idp.srv.URL,
		"sub":                "subject-123",
		"aud":                testIdPClientID,
		"exp":                now.Add(time.Hour).Unix(),
		"iat":                now.Unix(),
		"nonce":              idp.nonce,
		"preferred_username": "dana",
	})
	if err != nil {
		t.Errorf("marshal id_token claims: %v", err)
		return ""
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT","kid":"test-key"}`))
	signingInput := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, idp.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Errorf("sign id_token: %v", err)
		return ""
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("test idp write: %v", err)
	}
}
