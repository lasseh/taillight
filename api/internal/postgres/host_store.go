package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
)

// topErrorsPerHost caps the number of top error patterns returned per host.
const topErrorsPerHost = 4

// ListHosts returns per-host stats for the hosts overview page.
// When includeNetlog is true the queries union srvlog and netlog tables.
func (s *Store) ListHosts(ctx context.Context, rangeDur time.Duration, includeNetlog bool) ([]model.HostEntry, error) {
	since := time.Now().UTC().Add(-rangeDur)
	prevStart := since.Add(-rangeDur)
	sparkSince := time.Now().UTC().Add(-24 * time.Hour)
	msgSince := time.Now().UTC().Add(-24 * time.Hour)

	// Build Q1: per-host severity rows (current period).
	q1 := `WITH combined AS (
    SELECT hostname, severity, SUM(cnt) AS cnt, 'srvlog' AS feed, MAX(bucket) AS last_bucket
    FROM srvlog_summary_hourly WHERE bucket >= $1
    GROUP BY hostname, severity`
	if includeNetlog {
		q1 += `
    UNION ALL
    SELECT hostname, severity, SUM(cnt) AS cnt, 'netlog' AS feed, MAX(bucket) AS last_bucket
    FROM netlog_summary_hourly WHERE bucket >= $1
    GROUP BY hostname, severity`
	}
	q1 += `
)
SELECT hostname, severity, SUM(cnt) AS cnt,
       CASE WHEN COUNT(DISTINCT feed) > 1 THEN 'both' ELSE MIN(feed) END AS feed,
       MAX(last_bucket) AS last_seen_at
FROM combined
GROUP BY hostname, severity
ORDER BY hostname, severity`

	// Build Q2: previous-period totals per host (for trend).
	q2 := `SELECT hostname, COALESCE(SUM(cnt), 0) AS prev_total
FROM srvlog_summary_hourly WHERE bucket >= $1 AND bucket < $2
GROUP BY hostname`
	if includeNetlog {
		q2 += `
UNION ALL
SELECT hostname, COALESCE(SUM(cnt), 0) AS prev_total
FROM netlog_summary_hourly WHERE bucket >= $1 AND bucket < $2
GROUP BY hostname`
	}

	// Build Q3: hourly activity buckets (always last 24h).
	q3 := `WITH combined AS (
    SELECT hostname, bucket AS hr, SUM(cnt) AS cnt,
           SUM(CASE WHEN severity <= 3 THEN cnt ELSE 0 END) AS err_cnt
    FROM srvlog_summary_hourly WHERE bucket >= $1
    GROUP BY hostname, bucket`
	if includeNetlog {
		q3 += `
    UNION ALL
    SELECT hostname, bucket AS hr, SUM(cnt) AS cnt,
           SUM(CASE WHEN severity <= 3 THEN cnt ELSE 0 END) AS err_cnt
    FROM netlog_summary_hourly WHERE bucket >= $1
    GROUP BY hostname, bucket`
	}
	q3 += `
)
SELECT hostname, hr, SUM(cnt) AS count, SUM(err_cnt) AS error_count
FROM combined
GROUP BY hostname, hr
ORDER BY hostname, hr`

	// Build Q4: top error patterns per host (24h, scoped to hostnames from Q1).
	q4 := `WITH ranked AS (
    SELECT hostname, msg_pattern AS pattern, count(*) AS cnt,
           ROW_NUMBER() OVER (PARTITION BY hostname ORDER BY count(*) DESC) AS rn
    FROM srvlog_events
    WHERE received_at >= $1 AND severity <= 3 AND msg_pattern != ''
    GROUP BY hostname, msg_pattern`
	if includeNetlog {
		q4 += `
    UNION ALL
    SELECT hostname, msg_pattern AS pattern, count(*) AS cnt,
           ROW_NUMBER() OVER (PARTITION BY hostname ORDER BY count(*) DESC) AS rn
    FROM netlog_events
    WHERE received_at >= $1 AND severity <= 3 AND msg_pattern != ''
    GROUP BY hostname, msg_pattern`
	}
	q4 += `
)
SELECT hostname, pattern, cnt FROM ranked WHERE rn <= $2
ORDER BY hostname, cnt DESC`

	// Build Q5: precise last_seen_at per host from raw events.
	// The continuous aggregate's MAX(bucket) is hour-aligned, so this query
	// provides the actual most recent received_at timestamp.
	q5 := `SELECT hostname, MAX(received_at) AS last_seen
FROM srvlog_events WHERE received_at >= $1
GROUP BY hostname`
	if includeNetlog {
		q5 += `
UNION ALL
SELECT hostname, MAX(received_at) AS last_seen
FROM netlog_events WHERE received_at >= $1
GROUP BY hostname`
	}

	// Send all 5 queries in a single round-trip.
	batch := &pgx.Batch{}
	batch.Queue(q1, since)
	batch.Queue(q2, prevStart, since)
	batch.Queue(q3, sparkSince)
	batch.Queue(q4, msgSince, topErrorsPerHost)
	batch.Queue(q5, since)

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close() //nolint:errcheck // best-effort close

	// R1: assemble per-host data from severity rows.
	type hostAccum struct {
		feeds     map[string]bool
		lastSeen  *time.Time
		total     int64
		errors    int64
		sevCounts []model.SeverityCount
	}
	hosts := make(map[string]*hostAccum)
	hostOrder := make([]string, 0)

	r1, err := results.Query()
	if err != nil {
		return nil, fmt.Errorf("hosts q1: %w", err)
	}
	for r1.Next() {
		var hostname string
		var severity int
		var cnt int64
		var feed string
		var lastBucket *time.Time
		if err := r1.Scan(&hostname, &severity, &cnt, &feed, &lastBucket); err != nil {
			return nil, fmt.Errorf("hosts q1 scan: %w", err)
		}
		h, ok := hosts[hostname]
		if !ok {
			h = &hostAccum{
				feeds:     make(map[string]bool),
				sevCounts: make([]model.SeverityCount, 0),
			}
			hosts[hostname] = h
			hostOrder = append(hostOrder, hostname)
		}
		h.feeds[feed] = true
		h.total += cnt
		if severity <= model.SeverityErr {
			h.errors += cnt
		}
		if lastBucket != nil && (h.lastSeen == nil || lastBucket.After(*h.lastSeen)) {
			h.lastSeen = lastBucket
		}
		h.sevCounts = append(h.sevCounts, model.SeverityCount{
			Severity: severity,
			Label:    model.SeverityLabel(severity),
			Count:    cnt,
		})
	}
	r1.Close()
	if err := r1.Err(); err != nil {
		return nil, fmt.Errorf("hosts q1 rows: %w", err)
	}

	// Calculate severity percentages.
	for _, h := range hosts {
		for i := range h.sevCounts {
			if h.total > 0 {
				h.sevCounts[i].Pct = float64(h.sevCounts[i].Count) / float64(h.total) * 100
			}
		}
	}

	// R2: previous-period totals for trend.
	prevByHost := make(map[string]int64)
	r2, err := results.Query()
	if err != nil {
		return nil, fmt.Errorf("hosts q2: %w", err)
	}
	for r2.Next() {
		var hostname string
		var prevTotal int64
		if err := r2.Scan(&hostname, &prevTotal); err != nil {
			return nil, fmt.Errorf("hosts q2 scan: %w", err)
		}
		prevByHost[hostname] += prevTotal
	}
	r2.Close()
	if err := r2.Err(); err != nil {
		return nil, fmt.Errorf("hosts q2 rows: %w", err)
	}

	// R3: hourly activity buckets.
	hourlyByHost := make(map[string][]model.HourlyBucket)
	r3, err := results.Query()
	if err != nil {
		return nil, fmt.Errorf("hosts q3: %w", err)
	}
	for r3.Next() {
		var hostname string
		var bucket time.Time
		var count, errCount int64
		if err := r3.Scan(&hostname, &bucket, &count, &errCount); err != nil {
			return nil, fmt.Errorf("hosts q3 scan: %w", err)
		}
		hourlyByHost[hostname] = append(hourlyByHost[hostname], model.HourlyBucket{
			Bucket:     bucket,
			Count:      count,
			ErrorCount: errCount,
		})
	}
	r3.Close()
	if err := r3.Err(); err != nil {
		return nil, fmt.Errorf("hosts q3 rows: %w", err)
	}

	// R4: top error patterns.
	errorsByHost := make(map[string][]model.TopSource)
	r4, err := results.Query()
	if err != nil {
		return nil, fmt.Errorf("hosts q4: %w", err)
	}
	for r4.Next() {
		var hostname, pattern string
		var cnt int64
		if err := r4.Scan(&hostname, &pattern, &cnt); err != nil {
			return nil, fmt.Errorf("hosts q4 scan: %w", err)
		}
		errorsByHost[hostname] = append(errorsByHost[hostname], model.TopSource{
			Name:  pattern,
			Count: cnt,
		})
	}
	r4.Close()
	if err := r4.Err(); err != nil {
		return nil, fmt.Errorf("hosts q4 rows: %w", err)
	}

	// R5: precise last_seen_at from raw events (overrides Q1's bucket-aligned value).
	r5, err := results.Query()
	if err != nil {
		return nil, fmt.Errorf("hosts q5: %w", err)
	}
	for r5.Next() {
		var hostname string
		var lastSeen time.Time
		if err := r5.Scan(&hostname, &lastSeen); err != nil {
			return nil, fmt.Errorf("hosts q5 scan: %w", err)
		}
		if h, ok := hosts[hostname]; ok {
			if h.lastSeen == nil || lastSeen.After(*h.lastSeen) {
				ls := lastSeen
				h.lastSeen = &ls
			}
		}
	}
	r5.Close()
	if err := r5.Err(); err != nil {
		return nil, fmt.Errorf("hosts q5 rows: %w", err)
	}

	// Assemble final result.
	entries := make([]model.HostEntry, 0, len(hostOrder))
	for _, hostname := range hostOrder {
		h := hosts[hostname]

		// Derive feed badge.
		var feed string
		switch {
		case h.feeds["srvlog"] && h.feeds["netlog"]:
			feed = "both"
		case h.feeds["netlog"]:
			feed = "netlog"
		default:
			feed = "srvlog"
		}

		// Derive status.
		var status model.HostStatus
		switch {
		case h.total > 0 && float64(h.errors)/float64(h.total) > 0.03:
			status = model.HostStatusCritical
		case h.errors > 0:
			status = model.HostStatusWarning
		default:
			status = model.HostStatusHealthy
		}

		// Calculate trend.
		var trend float64
		if prev := prevByHost[hostname]; prev > 0 {
			trend = float64(h.total-prev) / float64(prev) * 100
		}

		buckets := hourlyByHost[hostname]
		if buckets == nil {
			buckets = make([]model.HourlyBucket, 0)
		}
		topErrors := errorsByHost[hostname]
		if topErrors == nil {
			topErrors = make([]model.TopSource, 0)
		}

		entries = append(entries, model.HostEntry{
			Hostname:          hostname,
			Feed:              feed,
			Status:            status,
			LastSeenAt:        h.lastSeen,
			TotalCount:        h.total,
			ErrorCount:        h.errors,
			Trend:             trend,
			SeverityBreakdown: h.sevCounts,
			HourlyBuckets:     buckets,
			TopErrors:         topErrors,
		})
	}

	return entries, nil
}
