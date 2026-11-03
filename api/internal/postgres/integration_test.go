package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/ingestbridge"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/mute"
)

// These tests exercise real query execution, row scanning, keyset pagination,
// and batch RETURNING ordering — the data-layer behaviour that pure SQL-builder
// unit tests cannot reach. They are gated on TEST_DATABASE_URL and SKIPPED by
// default, so `make test` needs no database. Run `make test-integration`, which
// stands up an ephemeral TimescaleDB and points TEST_DATABASE_URL at it.

// migrations memoises the one-time schema migration across integration tests.
var migrations struct {
	once sync.Once
	err  error
}

// testPool connects to TEST_DATABASE_URL, applies migrations once, and returns a
// pool. It skips when the env var is unset, and — as a safety net against
// pointing it at a real database — also skips (loudly) unless the target
// database name contains "test", since the tests TRUNCATE tables.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; run `make test-integration` to exercise DB integration tests")
	}
	if name := dbName(dsn); !strings.Contains(name, "test") {
		t.Skipf("refusing to run destructive integration tests against database %q (name must contain \"test\")", name)
	}

	migrations.once.Do(func() {
		m, err := migrate.New("file://../../migrations", dsn)
		if err != nil {
			migrations.err = fmt.Errorf("migrate new: %w", err)
			return
		}
		defer func() { _, _ = m.Close() }() //nolint:errcheck // best-effort close after migrate
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			migrations.err = fmt.Errorf("migrate up: %w", err)
		}
	})
	if migrations.err != nil {
		t.Fatalf("apply migrations: %v", migrations.err)
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func dbName(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}

func truncate(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	for _, tbl := range tables {
		if _, err := pool.Exec(context.Background(), "TRUNCATE "+tbl+" CASCADE"); err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}
}

// TestIntegration_SrvlogCursorPagination verifies keyset pagination has no
// gaps, duplicates, or ordering errors across page boundaries — the
// events[:limit] slice and nextCursor-from-events[limit] logic in ListSrvlogs.
func TestIntegration_SrvlogCursorPagination(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "srvlog_events")
	store := NewStore(pool)
	ctx := context.Background()

	const total = 7
	base := time.Now().Add(-time.Hour).UTC()
	for i := range total {
		if _, err := pool.Exec(ctx,
			`INSERT INTO srvlog_events (received_at, reported_at, hostname, fromhost_ip, severity, facility, message)
			 VALUES ($1, $1, 'host1', '10.0.0.1'::inet, $2, $3, $4)`,
			base.Add(time.Duration(i)*time.Second), 6, 1, fmt.Sprintf("msg-%d", i)); err != nil {
			t.Fatalf("insert event %d: %v", i, err)
		}
	}

	var collected []model.SrvlogEvent
	seen := make(map[int64]bool)
	var cursor *model.Cursor
	for page := 0; page < total+2; page++ {
		events, next, err := store.ListSrvlogs(ctx, model.SrvlogFilter{}, cursor, 3)
		if err != nil {
			t.Fatalf("ListSrvlogs page %d: %v", page, err)
		}
		for _, e := range events {
			if seen[e.ID] {
				t.Fatalf("duplicate event id %d across pages", e.ID)
			}
			seen[e.ID] = true
			collected = append(collected, e)
		}
		if next == nil {
			break
		}
		cursor = next
	}

	if len(collected) != total {
		t.Fatalf("collected %d events across pages, want %d", len(collected), total)
	}
	// Strict DESC ordering by (received_at, id) across the whole sequence.
	for i := 1; i < len(collected); i++ {
		prev, cur := collected[i-1], collected[i]
		if cur.ReceivedAt.After(prev.ReceivedAt) ||
			(cur.ReceivedAt.Equal(prev.ReceivedAt) && cur.ID > prev.ID) {
			t.Errorf("events out of DESC order at index %d", i)
		}
	}
}

// TestIntegration_AppLogCursorPagination guards the same keyset-pagination
// boundary on the applog path (the bug lived in all three stores).
func TestIntegration_AppLogCursorPagination(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "applog_events")
	store := NewStore(pool)
	ctx := context.Background()

	const total = 7
	ts := time.Now().UTC()
	batch := make([]model.AppLogEvent, total)
	for i := range batch {
		batch[i] = model.AppLogEvent{Timestamp: ts, Level: "INFO", Service: "svc", Host: "h", Msg: fmt.Sprintf("m-%d", i)}
	}
	if _, err := store.InsertLogBatch(ctx, batch); err != nil {
		t.Fatalf("InsertLogBatch: %v", err)
	}

	seen := make(map[int64]bool)
	var cursor *model.Cursor
	for page := 0; page < total+2; page++ {
		events, next, err := store.ListAppLogs(ctx, model.AppLogFilter{}, cursor, 3)
		if err != nil {
			t.Fatalf("ListAppLogs page %d: %v", page, err)
		}
		for _, e := range events {
			if seen[e.ID] {
				t.Fatalf("duplicate applog id %d across pages", e.ID)
			}
			seen[e.ID] = true
		}
		if next == nil {
			break
		}
		cursor = next
	}
	if len(seen) != total {
		t.Fatalf("collected %d applog events across pages, want %d", len(seen), total)
	}
}

// TestIntegration_AppLogBatchInsertOrder verifies InsertLogBatch consumes its
// RETURNING rows in input order, so the returned IDs map to the right events.
func TestIntegration_AppLogBatchInsertOrder(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "applog_events")
	store := NewStore(pool)
	ctx := context.Background()

	ts := time.Now().UTC()
	events := []model.AppLogEvent{
		{Timestamp: ts, Level: "INFO", Service: "svc", Host: "h", Msg: "first"},
		{Timestamp: ts, Level: "WARN", Service: "svc", Host: "h", Msg: "second"},
		{Timestamp: ts, Level: "ERROR", Service: "svc", Host: "h", Msg: "third"},
	}

	inserted, err := store.InsertLogBatch(ctx, events)
	if err != nil {
		t.Fatalf("InsertLogBatch: %v", err)
	}
	if len(inserted) != len(events) {
		t.Fatalf("inserted %d rows, want %d", len(inserted), len(events))
	}
	for i := range events {
		if inserted[i].Msg != events[i].Msg {
			t.Errorf("inserted[%d].Msg = %q, want %q (RETURNING order mismatch)", i, inserted[i].Msg, events[i].Msg)
		}
		if inserted[i].ID == 0 {
			t.Errorf("inserted[%d] has zero ID", i)
		}
		if i > 0 && inserted[i].ID <= inserted[i-1].ID {
			t.Errorf("IDs not strictly increasing: [%d]=%d after [%d]=%d", i, inserted[i].ID, i-1, inserted[i-1].ID)
		}
	}
}

