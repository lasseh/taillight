package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/ingestbridge"
	"github.com/lasseh/taillight/internal/model"
)

// These tests cover the ingest bridge's per-event fetch: the NOTIFY payload
// carries the row's received_at, and the fetch bounded by it reads only the
// chunk that holds the row, however much compressed history is retained.

// subSecond returns a received_at with non-zero microseconds, so a lossy
// round trip through the payload would fail the exact match.
func subSecond(t time.Time) time.Time {
	return t.UTC().Truncate(time.Second).Add(123457 * time.Microsecond)
}

// seedCompressedHistory fills five daily chunks six to ten days back with
// rows on several hosts and compresses them, as the columnstore policy would.
func seedCompressedHistory(t *testing.T, pool *pgxpool.Pool, table string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (received_at, reported_at, hostname, fromhost_ip, severity, facility, message)
		 SELECT now() - make_interval(days => d) + make_interval(secs => i), now(),
		        'history-' || (i %% 5), '10.0.0.1'::inet, 6, 1, 'history ' || i
		 FROM generate_series(6, 10) d, generate_series(1, 20) i`, table)); err != nil {
		t.Fatalf("seed %s history: %v", table, err)
	}
	var compressed int
	if err := pool.QueryRow(ctx,
		`SELECT count(compress_chunk(c, if_not_compressed => true))
		 FROM show_chunks($1::regclass, older_than => now() - interval '2 days') c`, table,
	).Scan(&compressed); err != nil {
		t.Fatalf("compress %s chunks: %v", table, err)
	}
	if compressed < 5 {
		t.Fatalf("compressed %d %s chunks, want at least 5", compressed, table)
	}
}

// insertFeedRow inserts one row into a syslog feed table and returns its id.
func insertFeedRow(t *testing.T, pool *pgxpool.Pool, table string, receivedAt any) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), fmt.Sprintf(
		`INSERT INTO %s (received_at, reported_at, hostname, fromhost_ip, severity, facility, message)
		 VALUES ($1, now(), 'edge-r1', '10.0.0.1'::inet, 3, 1, 'link flap on ge-0/0/0')
		 RETURNING id`, table), receivedAt).Scan(&id); err != nil {
		t.Fatalf("insert %s row: %v", table, err)
	}
	return id
}

// inCompressedChunk reports whether at falls inside a compressed chunk of table.
func inCompressedChunk(t *testing.T, pool *pgxpool.Pool, table string, at time.Time) bool {
	t.Helper()
	var compressed bool
	if err := pool.QueryRow(context.Background(),
		`SELECT is_compressed FROM timescaledb_information.chunks
		 WHERE hypertable_name = $1 AND range_start <= $2 AND range_end > $2`, table, at,
	).Scan(&compressed); err != nil {
		t.Fatalf("find %s chunk for %v: %v", table, at, err)
	}
	return compressed
}

// explainAnalyze runs EXPLAIN ANALYZE of query with args on a pool or a
// single connection and returns the plan.
func explainAnalyze(t *testing.T, conn interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, query string, args ...any) string {
	t.Helper()
	rows, err := conn.Query(context.Background(), "EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY OFF) "+query, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("collect plan: %v", err)
	}
	return strings.Join(lines, "\n")
}

// explainGenericPlan prepares query on its own connection and explains its
// generic plan, the plan Postgres may switch to for a statement pgx caches.
func explainGenericPlan(t *testing.T, query string, id int64, receivedAt time.Time) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx) //nolint:errcheck // test connection, nothing to recover
	for _, stmt := range []string{"SET plan_cache_mode = force_generic_plan", "PREPARE bridge_fetch AS " + query} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	return explainAnalyze(t, conn, fmt.Sprintf("EXECUTE bridge_fetch(%d, '%s')", id, receivedAt.UTC().Format(time.RFC3339Nano)))
}

// scansCompressed reports whether a plan reads compressed chunk data.
func scansCompressed(plan string) bool {
	return strings.Contains(plan, "ColumnarScan") || strings.Contains(plan, "DecompressChunk")
}

// TestIntegration_BridgeFetchSkipsCompressedChunks proves the bounded fetch
// reads no compressed chunk for a fresh row, in the plan Postgres picks for
// the bound values and in the generic plan, while the id-only lookup reads
// every compressed chunk. The id-only lookup still serves the detail view, so
// a row deep in compressed history stays readable by id.
func TestIntegration_BridgeFetchSkipsCompressedChunks(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()

	feeds := []struct {
		table   string
		columns []string
		atQuery func(int64, time.Time) (string, []any, error)
		getAt   func(context.Context, int64, time.Time) (int64, error)
		get     func(context.Context, int64) (int64, error)
	}{
		{
			table:   "srvlog_events",
			columns: srvlogColumns,
			atQuery: srvlogAtQuery,
			getAt: func(ctx context.Context, id int64, at time.Time) (int64, error) {
				e, err := store.GetSrvlogAt(ctx, id, at)
				return e.ID, err
			},
			get: func(ctx context.Context, id int64) (int64, error) {
				e, err := store.GetSrvlog(ctx, id)
				return e.ID, err
			},
		},
		{
			table:   "netlog_events",
			columns: netlogColumns,
			atQuery: netlogAtQuery,
			getAt: func(ctx context.Context, id int64, at time.Time) (int64, error) {
				e, err := store.GetNetlogAt(ctx, id, at)
				return e.ID, err
			},
			get: func(ctx context.Context, id int64) (int64, error) {
				e, err := store.GetNetlog(ctx, id)
				return e.ID, err
			},
		},
	}

	for _, feed := range feeds {
		t.Run(feed.table, func(t *testing.T) {
			truncate(t, pool, feed.table)
			seedCompressedHistory(t, pool, feed.table)

			var oldID int64
			var oldAt time.Time
			if err := pool.QueryRow(ctx, fmt.Sprintf(
				`SELECT id, received_at FROM %s ORDER BY received_at LIMIT 1`, feed.table),
			).Scan(&oldID, &oldAt); err != nil {
				t.Fatalf("find oldest row: %v", err)
			}
			if !inCompressedChunk(t, pool, feed.table, oldAt) {
				t.Fatalf("oldest row at %v is not in a compressed chunk", oldAt)
			}

			freshAt := subSecond(time.Now())
			freshID := insertFeedRow(t, pool, feed.table, freshAt)

			// Control: the id-only lookup reads every compressed chunk, so
			// the seed left the history the bounded fetch has to skip.
			idQuery, idArgs, err := psq.Select(feed.columns...).From(feed.table).Where(sq.Eq{"id": freshID}).ToSql()
			if err != nil {
				t.Fatalf("build id-only query: %v", err)
			}
			idPlan := explainAnalyze(t, pool, idQuery, idArgs...)
			t.Logf("id-only fetch plan:\n%s", idPlan)
			if !scansCompressed(idPlan) {
				t.Fatal("id-only fetch reads no compressed chunk; the control does not hold")
			}

			atQuery, atArgs, err := feed.atQuery(freshID, freshAt)
			if err != nil {
				t.Fatalf("build bounded query: %v", err)
			}
			plan := explainAnalyze(t, pool, atQuery, atArgs...)
			t.Logf("bounded fetch plan:\n%s", plan)
			if scansCompressed(plan) {
				t.Errorf("bounded fetch reads compressed chunks:\n%s", plan)
			}
			generic := explainGenericPlan(t, atQuery, freshID, freshAt)
			t.Logf("bounded fetch generic plan:\n%s", generic)
			if scansCompressed(generic) {
				t.Errorf("bounded fetch generic plan reads compressed chunks:\n%s", generic)
			}

			if got, err := feed.getAt(ctx, freshID, freshAt); err != nil || got != freshID {
				t.Errorf("bounded fetch of fresh row = %d, %v; want %d", got, err, freshID)
			}
			if got, err := feed.get(ctx, oldID); err != nil || got != oldID {
				t.Errorf("detail fetch of compressed row = %d, %v; want %d", got, err, oldID)
			}
			if got, err := feed.getAt(ctx, oldID, oldAt); err != nil || got != oldID {
				t.Errorf("bounded fetch of compressed row = %d, %v; want %d", got, err, oldID)
			}
			if _, err := feed.getAt(ctx, freshID, freshAt.Add(time.Microsecond)); !errors.Is(err, pgx.ErrNoRows) {
				t.Errorf("bounded fetch with another received_at: err = %v, want pgx.ErrNoRows", err)
			}
		})
	}
}

// TestIntegration_NotifyPayloadCarriesReceivedAt checks what the Listener
// reads from each feed's insert trigger: the id and the exact received_at; the
// id alone for a row whose received_at is infinite, whose insert must still
// succeed; and the id-only payload the trigger sent before it carried
// received_at.
func TestIntegration_NotifyPayloadCarriesReceivedAt(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "srvlog_events", "netlog_events")
	// The infinite received_at rows would break later list scans.
	t.Cleanup(func() { truncate(t, pool, "srvlog_events", "netlog_events") })
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ctx := context.Background()

	l := NewListener(os.Getenv("TEST_DATABASE_URL"), pool, 16, logger, []string{"srvlog_ingest", "netlog_ingest"})
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

	next := func(t *testing.T) Notification {
		t.Helper()
		select {
		case n, ok := <-ch:
			if !ok {
				t.Fatal("notification channel closed")
			}
			return n
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for notification")
		}
		return Notification{}
	}

	for _, feed := range []struct{ table, channel string }{
		{"srvlog_events", "srvlog_ingest"},
		{"netlog_events", "netlog_ingest"},
	} {
		t.Run(feed.table, func(t *testing.T) {
			at := subSecond(time.Now())
			id := insertFeedRow(t, pool, feed.table, at)
			if n := next(t); n.Channel != feed.channel || n.ID != id || !n.ReceivedAt.Equal(at) {
				t.Errorf("notification = %+v, want %s/%d at %v", n, feed.channel, id, at)
			}

			infID := insertFeedRow(t, pool, feed.table, "infinity")
			if n := next(t); n.Channel != feed.channel || n.ID != infID || !n.ReceivedAt.IsZero() {
				t.Errorf("notification = %+v, want %s/%d with no received_at", n, feed.channel, infID)
			}

			if _, err := pool.Exec(ctx, "SELECT pg_notify($1, $2)", feed.channel, fmt.Sprint(id)); err != nil {
				t.Fatalf("send id-only payload: %v", err)
			}
			if n := next(t); n.Channel != feed.channel || n.ID != id || !n.ReceivedAt.IsZero() {
				t.Errorf("notification = %+v, want %s/%d with no received_at", n, feed.channel, id)
			}
		})
	}
}

// boundedFetcher serves only fetches bounded by received_at, so an event
// delivered through it was fetched with the timestamp its NOTIFY payload
// carried.
type boundedFetcher struct{ *Store }

func (boundedFetcher) GetSrvlog(context.Context, int64) (model.SrvlogEvent, error) {
	return model.SrvlogEvent{}, errors.New("unbounded srvlog fetch")
}

func (boundedFetcher) GetNetlog(context.Context, int64) (model.NetlogEvent, error) {
	return model.NetlogEvent{}, errors.New("unbounded netlog fetch")
}

// TestIntegration_BridgeDeliversLateRow inserts, now, a row whose received_at
// falls in an already compressed chunk, as a disk-queue replay does, and
// checks the trigger, Listener and Dispatch still deliver it to the feed's
// broker and to the notification engine through the received_at its NOTIFY
// payload carries.
func TestIntegration_BridgeDeliversLateRow(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "srvlog_events", "netlog_events")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ctx := context.Background()
	seedCompressedHistory(t, pool, "srvlog_events")
	seedCompressedHistory(t, pool, "netlog_events")

	srvlogBroker := broker.NewSrvlogBroker(logger)
	netlogBroker := broker.NewNetlogBroker(logger)
	srvSub, err := srvlogBroker.Subscribe(model.SrvlogFilter{}, "")
	if err != nil {
		t.Fatalf("subscribe srvlog: %v", err)
	}
	netSub, err := netlogBroker.Subscribe(model.NetlogFilter{}, "")
	if err != nil {
		t.Fatalf("subscribe netlog: %v", err)
	}

	l := NewListener(os.Getenv("TEST_DATABASE_URL"), pool, 16, logger, []string{"srvlog_ingest", "netlog_ingest"})
	notifications, err := l.Listen(ctx)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	// Alerts: the ids Dispatch hands to the notification engine, per feed.
	srvNotified := make(chan int64, 4)
	netNotified := make(chan int64, 4)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		sinks := ingestbridge.Sinks{
			BroadcastSrvlog: srvlogBroker.Broadcast,
			BroadcastNetlog: netlogBroker.Broadcast,
			NotifySrvlog:    func(e model.SrvlogEvent) { srvNotified <- e.ID },
			NotifyNetlog:    func(e model.NetlogEvent) { netNotified <- e.ID },
		}
		for n := range notifications {
			ingestbridge.Dispatch(ctx, ingestbridge.Notification{Channel: n.Channel, ID: n.ID, ReceivedAt: n.ReceivedAt},
				boundedFetcher{NewStore(pool)}, nil, sinks, logger, 5*time.Second)
		}
	}()
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
		netlogBroker.Shutdown()
	})

	late := subSecond(time.Now().Add(-8 * 24 * time.Hour))
	for _, feed := range []struct {
		table    string
		sub      <-chan broker.Message
		notified <-chan int64
	}{
		{"srvlog_events", srvSub.Chan(), srvNotified},
		{"netlog_events", netSub.Chan(), netNotified},
	} {
		t.Run(feed.table, func(t *testing.T) {
			if !inCompressedChunk(t, pool, feed.table, late) {
				t.Fatalf("%v is not in a compressed chunk", late)
			}
			id := insertFeedRow(t, pool, feed.table, late)

			select {
			case msg, ok := <-feed.sub:
				if !ok {
					t.Fatal("subscription closed before delivery")
				}
				var event struct {
					ID         int64     `json:"id"`
					ReceivedAt time.Time `json:"received_at"`
				}
				if err := json.Unmarshal(msg.Data, &event); err != nil {
					t.Fatalf("unmarshal broadcast event: %v", err)
				}
				if event.ID != id || !event.ReceivedAt.Equal(late) {
					t.Errorf("delivered event %d at %v, want %d at %v", event.ID, event.ReceivedAt, id, late)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("timed out waiting for the late row to reach the broker")
			}

			select {
			case got := <-feed.notified:
				if got != id {
					t.Errorf("notification engine got event %d, want %d", got, id)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("timed out waiting for the late row to reach the notification engine")
			}
		})
	}
}
