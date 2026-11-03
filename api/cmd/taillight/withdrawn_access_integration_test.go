package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/taillight/internal/auth"
	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/config"
	ldapauth "github.com/lasseh/taillight/internal/ldap"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/mute"
	"github.com/lasseh/taillight/internal/postgres"
)

const (
	muteRuleBody    = `{"feed":"netlog","hostname":"edge-1","reason":"maintenance"}`
	mintAdminKeyReq = `{"name":"child","scopes":["admin"]}`
)

// fakeDirectory is an LDAP directory whose answer a test changes between
// logins. err, when set, is returned instead of a result.
type fakeDirectory struct {
	isAdmin bool
	err     error
}

func (d *fakeDirectory) Authenticate(_ context.Context, username, _ string) (*ldapauth.Result, error) {
	if d.err != nil {
		return nil, d.err
	}
	return &ldapauth.Result{Username: username, IsAdmin: d.isAdmin}, nil
}

// authTestRouter builds the production router over the test database with
// dir as the LDAP directory, after clearing the auth and mute tables.
func authTestRouter(t *testing.T, dir ldapauth.Authenticator) (http.Handler, *pgxpool.Pool, *postgres.AuthStore) {
	t.Helper()
	pool := applogTestPool(t)
	if _, err := pool.Exec(context.Background(), "TRUNCATE sessions, api_keys, users, mute_rules CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	logger := slog.New(slog.DiscardHandler)
	store := postgres.NewStore(pool)
	authStore := postgres.NewAuthStore(pool)
	t.Cleanup(authStore.Stop)
	router := setupRouter(config.Config{AuthEnabled: true, AuthReadEndpoints: true}, logger, store, authStore, dir, nil,
		broker.NewSrvlogBroker(logger), broker.NewNetlogBroker(logger), broker.NewAppLogBroker(logger),
		nil, nil, nil, nil, mute.NewSet(store, time.Now, logger))
	return router, pool, authStore
}

// call sends one request. key, when set, goes as a Bearer API key and
// session, when set, as the session cookie.
func call(t *testing.T, router http.Handler, method, path, body, key string, session *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if session != nil {
		req.AddCookie(session)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// passwordLogin posts a login for username and returns the response.
func passwordLogin(t *testing.T, router http.Handler, username string) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"username":"`+username+`","password":"directory-password"}`, "", nil)
}

// sessionFrom returns the session cookie a successful login set.
func sessionFrom(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "tl_session" && c.Value != "" {
			return c
		}
	}
	t.Fatal("login set no tl_session cookie")
	return nil
}

// mintKey creates an API key with the given scopes through the API and
// returns the raw key.
func mintKey(t *testing.T, router http.Handler, session *http.Cookie, scopes string) string {
	t.Helper()
	rec := call(t, router, http.MethodPost, "/api/v1/auth/keys", `{"name":"k","scopes":`+scopes+`}`, "", session)
	if rec.Code != http.StatusCreated {
		t.Fatalf("mint key = %d, want 201; body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode key: %v", err)
	}
	return resp.Key
}

// storeKey creates a user and an API key for it directly in the store, the
// way `taillight apikey` does, and returns the user and the raw key.
func storeKey(t *testing.T, authStore *postgres.AuthStore, username string, isAdmin bool, scopes ...string) (model.User, string) {
	t.Helper()
	ctx := context.Background()
	u, err := authStore.CreateUser(ctx, username, "unused-hash", isAdmin)
	if err != nil {
		t.Fatalf("CreateUser %s: %v", username, err)
	}
	key, hash, prefix, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if _, err := authStore.CreateAPIKey(ctx, u.ID.Bytes, username+"-key", hash, prefix, scopes, nil); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	return u, key
}

// TestIntegration_AdminScopeFollowsOwnerAdminFlag verifies on the production
// router that an admin-scope API key grants admin writes only while its owner
// is an admin, and that a demotion at login revokes the owner's admin keys.
func TestIntegration_AdminScopeFollowsOwnerAdminFlag(t *testing.T) {
	dir := &fakeDirectory{isAdmin: true}
	router, pool, authStore := authTestRouter(t, dir)

	_, adminKey := storeKey(t, authStore, "root", true, "admin")
	_, neverAdminKey := storeKey(t, authStore, "viewer", false, "admin")
	former, formerKey := storeKey(t, authStore, "former", true, "admin")
	// A demotion recorded before admin keys were revoked on demotion leaves
	// the key unrevoked.
	if _, err := pool.Exec(context.Background(), `UPDATE users SET is_admin = false WHERE id = $1`, former.ID); err != nil {
		t.Fatalf("demote: %v", err)
	}

	tests := []struct {
		name string
		key  string
		want int
	}{
		{"admin-owned admin key", adminKey, http.StatusCreated},
		{"admin key of a user who was never an admin", neverAdminKey, http.StatusForbidden},
		{"admin key of a demoted user", formerKey, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := call(t, router, http.MethodPost, "/api/v1/mute-rules", muteRuleBody, tt.key, nil); rec.Code != tt.want {
				t.Errorf("POST /mute-rules = %d, want %d; body %s", rec.Code, tt.want, rec.Body.String())
			}
			if rec := call(t, router, http.MethodPost, "/api/v1/auth/keys", mintAdminKeyReq, tt.key, nil); rec.Code != tt.want {
				t.Errorf("mint admin key = %d, want %d; body %s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}

	t.Run("demotion at login revokes admin keys", func(t *testing.T) {
		dir.isAdmin = true
		session := sessionFrom(t, passwordLogin(t, router, "dana"))
		key := mintKey(t, router, session, `["admin"]`)
		if rec := call(t, router, http.MethodPost, "/api/v1/mute-rules", muteRuleBody, key, nil); rec.Code != http.StatusCreated {
			t.Fatalf("admin key before demotion: POST /mute-rules = %d, want 201", rec.Code)
		}

		dir.isAdmin = false
		demoted := sessionFrom(t, passwordLogin(t, router, "dana"))
		if rec := call(t, router, http.MethodGet, "/api/v1/auth/me", "", "", demoted); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"is_admin":false`) {
			t.Fatalf("demoted session: GET /auth/me = %d %s, want 200 with is_admin false", rec.Code, rec.Body.String())
		}
		if rec := call(t, router, http.MethodPost, "/api/v1/mute-rules", muteRuleBody, key, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("admin key after demotion: POST /mute-rules = %d, want 401 (revoked)", rec.Code)
		}
		if rec := call(t, router, http.MethodPost, "/api/v1/mute-rules", muteRuleBody, "", demoted); rec.Code != http.StatusForbidden {
			t.Errorf("demoted session: POST /mute-rules = %d, want 403", rec.Code)
		}
	})
}