// TestIntegration_GetAPIKeyByHash verifies the API-key/user join maps every
// field correctly — the lookup the API-key middleware relies on.
func TestIntegration_GetAPIKeyByHash(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "api_keys", "users")
	authStore := NewAuthStore(pool)
	defer authStore.Stop()
	ctx := context.Background()

	user, err := authStore.CreateUser(ctx, "alice", "password-hash", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	const keyHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if _, err := authStore.CreateAPIKey(ctx, user.ID.Bytes, "ci-key", keyHash, "tl_abc1234", []string{"ingest"}, nil); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	kw, err := authStore.GetAPIKeyByHash(ctx, keyHash)
	if err != nil {
		t.Fatalf("GetAPIKeyByHash: %v", err)
	}
	if kw.User.Username != "alice" {
		t.Errorf("user.Username = %q, want alice", kw.User.Username)
	}
	if !kw.User.IsAdmin {
		t.Error("user.IsAdmin = false, want true")
	}
	if kw.Key.Name != "ci-key" {
		t.Errorf("key.Name = %q, want ci-key", kw.Key.Name)
	}
	if len(kw.Key.Scopes) != 1 || kw.Key.Scopes[0] != "ingest" {
		t.Errorf("key.Scopes = %v, want [ingest]", kw.Key.Scopes)
	}
}

// TestIntegration_UpsertOIDCUser proves the (issuer, subject) upsert semantics
// against real constraints: provision-on-first-login, profile refresh on
// return (email/is_admin, username stable), is_active left under local admin
// control, numeric-suffix fallback when the preferred username is held by an
// existing account, and issuer-scoped subject uniqueness.
func TestIntegration_UpsertOIDCUser(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "sessions", "api_keys", "users")
	authStore := NewAuthStore(pool)
	defer authStore.Stop()
	ctx := context.Background()

	const issuer = "https://login.example.com/realms/ops"

	// First login provisions.
	u, err := authStore.UpsertOIDCUser(ctx, issuer, "sub-1", "amelie", "amelie@example.com", false)
	if err != nil {
		t.Fatalf("UpsertOIDCUser (provision): %v", err)
	}
	if u.Username != "amelie" || u.AuthSource != "oidc" || u.PasswordHash != "" {
		t.Errorf("provisioned user = %q/%q/pw %q, want amelie/oidc/empty", u.Username, u.AuthSource, u.PasswordHash)
	}
	if u.Email == nil || *u.Email != "amelie@example.com" {
		t.Errorf("provisioned email = %v, want amelie@example.com", u.Email)
	}
	if u.IsAdmin || !u.IsActive {
		t.Errorf("provisioned flags admin=%v active=%v, want false/true", u.IsAdmin, u.IsActive)
	}

	// Returning login refreshes email and is_admin, keeps identity and username.
	u2, err := authStore.UpsertOIDCUser(ctx, issuer, "sub-1", "amelie-renamed", "new@example.com", true)
	if err != nil {
		t.Fatalf("UpsertOIDCUser (refresh): %v", err)
	}
	if u2.ID.Bytes != u.ID.Bytes {
		t.Error("returning login created a new user, want same row")
	}
	if u2.Username != "amelie" {
		t.Errorf("username changed to %q on refresh, want stable amelie", u2.Username)
	}
	if u2.Email == nil || *u2.Email != "new@example.com" {
		t.Errorf("refreshed email = %v, want new@example.com", u2.Email)
	}
	if !u2.IsAdmin {
		t.Error("is_admin not refreshed to true")
	}

	// Local deactivation survives a refreshing login (is_active untouched).
	if err := authStore.SetUserActive(ctx, u.ID.Bytes, false); err != nil {
		t.Fatalf("SetUserActive: %v", err)
	}
	u3, err := authStore.UpsertOIDCUser(ctx, issuer, "sub-1", "amelie", "new@example.com", true)
	if err != nil {
		t.Fatalf("UpsertOIDCUser (after deactivate): %v", err)
	}
	if u3.IsActive {
		t.Error("is_active flipped back to true by upsert, want it under local admin control")
	}

	// Username collision with an existing local account gets a numeric suffix
	// instead of linking or overwriting.
	if _, err := authStore.CreateUser(ctx, "bob", "password-hash", false); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	ub, err := authStore.UpsertOIDCUser(ctx, issuer, "sub-2", "bob", "bob@example.com", false)
	if err != nil {
		t.Fatalf("UpsertOIDCUser (collision): %v", err)
	}
	if ub.Username != "bob-2" {
		t.Errorf("collision username = %q, want bob-2", ub.Username)
	}
	local, err := authStore.GetUserByUsername(ctx, "bob")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if local.AuthSource != "local" || local.PasswordHash != "password-hash" {
		t.Errorf("local account mutated by OIDC collision: source=%q pw=%q", local.AuthSource, local.PasswordHash)
	}

	// Same subject under a different issuer is a distinct identity.
	uo, err := authStore.UpsertOIDCUser(ctx, "https://other-idp.example.com", "sub-1", "carol", "carol@example.com", false)
	if err != nil {
		t.Fatalf("UpsertOIDCUser (other issuer): %v", err)
	}
	if uo.ID.Bytes == u.ID.Bytes {
		t.Error("same subject under different issuer reused the row, want distinct user")
	}
}

