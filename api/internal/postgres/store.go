package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/taillight/internal/model"
)

var psq = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

const (
	// metaLimit caps the number of distinct values returned by meta queries.
	metaLimit = 10000
	// topSourcesLimit caps the number of top hosts/services returned in summaries.
	topSourcesLimit = 20
)

// Store provides query methods backed by a pgx connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new Store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Ping checks database connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// RetentionConfig specifies retention periods for each hypertable.
type RetentionConfig struct {
	SrvlogDays          int
	NetlogDays          int
	AppLogDays          int
	NotificationLogDays int
	RsyslogStatsDays    int
	MetricsDays         int
}

// ApplyRetentionPolicies updates TimescaleDB retention policies to match the given config.
func (s *Store) ApplyRetentionPolicies(ctx context.Context, cfg RetentionConfig) error {
	tables := []struct {
		name string
		days int
	}{
		{"srvlog_events", cfg.SrvlogDays},
		{"netlog_events", cfg.NetlogDays},
		{"applog_events", cfg.AppLogDays},
		{"notification_log", cfg.NotificationLogDays},
		{"rsyslog_stats", cfg.RsyslogStatsDays},
		{"taillight_metrics", cfg.MetricsDays},
	}

	for _, t := range tables {
		interval := fmt.Sprintf("%d days", t.days)
		if _, err := s.pool.Exec(ctx, "SELECT remove_retention_policy($1, if_exists => true)", t.name); err != nil {
			return fmt.Errorf("remove retention policy for %s: %w", t.name, err)
		}
		if _, err := s.pool.Exec(ctx, "SELECT add_retention_policy($1, INTERVAL '1 day' * $2, if_not_exists => true)", t.name, t.days); err != nil {
			return fmt.Errorf("add retention policy for %s (%s): %w", t.name, interval, err)
		}
	}
	return nil
}

// RefreshContinuousAggregates seeds the TimescaleDB continuous aggregates
// so that real-time aggregation has a watermark to work from. This must
// run outside a transaction (CALL cannot run inside BEGIN/COMMIT), so it
// is called at application startup rather than in a SQL migration.
func (s *Store) RefreshContinuousAggregates(ctx context.Context) error {
	for _, view := range []string{"srvlog_summary_hourly", "netlog_summary_hourly", "applog_summary_hourly"} {
		//nolint:gosec // view names are hardcoded constants, not user input.
		if _, err := s.pool.Exec(ctx,
			fmt.Sprintf("CALL refresh_continuous_aggregate('%s', NULL, now())", view),
		); err != nil {
			return fmt.Errorf("refresh %s: %w", view, err)
		}
	}
	return nil
}

// listMetaStrings reads distinct cached string values for a whitelisted column
// from a *_meta_cache table. The srvlog and netlog meta queries were identical
// apart from the cache table and the column whitelist, so the query/scan body
// lives here once; callers pass the table and their allow-list.
func (s *Store) listMetaStrings(ctx context.Context, cacheTable, column string, allowed map[string]struct{}) ([]string, error) {
	if _, ok := allowed[column]; !ok {
		return nil, fmt.Errorf("disallowed meta column: %s", column)
	}
	//nolint:gosec // cacheTable is a hardcoded literal from callers, not user input
	query := "SELECT value FROM " + cacheTable + " WHERE column_name = $1 ORDER BY value LIMIT $2"
	rows, err := s.pool.Query(ctx, query, column, metaLimit)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", column, err)
	}

	values, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", column, err)
	}
	return values, nil
}

// escapeLike escapes LIKE/ILIKE metacharacters so they are treated as literals.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func (s *Store) getVolume(ctx context.Context, table, groupCol string, interval model.VolumeInterval, rangeDur time.Duration) ([]model.VolumeBucket, error) {
	if !interval.IsValid() {
		return nil, fmt.Errorf("invalid volume interval: %s", interval)
	}
	since := time.Now().UTC().Add(-rangeDur)

	query := fmt.Sprintf(
		`SELECT time_bucket($1::interval, received_at) AS bucket,
		        %s, count(*) AS cnt
		 FROM %s
		 WHERE received_at >= $2
		 GROUP BY bucket, %s
		 ORDER BY bucket ASC`, groupCol, table, groupCol)

	rows, err := s.pool.Query(ctx, query, interval.String(), since)
	if err != nil {
		return nil, fmt.Errorf("%s volume query: %w", table, err)
	}
	defer rows.Close()

	type key = time.Time
	idx := make(map[key]int)
	var buckets []model.VolumeBucket

	for rows.Next() {
		var (
			bucket time.Time
			group  string
			cnt    int64
		)
		if err := rows.Scan(&bucket, &group, &cnt); err != nil {
			return nil, fmt.Errorf("scan %s volume row: %w", table, err)
		}

		i, ok := idx[bucket]
		if !ok {
			i = len(buckets)
			idx[bucket] = i
			buckets = append(buckets, model.VolumeBucket{
				Time:   bucket,
				ByHost: make(map[string]int64),
			})
		}
		buckets[i].Total += cnt
		buckets[i].ByHost[group] = cnt
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s volume rows: %w", table, err)
	}

	return buckets, nil
}
