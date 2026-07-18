package oidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

// Package-level RSA keys so each test doesn't pay key generation.
var (
	idpKey   = mustKey()
	wrongKey = mustKey()
)

func mustKey() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return k
}

// fakeIdP is an httptest-backed OpenID provider: discovery, JWKS, and a token
// endpoint that mints RS256-signed ID tokens. Tests tweak the exported fields
// between BeginLogin and CompleteLogin to shape the token the "IdP" returns.
type fakeIdP struct {
	srv     *httptest.Server
	signKey *rsa.PrivateKey // Key used to sign; JWKS always serves idpKey's public part.
	nonce   string          // Nonce embedded in the ID token (normally echoed from the auth request).
	claims  map[string]any  // Extra/override claims merged over the defaults.

	gotVerifier string // code_verifier received by the token endpoint.
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	f := &fakeIdP{signKey: idpKey}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{
			"issuer":                                f.srv.URL,
			"authorization_endpoint":                f.srv.URL + "/authorize",
			"token_endpoint":                        f.srv.URL + "/token",
			"jwks_uri":                              f.srv.URL + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		pub := &idpKey.PublicKey
		writeJSON(t, w, map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA",
				"alg": "RS256",
				"use": "sig",
				"kid": "test-key",
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   "AQAB",
			}},
		})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.gotVerifier = r.FormValue("code_verifier")
		writeJSON(t, w, map[string]any{
			"access_token": "test-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"id_token":     f.mintIDToken(t),
		})
	})

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// mintIDToken builds an RS256-signed JWT with sane defaults, overridden by
// f.claims (which can also replace iss/aud/exp/... to simulate bad tokens).
func (f *fakeIdP) mintIDToken(t *testing.T) string {
	t.Helper()
	now := time.Now()
	claims := map[string]any{
		"iss":   f.srv.URL,
		"sub":   "subject-123",
		"aud":   "taillight-client",
		"exp":   now.Add(time.Hour).Unix(),
		"iat":   now.Unix(),
		"nonce": f.nonce,
	}
	maps.Copy(claims, f.claims)

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT","kid":"test-key"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	signingInput := header + "." + base64.RawURLEncoding.EncodeToString(payload)

	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.signKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign id_token: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("fake idp write: %v", err)
	}
}