// TestIntegration_AdminDemotionRevokesAdminKeys verifies that a login which
// changes is_admin from true to false revokes the user's admin-scope API keys
// and leaves its other keys alone, for LDAP and OIDC users. Only that change
// revokes: a login that keeps the user an admin, or keeps it a non-admin,
// does not, and a later re-promotion does not bring revoked keys back.
func TestIntegration_AdminDemotionRevokesAdminKeys(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "sessions", "api_keys", "users")
	authStore := NewAuthStore(pool)
	defer authStore.Stop()
	ctx := context.Background()

	tests := []struct {
		name   string
		upsert func(isAdmin bool) (model.User, error)
	}{
		{"ldap", func(isAdmin bool) (model.User, error) {
			return authStore.UpsertLDAPUser(ctx, "dana", "", isAdmin)
		}},
		{"oidc", func(isAdmin bool) (model.User, error) {
			return authStore.UpsertOIDCUser(ctx, "https://login.example.com", "sub-erin", "erin", "", isAdmin)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			login := func(isAdmin bool) model.User {
				t.Helper()
				u, err := tt.upsert(isAdmin)
				if err != nil {
					t.Fatalf("upsert (admin=%v): %v", isAdmin, err)
				}
				if u.IsAdmin != isAdmin {
					t.Fatalf("is_admin = %v after upsert, want %v", u.IsAdmin, isAdmin)
				}
				return u
			}
			addKey := func(u model.User, name string, scopes ...string) string {
				t.Helper()
				hash := tt.name + "-" + name
				if _, err := authStore.CreateAPIKey(ctx, u.ID.Bytes, name, hash, "tl_x", scopes, nil); err != nil {
					t.Fatalf("CreateAPIKey %s: %v", name, err)
				}
				return hash
			}
			revoked := func(hash string) bool {
				t.Helper()
				var r bool
				if err := pool.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM api_keys WHERE key_hash = $1`, hash).Scan(&r); err != nil {
					t.Fatalf("read key %s: %v", hash, err)
				}
				return r
			}
			wantRevoked := func(step string, want map[string]bool) {
				t.Helper()
				for hash, w := range want {
					if got := revoked(hash); got != w {
						t.Errorf("%s: key %s revoked = %v, want %v", step, hash, got, w)
					}
				}
			}

			u := login(true)
			admin := addKey(u, "admin", "admin")
			mixed := addKey(u, "mixed", "read", "admin")
			read := addKey(u, "read", "read")

			login(true)
			wantRevoked("admin login again", map[string]bool{admin: false, mixed: false, read: false})

			u = login(false)
			wantRevoked("demoted", map[string]bool{admin: true, mixed: true, read: false})

			// A key added while the user is not an admin is not revoked by a
			// login that leaves it a non-admin; HasGrant refuses it admin.
			dormant := addKey(u, "dormant", "admin")
			login(false)
			wantRevoked("non-admin login again", map[string]bool{dormant: false, read: false})

			login(true)
			wantRevoked("re-promoted", map[string]bool{admin: true, mixed: true, dormant: false, read: false})
		})
	}
}

// TestIntegration_RevokeLDAPUser verifies that revoking an LDAP user clears
// its admin flag, revokes its admin-scope keys and deletes its sessions in one
// call, matches the username case-insensitively, keeps is_active and the
// user's other keys, and leaves rows of other sources alone.
func TestIntegration_RevokeLDAPUser(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "sessions", "api_keys", "users")
	authStore := NewAuthStore(pool)
	defer authStore.Stop()
	ctx := context.Background()
	expires := time.Now().Add(time.Hour)

	dana, err := authStore.UpsertLDAPUser(ctx, "dana", "", true)
	if err != nil {
		t.Fatalf("UpsertLDAPUser: %v", err)
	}
	local, err := authStore.CreateUser(ctx, "frank", "password-hash", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	for _, s := range []struct {
		hash string
		user model.User
	}{{"dana-session", dana}, {"frank-session", local}} {
		if err := authStore.CreateSession(ctx, s.hash, s.user.ID.Bytes, expires, "127.0.0.1", "test"); err != nil {
			t.Fatalf("CreateSession %s: %v", s.hash, err)
		}
	}
	if _, err := authStore.CreateAPIKey(ctx, dana.ID.Bytes, "admin", "dana-admin", "tl_x", []string{"admin"}, nil); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if _, err := authStore.CreateAPIKey(ctx, dana.ID.Bytes, "read", "dana-read", "tl_x", []string{"read"}, nil); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	for _, name := range []string{"DANA", "frank", "nobody"} {
		if err := authStore.RevokeLDAPUser(ctx, name); err != nil {
			t.Fatalf("RevokeLDAPUser %s: %v", name, err)
		}
	}

	got, err := authStore.GetUserByID(ctx, dana.ID.Bytes)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if got.IsAdmin || !got.IsActive {
		t.Errorf("ldap user admin=%v active=%v, want false/true", got.IsAdmin, got.IsActive)
	}
	if _, err := authStore.GetSession(ctx, "dana-session"); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("ldap user session: err = %v, want it deleted", err)
	}
	for hash, want := range map[string]bool{"dana-admin": true, "dana-read": false} {
		var revoked bool
		if err := pool.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM api_keys WHERE key_hash = $1`, hash).Scan(&revoked); err != nil {
			t.Fatalf("read key %s: %v", hash, err)
		}
		if revoked != want {
			t.Errorf("key %s revoked = %v, want %v", hash, revoked, want)
		}
	}

	// A local row with a matching name is not the directory's to revoke.
	if _, err := authStore.GetSession(ctx, "frank-session"); err != nil {
		t.Errorf("local user session: %v, want it kept", err)
	}
	if got, err := authStore.GetUserByID(ctx, local.ID.Bytes); err != nil || !got.IsAdmin {
		t.Errorf("local user admin=%v err=%v, want it kept", got.IsAdmin, err)
	}
}

// TestIntegration_ExternalSessionClampMigration runs migration 26 over
// sessions issued with the old 30-day lifetime: LDAP and OIDC sessions are
// cut to 12 hours from creation, which expires the older ones at once, while
// local sessions and already shorter sessions are left as they are.
func TestIntegration_ExternalSessionClampMigration(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "sessions", "api_keys", "users")
	authStore := NewAuthStore(pool)
	defer authStore.Stop()
	ctx := context.Background()

	ldapUser, err := authStore.UpsertLDAPUser(ctx, "dana", "", false)
	if err != nil {
		t.Fatalf("UpsertLDAPUser: %v", err)
	}
	oidcUser, err := authStore.UpsertOIDCUser(ctx, "https://login.example.com", "sub-1", "erin", "", false)
	if err != nil {
		t.Fatalf("UpsertOIDCUser: %v", err)
	}
	localUser, err := authStore.CreateUser(ctx, "frank", "password-hash", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	const day = 24 * time.Hour
	sessions := []struct {
		hash      string
		user      model.User
		age       time.Duration // created this long ago
		lifetime  time.Duration // expires_at - created_at before the migration
		wantLife  time.Duration // expires_at - created_at after it
		wantValid bool
	}{
		{"ldap-old", ldapUser, 13 * time.Hour, 30 * day, 12 * time.Hour, false},
		{"ldap-recent", ldapUser, time.Hour, 30 * day, 12 * time.Hour, true},
		{"ldap-short", ldapUser, 0, time.Hour, time.Hour, true},
		{"oidc-old", oidcUser, 13 * time.Hour, 30 * day, 12 * time.Hour, false},
		{"oidc-recent", oidcUser, time.Hour, 30 * day, 12 * time.Hour, true},
		{"local-old", localUser, 13 * time.Hour, 30 * day, 30 * day, true},
	}
	now := time.Now()
	for _, s := range sessions {
		created := now.Add(-s.age)
		if _, err := pool.Exec(ctx,
			`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES ($1, $2, $3, $4)`,
			s.hash, s.user.ID, created, created.Add(s.lifetime)); err != nil {
			t.Fatalf("insert session %s: %v", s.hash, err)
		}
	}

	up, err := os.ReadFile("../../migrations/000026_external_session_lifetime.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	for _, s := range sessions {
		var lifeSec int64
		if err := pool.QueryRow(ctx,
			`SELECT EXTRACT(EPOCH FROM expires_at - created_at)::bigint FROM sessions WHERE token_hash = $1`,
			s.hash).Scan(&lifeSec); err != nil {
			t.Fatalf("read session %s: %v", s.hash, err)
		}
		if got := time.Duration(lifeSec) * time.Second; got != s.wantLife {
			t.Errorf("%s lifetime = %v, want %v", s.hash, got, s.wantLife)
		}
		_, err := authStore.GetSession(ctx, s.hash)
		if valid := err == nil; valid != s.wantValid {
			t.Errorf("%s valid = %v (err %v), want %v", s.hash, valid, err, s.wantValid)
		}
	}
}

// TestIntegration_AnalysisScheduleNotifyChannels verifies the notify_channel_ids
// BIGINT[] column round-trips through pgx as []int64 on both the schedule and
// the report — the encode/scan path that compiles but can only be proven
// against a real Postgres. It also confirms an empty list persists as '{}'
// (not NULL) and reads back as an empty slice.
func TestIntegration_AnalysisScheduleNotifyChannels(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "analysis_schedules", "analysis_reports")
	store := NewStore(pool)
	ctx := context.Background()

	created, err := store.CreateAnalysisSchedule(ctx, model.AnalysisSchedule{
		Name:             "nightly-srvlog",
		Enabled:          true,
		Feed:             "srvlog",
		Frequency:        "daily",
		TimeOfDay:        "03:00",
		Timezone:         "Europe/Oslo",
		NotifyChannelIDs: []int64{7, 42},
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	if got := created.NotifyChannelIDs; len(got) != 2 || got[0] != 7 || got[1] != 42 {
		t.Fatalf("create returned NotifyChannelIDs = %v, want [7 42]", got)
	}

	got, err := store.GetAnalysisSchedule(ctx, created.ID)
	if err != nil {
		t.Fatalf("get schedule: %v", err)
	}
	if len(got.NotifyChannelIDs) != 2 || got.NotifyChannelIDs[0] != 7 || got.NotifyChannelIDs[1] != 42 {
		t.Fatalf("get returned NotifyChannelIDs = %v, want [7 42]", got.NotifyChannelIDs)
	}

	// Clearing the list must persist as an empty slice, never NULL.
	got.NotifyChannelIDs = nil
	updated, err := store.UpdateAnalysisSchedule(ctx, created.ID, got)
	if err != nil {
		t.Fatalf("update schedule: %v", err)
	}
	if len(updated.NotifyChannelIDs) != 0 {
		t.Fatalf("after clearing, NotifyChannelIDs = %v, want empty", updated.NotifyChannelIDs)
	}

	// The report snapshot column round-trips the same way.
	rep, err := store.InsertPendingReport(ctx, model.AnalysisReport{
		Feed:             "srvlog",
		PromptMode:       model.AnalysisModeDaily,
		PeriodStart:      time.Now().Add(-24 * time.Hour),
		PeriodEnd:        time.Now(),
		NotifyChannelIDs: []int64{7, 42},
	})
	if err != nil {
		t.Fatalf("insert pending report: %v", err)
	}
	readBack, err := store.GetReport(ctx, rep.ID)
	if err != nil {
		t.Fatalf("get report: %v", err)
	}
	if len(readBack.NotifyChannelIDs) != 2 || readBack.NotifyChannelIDs[0] != 7 || readBack.NotifyChannelIDs[1] != 42 {
		t.Fatalf("report NotifyChannelIDs = %v, want [7 42]", readBack.NotifyChannelIDs)
	}
}

// TestIntegration_MuteRuleLifecycle runs migration 24 and the four mute rule
// store methods against a real Postgres: empty match fields round-trip
// through NULL, the list window and its ordering, LEAST on end, revive via
// set-expiry, ErrNoRows on unknown ids, and the at-least-one-match CHECK.
func TestIntegration_MuteRuleLifecycle(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "mute_rules")
	store := NewStore(pool)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	var _ mute.Store = store

	host, err := store.CreateMuteRule(ctx, mute.Rule{
		Feed: mute.FeedNetlog, Hostname: "edge*", Search: "bundle ae30",
		Reason: "known flap", CreatedBy: "lasse", ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create netlog rule: %v", err)
	}
	if host.ID == 0 || host.CreatedAt.IsZero() {
		t.Fatalf("create returned id=%d created_at=%v, want both set", host.ID, host.CreatedAt)
	}
	prog, err := store.CreateMuteRule(ctx, mute.Rule{
		Feed: mute.FeedSrvlog, Programname: "cron",
		Reason: "noisy", CreatedBy: "ops", ExpiresAt: now.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("create srvlog rule: %v", err)
	}

	// Ordered expires_at DESC; empty match fields come back as "".
	active := listMuteRules(t, store, now, prog.ID, host.ID)
	wantMuteRule(t, active[0], prog)
	wantMuteRule(t, active[1], host)

	// End: expires_at becomes now and the rule leaves the active list, but
	// stays in the 7-day window. A later repeat end keeps the first time.
	if err := store.EndMuteRule(ctx, host.ID, now); err != nil {
		t.Fatalf("end: %v", err)
	}
	if err := store.EndMuteRule(ctx, host.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("repeat end: %v", err)
	}
	listMuteRules(t, store, now, prog.ID)
	recent := listMuteRules(t, store, now.Add(-mute.ExpiredListWindow), prog.ID, host.ID)
	ended := host
	ended.ExpiresAt = now
	wantMuteRule(t, recent[1], ended)

	// Set-expiry revives the ended rule and returns the whole row.
	revived, err := store.SetMuteRuleExpiry(ctx, host.ID, now.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("set expiry: %v", err)
	}
	ended.ExpiresAt = now.Add(3 * time.Hour)
	wantMuteRule(t, revived, ended)
	listMuteRules(t, store, now, host.ID, prog.ID)

	if _, err := store.SetMuteRuleExpiry(ctx, 1<<40, now); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("set expiry on unknown id: err = %v, want pgx.ErrNoRows", err)
	}
	if err := store.EndMuteRule(ctx, 1<<40, now); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("end unknown id: err = %v, want pgx.ErrNoRows", err)
	}

	// The CHECK constraint backs Validate: no match field at all is rejected.
	if _, err := store.CreateMuteRule(ctx, mute.Rule{
		Feed: mute.FeedNetlog, Reason: "r", CreatedBy: "lasse", ExpiresAt: now.Add(time.Hour),
	}); err == nil {
		t.Fatal("create with no match field succeeded, want CHECK violation")
	}
}

// TestIntegration_ListNetlogsExcludesMuted runs the hide_muted exclusion
// against real Postgres: a row matching a rule's conjunction (with a
// differently-cased search) is excluded, a row on the same host that misses
// the search is kept, and a wildcard-host rule excludes by ILIKE.
func TestIntegration_ListNetlogsExcludesMuted(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "netlog_events")
	store := NewStore(pool)
	ctx := context.Background()
	now := time.Now().UTC()

	insertNetlogEvent(t, pool, now.Add(-3*time.Second), "leaf-02", "", 3, "bundle AE30 link down")
	insertNetlogEvent(t, pool, now.Add(-2*time.Second), "leaf-02", "", 3, "bgp neighbor up")
	insertNetlogEvent(t, pool, now.Add(-time.Second), "Edge-01", "", 3, "anything at all")

	set := mute.NewSet(nil, func() time.Time { return now }, slog.New(slog.DiscardHandler))
	set.Replace([]mute.Rule{
		{ID: 1, Feed: mute.FeedNetlog, Hostname: "leaf-02", Search: "ae30", ExpiresAt: now.Add(time.Hour)},
		{ID: 2, Feed: mute.FeedNetlog, Hostname: "edge*", ExpiresAt: now.Add(time.Hour)},
	})

	events, _, err := store.ListNetlogs(ctx, model.NetlogFilter{Exclude: set.Active().NetlogExclusions()}, nil, 100)
	if err != nil {
		t.Fatalf("ListNetlogs: %v", err)
	}
	if len(events) != 1 || events[0].Message != "bgp neighbor up" {
		t.Fatalf("ListNetlogs with exclusions = %+v, want only the unmuted leaf-02 row", events)
	}

	all, _, err := store.ListNetlogs(ctx, model.NetlogFilter{}, nil, 100)
	if err != nil {
		t.Fatalf("ListNetlogs without exclusions: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ListNetlogs without exclusions returned %d rows, want 3", len(all))
	}
}

