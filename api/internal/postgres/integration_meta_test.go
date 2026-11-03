package postgres

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// metaFeed describes one feed's events table and meta cache for the meta
// cache integration tests.
type metaFeed struct {
	name   string
	events string
	cache  string
	column string // a cached column, for single-value checks
	// insert logs one event at received_at whose cached columns all hold value.
	insert string
	// flood logs $1 events whose cached columns all hold distinct names that
	// sort ahead of, and contain, the real value used below.
	flood string
}

var metaFeeds = []metaFeed{
	{
		name: "netlog", events: "netlog_events", cache: "netlog_meta_cache", column: "hostname",
		insert: `INSERT INTO netlog_events (received_at, reported_at, hostname, fromhost_ip, programname, syslogtag, severity, facility, message)
			VALUES ($1, $1, $2, '10.0.0.1'::inet, $2, $2, 6, 1, 'link up')`,
		flood: `INSERT INTO netlog_events (received_at, reported_at, hostname, fromhost_ip, programname, syslogtag, severity, facility, message)
			SELECT now(), now(), n, '10.0.0.9'::inet, n, n, 6, 1, 'flood'
			FROM (SELECT 'a-zz-core-1-' || lpad(g::text, 6, '0') AS n FROM generate_series(1, $1::int) g) s`,
	},
	{
		name: "srvlog", events: "srvlog_events", cache: "srvlog_meta_cache", column: "hostname",
		insert: `INSERT INTO srvlog_events (received_at, reported_at, hostname, fromhost_ip, programname, syslogtag, severity, facility, message)
			VALUES ($1, $1, $2, '10.0.0.2'::inet, $2, $2, 6, 1, 'session opened')`,
		flood: `INSERT INTO srvlog_events (received_at, reported_at, hostname, fromhost_ip, programname, syslogtag, severity, facility, message)
			SELECT now(), now(), n, '10.0.0.9'::inet, n, n, 6, 1, 'flood'
			FROM (SELECT 'a-zz-core-1-' || lpad(g::text, 6, '0') AS n FROM generate_series(1, $1::int) g) s`,
	},
	{
		name: "applog", events: "applog_events", cache: "applog_meta_cache", column: "service",
		insert: `INSERT INTO applog_events (received_at, timestamp, level, service, component, host, msg)
			VALUES ($1, $1, 'INFO', $2, $2, $2, 'started')`,
		flood: `INSERT INTO applog_events (received_at, timestamp, level, service, component, host, msg)
			SELECT now(), now(), 'INFO', n, n, n, 'flood'
			FROM (SELECT 'a-zz-core-1-' || lpad(g::text, 6, '0') AS n FROM generate_series(1, $1::int) g) s`,
	},
}

// metaLists returns every list the API serves from the feed's meta cache,
// each reduced to its names.
func metaLists(store *Store, feed string) map[string]func(context.Context, string) ([]string, error) {
	hostEntries := func(ctx context.Context, search string) ([]string, error) {
		entries, err := store.ListAnalysisHostEntries(ctx, feed, search)
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Hostname
		}
		return names, err
	}
	switch feed {
	case "netlog":
		return map[string]func(context.Context, string) ([]string, error){
			"hosts": store.ListNetlogHosts, "programs": store.ListNetlogPrograms, "tags": store.ListNetlogTags,
			"analysis hosts": hostEntries,
		}
	case "srvlog":
		return map[string]func(context.Context, string) ([]string, error){
			"hosts": store.ListSrvlogHosts, "programs": store.ListSrvlogPrograms, "tags": store.ListSrvlogTags,
			"analysis hosts": hostEntries,
		}
	default:
		return map[string]func(context.Context, string) ([]string, error){
			"services": store.ListServices, "components": store.ListComponents, "hosts": store.ListAppLogHosts,
			"analysis services": func(ctx context.Context, search string) ([]string, error) {
				entries, err := store.ListAnalysisServiceEntries(ctx, search)
				names := make([]string, len(entries))
				for i, e := range entries {
					names[i] = e.Service
				}
				return names, err
			},
		}
	}
}

