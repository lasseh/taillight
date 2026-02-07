package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
	user *model.User
	err  error
}

func (m *mockAPIKeyLookup) GetAPIKeyUser(_ context.Context, _ string) (*model.User, error) {
	return m.user, m.err
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

	req := httptest.NewRequest(http.MethodGet, "/", nil)
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
		configKeys []string
		cookie     *http.Cookie
		bearer     string
		wantStatus int
		wantUser   string
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
			apiKeys:    &mockAPIKeyLookup{user: testUser},
			bearer:     "tl_someapikey123",
			wantStatus: http.StatusOK,
			wantUser:   "testuser",
		},
		{
			name:       "api key lookup error falls through to config keys",
			sessions:   &mockSessionLookup{},
			apiKeys:    &mockAPIKeyLookup{err: errors.New("not found")},
			configKeys: []string{"config-key-1"},
			bearer:     "tl_badkey",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "valid config key",
			sessions:   &mockSessionLookup{},
			apiKeys:    &mockAPIKeyLookup{},
			configKeys: []string{"my-config-key"},
			bearer:     "my-config-key",
			wantStatus: http.StatusOK,
		},
		{
			name:       "config key mismatch",
			sessions:   &mockSessionLookup{},
			apiKeys:    &mockAPIKeyLookup{},
			configKeys: []string{"my-config-key"},
			bearer:     "wrong-key",
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
				w.WriteHeader(http.StatusOK)
			})

			mw := SessionOrAPIKey(tt.sessions, tt.apiKeys, tt.configKeys)
			handler := mw(inner)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
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
			req := httptest.NewRequest(http.MethodGet, "/", nil)
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

func TestConstantTimeMatch(t *testing.T) {
	tests := []struct {
		name  string
		keys  []string
		token string
		want  bool
	}{
		{
			name:  "match first key",
			keys:  []string{"key1", "key2"},
			token: "key1",
			want:  true,
		},
		{
			name:  "match second key",
			keys:  []string{"key1", "key2"},
			token: "key2",
			want:  true,
		},
		{
			name:  "no match",
			keys:  []string{"key1", "key2"},
			token: "key3",
			want:  false,
		},
		{
			name:  "empty keys",
			keys:  nil,
			token: "key1",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := constantTimeMatch(tt.keys, tt.token)
			if got != tt.want {
				t.Errorf("constantTimeMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWriteJSONError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSONError(rec, http.StatusForbidden, "forbidden", "access denied")

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
