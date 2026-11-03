package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
)

// topErrorsPerHost caps the number of top error patterns returned per host.
const topErrorsPerHost = 4

// hostSortOrder maps a model.HostSorts key to its ORDER BY clause. Hostname
// breaks ties so pages are stable.
var hostSortOrder = map[string]string{
	"errors":    "errors DESC, hostname",
	"total":     "total DESC, hostname",
	"hostname":  "hostname",
	"last_seen": "last_seen_at DESC NULLS LAST, hostname",
	"trend":     "trend DESC, hostname",
}

// ListHosts returns one page of per-host stats for the hosts overview,
// across the srvlog and netlog tables. The index query computes each host's
// totals, feed, status and trend and applies the search, filters, sort and
// paging in SQL, so only one page of hosts reaches the API. The detail
// queries then run for that page's hosts only.
func (s *Store) ListHosts(ctx context.Context, q model.HostQuery) (model.HostPage, error) {
	since := time.Now().UTC().Add(-q.RangeDur)
	prevStart := since.Add(-q.RangeDur)
	sparkSince := time.Now().UTC().Add(-24 * time.Hour)
	msgSince := time.Now().UTC().Add(-24 * time.Hour)

	entries, total, err := s.listHostIndex(ctx, q, since, prevStart)
	if err != nil {
		return model.HostPage{}, err
	}
	if len(entries) == 0 {
		return model.HostPage{Hosts: entries, Total: total}, nil
	}

	names := make([]string, len(entries))
	for i := range entries {
		names[i] = entries[i].Hostname
	}

	// Send the 3 detail queries in a single round-trip.
	batch := &pgx.Batch{}
	batch.Queue(hostSeverityQuery, since, names)
	batch.Queue(hostHourlyQuery, sparkSince, names)
	batch.Queue(hostTopErrorsQuery, msgSince, topErrorsPerHost, names)

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close() //nolint:errcheck // best-effort close

	sevByHost, err := scanHostSeverities(results)
	if err != nil {
		return model.HostPage{}, err
	}
	hourlyByHost, err := scanHostHourly(results)
	if err != nil {
		return model.HostPage{}, err
	}
	errorsByHost, err := scanHostTopErrors(results)
	if err != nil {
		return model.HostPage{}, err
	}

	for i := range entries {
		e := &entries[i]
		e.SeverityBreakdown = sevByHost[e.Hostname]
		if e.SeverityBreakdown == nil {
			e.SeverityBreakdown = make([]model.SeverityCount, 0)
		}
		for j := range e.SeverityBreakdown {
			if e.TotalCount > 0 {
				e.SeverityBreakdown[j].Pct = float64(e.SeverityBreakdown[j].Count) / float64(e.TotalCount) * 100
			}
		}
		e.HourlyBuckets = hourlyByHost[e.Hostname]
		if e.HourlyBuckets == nil {
			e.HourlyBuckets = make([]model.HourlyBucket, 0)
		}
		e.TopErrors = errorsByHost[e.Hostname]
		if e.TopErrors == nil {
			e.TopErrors = make([]model.TopSource, 0)
		}
	}

	return model.HostPage{Hosts: entries, Total: total}, nil
}