// newProvider builds a Provider against the fake IdP with the given gating
// config (issuer/client/redirect are filled in by the harness).
func newProvider(f *fakeIdP, cfg Config) *Provider {
	cfg.IssuerURL = f.srv.URL
	cfg.ClientID = "taillight-client"
	cfg.ClientSecret = "test-secret"
	cfg.RedirectURL = "http://taillight.local/api/v1/auth/oidc/callback"
	return New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestBeginLoginAuthURL(t *testing.T) {
	f := newFakeIdP(t)
	p := newProvider(f, Config{})

	authURL, ls, err := p.BeginLogin()
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	if ls.State == "" || ls.Nonce == "" || ls.Verifier == "" {
		t.Fatalf("LoginState has empty fields: %+v", ls)
	}
	if ls.State == ls.Nonce {
		t.Error("state and nonce must be independent values")
	}

	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	if got := f.srv.URL + "/authorize"; !strings.HasPrefix(authURL, got) {
		t.Errorf("auth URL %q does not target authorization endpoint %q", authURL, got)
	}

	q := u.Query()
	if q.Get("state") != ls.State {
		t.Errorf("state param = %q, want %q", q.Get("state"), ls.State)
	}
	if q.Get("nonce") != ls.Nonce {
		t.Errorf("nonce param = %q, want %q", q.Get("nonce"), ls.Nonce)
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q, want code", q.Get("response_type"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	wantChallenge := sha256.Sum256([]byte(ls.Verifier))
	if q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(wantChallenge[:]) {
		t.Errorf("code_challenge does not match S256(verifier)")
	}
	scopes := strings.Fields(q.Get("scope"))
	for _, want := range []string{"openid", "profile", "email"} {
		if !slices.Contains(scopes, want) {
			t.Errorf("scope %q missing from %q", want, q.Get("scope"))
		}
	}
}

func TestBeginLoginExtraScopes(t *testing.T) {
	f := newFakeIdP(t)
	p := newProvider(f, Config{Scopes: []string{"groups", "email"}}) // "email" already default — no dup.

	authURL, _, err := p.BeginLogin()
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	u, _ := url.Parse(authURL)
	scopes := strings.Fields(u.Query().Get("scope"))
	if !slices.Contains(scopes, "groups") {
		t.Errorf("extra scope groups missing from %q", scopes)
	}
	n := 0
	for _, s := range scopes {
		if s == "email" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("scope email appears %d times, want 1", n)
	}
}

func TestCompleteLogin(t *testing.T) {
	verifiedEmail := map[string]any{
		"email":              "amelie.ops@example.com",
		"email_verified":     true,
		"preferred_username": "Amelie.Ops",
	}

	tests := []struct {
		name       string
		cfg        Config
		claims     map[string]any
		wrongKey   bool
		wrongNonce bool

		want          *Identity // nil means an error is expected
		wantForbidden bool      // expected error is ErrNotAuthorized
	}{
		{
			name:   "happy path maps claims and lowercases username",
			cfg:    Config{EmailVerifiedRequired: true},
			claims: verifiedEmail,
			want: &Identity{
				Subject:  "subject-123",
				Username: "amelie.ops",
				Email:    "amelie.ops@example.com",
			},
		},
		{
			name: "admin group grants is_admin case-insensitively",
			cfg:  Config{EmailVerifiedRequired: true, AdminGroups: []string{"NetOps-Admins"}},
			claims: merge(verifiedEmail, map[string]any{
				"groups": []any{"users", "netops-admins"},
			}),
			want: &Identity{
				Subject:  "subject-123",
				Username: "amelie.ops",
				Email:    "amelie.ops@example.com",
				IsAdmin:  true,
			},
		},
		{
			name: "custom username claim",
			cfg:  Config{UsernameClaim: "upn"},
			claims: map[string]any{
				"upn": "AMELIE@corp",
			},
			want: &Identity{Subject: "subject-123", Username: "amelie@corp"},
		},
		{
			name: "username falls back to email local-part",
			cfg:  Config{},
			claims: map[string]any{
				"email":          "fallback.user@example.com",
				"email_verified": true,
			},
			want: &Identity{
				Subject:  "subject-123",
				Username: "fallback.user",
				Email:    "fallback.user@example.com",
			},
		},
		{
			name:   "username falls back to subject",
			cfg:    Config{},
			claims: map[string]any{},
			want:   &Identity{Subject: "subject-123", Username: "subject-123"},
		},
		{
			name:     "bad signature rejected",
			cfg:      Config{},
			claims:   verifiedEmail,
			wrongKey: true,
		},
		{
			name:   "wrong audience rejected",
			cfg:    Config{},
			claims: merge(verifiedEmail, map[string]any{"aud": "some-other-client"}),
		},
		{
			name:   "expired token rejected",
			cfg:    Config{},
			claims: merge(verifiedEmail, map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}),
		},
		{
			name:   "wrong issuer rejected",
			cfg:    Config{},
			claims: merge(verifiedEmail, map[string]any{"iss": "https://evil.example.com"}),
		},
		{
			name:       "nonce mismatch rejected",
			cfg:        Config{},
			claims:     verifiedEmail,
			wrongNonce: true,
		},
		{
			name:   "allowed domain admits",
			cfg:    Config{EmailVerifiedRequired: true, AllowedDomains: []string{"Example.COM"}},
			claims: verifiedEmail,
			want: &Identity{
				Subject:  "subject-123",
				Username: "amelie.ops",
				Email:    "amelie.ops@example.com",
			},
		},
		{
			name:          "unlisted domain denied",
			cfg:           Config{EmailVerifiedRequired: true, AllowedDomains: []string{"other.org"}},
			claims:        verifiedEmail,
			wantForbidden: true,
		},
		{
			name: "allowed user admits when domain does not match",
			cfg: Config{
				EmailVerifiedRequired: true,
				AllowedDomains:        []string{"other.org"},
				AllowedUsers:          []string{"amelie.ops@example.com"},
			},
			claims: verifiedEmail,
			want: &Identity{
				Subject:  "subject-123",
				Username: "amelie.ops",
				Email:    "amelie.ops@example.com",
			},
		},
		{
			name:          "missing email denied under domain gating",
			cfg:           Config{EmailVerifiedRequired: true, AllowedDomains: []string{"example.com"}},
			claims:        map[string]any{"preferred_username": "ghost"},
			wantForbidden: true,
		},
		{
			name:          "unverified email denied by default-strict posture",
			cfg:           Config{EmailVerifiedRequired: true},
			claims:        merge(verifiedEmail, map[string]any{"email_verified": false}),
			wantForbidden: true,
		},
		{
			name:          "missing email_verified claim counts as unverified",
			cfg:           Config{EmailVerifiedRequired: true, AllowedDomains: []string{"example.com"}},
			claims:        map[string]any{"email": "amelie.ops@example.com"},
			wantForbidden: true,
		},
		{
			name:   "email_verified as string true tolerated",
			cfg:    Config{EmailVerifiedRequired: true, AllowedDomains: []string{"example.com"}},
			claims: merge(verifiedEmail, map[string]any{"email_verified": "true"}),
			want: &Identity{
				Subject:  "subject-123",
				Username: "amelie.ops",
				Email:    "amelie.ops@example.com",
			},
		},
		{
			name:   "verified-email override admits unverified",
			cfg:    Config{EmailVerifiedRequired: false},
			claims: merge(verifiedEmail, map[string]any{"email_verified": false}),
			want: &Identity{
				Subject:  "subject-123",
				Username: "amelie.ops",
				Email:    "amelie.ops@example.com",
			},
		},
		{
			name: "allowed group admits",
			cfg:  Config{AllowedGroups: []string{"netops"}},
			claims: merge(verifiedEmail, map[string]any{
				"groups": []any{"netops", "everyone"},
			}),
			want: &Identity{
				Subject:  "subject-123",
				Username: "amelie.ops",
				Email:    "amelie.ops@example.com",
			},
		},
		{
			name:          "non-member denied under group gating",
			cfg:           Config{AllowedGroups: []string{"netops"}},
			claims:        merge(verifiedEmail, map[string]any{"groups": []any{"everyone"}}),
			wantForbidden: true,
		},
		{
			name:          "missing groups claim denied under group gating",
			cfg:           Config{AllowedGroups: []string{"netops"}},
			claims:        verifiedEmail,
			wantForbidden: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeIdP(t)
			p := newProvider(f, tt.cfg)

			_, ls, err := p.BeginLogin()
			if err != nil {
				t.Fatalf("BeginLogin: %v", err)
			}
			f.nonce = ls.Nonce
			if tt.wrongNonce {
				f.nonce = "not-the-nonce"
			}
			f.claims = tt.claims
			if tt.wrongKey {
				f.signKey = wrongKey
			}

			ident, err := p.CompleteLogin(context.Background(), "test-code", ls)

			if tt.want == nil {
				if err == nil {
					t.Fatalf("CompleteLogin succeeded (%+v), want error", ident)
				}
				if got := errors.Is(err, ErrNotAuthorized); got != tt.wantForbidden {
					t.Fatalf("errors.Is(err, ErrNotAuthorized) = %v, want %v (err: %v)", got, tt.wantForbidden, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("CompleteLogin: %v", err)
			}
			tt.want.Issuer = f.srv.URL
			if *ident != *tt.want {
				t.Errorf("identity = %+v, want %+v", *ident, *tt.want)
			}
			// PKCE round-trip: the token endpoint must receive the original verifier.
			if f.gotVerifier != ls.Verifier {
				t.Errorf("token endpoint got code_verifier %q, want %q", f.gotVerifier, ls.Verifier)
			}
		})
	}
}

// merge returns a new map with overrides applied over base.
func merge(base, overrides map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(overrides))
	maps.Copy(out, base)
	maps.Copy(out, overrides)
	return out
}
