package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lasseh/taillight/internal/auth"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/postgres"
)

// mockAuthStore implements AuthStore for testing.
type mockAuthStore struct {
	user        model.User
	userErr     error
	users       []model.User
	usersErr    error
	sessionErr  error
	sessionDel  error
	pruneErr    error
	updateLogin error
	keys        []model.APIKeyRow
	keysErr     error
	createKey   model.APIKeyRow
	createErr   error
	revokeErr   error
	keyByID     model.APIKeyRow
	keyByIDErr  error
	activeErr   error
	delSessErr  error
	updatePwErr error
	emailErr    error
	refetchUser model.User
	refetchErr  error
}

func (m *mockAuthStore) CreateUser(_ context.Context, _, _ string, _ bool) (model.User, error) {
	return m.user, m.userErr
}

func (m *mockAuthStore) GetUserByUsername(_ context.Context, _ string) (model.User, error) {
	return m.user, m.userErr
}

func (m *mockAuthStore) GetUserByID(_ context.Context, _ [16]byte) (model.User, error) {
	if m.refetchErr != nil {
		return model.User{}, m.refetchErr
	}
	return m.refetchUser, nil
}

func (m *mockAuthStore) UpdateLastLogin(_ context.Context, _ [16]byte) error {
	return m.updateLogin
}

func (m *mockAuthStore) UpdateEmail(_ context.Context, _ [16]byte, _ string) error {
	return m.emailErr
}

func (m *mockAuthStore) ListUsers(_ context.Context) ([]model.User, error) {
	return m.users, m.usersErr
}

func (m *mockAuthStore) SetUserActive(_ context.Context, _ [16]byte, _ bool) error {
	return m.activeErr
}

func (m *mockAuthStore) UpdatePassword(_ context.Context, _ [16]byte, _ string) error {
	return m.updatePwErr
}

func (m *mockAuthStore) CreateSession(_ context.Context, _ string, _ [16]byte, _ time.Time, _, _ string) error {
	return m.sessionErr
}

func (m *mockAuthStore) GetSession(_ context.Context, _ string) (postgres.SessionWithUser, error) {
	return postgres.SessionWithUser{}, nil
}

func (m *mockAuthStore) DeleteSession(_ context.Context, _ string) error {
	return m.sessionDel
}

func (m *mockAuthStore) DeleteUserSessions(_ context.Context, _ [16]byte) error {
	return m.delSessErr
}

func (m *mockAuthStore) PruneUserSessions(_ context.Context, _ [16]byte, _ int) error {
	return m.pruneErr
}

func (m *mockAuthStore) CleanExpiredSessions(_ context.Context) (int64, error) {
	return 0, nil
}

func (m *mockAuthStore) CreateAPIKey(_ context.Context, _ [16]byte, _, _, _ string, _ []string, _ *time.Time) (model.APIKeyRow, error) {
	return m.createKey, m.createErr
}

func (m *mockAuthStore) GetAPIKeyByHash(_ context.Context, _ string) (postgres.APIKeyWithUser, error) {
	return postgres.APIKeyWithUser{}, nil
}

func (m *mockAuthStore) ListAPIKeysByUser(_ context.Context, _ [16]byte) ([]model.APIKeyRow, error) {
	return m.keys, m.keysErr
}

func (m *mockAuthStore) RevokeAPIKey(_ context.Context, _ [16]byte) error {
	return m.revokeErr
}

func (m *mockAuthStore) GetAPIKeyByID(_ context.Context, _ [16]byte) (model.APIKeyRow, error) {
	return m.keyByID, m.keyByIDErr
}