// TestIntegration_MetaSearchAndPruneAfterFlood logs more than metaLimit names
// that sort ahead of a real value and contain it: the default lists leave the
// real value out, and a search for it returns it first. Once the flood's
// events have aged out, the prune removes the flood names and the real value
// is back in the default lists.
func TestIntegration_MetaSearchAndPruneAfterFlood(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()
	const realName = "zz-core-1"
	retention := RetentionConfig{SrvlogDays: 90, NetlogDays: 90, AppLogDays: 90}

	for _, f := range metaFeeds {
		t.Run(f.name, func(t *testing.T) {
			truncate(t, pool, f.events, f.cache)
			t.Cleanup(func() { truncate(t, pool, f.events, f.cache) })
			if _, err := pool.Exec(ctx, f.flood, metaLimit+1); err != nil {
				t.Fatalf("log flood: %v", err)
			}
			if _, err := pool.Exec(ctx, f.insert, time.Now().UTC(), realName); err != nil {
				t.Fatalf("log real event: %v", err)
			}

			lists := metaLists(store, f.name)
			for name, list := range lists {
				all, err := list(ctx, "")
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if len(all) != metaLimit || slices.Contains(all, realName) {
					t.Errorf("%s during the flood: %d names, real listed %v; want %d without it",
						name, len(all), slices.Contains(all, realName), metaLimit)
				}
				for _, q := range []string{realName, "ZZ-CORE-1"} {
					got, err := list(ctx, q)
					if err != nil {
						t.Fatalf("%s search %q: %v", name, q, err)
					}
					if len(got) == 0 || got[0] != realName {
						first := ""
						if len(got) > 0 {
							first = got[0]
						}
						t.Errorf("%s search %q: first of %d = %q, want %q", name, q, len(got), first, realName)
					}
				}
			}

			// The flood's events leave retention, and its names stop being
			// refreshed. One name was last seen just inside the prune's slack
			// past retention and stays.
			if _, err := pool.Exec(ctx, "DELETE FROM "+f.events+" WHERE "+f.column+" <> $1", realName); err != nil {
				t.Fatalf("drop flood events: %v", err)
			}
			if _, err := pool.Exec(ctx, "UPDATE "+f.cache+` SET last_seen_at = CASE WHEN value = 'a-zz-core-1-000001'
				THEN now() - INTERVAL '91 days' ELSE now() - INTERVAL '92 days' END WHERE value <> $1`, realName); err != nil {
				t.Fatalf("age flood names: %v", err)
			}
			if _, err := store.PruneMetaCaches(ctx, retention); err != nil {
				t.Fatalf("PruneMetaCaches: %v", err)
			}

			var left int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+f.cache+" WHERE value <> $1", realName).Scan(&left); err != nil {
				t.Fatalf("count flood names: %v", err)
			}
			// The kept name is cached once per column, and every cache holds
			// three columns.
			if left != 3 {
				t.Errorf("flood names left after prune = %d, want 3", left)
			}
			for name, list := range lists {
				all, err := list(ctx, "")
				if err != nil {
					t.Fatalf("%s after prune: %v", name, err)
				}
				if !slices.Contains(all, realName) {
					t.Errorf("%s after prune = %v, want it to list %q", name, all, realName)
				}
			}
		})
	}
}

// metaLastSeen reads one cached value's last_seen_at.
func metaLastSeen(t *testing.T, pool *pgxpool.Pool, f metaFeed, value string) time.Time {
	t.Helper()
	var seen time.Time
	if err := pool.QueryRow(context.Background(),
		"SELECT last_seen_at FROM "+f.cache+" WHERE column_name = $1 AND value = $2",
		f.column, value).Scan(&seen); err != nil {
		t.Fatalf("read last_seen_at of %s: %v", value, err)
	}
	return seen
}

