package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
)

var srvlogColumns = []string{
	"id", "received_at", "reported_at", "hostname", "fromhost_ip",
	"programname", "msgid", "severity", "facility", "syslogtag",
	"structured_data", "message", "raw_message",
}

// allowedMetaColumns is a whitelist of columns that can be used in meta queries.
var allowedMetaColumns = map[string]struct{}{
	"hostname":    {},
	"programname": {},
	"syslogtag":   {},
}

// GetSrvlog returns a single event by ID.
func (s *Store) GetSrvlog(ctx context.Context, id int64) (model.SrvlogEvent, error) {
	query, args, err := psq.
		Select(srvlogColumns...).
		From("srvlog_events").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return model.SrvlogEvent{}, fmt.Errorf("build query: %w", err)
	}

	var e model.SrvlogEvent
	err = scanSrvlog(s.pool.QueryRow(ctx, query, args...), &e)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.SrvlogEvent{}, err
	}
	if err != nil {
		return model.SrvlogEvent{}, fmt.Errorf("get event %d: %w", id, err)
	}
	return e, nil
}

// ListSrvlogs returns events matching the filter with cursor-based pagination.
// It returns one extra row to determine if more pages exist.
func (s *Store) ListSrvlogs(ctx context.Context, f model.SrvlogFilter, cursor *model.Cursor, limit int) ([]model.SrvlogEvent, *model.Cursor, error) {
	qb := psq.Select(srvlogColumns...).From("srvlog_events")
	qb = applySrvlogFilter(qb, f)

	// Keyset (cursor) pagination using tuple comparison — Postgres evaluates
	// (received_at, id) as a composite key, giving stable ordering without OFFSET.
	if cursor != nil {
		qb = qb.Where("(received_at, id) < (?, ?)", cursor.ReceivedAt, cursor.ID)
	}

	qb = qb.OrderBy("received_at DESC", "id DESC").Limit(uint64(limit + 1))

	query, args, err := qb.ToSql()
	if err != nil {
		return nil, nil, fmt.Errorf("build query: %w", err)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("list events: %w", err)
	}

	events, err := collectSrvlogs(rows)
	if err != nil {
		return nil, nil, err
	}

	var nextCursor *model.Cursor
	if limit > 0 && len(events) > limit {
		// Cursor is the LAST RETURNED row; the next page queries strictly before
		// it. events[limit] is the peek row used only to detect a next page —
		// using it as the cursor would exclude it from the next query without
		// ever returning it, dropping one event per page boundary.
		last := events[limit-1]
		nextCursor = &model.Cursor{
			ReceivedAt: last.ReceivedAt,
			ID:         last.ID,
		}
		events = events[:limit]
	}

	return events, nextCursor, nil
}

// ListSrvlogsSince returns events with id > sinceID matching the filter,
// ordered chronologically (ASC). Used for SSE Last-Event-ID reconnect backfill.
func (s *Store) ListSrvlogsSince(ctx context.Context, f model.SrvlogFilter, sinceID int64, limit int) ([]model.SrvlogEvent, error) {
	qb := psq.Select(srvlogColumns...).From("srvlog_events")
	qb = applySrvlogFilter(qb, f)
	qb = qb.Where(sq.Gt{"id": sinceID})
	qb = qb.OrderBy("id ASC").Limit(uint64(limit))

	query, args, err := qb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list events since %d: %w", sinceID, err)
	}

	return collectSrvlogs(rows)
}

// ListSrvlogHosts returns distinct hostnames ordered alphabetically.
func (s *Store) ListSrvlogHosts(ctx context.Context) ([]string, error) {
	return s.listDistinctStrings(ctx, "hostname")
}

// ListSrvlogPrograms returns distinct program names ordered alphabetically.
func (s *Store) ListSrvlogPrograms(ctx context.Context) ([]string, error) {
	return s.listDistinctStrings(ctx, "programname")
}

// ListSrvlogTags returns distinct syslog tags ordered alphabetically.
func (s *Store) ListSrvlogTags(ctx context.Context) ([]string, error) {
	return s.listDistinctStrings(ctx, "syslogtag")
}

// ListSrvlogFacilities returns distinct facility codes ordered numerically.
func (s *Store) ListSrvlogFacilities(ctx context.Context) ([]int, error) {
	rows, err := s.pool.Query(ctx, "SELECT facility FROM srvlog_facility_cache ORDER BY facility LIMIT $1", metaLimit)
	if err != nil {
		return nil, fmt.Errorf("list facilities: %w", err)
	}

	facilities, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return nil, fmt.Errorf("scan facility: %w", err)
	}
	return facilities, nil
}

