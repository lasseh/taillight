package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lasseh/taillight/internal/httputil"
	"github.com/lasseh/taillight/internal/model"
)

// mockSessionLookup implements SessionLookup for tests.
type mockSessionLookup struct {
	user *model.User
	err  error
}

func (m *mockSessionLookup) GetSessionUser(_ context.Context, _ string) (*model.User, error) {
	return m.user, m.err
}

// mockAPIKeyLookup implements APIKeyLookup for tests.
type mockAPIKeyLookup struct {
	user   *model.User
	scopes []string
	keyID  pgtype.UUID
	err    error
}

func (m *mockAPIKeyLookup) GetAPIKeyUser(_ context.Context, _ string) (*model.User, []string, pgtype.UUID, error) {
	return m.user, m.scopes, m.keyID, m.err
}

func TestAllowAnonymous(t *testing.T) {
	handler := AllowAnonymous(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil {
			t.Fatal("expected user in context")
		}
		if user.Username != "anonymous" {
			t.Errorf("got username %q, want %q", user.Username, "anonymous")
		}
		if !user.IsActive {
			t.Error("expected anonymous user to be active")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestSessionOrAPIKey(t *testing.T) {
	testUser := &model.User{Username: "testuser", IsActive: true}

	tests := []struct {
		name       string
		sessions   *mockSessionLookup
		apiKeys    *mockAPIKeyLookup
		cookie     *http.Cookie
		bearer     string
		wantStatus int
		wantUser   string
		wantScopes []string
	}{
		{
			name:       "valid session cookie",
			sessions:   &mockSessionLookup{user: testUser},
			apiKeys:    &mockAPIKeyLookup{},
			cookie:     &http.Cookie{Name: "tl_session", Value: "valid-token"},
			wantStatus: http.StatusOK,
			wantUser:   "testuser",
		},
		{
			name:       "session lookup error falls through",
			sessions:   &mockSessionLookup{err: errors.New("db error")},
			apiKeys:    &mockAPIKeyLookup{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "session returns nil user falls through",
			sessions:   &mockSessionLookup{user: nil},
			apiKeys:    &mockAPIKeyLookup{},
			cookie:     &http.Cookie{Name: "tl_session", Value: "bad-token"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "valid api key with tl_ prefix",
			sessions:   &mockSessionLookup{},
			apiKeys:    &mockAPIKeyLookup{user: testUser, scopes: []string{"read"}},
			bearer:     "tl_someapikey123",
			wantStatus: http.StatusOK,
			wantUser:   "testuser",
			wantScopes: []string{"read"},
		},
		{
			name:       "unknown api key returns unauthorized",
			sessions:   &mockSessionLookup{},
			apiKeys:    &mockAPIKeyLookup{err: fmt.Errorf("get api key by hash: %w", pgx.ErrNoRows)},
			bearer:     "tl_badkey",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "unknown session returns unauthorized",
			sessions:   &mockSessionLookup{err: fmt.Errorf("get session: %w", pgx.ErrNoRows)},
			apiKeys:    &mockAPIKeyLookup{},
			cookie:     &http.Cookie{Name: "tl_session", Value: "expired-token"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "failed session lookup returns unavailable",
			sessions:   &mockSessionLookup{err: context.DeadlineExceeded},
			apiKeys:    &mockAPIKeyLookup{},
			cookie:     &http.Cookie{Name: "tl_session", Value: "valid-token"},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "failed api key lookup returns unavailable",
			sessions:   &mockSessionLookup{},
			apiKeys:    &mockAPIKeyLookup{err: errors.New("db error")},
			bearer:     "tl_someapikey123",
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "failed session lookup with unknown api key returns unavailable",
			sessions:   &mockSessionLookup{err: errors.New("db error")},
			apiKeys:    &mockAPIKeyLookup{err: pgx.ErrNoRows},
			cookie:     &http.Cookie{Name: "tl_session", Value: "valid-token"},
			bearer:     "tl_badkey",
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "failed session lookup with valid api key succeeds",
			sessions:   &mockSessionLookup{err: errors.New("db error")},
			apiKeys:    &mockAPIKeyLookup{user: testUser, scopes: []string{"read"}},
			cookie:     &http.Cookie{Name: "tl_session", Value: "valid-token"},
			bearer:     "tl_someapikey123",
			wantStatus: http.StatusOK,
			wantUser:   "testuser",
		},
		{
			name:       "non-tl_ bearer returns unauthorized",
			sessions:   &mockSessionLookup{},
			apiKeys:    &mockAPIKeyLookup{},
			bearer:     "some-random-key",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "no credentials",
			sessions:   &mockSessionLookup{},
			apiKeys:    &mockAPIKeyLookup{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "empty session cookie ignored",
			sessions:   &mockSessionLookup{},
			apiKeys:    &mockAPIKeyLookup{},
			cookie:     &http.Cookie{Name: "tl_session", Value: ""},
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.wantUser != "" {
					user := UserFromContext(r.Context())
					if user != nil && user.Username != tt.wantUser {
						t.Errorf("got username %q, want %q", user.Username, tt.wantUser)
					}
				}
				if tt.wantScopes != nil {
					scopes := ScopesFromContext(r.Context())
					if len(scopes) != len(tt.wantScopes) {
						t.Errorf("got %d scopes, want %d", len(scopes), len(tt.wantScopes))
					}
				}
				w.WriteHeader(http.StatusOK)
			})

			mw := SessionOrAPIKey(tt.sessions, tt.apiKeys)
			handler := mw(inner)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			if tt.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tt.bearer)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusServiceUnavailable {
				var body httputil.ErrorBody
				if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Error.Code != "auth_unavailable" {
					t.Errorf("503 body = %+v (decode err %v), want code auth_unavailable", body, err)
				}
			}
		})
	}
}

// deadlineLookup records the deadline each credential lookup ran under.
type deadlineLookup struct {
	deadlines []time.Duration
}

func (d *deadlineLookup) record(ctx context.Context) {
	if dl, ok := ctx.Deadline(); ok {
		d.deadlines = append(d.deadlines, time.Until(dl))
	} else {
		d.deadlines = append(d.deadlines, -1)
	}
}

func (d *deadlineLookup) GetSessionUser(ctx context.Context, _ string) (*model.User, error) {
	d.record(ctx)
	return nil, pgx.ErrNoRows
}

func (d *deadlineLookup) GetAPIKeyUser(ctx context.Context, _ string) (*model.User, []string, pgtype.UUID, error) {
	d.record(ctx)
	return nil, nil, pgtype.UUID{}, pgx.ErrNoRows
}

// Each credential lookup runs under its own short deadline even when the
// request has none, so a lookup stuck waiting for a database connection
// cannot hold the request open.
func TestSessionOrAPIKeyLookupsHaveDeadline(t *testing.T) {
	lookups := &deadlineLookup{}
	handler := SessionOrAPIKey(lookups, lookups)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("handler reached with unknown credentials")
	}))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "tl_session", Value: "token"})
	req.Header.Set("Authorization", "Bearer tl_key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if len(lookups.deadlines) != 2 {
		t.Fatalf("got %d lookups, want 2 (session, then API key)", len(lookups.deadlines))
	}
	for i, d := range lookups.deadlines {
		if d <= 0 || d > lookupTimeout {
			t.Errorf("lookup %d deadline in %v, want within (0, %v]", i, d, lookupTimeout)
		}
	}
}