// listMuteRules lists the rules expiring after the given time and fails
// unless their ids are exactly wantIDs, in order.
func listMuteRules(t *testing.T, store *Store, expiresAfter time.Time, wantIDs ...int64) []mute.Rule {
	t.Helper()
	rules, err := store.ListMuteRules(context.Background(), expiresAfter)
	if err != nil {
		t.Fatalf("list mute rules: %v", err)
	}
	ids := make([]int64, len(rules))
	for i, r := range rules {
		ids[i] = r.ID
	}
	if !slices.Equal(ids, wantIDs) {
		t.Fatalf("list mute rules after %v = ids %v, want %v", expiresAfter, ids, wantIDs)
	}
	return rules
}

// wantMuteRule compares every field, using time.Equal for the timestamps.
func wantMuteRule(t *testing.T, got, want mute.Rule) {
	t.Helper()
	norm := got
	norm.CreatedAt, norm.ExpiresAt = want.CreatedAt, want.ExpiresAt
	if norm != want || !got.CreatedAt.Equal(want.CreatedAt) || !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Fatalf("mute rule = %+v, want %+v", got, want)
	}
}

// TestIntegration_ListenerShutdownWhileListening exercises Listen followed by
// Shutdown while the recv goroutine is blocked in WaitForNotification. Under
// -race this verifies the listen goroutine is the connection's single owner —
// Shutdown must never Close the conn itself (audit issue 05).
func TestIntegration_ListenerShutdownWhileListening(t *testing.T) {
	pool := testPool(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	l := NewListener(os.Getenv("TEST_DATABASE_URL"), pool, 16, logger, []string{"srvlog_ingest"})

	ctx := context.Background()
	ch, err := l.Listen(ctx)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	// Prove the recv loop is live before shutting down mid-listen.
	if _, err := pool.Exec(ctx, "SELECT pg_notify('srvlog_ingest', '1')"); err != nil {
		t.Fatalf("pg_notify: %v", err)
	}
	select {
	case n := <-ch:
		if n.Channel != "srvlog_ingest" || n.ID != 1 {
			t.Fatalf("notification = %+v, want srvlog_ingest/1", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for notification")
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := l.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	// Shutdown waits for the listen goroutine, which closes ch before
	// signalling done — so the channel must already be closed.
	if _, ok := <-ch; ok {
		t.Fatal("notification channel still open after shutdown")
	}
}

// TestIntegration_ListenerToBrokerDelivery exercises the ingest fan-out path
// end to end: an INSERT into srvlog_events fires the pg_notify trigger, the
// Listener receives the notification, and ingestbridge.Dispatch fetches the
// row and broadcasts it to a subscribed broker client — the same wiring
// serve.go's startBackgroundWorkers builds in production.
func TestIntegration_ListenerToBrokerDelivery(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "srvlog_events")
	store := NewStore(pool)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ctx := context.Background()

	srvlogBroker := broker.NewSrvlogBroker(logger)
	sub, err := srvlogBroker.Subscribe(model.SrvlogFilter{}, "")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	l := NewListener(os.Getenv("TEST_DATABASE_URL"), pool, 16, logger, []string{"srvlog_ingest"})
	notifications, err := l.Listen(ctx)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	// Mirror the serve.go worker: fetch each notified row by ID and
	// broadcast the full event to SSE subscribers.
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		for n := range notifications {
			ingestbridge.Dispatch(ctx, ingestbridge.Notification{Channel: n.Channel, ID: n.ID, ReceivedAt: n.ReceivedAt},
				store, nil, ingestbridge.Sinks{BroadcastSrvlog: srvlogBroker.Broadcast}, logger, 5*time.Second)
		}
	}()

	// Ordered teardown, mirroring serve.go shutdown: listener first (closes
	// the notifications channel, which stops the worker), then the broker.
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := l.Shutdown(shutdownCtx); err != nil {
			t.Errorf("shutdown listener: %v", err)
		}
		select {
		case <-workerDone:
		case <-time.After(5 * time.Second):
			t.Error("dispatch worker did not exit after listener shutdown")
		}
		srvlogBroker.Shutdown()
	})

	var insertedID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO srvlog_events (received_at, reported_at, hostname, fromhost_ip, severity, facility, message)
		 VALUES (now(), now(), 'edge-r1', '10.0.0.1'::inet, 3, 1, 'link flap on ge-0/0/0')
		 RETURNING id`).Scan(&insertedID); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	select {
	case msg, ok := <-sub.Chan():
		if !ok {
			t.Fatal("subscription channel closed before delivery")
		}
		if msg.ID != insertedID {
			t.Errorf("message ID = %d, want %d", msg.ID, insertedID)
		}
		var event model.SrvlogEvent
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			t.Fatalf("unmarshal broadcast event: %v", err)
		}
		if event.ID != insertedID {
			t.Errorf("event.ID = %d, want %d", event.ID, insertedID)
		}
		if event.Hostname != "edge-r1" {
			t.Errorf("event.Hostname = %q, want edge-r1", event.Hostname)
		}
		if event.Message != "link flap on ge-0/0/0" {
			t.Errorf("event.Message = %q, want link flap on ge-0/0/0", event.Message)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for broker delivery")
	}
}

// TestIntegration_ListenerGapFillAfterReconnect kills the LISTEN connection
// server-side, inserts rows while the listener is disconnected (their
// pg_notify is lost), and verifies each row ID and its received_at are still
// delivered after reconnect via the gap-fill query; a row whose received_at
// is infinite goes out with its ID alone. The test waits for the terminated
// backend's PID to disappear from pg_stat_activity before inserting, so the
// notification is provably lost rather than racing the disconnect.
func TestIntegration_ListenerGapFillAfterReconnect(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "srvlog_events")
	// The infinite received_at row would break later list scans.
	t.Cleanup(func() { truncate(t, pool, "srvlog_events") })
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ctx := context.Background()

	l := NewListener(os.Getenv("TEST_DATABASE_URL"), pool, 16, logger, []string{"srvlog_ingest"})
	ch, err := l.Listen(ctx)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := l.Shutdown(shutdownCtx); err != nil {
			t.Errorf("shutdown listener: %v", err)
		}
	})

	insertEvent := func(msg string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx,
			`INSERT INTO srvlog_events (received_at, reported_at, hostname, fromhost_ip, severity, facility, message)
			 VALUES (now(), now(), 'host1', '10.0.0.1'::inet, 6, 1, $1)
			 RETURNING id`, msg).Scan(&id); err != nil {
			t.Fatalf("insert event %q: %v", msg, err)
		}
		return id
	}

	// Deliver one event normally so the listener records a last-seen ID —
	// the baseline the gap-fill query resumes from.
	baselineID := insertEvent("baseline")
	select {
	case n := <-ch:
		if n.Channel != "srvlog_ingest" || n.ID != baselineID {
			t.Fatalf("notification = %+v, want srvlog_ingest/%d", n, baselineID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for baseline notification")
	}

	// Find and terminate the dedicated LISTEN backend. Tracking its PID lets
	// us distinguish the dead connection from the reconnected one.
	var listenPID int
	if err := pool.QueryRow(ctx,
		`SELECT pid FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND query LIKE 'LISTEN %'`,
	).Scan(&listenPID); err != nil {
		t.Fatalf("find LISTEN backend: %v", err)
	}
	if _, err := pool.Exec(ctx, `SELECT pg_terminate_backend($1)`, listenPID); err != nil {
		t.Fatalf("terminate LISTEN backend: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		var alive int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM pg_stat_activity WHERE pid = $1`, listenPID,
		).Scan(&alive); err != nil {
			t.Fatalf("poll terminated backend: %v", err)
		}
		if alive == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("LISTEN backend %d still alive after terminate", listenPID)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// These rows' pg_notify has no listener to reach; only the gap fill (or a
	// re-established LISTEN, if reconnect won the race) can deliver their IDs.
	infiniteID := insertFeedRow(t, pool, "srvlog_events", "infinity")
	missedID := insertEvent("missed while disconnected")
	var missedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT received_at FROM srvlog_events WHERE id = $1`, missedID).Scan(&missedAt); err != nil {
		t.Fatalf("read missed row: %v", err)
	}

	// Reconnect backoff starts at 1s, then the gap fill replays IDs above
	// the baseline. Generous timeout for slow CI.
	for _, want := range []Notification{
		{Channel: "srvlog_ingest", ID: infiniteID},
		{Channel: "srvlog_ingest", ID: missedID, ReceivedAt: missedAt},
	} {
		select {
		case n, ok := <-ch:
			if !ok {
				t.Fatal("notification channel closed before gap-fill delivery")
			}
			if n.Channel != want.Channel || n.ID != want.ID || !n.ReceivedAt.Equal(want.ReceivedAt) {
				t.Fatalf("notification = %+v, want %+v", n, want)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("timed out waiting for gap-fill delivery after reconnect")
		}
	}
}

// insertNetlogEvent seeds one netlog row. msg_pattern is not written here: a
// BEFORE INSERT trigger computes it from message, which is why an empty
// message is how a row ends up with no usable event signature at all.
func insertNetlogEvent(t *testing.T, pool *pgxpool.Pool, at time.Time, host, msgid string, severity int, message string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO netlog_events (received_at, reported_at, hostname, fromhost_ip, msgid, severity, facility, message)
		 VALUES ($1, $1, $2, '10.0.0.1'::inet, $3, $4, 1, $5)`,
		at, host, msgid, severity, message); err != nil {
		t.Fatalf("insert netlog event: %v", err)
	}
}