// TestIntegration_MetaLastSeenThrottled checks the triggers record when a
// value last logged, writing it at most once an hour per value and never
// moving it back for an older event.
func TestIntegration_MetaLastSeenThrottled(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	t0 := time.Now().UTC().Add(-6 * time.Hour).Truncate(time.Microsecond)

	for _, f := range metaFeeds {
		t.Run(f.name, func(t *testing.T) {
			truncate(t, pool, f.events, f.cache)
			t.Cleanup(func() { truncate(t, pool, f.events, f.cache) })
			for _, step := range []struct {
				name       string
				receivedAt time.Time
				want       time.Time
			}{
				{"first event caches the value", t0, t0},
				{"event inside the hour leaves it", t0.Add(30 * time.Minute), t0},
				{"event past the hour refreshes it", t0.Add(2 * time.Hour), t0.Add(2 * time.Hour)},
				{"older event never moves it back", t0.Add(-3 * time.Hour), t0.Add(2 * time.Hour)},
			} {
				if _, err := pool.Exec(ctx, f.insert, step.receivedAt, "core-1"); err != nil {
					t.Fatalf("%s: log event: %v", step.name, err)
				}
				if got := metaLastSeen(t, pool, f, "core-1"); !got.Equal(step.want) {
					t.Errorf("%s: last_seen_at = %v, want %v", step.name, got, step.want)
				}
			}
		})
	}
}

// TestIntegration_MetaTriggerDoesNotWaitOnHeldRows logs a value while another
// open transaction holds its cache row, as a concurrent ingest batch does
// after refreshing it. The second insert must not wait for the first to end:
// batches that waited on each other's rows could deadlock.
func TestIntegration_MetaTriggerDoesNotWaitOnHeldRows(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	for _, f := range metaFeeds {
		t.Run(f.name, func(t *testing.T) {
			truncate(t, pool, f.events, f.cache)
			t.Cleanup(func() { truncate(t, pool, f.events, f.cache) })
			// The first event also creates the chunk the later ones land in,
			// so neither insert below waits on the other's chunk creation.
			if _, err := pool.Exec(ctx, f.insert, time.Now().UTC(), "core-1"); err != nil {
				t.Fatalf("log first event: %v", err)
			}
			if _, err := pool.Exec(ctx, "UPDATE "+f.cache+" SET last_seen_at = now() - INTERVAL '2 hours'"); err != nil {
				t.Fatalf("age cache rows: %v", err)
			}

			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer tx.Rollback(ctx) //nolint:errcheck // Rollback after commit is a no-op.
			now := time.Now().UTC().Truncate(time.Microsecond)
			if _, err := tx.Exec(ctx, f.insert, now, "core-1"); err != nil {
				t.Fatalf("log event in the open transaction: %v", err)
			}

			waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if _, err := pool.Exec(waitCtx, f.insert, now, "core-1"); err != nil {
				t.Fatalf("log event while the row is held: %v", err)
			}

			if err := tx.Commit(ctx); err != nil {
				t.Fatalf("commit: %v", err)
			}
			if got := metaLastSeen(t, pool, f, "core-1"); !got.Equal(now) {
				t.Errorf("last_seen_at = %v, want %v", got, now)
			}
		})
	}
}

// TestIntegration_MetaLastSeenBackfill runs migration 28 over cache rows
// written before it, which have no last_seen_at: each gets one, and so does
// a row written later without it.
func TestIntegration_MetaLastSeenBackfill(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	up, err := os.ReadFile("../../migrations/000028_meta_cache_last_seen.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	for _, f := range metaFeeds {
		truncate(t, pool, f.cache)
		if _, err := pool.Exec(ctx, "INSERT INTO "+f.cache+" (column_name, value, last_seen_at) VALUES ($1, 'cached-before', NULL)", f.column); err != nil {
			t.Fatalf("seed %s: %v", f.cache, err)
		}
	}
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	for _, f := range metaFeeds {
		t.Run(f.name, func(t *testing.T) {
			t.Cleanup(func() { truncate(t, pool, f.cache) })
			if _, err := pool.Exec(ctx, "INSERT INTO "+f.cache+" (column_name, value) VALUES ($1, 'cached-after')", f.column); err != nil {
				t.Fatalf("insert without last_seen_at: %v", err)
			}
			for _, value := range []string{"cached-before", "cached-after"} {
				if got := metaLastSeen(t, pool, f, value); time.Since(got) > time.Minute {
					t.Errorf("%s last_seen_at = %v, want about now", value, got)
				}
			}
		})
	}
}