func testUser(t *testing.T) model.User {
	t.Helper()
	hash, err := auth.HashPassword("correctpassword")
	if err != nil {
		t.Fatal(err)
	}
	return model.User{
		ID:           pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		Username:     "testuser",
		PasswordHash: hash,
		IsActive:     true,
		CreatedAt:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestLogin(t *testing.T) {
	user := testUser(t)

	tests := []struct {
		name       string
		body       string
		store      *mockAuthStore
		wantStatus int
	}{
		{
			name:       "success",
			body:       `{"username":"testuser","password":"correctpassword"}`,
			store:      &mockAuthStore{user: user},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid json",
			body:       `{bad json}`,
			store:      &mockAuthStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty username",
			body:       `{"username":"","password":"pass"}`,
			store:      &mockAuthStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty password",
			body:       `{"username":"user","password":""}`,
			store:      &mockAuthStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "user not found",
			body:       `{"username":"noone","password":"pass"}`,
			store:      &mockAuthStore{userErr: pgx.ErrNoRows},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong password",
			body:       `{"username":"testuser","password":"wrongpassword"}`,
			store:      &mockAuthStore{user: user},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "inactive user",
			body: `{"username":"testuser","password":"correctpassword"}`,
			store: func() *mockAuthStore {
				u := user
				u.IsActive = false
				return &mockAuthStore{user: u}
			}(),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "db error",
			body:       `{"username":"testuser","password":"pass"}`,
			store:      &mockAuthStore{userErr: errors.New("db down")},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "session creation error",
			body:       `{"username":"testuser","password":"correctpassword"}`,
			store:      &mockAuthStore{user: user, sessionErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewAuthHandler(tt.store)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			h.Login(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				// Verify session cookie is set.
				cookies := rec.Result().Cookies()
				found := false
				for _, c := range cookies {
					if c.Name == "tl_session" && c.Value != "" {
						found = true
					}
				}
				if !found {
					t.Error("expected tl_session cookie to be set")
				}

				// Verify response body contains user info.
				var resp loginResponse
				if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if resp.User.Username != "testuser" {
					t.Errorf("got username %q, want %q", resp.User.Username, "testuser")
				}
			}
		})
	}
}

func TestLogout(t *testing.T) {
	tests := []struct {
		name       string
		cookie     *http.Cookie
		store      *mockAuthStore
		wantStatus int
	}{
		{
			name:       "success",
			cookie:     &http.Cookie{Name: "tl_session", Value: "some-token"},
			store:      &mockAuthStore{},
			wantStatus: http.StatusOK,
		},
		{
			name:       "no cookie",
			store:      &mockAuthStore{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "delete session error still clears cookie",
			cookie:     &http.Cookie{Name: "tl_session", Value: "some-token"},
			store:      &mockAuthStore{sessionDel: errors.New("db error")},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewAuthHandler(tt.store)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			rec := httptest.NewRecorder()

			h.Logout(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				// Verify cookie is cleared.
				cookies := rec.Result().Cookies()
				for _, c := range cookies {
					if c.Name == "tl_session" && c.MaxAge != -1 {
						t.Error("expected tl_session cookie MaxAge to be -1")
					}
				}
			}
		})
	}
}

func TestMe(t *testing.T) {
	tests := []struct {
		name       string
		user       *model.User
		wantStatus int
	}{
		{
			name:       "authenticated",
			user:       &model.User{ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, Username: "me", IsActive: true, CreatedAt: time.Now()},
			wantStatus: http.StatusOK,
		},
		{
			name:       "not authenticated",
			user:       nil,
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewAuthHandler(&mockAuthStore{})
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
			if tt.user != nil {
				req = req.WithContext(auth.WithUser(req.Context(), tt.user))
			}
			rec := httptest.NewRecorder()

			h.Me(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestCreateKey(t *testing.T) {
	user := &model.User{ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, Username: "testuser", IsActive: true}

	tests := []struct {
		name       string
		user       *model.User
		body       string
		store      *mockAuthStore
		wantStatus int
	}{
		{
			name:       "success",
			user:       user,
			body:       `{"name":"my-key","scopes":["read"]}`,
			store:      &mockAuthStore{},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "not authenticated",
			user:       nil,
			body:       `{"name":"my-key","scopes":["read"]}`,
			store:      &mockAuthStore{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid json",
			user:       user,
			body:       `{bad}`,
			store:      &mockAuthStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty name",
			user:       user,
			body:       `{"name":"","scopes":["read"]}`,
			store:      &mockAuthStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing scopes",
			user:       user,
			body:       `{"name":"my-key"}`,
			store:      &mockAuthStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid scope",
			user:       user,
			body:       `{"name":"my-key","scopes":["bogus"]}`,
			store:      &mockAuthStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "store error",
			user:       user,
			body:       `{"name":"my-key","scopes":["admin"]}`,
			store:      &mockAuthStore{createErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "with expires_at",
			user:       user,
			body:       `{"name":"my-key","scopes":["ingest"],"expires_at":"2030-01-01T00:00:00Z"}`,
			store:      &mockAuthStore{},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "invalid expires_at",
			user:       user,
			body:       `{"name":"my-key","scopes":["read"],"expires_at":"not-a-date"}`,
			store:      &mockAuthStore{},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewAuthHandler(tt.store)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/keys", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			if tt.user != nil {
				req = req.WithContext(auth.WithUser(req.Context(), tt.user))
			}
			rec := httptest.NewRecorder()

			h.CreateKey(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestListKeys(t *testing.T) {
	user := &model.User{ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, Username: "testuser"}

	tests := []struct {
		name       string
		user       *model.User
		store      *mockAuthStore
		wantStatus int
	}{
		{
			name:       "success",
			user:       user,
			store:      &mockAuthStore{keys: []model.APIKeyRow{}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "not authenticated",
			user:       nil,
			store:      &mockAuthStore{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "store error",
			user:       user,
			store:      &mockAuthStore{keysErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewAuthHandler(tt.store)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/keys", nil)
			if tt.user != nil {
				req = req.WithContext(auth.WithUser(req.Context(), tt.user))
			}
			rec := httptest.NewRecorder()

			h.ListKeys(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestRevokeKey(t *testing.T) {
	user := &model.User{ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, Username: "testuser"}
	ownedKey := model.APIKeyRow{
		ID:     pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		UserID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, // matches user
	}

	tests := []struct {
		name       string
		user       *model.User
		keyID      string
		store      *mockAuthStore
		wantStatus int
	}{
		{
			name:       "success",
			user:       user,
			keyID:      "00000001-0000-0000-0000-000000000000",
			store:      &mockAuthStore{keyByID: ownedKey},
			wantStatus: http.StatusOK,
		},
		{
			name:       "not authenticated",
			user:       nil,
			keyID:      "00000001-0000-0000-0000-000000000000",
			store:      &mockAuthStore{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid uuid",
			user:       user,
			keyID:      "not-a-uuid",
			store:      &mockAuthStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "key not found",
			user:       user,
			keyID:      "00000001-0000-0000-0000-000000000000",
			store:      &mockAuthStore{keyByIDErr: pgx.ErrNoRows},
			wantStatus: http.StatusNotFound,
		},
		{
			name:  "other users key forbidden",
			user:  user,
			keyID: "00000001-0000-0000-0000-000000000000",
			store: &mockAuthStore{keyByID: model.APIKeyRow{
				ID:     pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
				UserID: pgtype.UUID{Bytes: [16]byte{99}, Valid: true}, // different user
			}},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "store error",
			user:       user,
			keyID:      "00000001-0000-0000-0000-000000000000",
			store:      &mockAuthStore{keyByID: ownedKey, revokeErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewAuthHandler(tt.store)

			// Use chi router to inject URL params.
			r := chi.NewRouter()
			r.Delete("/keys/{id}", h.RevokeKey)

			req := httptest.NewRequest(http.MethodDelete, "/keys/"+tt.keyID, nil)
			if tt.user != nil {
				req = req.WithContext(auth.WithUser(req.Context(), tt.user))
			}
			rec := httptest.NewRecorder()

			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestFormatUUID(t *testing.T) {
	id := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	got := formatUUID(id)
	want := "01020304-0506-0708-090a-0b0c0d0e0f10"
	if got != want {
		t.Errorf("formatUUID() = %q, want %q", got, want)
	}
}

func TestParseUUID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid with hyphens", input: "01020304-0506-0708-090a-0b0c0d0e0f10"},
		{name: "valid without hyphens", input: "0102030405060708090a0b0c0d0e0f10"},
		{name: "too short", input: "0102", wantErr: true},
		{name: "invalid hex", input: "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseUUID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseUUID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestGravatarURL(t *testing.T) {
	tests := []struct {
		name  string
		email *string
	}{
		{name: "nil email", email: nil},
		{name: "valid email", email: strPtr("user@example.com")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gravatarURL(tt.email)
			if got == "" {
				t.Error("expected non-empty gravatar URL")
			}
			if !bytes.Contains([]byte(got), []byte("gravatar.com/avatar/")) {
				t.Errorf("expected gravatar URL, got %q", got)
			}
		})
	}
}

func TestStripPort(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{name: "with port", addr: "192.168.1.1:8080", want: "192.168.1.1"},
		{name: "bare ip", addr: "192.168.1.1", want: "192.168.1.1"},
		{name: "ipv6 with port", addr: "[::1]:8080", want: "::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripPort(tt.addr)
			if got != tt.want {
				t.Errorf("stripPort(%q) = %q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}

func TestIsSecureRequest(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   bool
	}{
		{name: "http", want: false},
		{name: "x-forwarded-proto https", header: "https", want: true},
		{name: "x-forwarded-proto http", header: "http", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("X-Forwarded-Proto", tt.header)
			}
			got := isSecureRequest(req)
			if got != tt.want {
				t.Errorf("isSecureRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func strPtr(s string) *string {
	return &s
}
