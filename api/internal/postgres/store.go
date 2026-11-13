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

// metaPruneSlack is how long past a feed's retention a meta cache value is
// kept after its last_seen_at. The triggers refresh last_seen_at at most once
// an hour (migration 000028), and retention drops events in whole one-day
// chunks, so a value's last event can be up to this much newer than the
// cutoff alone suggests.
const metaPruneSlack = "1 day 1 hour"

// PruneMetaCaches deletes the meta cache values whose last event is older
// than their feed's retention, so the filter lists stop offering values that
// can no longer have events. It returns the number of values deleted.
func (s *Store) PruneMetaCaches(ctx context.Context, cfg RetentionConfig) (int64, error) {
	caches := []struct {
		table string
		days  int
	}{
		{"srvlog_meta_cache", cfg.SrvlogDays},
		{"netlog_meta_cache", cfg.NetlogDays},
		{"applog_meta_cache", cfg.AppLogDays},
	}

	var total int64
	for _, c := range caches {
		//nolint:gosec // table names and metaPruneSlack are hardcoded constants, not user input.
		// A row with no last_seen_at was cached by the trigger that ran
		// before migration 000028; it counts as stale, and logs back in on
		// its next event.
		query := "DELETE FROM " + c.table +
			" WHERE last_seen_at IS NULL OR last_seen_at < now() - make_interval(days => $1) - INTERVAL '" + metaPruneSlack + "'"
		tag, err := s.pool.Exec(ctx, query, c.days)
		if err != nil {
			return total, fmt.Errorf("prune %s: %w", c.table, err)
		}
		total += tag.RowsAffected()
	}
	return total, nil
}

// listMetaStrings reads cached string values for a whitelisted column from a
// *_meta_cache table, at most metaLimit of them in value order. A non-empty
// search narrows the list as metaSearch describes. The srvlog, netlog and
// applog meta queries were identical apart from the cache table and the
// column whitelist, so the query/scan body lives here once; callers pass the
// table and their allow-list.
func (s *Store) listMetaStrings(ctx context.Context, cacheTable, column, search string, allowed map[string]struct{}) ([]string, error) {
	if _, ok := allowed[column]; !ok {
		return nil, fmt.Errorf("disallowed meta column: %s", column)
	}
	where, order, args := metaSearch("value", search, []any{column})
	args = append(args, metaLimit)
	//nolint:gosec // cacheTable is a hardcoded literal from callers; where and order hold only placeholders.
	query := fmt.Sprintf("SELECT value FROM %s WHERE column_name = $1%s ORDER BY %s LIMIT $%d",
		cacheTable, where, order, len(args))
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", column, err)
	}

	values, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", column, err)
	}
	return values, nil
}

// metaSearch returns the WHERE and ORDER BY parts that narrow a meta cache
// query on col to the values containing search, case-insensitively. A value
// equal to search sorts first, then values equal to it ignoring case, so
// neither values that merely contain the name nor its case variants can push
// it past the list's cap. args are the query's
// arguments so far; they come back with the search arguments appended. An
// empty search narrows nothing and orders by col.
func metaSearch(col, search string, args []any) (where, order string, _ []any) {
	if search == "" {
		return "", col, args
	}
	args = append(args, "%"+escapeLike(search)+"%", search)
	where = fmt.Sprintf(" AND %s ILIKE $%d", col, len(args)-1)
	order = fmt.Sprintf("%[1]s = $%[2]d DESC, lower(%[1]s) = lower($%[2]d) DESC, %[1]s", col, len(args))
	return where, order, args
}

// escapeLike escapes LIKE/ILIKE metacharacters so they are treated as literals.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// getVolume returns time-bucketed event counts of table grouped by groupCol.
// Only the model.VolumeGroupLimit groups with the most events over the range
// are broken out; the rest are summed into each bucket's Other, so the
// response size does not grow with the number of distinct groups. With
// p.TotalsOnly it skips the breakdown and returns only per-bucket totals.
func (s *Store) getVolume(ctx context.Context, table, groupCol string, p model.VolumeParams) ([]model.VolumeBucket, error) {
	if !p.Interval.IsValid() {
		return nil, fmt.Errorf("invalid volume interval: %s", p.Interval)
	}
	since := time.Now().UTC().Add(-p.RangeDur)

	query := fmt.Sprintf(
		`WITH counts AS (
		     SELECT time_bucket($1::interval, received_at) AS bucket,
		            %s AS grp, count(*) AS cnt
		     FROM %s
		     WHERE received_at >= $2
		     GROUP BY bucket, grp
		 ), top_groups AS (
		     SELECT grp FROM counts
		     GROUP BY grp
		     ORDER BY sum(cnt) DESC, grp
		     LIMIT $3
		 )
		 SELECT c.bucket, t.grp, sum(c.cnt)::bigint AS cnt
		 FROM counts c
		 LEFT JOIN top_groups t ON t.grp = c.grp
		 GROUP BY c.bucket, t.grp
		 ORDER BY c.bucket ASC`, groupCol, table)
	args := []any{p.Interval.String(), since, model.VolumeGroupLimit}
	if p.TotalsOnly {
		query = fmt.Sprintf(
			`SELECT time_bucket($1::interval, received_at) AS bucket,
			        NULL::text AS grp, count(*) AS cnt
			 FROM %s
			 WHERE received_at >= $2
			 GROUP BY bucket
			 ORDER BY bucket ASC`, table)
		args = args[:2]
	}

	rows, err := s.pool.Query(ctx, query, args...)
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
			group  *string
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
		if p.TotalsOnly {
			continue
		}
		if group == nil {
			buckets[i].Other += cnt
		} else {
			buckets[i].ByHost[*group] = cnt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s volume rows: %w", table, err)
	}

	return buckets, nil
}