func TestRequireScope(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	testUser := &model.User{Username: "testuser", IsActive: true}

	tests := []struct {
		name       string
		scope      string
		ctxScopes  []string
		nilScopes  bool
		noUser     bool
		wantStatus int
	}{
		{
			name:       "no user returns 401",
			scope:      "read",
			noUser:     true,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "session auth (nil scopes) allowed",
			scope:      "read",
			nilScopes:  true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "matching scope allowed",
			scope:      "read",
			ctxScopes:  []string{"read"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "admin scope grants access to anything",
			scope:      "read",
			ctxScopes:  []string{"admin"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "admin scope grants ingest access",
			scope:      "ingest",
			ctxScopes:  []string{"admin"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "wrong scope forbidden",
			scope:      "admin",
			ctxScopes:  []string{"read"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "ingest scope cannot read",
			scope:      "read",
			ctxScopes:  []string{"ingest"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "empty scopes forbidden",
			scope:      "read",
			ctxScopes:  []string{},
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := RequireScope(tt.scope)
			handler := mw(okHandler)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
			ctx := req.Context()
			if !tt.noUser {
				ctx = WithUser(ctx, testUser)
			}
			if !tt.nilScopes {
				ctx = WithScopes(ctx, tt.ctxScopes)
			}
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

// TestSessionOnlyGates pins which API keys pass the account gates: minting
// accepts sessions only, and account writes accept a session or a key that
// holds the admin grant.
func TestSessionOnlyGates(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	admin := &model.User{Username: "admin", IsActive: true, IsAdmin: true}
	regular := &model.User{Username: "regular", IsActive: true}

	tests := []struct {
		name         string
		user         *model.User
		keyScopes    []string // nil = session auth
		wantSession  int      // RequireSession
		wantAdminKey int      // RequireSessionOrAdminKey
	}{
		{"regular session", regular, nil, http.StatusOK, http.StatusOK},
		{"admin session", admin, nil, http.StatusOK, http.StatusOK},
		{"admin-owned admin key", admin, []string{"admin"}, http.StatusForbidden, http.StatusOK},
		{"admin key of a non-admin owner", regular, []string{"admin"}, http.StatusForbidden, http.StatusForbidden},
		{"admin-owned read key", admin, []string{"read"}, http.StatusForbidden, http.StatusForbidden},
		{"admin-owned ingest key", admin, []string{"ingest"}, http.StatusForbidden, http.StatusForbidden},
		{"admin-owned key with no scopes", admin, []string{}, http.StatusForbidden, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := WithUser(context.Background(), tt.user)
			if tt.keyScopes != nil {
				ctx = WithScopes(ctx, tt.keyScopes)
				ctx = WithAPIKeyID(ctx, pgtype.UUID{Bytes: [16]byte{7}, Valid: true})
			}
			for _, g := range []struct {
				name string
				mw   func(http.Handler) http.Handler
				want int
			}{
				{"RequireSession", RequireSession, tt.wantSession},
				{"RequireSessionOrAdminKey", RequireSessionOrAdminKey, tt.wantAdminKey},
			} {
				req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/", nil)
				rec := httptest.NewRecorder()
				g.mw(okHandler).ServeHTTP(rec, req)
				if rec.Code != g.want {
					t.Errorf("%s: got status %d, want %d", g.name, rec.Code, g.want)
				}
			}
		})
	}
}

func TestWithUserAndUserFromContext(t *testing.T) {
	user := &model.User{Username: "ctx-user", IsActive: true}
	ctx := WithUser(context.Background(), user)

	got := UserFromContext(ctx)
	if got == nil {
		t.Fatal("expected user from context, got nil")
	}
	if got.Username != "ctx-user" {
		t.Errorf("got username %q, want %q", got.Username, "ctx-user")
	}
}

func TestUserFromContextEmpty(t *testing.T) {
	got := UserFromContext(context.Background())
	if got != nil {
		t.Errorf("expected nil user from empty context, got %v", got)
	}
}

func TestExtractBearer(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			name:   "valid bearer",
			header: "Bearer mytoken123",
			want:   "mytoken123",
		},
		{
			name:   "no bearer prefix",
			header: "Basic dXNlcjpwYXNz",
			want:   "",
		},
		{
			name:   "empty header",
			header: "",
			want:   "",
		},
		{
			name:   "bearer with empty token",
			header: "Bearer ",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			got := extractBearer(req)
			if got != tt.want {
				t.Errorf("extractBearer() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteJSONError(t *testing.T) {
	rec := httptest.NewRecorder()
	httputil.WriteError(rec, http.StatusForbidden, "forbidden", "access denied")

	if rec.Code != http.StatusForbidden {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusForbidden)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("got content-type %q, want %q", ct, "application/json")
	}
	body := rec.Body.String()
	if body == "" {
		t.Fatal("expected non-empty body")
	}
}

func TestScopesContext(t *testing.T) {
	// nil scopes by default.
	got := ScopesFromContext(context.Background())
	if got != nil {
		t.Errorf("expected nil scopes from empty context, got %v", got)
	}

	// Store and retrieve scopes.
	scopes := []string{"read", "ingest"}
	ctx := WithScopes(context.Background(), scopes)
	got = ScopesFromContext(ctx)
	if len(got) != 2 || got[0] != "read" || got[1] != "ingest" {
		t.Errorf("got scopes %v, want %v", got, scopes)
	}
}

func TestDenyWrites(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name       string
		method     string
		path       string
		exempt     []string
		remoteAddr string
		wantStatus int
	}{
		{"GET allowed", http.MethodGet, "/api/v1/srvlog", nil, "", http.StatusOK},
		{"HEAD allowed", http.MethodHead, "/api/v1/srvlog", nil, "", http.StatusOK},
		{"OPTIONS allowed", http.MethodOptions, "/api/v1/srvlog", nil, "", http.StatusOK},
		{"POST blocked", http.MethodPost, "/api/v1/notifications/channels", nil, "", http.StatusForbidden},
		{"PUT blocked", http.MethodPut, "/api/v1/notifications/channels/1", nil, "", http.StatusForbidden},
		{"DELETE blocked", http.MethodDelete, "/api/v1/notifications/rules/1", nil, "", http.StatusForbidden},
		{"POST exempt from private IP", http.MethodPost, "/api/v1/applog/ingest", []string{"/api/v1/applog/ingest"}, "172.18.0.5:12345", http.StatusOK},
		{"POST exempt from loopback", http.MethodPost, "/api/v1/applog/ingest", []string{"/api/v1/applog/ingest"}, "127.0.0.1:54321", http.StatusOK},
		{"POST exempt from public IP blocked", http.MethodPost, "/api/v1/applog/ingest", []string{"/api/v1/applog/ingest"}, "203.0.113.50:12345", http.StatusForbidden},
		{"POST non-exempt still blocked", http.MethodPost, "/api/v1/notifications/channels", []string{"/api/v1/applog/ingest"}, "", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := DenyWrites(tt.exempt...)
			// Wrap with ClientIPFromRemoteAddr so DenyWrites' GetClientIP lookup
			// is populated from the request's TCP peer, mirroring the router.
			handler := middleware.ClientIPFromRemoteAddr(mw(okHandler))

			req := httptest.NewRequestWithContext(context.Background(), tt.method, tt.path, nil)
			if tt.remoteAddr != "" {
				req.RemoteAddr = tt.remoteAddr
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

// TestDenyWritesRawPeerCheck verifies the exempt-path gate also checks the raw
// TCP peer: a spoofed private real_ip_header from a public peer — or a public
// client arriving via a private-IP proxy — cannot reach the exempt endpoint
// (June audit issue 14).
func TestDenyWritesRawPeerCheck(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name       string
		remoteAddr string
		xRealIP    string
		wantStatus int
	}{
		{"private header from public peer blocked", "203.0.113.50:12345", "10.0.0.1", http.StatusForbidden},
		{"public header from private peer blocked", "172.18.0.5:12345", "203.0.113.9", http.StatusForbidden},
		{"private header from private peer allowed", "172.18.0.5:12345", "10.0.0.1", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := DenyWrites("/api/v1/applog/ingest")
			// Wrap with ClientIPFromHeader to mirror a real_ip_header deployment,
			// where the resolved client IP comes from the proxy header.
			handler := middleware.ClientIPFromHeader("X-Real-IP")(mw(okHandler))

			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/applog/ingest", nil)
			req.RemoteAddr = tt.remoteAddr
			req.Header.Set("X-Real-IP", tt.xRealIP)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

// TestHasGrant pins that the admin scope follows the user's current admin
// flag for API keys as well as sessions, so an admin-scope key stops granting
// admin once its owner is not an admin, while its other grants are unchanged.
func TestHasGrant(t *testing.T) {
	admin := &model.User{Username: "admin", IsActive: true, IsAdmin: true}
	regular := &model.User{Username: "regular", IsActive: true}

	tests := []struct {
		name   string
		scopes []string // nil = session auth
		user   *model.User
		scope  string
		want   bool
	}{
		{"admin session holds admin", nil, admin, ScopeAdmin, true},
		{"regular session lacks admin", nil, regular, ScopeAdmin, false},
		{"regular session holds read", nil, regular, "read", true},
		{"admin-owned admin key holds admin", []string{"admin"}, admin, ScopeAdmin, true},
		{"admin key of a non-admin owner lacks admin", []string{"admin"}, regular, ScopeAdmin, false},
		{"admin key of a non-admin owner still holds read", []string{"admin"}, regular, "read", true},
		{"admin key of a non-admin owner still holds ingest", []string{"admin"}, regular, "ingest", true},
		{"admin owner's read key lacks admin", []string{"read"}, admin, ScopeAdmin, false},
		{"admin owner's read key holds read", []string{"read"}, admin, "read", true},
		{"no user", nil, nil, "read", false},
		{"no user with admin key", []string{"admin"}, nil, ScopeAdmin, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasGrant(tt.scopes, tt.user, tt.scope); got != tt.want {
				t.Errorf("HasGrant(%v, %v, %q) = %v, want %v", tt.scopes, tt.user, tt.scope, got, tt.want)
			}
		})
	}
}

func TestHasScope(t *testing.T) {
	tests := []struct {
		name   string
		scopes []string
		target string
		want   bool
	}{
		{"exact match", []string{"read"}, "read", true},
		{"admin grants all", []string{"admin"}, "read", true},
		{"admin grants ingest", []string{"admin"}, "ingest", true},
		{"no match", []string{"read"}, "ingest", false},
		{"empty scopes", []string{}, "read", false},
		{"multiple scopes match", []string{"read", "ingest"}, "ingest", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasScope(tt.scopes, tt.target)
			if got != tt.want {
				t.Errorf("hasScope(%v, %q) = %v, want %v", tt.scopes, tt.target, got, tt.want)
			}
		})
	}
}