// insertSrvlogEvent mirrors insertNetlogEvent for the other syslog feed.
func insertSrvlogEvent(t *testing.T, pool *pgxpool.Pool, at time.Time, host, msgid string, severity int, message string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO srvlog_events (received_at, reported_at, hostname, fromhost_ip, msgid, severity, facility, message)
		 VALUES ($1, $1, $2, '10.0.0.2'::inet, $3, $4, 1, $5)`,
		at, host, msgid, severity, message); err != nil {
		t.Fatalf("insert srvlog event: %v", err)
	}
}

// syslogFeeds are the feeds the analysis queries in analysis_store.go serve;
// every test below runs against each so a schema or index change on either
// table is caught.
var syslogFeeds = []string{"netlog", "srvlog"}

// insertSyslogEvent seeds one row on the given feed.
func insertSyslogEvent(t *testing.T, pool *pgxpool.Pool, feed string, at time.Time, host, msgid string, severity int, message string) {
	t.Helper()
	switch feed {
	case "netlog":
		insertNetlogEvent(t, pool, at, host, msgid, severity, message)
	case "srvlog":
		insertSrvlogEvent(t, pool, at, host, msgid, severity, message)
	default:
		t.Fatalf("no insert helper for feed %q", feed)
	}
}

// TestIntegration_GetTopErrorHosts covers the query rewritten to drop its
// materialized-CTE fence: host ordering, the msgid-or-msg_pattern signature
// fallback, the LEFT JOIN that keeps a host with no usable signature, and both
// filters (severity and window) that the fence used to defeat.
func TestIntegration_GetTopErrorHosts(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()

	for _, feed := range syslogFeeds {
		t.Run(feed, func(t *testing.T) {
			truncate(t, pool, "netlog_events", "srvlog_events")
			now := time.Now().UTC()
			inWindow := now.Add(-time.Hour)
			beforeWindow := now.Add(-48 * time.Hour)
			since := now.Add(-24 * time.Hour)

			// host-a: 5 errors in window; AAA is the most common signature.
			for range 3 {
				insertSyslogEvent(t, pool, feed, inWindow, "host-a", "AAA", 3, "cpu threshold exceeded")
			}
			for range 2 {
				insertSyslogEvent(t, pool, feed, inWindow, "host-a", "BBB", 3, "bgp peer down")
			}
			// ...plus 4 errors before the window, which must not be counted.
			for range 4 {
				insertSyslogEvent(t, pool, feed, beforeWindow, "host-a", "AAA", 3, "cpu threshold exceeded")
			}

			// host-b: msgid is the RFC 5424 NILVALUE, so the signature falls back to
			// the trigger-computed msg_pattern.
			for range 3 {
				insertSyslogEvent(t, pool, feed, inWindow, "host-b", "-", 3, "link down on ge-0/0/1")
			}

			// host-c: empty message => empty msg_pattern => no signature to report.
			insertSyslogEvent(t, pool, feed, inWindow, "host-c", "", 3, "")

			// host-d: informational only, below the severity <= 3 error bar.
			for range 10 {
				insertSyslogEvent(t, pool, feed, inWindow, "host-d", "INFO", 6, "interface up")
			}

			got, err := store.GetTopErrorHosts(ctx, model.AnalysisScope{Feed: feed}, since, 15)
			if err != nil {
				t.Fatalf("GetTopErrorHosts: %v", err)
			}

			want := []model.HostErrorCount{
				{Hostname: "host-a", Count: 5, TopMsgID: "AAA"},
				{Hostname: "host-b", Count: 3, TopMsgID: "link down on ge-<n>/<n>/<n>"},
				{Hostname: "host-c", Count: 1, TopMsgID: ""},
			}
			if len(got) != len(want) {
				t.Fatalf("got %d hosts (%+v), want %d", len(got), got, len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("host %d = %+v, want %+v", i, got[i], want[i])
				}
			}
		})
	}
}

// TestIntegration_GetVolumeTimeline covers the hourly-aggregate path, where
// SUM(cnt) FILTER returns NULL for a bucket with no errors. Scanning that NULL
// into int64 used to fail the whole timeline.
func TestIntegration_GetVolumeTimeline(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()

	for _, feed := range syslogFeeds {
		t.Run(feed, func(t *testing.T) {
			truncate(t, pool, "netlog_events", "srvlog_events")
			now := time.Now().UTC()
			quietHour := now.Truncate(time.Hour).Add(-4 * time.Hour)
			errorHour := now.Truncate(time.Hour).Add(-2 * time.Hour)

			// quietHour: informational only, so no row passes severity <= 3.
			for range 4 {
				insertSyslogEvent(t, pool, feed, quietHour.Add(30*time.Minute), "host-a", "INFO", 6, "interface up")
			}
			insertSyslogEvent(t, pool, feed, errorHour.Add(30*time.Minute), "host-a", "ERR", 3, "bgp peer down")

			// No host filter and 60-minute buckets select the aggregate path.
			got, err := store.GetVolumeTimeline(ctx, model.AnalysisScope{Feed: feed}, now.Add(-24*time.Hour), now, 60)
			if err != nil {
				t.Fatalf("GetVolumeTimeline: %v", err)
			}

			want := []model.AnalysisVolumeBucket{
				{Bucket: quietHour, Total: 4, ErrorCount: 0},
				{Bucket: errorHour, Total: 1, ErrorCount: 1},
			}
			if len(got) != len(want) {
				t.Fatalf("got %d buckets (%+v), want %d", len(got), got, len(want))
			}
			for i := range want {
				if !got[i].Bucket.Equal(want[i].Bucket) || got[i].Total != want[i].Total || got[i].ErrorCount != want[i].ErrorCount {
					t.Errorf("bucket %d = %+v, want %+v", i, got[i], want[i])
				}
			}
		})
	}
}

// TestIntegration_GetNewMsgIDs covers the NOT EXISTS -> EXCEPT rewrite: a
// signature counts as new only when it appears in the current window and not
// in the baseline window.
func TestIntegration_GetNewMsgIDs(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()

	for _, feed := range syslogFeeds {
		t.Run(feed, func(t *testing.T) {
			truncate(t, pool, "netlog_events", "srvlog_events")
			now := time.Now().UTC()
			since := now.Add(-24 * time.Hour)
			baselineSince := since.Add(-7 * 24 * time.Hour)
			current := now.Add(-time.Hour)
			baseline := now.Add(-72 * time.Hour)

			insertSyslogEvent(t, pool, feed, current, "host-a", "NEW_ONE", 4, "first time seen")
			insertSyslogEvent(t, pool, feed, current, "host-a", "BOTH", 4, "seen before")
			insertSyslogEvent(t, pool, feed, baseline, "host-a", "BOTH", 4, "seen before")
			insertSyslogEvent(t, pool, feed, baseline, "host-a", "OLD_ONE", 4, "only in baseline")
			// Empty signature in the current window must not surface as "new".
			insertSyslogEvent(t, pool, feed, current, "host-b", "", 4, "")

			got, err := store.GetNewMsgIDs(ctx, model.AnalysisScope{Feed: feed}, since, baselineSince)
			if err != nil {
				t.Fatalf("GetNewMsgIDs: %v", err)
			}

			want := []string{"NEW_ONE"}
			if len(got) != len(want) || got[0] != want[0] {
				t.Fatalf("GetNewMsgIDs = %v, want %v", got, want)
			}
		})
	}
}

// TestIntegration_AnalysisQueriesHaveNoCTEScan is the regression guard for the
// production outage: a CTE read more than once is materialized, which fences
// the time predicate off from the scan and makes the query read every chunk in
// the hypertable. A CTE Scan in either plan means that fence is back.
func TestIntegration_AnalysisQueriesHaveNoCTEScan(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "netlog_events", "srvlog_events")
	ctx := context.Background()

	// Spread rows over several days on both feeds so there are chunks to
	// exclude.
	now := time.Now().UTC()
	for i := range 10 {
		for _, feed := range syslogFeeds {
			insertSyslogEvent(t, pool, feed, now.Add(-time.Duration(i)*24*time.Hour),
				"host-a", "AAA", 3, "cpu threshold exceeded")
		}
	}

	since := now.Add(-24 * time.Hour)
	baselineSince := since.Add(-7 * 24 * time.Hour)

	for _, tc := range []struct {
		name  string
		scope model.AnalysisScope
	}{
		{"netlog all hosts", model.AnalysisScope{Feed: "netlog"}},
		{"netlog scoped", model.AnalysisScope{Feed: "netlog", Hosts: []string{"host-a"}}},
		{"srvlog all hosts", model.AnalysisScope{Feed: "srvlog"}},
		{"srvlog scoped", model.AnalysisScope{Feed: "srvlog", Hosts: []string{"host-a"}}},
	} {
		for _, q := range []struct {
			name string
			sql  string
			args []any
		}{
			{"topErrorHostsQuery", topErrorHostsQuery(tc.scope), appendHostsArg([]any{since, 15}, tc.scope)},
			{"newMsgIDsQuery", newMsgIDsQuery(tc.scope), appendHostsArg([]any{since, baselineSince}, tc.scope)},
		} {
			t.Run(tc.name+"/"+q.name, func(t *testing.T) {
				rows, err := pool.Query(ctx, "EXPLAIN "+q.sql, q.args...)
				if err != nil {
					t.Fatalf("explain: %v", err)
				}
				defer rows.Close()

				var plan strings.Builder
				for rows.Next() {
					var line string
					if err := rows.Scan(&line); err != nil {
						t.Fatalf("scan plan row: %v", err)
					}
					plan.WriteString(line)
					plan.WriteByte('\n')
				}
				if err := rows.Err(); err != nil {
					t.Fatalf("plan rows: %v", err)
				}
				if strings.Contains(plan.String(), "CTE Scan") {
					t.Errorf("plan contains a CTE Scan — the event source is materialized "+
						"and chunk exclusion is lost:\n%s", plan.String())
				}
			})
		}
	}
}

// TestIntegration_RetentionPoliciesMatchIntent asserts every hypertable ends up
// with the retention window its migration intended.
//
// This is the check that was missing when a production database ran for months
// with a drifted policy: `add_retention_policy(..., if_not_exists => true)` is
// a no-op when a policy already exists with different arguments, so the
// migrations could not correct it and nothing verified the result. Comparing
// the whole set also catches a new hypertable that forgets retention entirely,
// or one that quietly gains it.
func TestIntegration_RetentionPoliciesMatchIntent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	want := map[string]string{
		"srvlog_events":     "90 days",
		"netlog_events":     "90 days",
		"applog_events":     "90 days",
		"notification_log":  "30 days",
		"rsyslog_stats":     "30 days",
		"taillight_metrics": "30 days",
	}

	rows, err := pool.Query(ctx, `
		SELECT hypertable_name, config->>'drop_after'
		FROM timescaledb_information.jobs
		WHERE proc_name = 'policy_retention'`)
	if err != nil {
		t.Fatalf("query retention policies: %v", err)
	}
	defer rows.Close()

	got := make(map[string]string)
	for rows.Next() {
		var table string
		var dropAfter *string
		if err := rows.Scan(&table, &dropAfter); err != nil {
			t.Fatalf("scan retention policy: %v", err)
		}
		if dropAfter == nil {
			// drop_created_before instead of drop_after retires chunks by
			// creation time, which does not track the data's age at all.
			t.Errorf("retention policy on %s has no drop_after", table)
			continue
		}
		got[table] = *dropAfter
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("retention policy rows: %v", err)
	}

	for table, wantAfter := range want {
		switch gotAfter, ok := got[table]; {
		case !ok:
			t.Errorf("%s has no retention policy; want drop_after %s", table, wantAfter)
		case gotAfter != wantAfter:
			t.Errorf("%s drop_after = %s, want %s", table, gotAfter, wantAfter)
		}
	}
	for table := range got {
		if _, ok := want[table]; !ok {
			t.Errorf("unexpected retention policy on %s (drop_after %s); add it to want if intended", table, got[table])
		}
	}
}

// TestIntegration_SafeReportedAtFallsBackToNow covers the function the rsyslog
// insert templates wrap the sender's timestamp in: a value Postgres cannot
// store as timestamptz yields now() instead of an error.
func TestIntegration_SafeReportedAtFallsBackToNow(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		in      string
		wantNow bool
	}{
		{"valid rfc3339", "2026-10-30T12:00:00.123456+02:00", false},
		{"impossible calendar date", "2026-02-30T12:00:00+00:00", true},
		{"year zero", "0000-01-01T00:00:00+00:00", true},
		{"utc offset out of range", "2026-10-30T12:00:00+16:00", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got, now time.Time
			if err := pool.QueryRow(ctx, `SELECT safe_reported_at($1), now()`, tt.in).Scan(&got, &now); err != nil {
				t.Fatalf("safe_reported_at(%q): %v", tt.in, err)
			}
			if tt.wantNow {
				if !got.Equal(now) {
					t.Errorf("safe_reported_at(%q) = %v, want now() %v", tt.in, got, now)
				}
				return
			}
			want, err := time.Parse(time.RFC3339Nano, tt.in)
			if err != nil {
				t.Fatalf("parse %q: %v", tt.in, err)
			}
			if !got.Equal(want) {
				t.Errorf("safe_reported_at(%q) = %v, want %v", tt.in, got, want)
			}
		})
	}
}

// TestIntegration_SyslogBatchWithRefusedTimestampCommits replays an ompgsql
// batch: a row whose sender timestamp Postgres refuses, then a normal row, in
// one transaction. Both rows must commit, the first with now() as reported_at,
// so one bad timestamp cannot fail the batch and hold the feed's queue.
func TestIntegration_SyslogBatchWithRefusedTimestampCommits(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	for _, table := range []string{"srvlog_events", "netlog_events"} {
		t.Run(table, func(t *testing.T) {
			truncate(t, pool, table)

			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer tx.Rollback(ctx) //nolint:errcheck // Rollback after commit is a no-op.

			var txNow time.Time
			if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&txNow); err != nil {
				t.Fatalf("select now: %v", err)
			}
			insert := `INSERT INTO ` + table + ` (received_at, reported_at, hostname, fromhost_ip, severity, facility, message)
				VALUES (now(), safe_reported_at($1), $2, '10.0.0.1'::inet, 5, 1, 'msg')`
			for _, row := range []struct{ ts, host string }{
				{"2026-02-30T12:00:00+00:00", "refused-ts"},
				{"2026-10-30T12:00:00+00:00", "valid-ts"},
			} {
				if _, err := tx.Exec(ctx, insert, row.ts, row.host); err != nil {
					t.Fatalf("insert %s: %v", row.host, err)
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatalf("commit: %v", err)
			}

			want := map[string]time.Time{
				"refused-ts": txNow,
				"valid-ts":   time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC),
			}
			rows, err := pool.Query(ctx, `SELECT hostname, reported_at FROM `+table)
			if err != nil {
				t.Fatalf("query rows: %v", err)
			}
			got := make(map[string]time.Time)
			for rows.Next() {
				var host string
				var reportedAt time.Time
				if err := rows.Scan(&host, &reportedAt); err != nil {
					t.Fatalf("scan: %v", err)
				}
				got[host] = reportedAt
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("rows: %v", err)
			}
			if len(got) != len(want) {
				t.Fatalf("stored %d rows, want %d: %v", len(got), len(want), got)
			}
			for host, wantAt := range want {
				if !got[host].Equal(wantAt) {
					t.Errorf("%s reported_at = %v, want %v", host, got[host], wantAt)
				}
			}
		})
	}
}
