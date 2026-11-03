package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/auth"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/postgres"
)

// storeSession creates a user and a session for it directly in the store and
// returns the user and the session cookie. It does not log in, so it leaves
// the per-IP login rate limit, shared by every test in the package, unspent.
func storeSession(t *testing.T, authStore *postgres.AuthStore, username string, isAdmin bool) (model.User, *http.Cookie) {
	t.Helper()
	ctx := context.Background()
	u, err := authStore.CreateUser(ctx, username, "unused-hash", isAdmin)
	if err != nil {
		t.Fatalf("CreateUser %s: %v", username, err)
	}
	raw, hash, err := auth.GenerateSessionToken()
	if err != nil {
		t.Fatalf("GenerateSessionToken: %v", err)
	}
	if err := authStore.CreateSession(ctx, hash, u.ID.Bytes, time.Now().Add(time.Hour), "192.0.2.1", "test"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return u, &http.Cookie{Name: "tl_session", Value: raw}
}

// newKey mints an API key with the given scopes through the API and returns
// the raw key and its id.
func newKey(t *testing.T, router http.Handler, session *http.Cookie, scopes string) (key, id string) {
	t.Helper()
	rec := call(t, router, http.MethodPost, "/api/v1/auth/keys", `{"name":"k","scopes":`+scopes+`}`, "", session)
	if rec.Code != http.StatusCreated {
		t.Fatalf("mint key = %d, want 201; body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Key     string `json:"key"`
		KeyInfo struct {
			ID string `json:"id"`
		} `json:"key_info"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode key: %v", err)
	}
	return resp.Key, resp.KeyInfo.ID
}

// meStatus returns the status of GET /auth/me with key, which shows whether
// the key still authenticates.
func meStatus(t *testing.T, router http.Handler, key string) int {
	t.Helper()
	return call(t, router, http.MethodGet, "/api/v1/auth/me", "", key, nil).Code
}

// TestIntegration_KeysCannotMintKeys verifies on the production router that
// only a session mints API keys: every key is refused, an admin key included.
func TestIntegration_KeysCannotMintKeys(t *testing.T) {
	router, pool, authStore := authTestRouter(t, nil)
	_, adminSession := storeSession(t, authStore, "alice", true)
	_, userSession := storeSession(t, authStore, "bob", false)
	ingestKey, _ := newKey(t, router, adminSession, `["ingest"]`)
	readKey, _ := newKey(t, router, adminSession, `["read"]`)
	adminKey, _ := newKey(t, router, adminSession, `["admin"]`)
	newKey(t, router, userSession, `["read"]`) // a non-admin session mints too

	tests := []struct {
		name, key, scopes string
	}{
		{"ingest key", ingestKey, `["ingest"]`},
		{"read key", readKey, `["read"]`},
		{"admin key", adminKey, `["admin"]`},
		{"admin key asking for a lower scope", adminKey, `["ingest"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"name":"child","scopes":` + tt.scopes + `}`
			if rec := call(t, router, http.MethodPost, "/api/v1/auth/keys", body, tt.key, nil); rec.Code != http.StatusForbidden {
				t.Errorf("POST /auth/keys = %d, want 403; body %s", rec.Code, rec.Body.String())
			}
		})
	}

	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM api_keys`).Scan(&n); err != nil {
		t.Fatalf("count keys: %v", err)
	}
	if n != 4 {
		t.Errorf("api_keys holds %d rows, want the 4 minted by sessions", n)
	}
}

// TestIntegration_ReadAndIngestKeysCannotChangeTheAccount verifies on the
// production router that read and ingest keys are refused on the account
// writes and keep their reads, while a session and an admin key still write.
func TestIntegration_ReadAndIngestKeysCannotChangeTheAccount(t *testing.T) {
	router, _, authStore := authTestRouter(t, nil)
	_, adminSession := storeSession(t, authStore, "alice", true)
	_, session := storeSession(t, authStore, "bob", false)
	adminKey, _ := newKey(t, router, adminSession, `["admin"]`)
	ingestKey, _ := newKey(t, router, session, `["ingest"]`)
	readKey, _ := newKey(t, router, session, `["read"]`)
	sibling, siblingID := newKey(t, router, session, `["ingest"]`)

	writes := func(scopes string) []struct{ method, path, body string } {
		return []struct{ method, path, body string }{
			{http.MethodPatch, "/api/v1/auth/me/email", `{"email":"changed@example.invalid"}`},
			{http.MethodPatch, "/api/v1/auth/me/preferences", `{"preferences":{"theme":"changed"}}`},
			{http.MethodPost, "/api/v1/auth/keys", `{"name":"child","scopes":` + scopes + `}`},
			{http.MethodDelete, "/api/v1/auth/keys/" + siblingID, ""},
			{http.MethodPost, "/api/v1/auth/sessions/revoke-all", ""},
		}
	}

	for _, k := range []struct{ name, key, scopes string }{
		{"ingest key", ingestKey, `["ingest"]`},
		{"read key", readKey, `["read"]`},
	} {
		t.Run(k.name, func(t *testing.T) {
			for _, w := range writes(k.scopes) {
				if rec := call(t, router, w.method, w.path, w.body, k.key, nil); rec.Code != http.StatusForbidden {
					t.Errorf("%s %s = %d, want 403; body %s", w.method, w.path, rec.Code, rec.Body.String())
				}
			}
			for _, path := range []string{"/api/v1/auth/me", "/api/v1/auth/keys"} {
				if rec := call(t, router, http.MethodGet, path, "", k.key, nil); rec.Code != http.StatusOK {
					t.Errorf("GET %s = %d, want 200", path, rec.Code)
				}
			}
		})
	}

	// The refused writes changed nothing.
	me := call(t, router, http.MethodGet, "/api/v1/auth/me", "", "", session)
	if me.Code != http.StatusOK {
		t.Fatalf("owner's session after refused writes: GET /auth/me = %d, want 200", me.Code)
	}
	if strings.Contains(me.Body.String(), "changed") {
		t.Errorf("refused writes changed the account: %s", me.Body.String())
	}
	if code := meStatus(t, router, sibling); code != http.StatusOK {
		t.Errorf("sibling key after refused revoke: GET /auth/me = %d, want 200", code)
	}

	t.Run("admin key writes", func(t *testing.T) {
		_, ownID := newKey(t, router, adminSession, `["read"]`)
		for _, w := range []struct{ method, path, body string }{
			{http.MethodPatch, "/api/v1/auth/me/email", `{"email":"alice@example.invalid"}`},
			{http.MethodPatch, "/api/v1/auth/me/preferences", `{"preferences":{"theme":"dark"}}`},
			{http.MethodDelete, "/api/v1/auth/keys/" + ownID, ""},
			{http.MethodPost, "/api/v1/auth/sessions/revoke-all", ""},
		} {
			if rec := call(t, router, w.method, w.path, w.body, adminKey, nil); rec.Code != http.StatusOK {
				t.Errorf("%s %s = %d, want 200; body %s", w.method, w.path, rec.Code, rec.Body.String())
			}
		}
		if rec := call(t, router, http.MethodGet, "/api/v1/auth/me", "", "", adminSession); rec.Code != http.StatusUnauthorized {
			t.Errorf("admin session after revoke-all by admin key: GET /auth/me = %d, want 401", rec.Code)
		}
	})

	t.Run("session writes", func(t *testing.T) {
		for _, w := range []struct{ method, path, body string }{
			{http.MethodPatch, "/api/v1/auth/me/email", `{"email":"bob@example.invalid"}`},
			{http.MethodPatch, "/api/v1/auth/me/preferences", `{"preferences":{"theme":"dark"}}`},
			{http.MethodDelete, "/api/v1/auth/keys/" + siblingID, ""},
			{http.MethodPost, "/api/v1/auth/sessions/revoke-all", ""},
		} {
			if rec := call(t, router, w.method, w.path, w.body, "", session); rec.Code != http.StatusOK {
				t.Errorf("%s %s = %d, want 200; body %s", w.method, w.path, rec.Code, rec.Body.String())
			}
		}
		if code := meStatus(t, router, sibling); code != http.StatusUnauthorized {
			t.Errorf("sibling key after revoke by session: GET /auth/me = %d, want 401", code)
		}
		if rec := call(t, router, http.MethodGet, "/api/v1/auth/me", "", "", session); rec.Code != http.StatusUnauthorized {
			t.Errorf("session after revoke-all: GET /auth/me = %d, want 401", rec.Code)
		}
	})
}

// TestIntegration_RevokingAnotherUsersKeyNeedsAdminGrant verifies on the
// production router that revoking another user's key needs an admin session
// or an admin-scope key of an admin, and that an owner revokes their own key.
func TestIntegration_RevokingAnotherUsersKeyNeedsAdminGrant(t *testing.T) {
	router, _, authStore := authTestRouter(t, nil)
	alice, adminSession := storeSession(t, authStore, "alice", true)
	_, ownerSession := storeSession(t, authStore, "bob", false)
	ingestKey, _ := newKey(t, router, adminSession, `["ingest"]`)
	readKey, _ := newKey(t, router, adminSession, `["read"]`)
	adminKey, _ := newKey(t, router, adminSession, `["admin"]`)
	// The API refuses a key without scopes, so this one goes in through the store.
	emptyKey, hash, prefix, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if _, err := authStore.CreateAPIKey(context.Background(), alice.ID.Bytes, "no-scopes", hash, prefix, []string{}, nil); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	_, nonAdminAdminKey := storeKey(t, authStore, "carol", false, "admin")

	tests := []struct {
		name    string
		key     string
		session *http.Cookie
		want    int
	}{
		{"admin-owned ingest key", ingestKey, nil, http.StatusForbidden},
		{"admin-owned read key", readKey, nil, http.StatusForbidden},
		{"admin-owned key with no scopes", emptyKey, nil, http.StatusForbidden},
		{"admin-scope key of a non-admin owner", nonAdminAdminKey, nil, http.StatusForbidden},
		{"admin session", "", adminSession, http.StatusOK},
		{"admin-owned admin key", adminKey, nil, http.StatusOK},
		{"owner's session on the owner's key", "", ownerSession, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, targetID := newKey(t, router, ownerSession, `["ingest"]`)
			rec := call(t, router, http.MethodDelete, "/api/v1/auth/keys/"+targetID, "", tt.key, tt.session)
			if rec.Code != tt.want {
				t.Fatalf("DELETE /auth/keys/{id} = %d, want %d; body %s", rec.Code, tt.want, rec.Body.String())
			}
			wantTarget := http.StatusOK
			if tt.want == http.StatusOK {
				wantTarget = http.StatusUnauthorized
			}
			if code := meStatus(t, router, target); code != wantTarget {
				t.Errorf("target key afterwards: GET /auth/me = %d, want %d", code, wantTarget)
			}
		})
	}
}
