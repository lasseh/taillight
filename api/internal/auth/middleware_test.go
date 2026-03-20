package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
	err    error
}

func (m *mockAPIKeyLookup) GetAPIKeyUser(_ context.Context, _ string) (*model.User, []string, error) {
	return m.user, m.scopes, m.err
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
			name:       "api key lookup error returns unauthorized",
			sessions:   &mockSessionLookup{},
			apiKeys:    &mockAPIKeyLookup{err: errors.New("not found")},
			bearer:     "tl_badkey",
			wantStatus: http.StatusUnauthorized,
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
		})
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
		{"GET allowed", http.MethodGet, "/api/v1/syslog", nil, "", http.StatusOK},
		{"HEAD allowed", http.MethodHead, "/api/v1/syslog", nil, "", http.StatusOK},
		{"OPTIONS allowed", http.MethodOptions, "/api/v1/syslog", nil, "", http.StatusOK},
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
			handler := mw(okHandler)

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