// getSyslogSummary returns summary statistics for a syslog-plane event feed
// over the given range, reading from its hourly continuous aggregate. The
// srvlog and netlog summaries were identical apart from the aggregate table,
// so the query/scan body lives here once (mirrors getVolume); callers pass
// their feed's *_summary_hourly view.
func (s *Store) getSyslogSummary(ctx context.Context, aggTable string, rangeDur time.Duration) (model.SyslogSummary, error) {
	since := time.Now().UTC().Add(-rangeDur)
	prevStart := since.Add(-rangeDur)

	// Get current totals by severity from the continuous aggregate.
	query := fmt.Sprintf(
		`SELECT severity, SUM(cnt) AS cnt
		 FROM %s
		 WHERE bucket >= $1
		 GROUP BY severity
		 ORDER BY severity`, aggTable)

	rows, err := s.pool.Query(ctx, query, since)
	if err != nil {
		return model.SyslogSummary{}, fmt.Errorf("%s summary query: %w", aggTable, err)
	}
	defer rows.Close()

	var summary model.SyslogSummary
	summary.SeverityBreakdown = make([]model.SeverityCount, 0)
	summary.TopHosts = make([]model.TopSource, 0)

	for rows.Next() {
		var sev int
		var cnt int64
		if err := rows.Scan(&sev, &cnt); err != nil {
			return model.SyslogSummary{}, fmt.Errorf("scan %s severity: %w", aggTable, err)
		}
		summary.Total += cnt
		if sev <= model.SeverityErr {
			summary.Errors += cnt
		}
		if sev == model.SeverityWarning {
			summary.Warnings += cnt
		}
		summary.SeverityBreakdown = append(summary.SeverityBreakdown, model.SeverityCount{
			Severity: sev,
			Label:    model.SeverityLabel(sev),
			Count:    cnt,
		})
	}
	if err := rows.Err(); err != nil {
		return model.SyslogSummary{}, fmt.Errorf("%s severity rows: %w", aggTable, err)
	}

	// Get previous period total for trend calculation.
	var prevTotal int64
	err = s.pool.QueryRow(ctx,
		fmt.Sprintf("SELECT COALESCE(SUM(cnt), 0) FROM %s WHERE bucket >= $1 AND bucket < $2", aggTable),
		prevStart, since).Scan(&prevTotal)
	if err != nil {
		return model.SyslogSummary{}, fmt.Errorf("%s prev total query: %w", aggTable, err)
	}

	if prevTotal > 0 {
		summary.Trend = float64(summary.Total-prevTotal) / float64(prevTotal) * 100
	}

	// Get top hosts from the continuous aggregate.
	hostQuery := fmt.Sprintf(
		`SELECT hostname, SUM(cnt) AS cnt
		 FROM %s
		 WHERE bucket >= $1
		 GROUP BY hostname
		 ORDER BY cnt DESC
		 LIMIT $2`, aggTable)

	hostRows, err := s.pool.Query(ctx, hostQuery, since, topSourcesLimit)
	if err != nil {
		return model.SyslogSummary{}, fmt.Errorf("%s top hosts query: %w", aggTable, err)
	}

	hosts, err := pgx.CollectRows(hostRows, func(row pgx.CollectableRow) (model.TopSource, error) {
		var ts model.TopSource
		err := row.Scan(&ts.Name, &ts.Count)
		return ts, err
	})
	if err != nil {
		return model.SyslogSummary{}, fmt.Errorf("scan %s host: %w", aggTable, err)
	}
	summary.TopHosts = append(summary.TopHosts, hosts...)

	// Calculate percentages for top hosts.
	for i := range summary.TopHosts {
		if summary.Total > 0 {
			summary.TopHosts[i].Pct = float64(summary.TopHosts[i].Count) / float64(summary.Total) * 100
		}
	}

	// Calculate percentages for severity breakdown.
	for i := range summary.SeverityBreakdown {
		if summary.Total > 0 {
			summary.SeverityBreakdown[i].Pct = float64(summary.SeverityBreakdown[i].Count) / float64(summary.Total) * 100
		}
	}

	return summary, nil
}