// hostIndexCTE ranks every host seen in the current period ($1) of the
// srvlog and netlog hourly aggregates. prev covers the previous period ($2 up
// to $1) for the trend; seen takes the precise last-seen time from raw events,
// since the aggregates' MAX(bucket) is hour-aligned. A host whose error share
// is above 3% is critical, one with any errors a warning.
const hostIndexCTE = `WITH cur AS (
    SELECT hostname, 'srvlog' AS feed, SUM(cnt) AS total,
           SUM(cnt) FILTER (WHERE severity <= 3) AS errors, MAX(bucket) AS last_bucket
    FROM srvlog_summary_hourly WHERE bucket >= $1
    GROUP BY hostname
    UNION ALL
    SELECT hostname, 'netlog' AS feed, SUM(cnt) AS total,
           SUM(cnt) FILTER (WHERE severity <= 3) AS errors, MAX(bucket) AS last_bucket
    FROM netlog_summary_hourly WHERE bucket >= $1
    GROUP BY hostname
), prev AS (
    SELECT hostname, SUM(cnt)::bigint AS prev_total FROM (
        SELECT hostname, cnt FROM srvlog_summary_hourly WHERE bucket >= $2 AND bucket < $1
        UNION ALL
        SELECT hostname, cnt FROM netlog_summary_hourly WHERE bucket >= $2 AND bucket < $1
    ) p
    GROUP BY hostname
), seen AS (
    SELECT hostname, MAX(last_seen) AS last_seen FROM (
        SELECT hostname, MAX(received_at) AS last_seen
        FROM srvlog_events WHERE received_at >= $1
        GROUP BY hostname
        UNION ALL
        SELECT hostname, MAX(received_at) AS last_seen
        FROM netlog_events WHERE received_at >= $1
        GROUP BY hostname
    ) e
    GROUP BY hostname
), hosts AS (
    SELECT hostname,
           CASE WHEN COUNT(*) > 1 THEN 'both' ELSE MIN(feed) END AS feed,
           SUM(total)::bigint AS total,
           COALESCE(SUM(errors), 0)::bigint AS errors,
           MAX(last_bucket) AS last_bucket
    FROM cur
    GROUP BY hostname
), ranked AS (
    SELECT h.hostname, h.feed, h.total, h.errors,
           GREATEST(h.last_bucket, s.last_seen) AS last_seen_at,
           CASE WHEN h.total > 0 AND h.errors::float8 / h.total > 0.03 THEN 'critical'
                WHEN h.errors > 0 THEN 'warning'
                ELSE 'healthy' END AS status,
           CASE WHEN p.prev_total > 0
                THEN (h.total - p.prev_total)::float8 / p.prev_total * 100
                ELSE 0 END AS trend
    FROM hosts h
    LEFT JOIN prev p ON p.hostname = h.hostname
    LEFT JOIN seen s ON s.hostname = h.hostname
)`