func (s *Store) listDistinctStrings(ctx context.Context, column string) ([]string, error) {
	return s.listMetaStrings(ctx, "srvlog_meta_cache", column, allowedMetaColumns)
}

// GetVolume returns time-bucketed event counts grouped by hostname.
func (s *Store) GetVolume(ctx context.Context, interval model.VolumeInterval, rangeDur time.Duration) ([]model.VolumeBucket, error) {
	return s.getVolume(ctx, "srvlog_events", "hostname", interval, rangeDur)
}

// GetSeverityVolume returns time-bucketed event counts grouped by srvlog severity.
func (s *Store) GetSeverityVolume(ctx context.Context, interval model.VolumeInterval, rangeDur time.Duration) ([]model.SeverityVolumeBucket, error) {
	if !interval.IsValid() {
		return nil, fmt.Errorf("invalid volume interval: %s", interval)
	}
	since := time.Now().UTC().Add(-rangeDur)

	query := `SELECT time_bucket($1::interval, received_at) AS bucket,
	                 severity, count(*) AS cnt
	          FROM srvlog_events
	          WHERE received_at >= $2
	          GROUP BY bucket, severity
	          ORDER BY bucket ASC`

	rows, err := s.pool.Query(ctx, query, interval.String(), since)
	if err != nil {
		return nil, fmt.Errorf("severity volume query: %w", err)
	}
	defer rows.Close()

	type key = time.Time
	idx := make(map[key]int)
	var buckets []model.SeverityVolumeBucket

	for rows.Next() {
		var (
			bucket time.Time
			sev    int
			cnt    int64
		)
		if err := rows.Scan(&bucket, &sev, &cnt); err != nil {
			return nil, fmt.Errorf("scan severity volume row: %w", err)
		}

		i, ok := idx[bucket]
		if !ok {
			i = len(buckets)
			idx[bucket] = i
			buckets = append(buckets, model.SeverityVolumeBucket{
				Time:       bucket,
				BySeverity: make(map[string]int64),
			})
		}
		buckets[i].Total += cnt
		buckets[i].BySeverity[model.SeverityLabel(sev)] = cnt
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("severity volume rows: %w", err)
	}

	return buckets, nil
}