// TestIntegration_DirectoryRefusalEndsSession verifies on the production
// router that an LDAP login gets a 12-hour cookie, that a login the directory
// refuses after the password bind ends the user's live session and admin
// key, and that the denials which need no password leave the session valid.
func TestIntegration_DirectoryRefusalEndsSession(t *testing.T) {
	dir := &fakeDirectory{isAdmin: true}
	router, _, _ := authTestRouter(t, dir)
	const username = "erin"

	session := sessionFrom(t, passwordLogin(t, router, username))
	if want := int((12 * time.Hour).Seconds()); session.MaxAge != want {
		t.Errorf("LDAP session cookie MaxAge = %d, want %d", session.MaxAge, want)
	}
	key := mintKey(t, router, session, `["admin"]`)
	me := func() int {
		return call(t, router, http.MethodGet, "/api/v1/auth/me", "", "", session).Code
	}

	for _, err := range []error{ldapauth.ErrInvalidPassword, ldapauth.ErrUserNotFound} {
		dir.err = err
		if rec := passwordLogin(t, router, username); rec.Code != http.StatusUnauthorized {
			t.Fatalf("login with directory answer %q = %d, want 401", err, rec.Code)
		}
		if code := me(); code != http.StatusOK {
			t.Fatalf("after directory answer %q: GET /auth/me with the old session = %d, want 200", err, code)
		}
	}

	dir.err = ldapauth.ErrNotAuthorized
	if rec := passwordLogin(t, router, username); rec.Code != http.StatusUnauthorized {
		t.Fatalf("login with directory answer %q = %d, want 401", dir.err, rec.Code)
	}
	if code := me(); code != http.StatusUnauthorized {
		t.Errorf("after directory refusal: GET /auth/me with the old session = %d, want 401", code)
	}
	if rec := call(t, router, http.MethodPost, "/api/v1/mute-rules", muteRuleBody, key, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("after directory refusal: admin key POST /mute-rules = %d, want 401 (revoked)", rec.Code)
	}
}
