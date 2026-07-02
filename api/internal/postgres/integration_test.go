package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/ingestbridge"
	"github.com/lasseh/taillight/internal/model"
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
			ingestbridge.Dispatch(ctx, ingestbridge.Notification{Channel: n.Channel, ID: n.ID},
				store, srvlogBroker.Broadcast, nil, logger, 5*time.Second)
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
// server-side, inserts a row while the listener is disconnected (that row's
// pg_notify is lost), and verifies the row ID is still delivered after
// reconnect via the gap-fill query. The test waits for the terminated
// backend's PID to disappear from pg_stat_activity before inserting, so the
// notification is provably lost rather than racing the disconnect.
func TestIntegration_ListenerGapFillAfterReconnect(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "srvlog_events")
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

	// This row's pg_notify has no listener to reach; only the gap fill (or a
	// re-established LISTEN, if reconnect won the race) can deliver its ID.
	missedID := insertEvent("missed while disconnected")

	// Reconnect backoff starts at 1s, then the gap fill replays IDs above
	// the baseline. Generous timeout for slow CI.
	select {
	case n, ok := <-ch:
		if !ok {
			t.Fatal("notification channel closed before gap-fill delivery")
		}
		if n.Channel != "srvlog_ingest" || n.ID != missedID {
			t.Fatalf("notification = %+v, want srvlog_ingest/%d", n, missedID)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for gap-fill delivery after reconnect")
	}
}