// listHostIndex runs the index query: the hosts matching q, sorted and cut to
// one page, plus the number of hosts that match. The count comes from its own
// subquery so it stays right when the offset is past the last match.
func (s *Store) listHostIndex(ctx context.Context, q model.HostQuery, since, prevStart time.Time) ([]model.HostEntry, int, error) {
	order, ok := hostSortOrder[q.Sort]
	if !ok {
		return nil, 0, fmt.Errorf("invalid host sort: %q", q.Sort)
	}

	args := []any{since, prevStart}
	where := []string{"true"}
	if q.Search != "" {
		args = append(args, "%"+escapeLike(q.Search)+"%")
		where = append(where, fmt.Sprintf("hostname ILIKE $%d", len(args)))
	}
	if q.Status != "" {
		args = append(args, string(q.Status))
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if q.Feed != "" {
		args = append(args, q.Feed)
		where = append(where, fmt.Sprintf("feed = $%d", len(args)))
	}
	args = append(args, q.Limit, q.Offset)

	//nolint:gosec // order comes from hostSortOrder and where holds only placeholders.
	query := hostIndexCTE + fmt.Sprintf(`, filtered AS (
    SELECT * FROM ranked WHERE %s
)
SELECT f.matched, p.hostname, p.feed, p.status, p.last_seen_at, p.total, p.errors, p.trend
FROM (SELECT count(*) AS matched FROM filtered) f
LEFT JOIN LATERAL (
    SELECT * FROM filtered ORDER BY %s LIMIT $%d OFFSET $%d
) p ON true
ORDER BY %s`, strings.Join(where, " AND "), order, len(args)-1, len(args), order)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("hosts index: %w", err)
	}
	defer rows.Close()

	entries := make([]model.HostEntry, 0, q.Limit)
	var matched int
	for rows.Next() {
		var (
			hostname, feed, status *string
			lastSeen               *time.Time
			total, errs            *int64
			trend                  *float64
		)
		if err := rows.Scan(&matched, &hostname, &feed, &status, &lastSeen, &total, &errs, &trend); err != nil {
			return nil, 0, fmt.Errorf("hosts index scan: %w", err)
		}
		if hostname == nil {
			continue // the page is empty; the row only carries the count
		}
		entries = append(entries, model.HostEntry{
			Hostname:   *hostname,
			Feed:       *feed,
			Status:     model.HostStatus(*status),
			LastSeenAt: lastSeen,
			TotalCount: *total,
			ErrorCount: *errs,
			Trend:      *trend,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("hosts index rows: %w", err)
	}
	return entries, matched, nil
}

// hostSeverityQuery is the per-host severity breakdown (current period) for
// the hosts in $2.
const hostSeverityQuery = `SELECT hostname, severity, SUM(cnt)::bigint AS cnt FROM (
    SELECT hostname, severity, cnt FROM srvlog_summary_hourly
    WHERE bucket >= $1 AND hostname = ANY($2)
    UNION ALL
    SELECT hostname, severity, cnt FROM netlog_summary_hourly
    WHERE bucket >= $1 AND hostname = ANY($2)
) c
GROUP BY hostname, severity
ORDER BY hostname, severity`

// hostHourlyQuery is the hourly activity (always last 24h) for the hosts in $2.
const hostHourlyQuery = `WITH combined AS (
    SELECT hostname, bucket AS hr, SUM(cnt) AS cnt,
           SUM(CASE WHEN severity <= 3 THEN cnt ELSE 0 END) AS err_cnt
    FROM srvlog_summary_hourly WHERE bucket >= $1 AND hostname = ANY($2)
    GROUP BY hostname, bucket
    UNION ALL
    SELECT hostname, bucket AS hr, SUM(cnt) AS cnt,
           SUM(CASE WHEN severity <= 3 THEN cnt ELSE 0 END) AS err_cnt
    FROM netlog_summary_hourly WHERE bucket >= $1 AND hostname = ANY($2)
    GROUP BY hostname, bucket
)
SELECT hostname, hr, SUM(cnt) AS count, SUM(err_cnt) AS error_count
FROM combined
GROUP BY hostname, hr
ORDER BY hostname, hr`

// hostTopErrorsQuery is the top error patterns (24h) for the hosts in $3.
const hostTopErrorsQuery = `WITH ranked AS (
    SELECT hostname, msg_pattern AS pattern, count(*) AS cnt,
           ROW_NUMBER() OVER (PARTITION BY hostname ORDER BY count(*) DESC) AS rn
    FROM srvlog_events
    WHERE received_at >= $1 AND severity <= 3 AND msg_pattern != '' AND hostname = ANY($3)
    GROUP BY hostname, msg_pattern
    UNION ALL
    SELECT hostname, msg_pattern AS pattern, count(*) AS cnt,
           ROW_NUMBER() OVER (PARTITION BY hostname ORDER BY count(*) DESC) AS rn
    FROM netlog_events
    WHERE received_at >= $1 AND severity <= 3 AND msg_pattern != '' AND hostname = ANY($3)
    GROUP BY hostname, msg_pattern
)
SELECT hostname, pattern, cnt FROM ranked WHERE rn <= $2
ORDER BY hostname, cnt DESC`

// scanHostSeverities reads the severity breakdown per host.
func scanHostSeverities(results pgx.BatchResults) (map[string][]model.SeverityCount, error) {
	sevByHost := make(map[string][]model.SeverityCount)
	rows, err := results.Query()
	if err != nil {
		return nil, fmt.Errorf("hosts severities: %w", err)
	}
	for rows.Next() {
		var hostname string
		var severity int
		var cnt int64
		if err := rows.Scan(&hostname, &severity, &cnt); err != nil {
			return nil, fmt.Errorf("hosts severities scan: %w", err)
		}
		sevByHost[hostname] = append(sevByHost[hostname], model.SeverityCount{
			Severity: severity,
			Label:    model.SeverityLabel(severity),
			Count:    cnt,
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hosts severities rows: %w", err)
	}
	return sevByHost, nil
}

// scanHostHourly reads the hourly activity buckets.
func scanHostHourly(results pgx.BatchResults) (map[string][]model.HourlyBucket, error) {
	hourlyByHost := make(map[string][]model.HourlyBucket)
	rows, err := results.Query()
	if err != nil {
		return nil, fmt.Errorf("hosts hourly: %w", err)
	}
	for rows.Next() {
		var hostname string
		var bucket time.Time
		var count, errCount int64
		if err := rows.Scan(&hostname, &bucket, &count, &errCount); err != nil {
			return nil, fmt.Errorf("hosts hourly scan: %w", err)
		}
		hourlyByHost[hostname] = append(hourlyByHost[hostname], model.HourlyBucket{
			Bucket:     bucket,
			Count:      count,
			ErrorCount: errCount,
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hosts hourly rows: %w", err)
	}
	return hourlyByHost, nil
}

// scanHostTopErrors reads the top error patterns per host.
func scanHostTopErrors(results pgx.BatchResults) (map[string][]model.TopSource, error) {
	errorsByHost := make(map[string][]model.TopSource)
	rows, err := results.Query()
	if err != nil {
		return nil, fmt.Errorf("hosts top errors: %w", err)
	}
	for rows.Next() {
		var hostname, pattern string
		var cnt int64
		if err := rows.Scan(&hostname, &pattern, &cnt); err != nil {
			return nil, fmt.Errorf("hosts top errors scan: %w", err)
		}
		errorsByHost[hostname] = append(errorsByHost[hostname], model.TopSource{
			Name:  pattern,
			Count: cnt,
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hosts top errors rows: %w", err)
	}
	return errorsByHost, nil
}