// GetSrvlogSummary returns summary statistics for the given range.
// Uses srvlog_summary_hourly continuous aggregate.
func (s *Store) GetSrvlogSummary(ctx context.Context, rangeDur time.Duration) (model.SyslogSummary, error) {
	since := time.Now().UTC().Add(-rangeDur)
	prevStart := since.Add(-rangeDur)

	// Get current totals by severity from the continuous aggregate.
	query := `SELECT severity, SUM(cnt) AS cnt
	          FROM srvlog_summary_hourly
	          WHERE bucket >= $1
	          GROUP BY severity
	          ORDER BY severity`

	rows, err := s.pool.Query(ctx, query, since)
	if err != nil {
		return model.SrvlogSummary{}, fmt.Errorf("srvlog summary query: %w", err)
	}
	defer rows.Close()

	var summary model.SrvlogSummary
	summary.SeverityBreakdown = make([]model.SeverityCount, 0)
	summary.TopHosts = make([]model.TopSource, 0)

	for rows.Next() {
		var sev int
		var cnt int64
		if err := rows.Scan(&sev, &cnt); err != nil {
			return model.SrvlogSummary{}, fmt.Errorf("scan severity: %w", err)
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
		return model.SrvlogSummary{}, fmt.Errorf("severity rows: %w", err)
	}

	// Get previous period total for trend calculation.
	var prevTotal int64
	err = s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(cnt), 0) FROM srvlog_summary_hourly WHERE bucket >= $1 AND bucket < $2`,
		prevStart, since).Scan(&prevTotal)
	if err != nil {
		return model.SrvlogSummary{}, fmt.Errorf("prev total query: %w", err)
	}

	if prevTotal > 0 {
		summary.Trend = float64(summary.Total-prevTotal) / float64(prevTotal) * 100
	}

	// Get top hosts from the continuous aggregate.
	hostQuery := `SELECT hostname, SUM(cnt) AS cnt
	              FROM srvlog_summary_hourly
	              WHERE bucket >= $1
	              GROUP BY hostname
	              ORDER BY cnt DESC
	              LIMIT $2`

	hostRows, err := s.pool.Query(ctx, hostQuery, since, topSourcesLimit)
	if err != nil {
		return model.SrvlogSummary{}, fmt.Errorf("top hosts query: %w", err)
	}

	hosts, err := pgx.CollectRows(hostRows, func(row pgx.CollectableRow) (model.TopSource, error) {
		var ts model.TopSource
		err := row.Scan(&ts.Name, &ts.Count)
		return ts, err
	})
	if err != nil {
		return model.SrvlogSummary{}, fmt.Errorf("scan host: %w", err)
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

// GetSrvlogDeviceSummary returns aggregated device information for the given hostname.
// It fetches last-seen time, severity breakdown (7d), top normalized messages (7d),
// and recent critical logs using a single pgx.Batch round-trip.
func (s *Store) GetSrvlogDeviceSummary(ctx context.Context, hostname string) (model.SrvlogDeviceSummary, error) {
	summary := model.SrvlogDeviceSummary{
		Hostname:          hostname,
		SeverityBreakdown: make([]model.SeverityCount, 0),
		TopMessages:       make([]model.TopMessage, 0),
		CriticalLogs:      make([]model.SrvlogEvent, 0),
		Activity:          make([]model.ActivityBucket, 0),
	}

	since := time.Now().UTC().Add(-7 * 24 * time.Hour)

	// Build critical logs query via squirrel for column list consistency.
	critQuery, critArgs, err := psq.
		Select(srvlogColumns...).
		From("srvlog_events").
		Where(sq.Eq{"hostname": hostname}).
		Where(sq.GtOrEq{"received_at": since}).
		Where(sq.LtOrEq{"severity": model.SeverityErr}).
		OrderBy("received_at DESC", "id DESC").
		Limit(50).
		ToSql()
	if err != nil {
		return summary, fmt.Errorf("build critical logs query: %w", err)
	}

	// Send all 4 queries in a single round-trip.
	batch := &pgx.Batch{}

	// Q1: last seen (most recent event for this host).
	batch.Queue(
		"SELECT MAX(received_at) FROM srvlog_events WHERE hostname = $1",
		hostname,
	)

	// Q2: severity breakdown (7 days).
	batch.Queue(
		`SELECT severity, count(*) AS cnt
		 FROM srvlog_events
		 WHERE hostname = $1 AND received_at >= $2
		 GROUP BY severity
		 ORDER BY severity`,
		hostname, since,
	)

	// Q3: top normalized messages (24h).
	// Uses the pre-computed msg_pattern column (populated by trigger on INSERT)
	// so no regex runs at query time. Rows with empty msg_pattern (pre-migration
	// data) are excluded — fully populated within 24h of deploying migration 000012.
	// CTE aggregates per pattern, then joins back to get severity from the
	// actual latest event (max id) — avoids min(severity) / max(id) mismatch.
	msgSince := time.Now().UTC().Add(-24 * time.Hour)
	batch.Queue(
		`WITH agg AS (
		     SELECT msg_pattern AS pattern,
		         min(message) AS sample,
		         count(*) AS cnt,
		         max(id) AS latest_id,
		         max(received_at) AS latest_at
		     FROM srvlog_events
		     WHERE hostname = $1 AND received_at >= $2 AND msg_pattern != ''
		     GROUP BY msg_pattern
		     ORDER BY cnt DESC, msg_pattern
		     LIMIT 10
		 )
		 SELECT a.pattern, a.sample, a.cnt, a.latest_id, a.latest_at, e.severity
		 FROM agg a
		 JOIN srvlog_events e ON e.id = a.latest_id`,
		hostname, msgSince,
	)

	// Q4: recent critical logs.
	batch.Queue(critQuery, critArgs...)

	// Q5: latest IP for this host.
	batch.Queue(
		"SELECT fromhost_ip FROM srvlog_events WHERE hostname = $1 ORDER BY received_at DESC, id DESC LIMIT 1",
		hostname,
	)

	// Q6: log activity time series (24h, 15-minute buckets). Powers the activity
	// chart in the device detail Severity Breakdown box. time_bucket only emits
	// buckets that have rows, so sparse hosts render gaps (matches /volume).
	batch.Queue(
		`SELECT time_bucket('15 minutes', received_at) AS bucket, count(*) AS cnt
		 FROM srvlog_events
		 WHERE hostname = $1 AND received_at >= $2
		 GROUP BY bucket
		 ORDER BY bucket ASC`,
		hostname, msgSince,
	)

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close() //nolint:errcheck // best-effort close

	// R1: last seen.
	var lastSeen *time.Time
	if err := results.QueryRow().Scan(&lastSeen); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return summary, fmt.Errorf("device last seen: %w", err)
	}
	summary.LastSeenAt = lastSeen

	// R2: severity breakdown.
	sevRows, err := results.Query()
	if err != nil {
		return summary, fmt.Errorf("device severity breakdown: %w", err)
	}

	for sevRows.Next() {
		var sev int
		var cnt int64
		if err := sevRows.Scan(&sev, &cnt); err != nil {
			return summary, fmt.Errorf("scan severity: %w", err)
		}
		if sev <= model.SeverityErr {
			summary.CriticalCount += cnt
		}
		summary.SeverityBreakdown = append(summary.SeverityBreakdown, model.SeverityCount{
			Severity: sev,
			Label:    model.SeverityLabel(sev),
			Count:    cnt,
		})
	}
	sevRows.Close()
	if err := sevRows.Err(); err != nil {
		return summary, fmt.Errorf("device severity rows: %w", err)
	}

	var total int64
	for _, sc := range summary.SeverityBreakdown {
		total += sc.Count
	}
	summary.TotalCount = total
	for i := range summary.SeverityBreakdown {
		if total > 0 {
			summary.SeverityBreakdown[i].Pct = float64(summary.SeverityBreakdown[i].Count) / float64(total) * 100
		}
	}

	// R3: top messages.
	msgRows, err := results.Query()
	if err != nil {
		return summary, fmt.Errorf("device top messages: %w", err)
	}

	messages, err := pgx.CollectRows(msgRows, func(row pgx.CollectableRow) (model.TopMessage, error) {
		var tm model.TopMessage
		if err := row.Scan(&tm.Pattern, &tm.Sample, &tm.Count, &tm.LatestID, &tm.LatestAt, &tm.Severity); err != nil {
			return tm, err
		}
		tm.SeverityLabel = model.SeverityLabel(tm.Severity)
		return tm, nil
	})
	if err != nil {
		return summary, fmt.Errorf("scan top message: %w", err)
	}
	summary.TopMessages = append(summary.TopMessages, messages...)

	// R4: critical logs.
	critRows, err := results.Query()
	if err != nil {
		return summary, fmt.Errorf("device critical logs: %w", err)
	}

	critEvents, err := collectSrvlogs(critRows)
	if err != nil {
		return summary, fmt.Errorf("collect critical logs: %w", err)
	}
	if critEvents != nil {
		summary.CriticalLogs = critEvents
	}

	// R5: latest IP.
	var ip netip.Addr
	if err := results.QueryRow().Scan(&ip); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return summary, fmt.Errorf("device latest ip: %w", err)
	}
	summary.FromhostIP = ip.String()

	// R6: log activity time series.
	actRows, err := results.Query()
	if err != nil {
		return summary, fmt.Errorf("device activity: %w", err)
	}

	activity, err := pgx.CollectRows(actRows, func(row pgx.CollectableRow) (model.ActivityBucket, error) {
		var ab model.ActivityBucket
		err := row.Scan(&ab.Time, &ab.Count)
		return ab, err
	})
	if err != nil {
		return summary, fmt.Errorf("scan activity bucket: %w", err)
	}
	summary.Activity = append(summary.Activity, activity...)

	return summary, nil
}

func collectSrvlogs(rows pgx.Rows) ([]model.SrvlogEvent, error) {
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.SrvlogEvent, error) {
		var e model.SrvlogEvent
		err := scanSrvlog(row, &e)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan event: %w", err)
	}
	return events, nil
}

func scanSrvlog(row pgx.Row, e *model.SrvlogEvent) error {
	var ip netip.Addr
	err := row.Scan(
		&e.ID, &e.ReceivedAt, &e.ReportedAt, &e.Hostname, &ip,
		&e.Programname, &e.MsgID, &e.Severity, &e.Facility, &e.SyslogTag,
		&e.StructuredData, &e.Message, &e.RawMessage,
	)
	if err != nil {
		return err
	}
	e.FromhostIP = ip.String()
	e.SeverityLabel = model.SeverityLabel(e.Severity)
	e.FacilityLabel = model.FacilityLabel(e.Facility)
	return nil
}
