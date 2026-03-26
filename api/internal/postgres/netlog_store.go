package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
)

var netlogColumns = []string{
	"id", "received_at", "reported_at", "hostname", "fromhost_ip",
	"programname", "msgid", "severity", "facility", "syslogtag",
	"structured_data", "message", "raw_message",
}

// allowedNetlogMetaColumns is a whitelist of columns that can be used in netlog meta queries.
var allowedNetlogMetaColumns = map[string]struct{}{
	"hostname":    {},
	"programname": {},
	"syslogtag":   {},
}

// GetNetlog returns a single netlog event by ID.
func (s *Store) GetNetlog(ctx context.Context, id int64) (model.NetlogEvent, error) {
	query, args, err := psq.
		Select(netlogColumns...).
		From("netlog_events").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return model.NetlogEvent{}, fmt.Errorf("build query: %w", err)
	}

	var e model.NetlogEvent
	err = scanNetlog(s.pool.QueryRow(ctx, query, args...), &e)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.NetlogEvent{}, err
	}
	if err != nil {
		return model.NetlogEvent{}, fmt.Errorf("get netlog event %d: %w", id, err)
	}
	return e, nil
}

// ListNetlogs returns netlog events matching the filter with cursor-based pagination.
// It returns one extra row to determine if more pages exist.
func (s *Store) ListNetlogs(ctx context.Context, f model.NetlogFilter, cursor *model.Cursor, limit int) ([]model.NetlogEvent, *model.Cursor, error) {
	qb := psq.Select(netlogColumns...).From("netlog_events")
	qb = applyNetlogFilter(qb, f)

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
		return nil, nil, fmt.Errorf("list netlog events: %w", err)
	}
	defer rows.Close()

	events, err := collectNetlogs(rows)
	if err != nil {
		return nil, nil, err
	}

	var nextCursor *model.Cursor
	if len(events) > limit {
		last := events[limit]
		nextCursor = &model.Cursor{
			ReceivedAt: last.ReceivedAt,
			ID:         last.ID,
		}
		events = events[:limit]
	}

	return events, nextCursor, nil
}

// ListNetlogsSince returns netlog events with id > sinceID matching the filter,
// ordered chronologically (ASC). Used for SSE Last-Event-ID reconnect backfill.
func (s *Store) ListNetlogsSince(ctx context.Context, f model.NetlogFilter, sinceID int64, limit int) ([]model.NetlogEvent, error) {
	qb := psq.Select(netlogColumns...).From("netlog_events")
	qb = applyNetlogFilter(qb, f)
	qb = qb.Where(sq.Gt{"id": sinceID})
	qb = qb.OrderBy("id ASC").Limit(uint64(limit))

	query, args, err := qb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list netlog events since %d: %w", sinceID, err)
	}
	defer rows.Close()

	return collectNetlogs(rows)
}

// ListNetlogHosts returns distinct hostnames from netlog_meta_cache ordered alphabetically.
func (s *Store) ListNetlogHosts(ctx context.Context) ([]string, error) {
	return s.listNetlogDistinctStrings(ctx, "hostname")
}

// ListNetlogPrograms returns distinct program names from netlog_meta_cache ordered alphabetically.
func (s *Store) ListNetlogPrograms(ctx context.Context) ([]string, error) {
	return s.listNetlogDistinctStrings(ctx, "programname")
}

// ListNetlogTags returns distinct syslog tags from netlog_meta_cache ordered alphabetically.
func (s *Store) ListNetlogTags(ctx context.Context) ([]string, error) {
	return s.listNetlogDistinctStrings(ctx, "syslogtag")
}

// ListNetlogFacilities returns distinct facility codes from netlog_facility_cache ordered numerically.
func (s *Store) ListNetlogFacilities(ctx context.Context) ([]int, error) {
	rows, err := s.pool.Query(ctx, "SELECT facility FROM netlog_facility_cache ORDER BY facility LIMIT $1", metaLimit)
	if err != nil {
		return nil, fmt.Errorf("list netlog facilities: %w", err)
	}
	defer rows.Close()

	facilities := make([]int, 0, 24)
	for rows.Next() {
		var f int
		if err := rows.Scan(&f); err != nil {
			return nil, fmt.Errorf("scan netlog facility: %w", err)
		}
		facilities = append(facilities, f)
	}
	return facilities, rows.Err()
}

func (s *Store) listNetlogDistinctStrings(ctx context.Context, column string) ([]string, error) {
	if _, ok := allowedNetlogMetaColumns[column]; !ok {
		return nil, fmt.Errorf("disallowed netlog meta column: %s", column)
	}
	rows, err := s.pool.Query(ctx, "SELECT value FROM netlog_meta_cache WHERE column_name = $1 ORDER BY value LIMIT $2", column, metaLimit)
	if err != nil {
		return nil, fmt.Errorf("list netlog %s: %w", column, err)
	}
	defer rows.Close()

	values := make([]string, 0, 64)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan netlog %s: %w", column, err)
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

// GetNetlogVolume returns time-bucketed netlog event counts grouped by hostname.
func (s *Store) GetNetlogVolume(ctx context.Context, interval model.VolumeInterval, rangeDur time.Duration) ([]model.VolumeBucket, error) {
	return s.getVolume(ctx, "netlog_events", "hostname", interval, rangeDur)
}

// GetNetlogSeverityVolume returns time-bucketed netlog event counts grouped by severity.
func (s *Store) GetNetlogSeverityVolume(ctx context.Context, interval model.VolumeInterval, rangeDur time.Duration) ([]model.SeverityVolumeBucket, error) {
	if !interval.IsValid() {
		return nil, fmt.Errorf("invalid volume interval: %s", interval)
	}
	since := time.Now().UTC().Add(-rangeDur)

	query := `SELECT time_bucket($1::interval, received_at) AS bucket,
	                 severity, count(*) AS cnt
	          FROM netlog_events
	          WHERE received_at >= $2
	          GROUP BY bucket, severity
	          ORDER BY bucket ASC`

	rows, err := s.pool.Query(ctx, query, interval.String(), since)
	if err != nil {
		return nil, fmt.Errorf("netlog severity volume query: %w", err)
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
			return nil, fmt.Errorf("scan netlog severity volume row: %w", err)
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
		return nil, fmt.Errorf("netlog severity volume rows: %w", err)
	}

	return buckets, nil
}

// GetNetlogSummary returns summary statistics for netlog events over the given range.
// Uses netlog_summary_hourly continuous aggregate.
func (s *Store) GetNetlogSummary(ctx context.Context, rangeDur time.Duration) (model.SyslogSummary, error) {
	since := time.Now().UTC().Add(-rangeDur)
	prevStart := since.Add(-rangeDur)

	// Get current totals by severity from the continuous aggregate.
	query := `SELECT severity, SUM(cnt) AS cnt
	          FROM netlog_summary_hourly
	          WHERE bucket >= $1
	          GROUP BY severity
	          ORDER BY severity`

	rows, err := s.pool.Query(ctx, query, since)
	if err != nil {
		return model.SrvlogSummary{}, fmt.Errorf("netlog summary query: %w", err)
	}
	defer rows.Close()

	var summary model.SrvlogSummary
	summary.SeverityBreakdown = make([]model.SeverityCount, 0)
	summary.TopHosts = make([]model.TopSource, 0)

	for rows.Next() {
		var sev int
		var cnt int64
		if err := rows.Scan(&sev, &cnt); err != nil {
			return model.SrvlogSummary{}, fmt.Errorf("scan netlog severity: %w", err)
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
		return model.SrvlogSummary{}, fmt.Errorf("netlog severity rows: %w", err)
	}

	// Get previous period total for trend calculation.
	var prevTotal int64
	err = s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(cnt), 0) FROM netlog_summary_hourly WHERE bucket >= $1 AND bucket < $2`,
		prevStart, since).Scan(&prevTotal)
	if err != nil {
		return model.SrvlogSummary{}, fmt.Errorf("netlog prev total query: %w", err)
	}

	if prevTotal > 0 {
		summary.Trend = float64(summary.Total-prevTotal) / float64(prevTotal) * 100
	}

	// Get top hosts from the continuous aggregate.
	hostQuery := `SELECT hostname, SUM(cnt) AS cnt
	              FROM netlog_summary_hourly
	              WHERE bucket >= $1
	              GROUP BY hostname
	              ORDER BY cnt DESC
	              LIMIT $2`

	hostRows, err := s.pool.Query(ctx, hostQuery, since, topSourcesLimit)
	if err != nil {
		return model.SrvlogSummary{}, fmt.Errorf("netlog top hosts query: %w", err)
	}
	defer hostRows.Close()

	for hostRows.Next() {
		var name string
		var cnt int64
		if err := hostRows.Scan(&name, &cnt); err != nil {
			return model.SrvlogSummary{}, fmt.Errorf("scan netlog host: %w", err)
		}
		summary.TopHosts = append(summary.TopHosts, model.TopSource{
			Name:  name,
			Count: cnt,
		})
	}
	if err := hostRows.Err(); err != nil {
		return model.SrvlogSummary{}, fmt.Errorf("netlog host rows: %w", err)
	}

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

// GetNetlogDeviceSummary returns aggregated device information for the given hostname
// from netlog_events. It fetches last-seen time, severity breakdown (7d), top normalized
// messages (24h), and recent critical logs using a single pgx.Batch round-trip.
func (s *Store) GetNetlogDeviceSummary(ctx context.Context, hostname string) (model.NetlogDeviceSummary, error) {
	summary := model.NetlogDeviceSummary{
		Hostname:          hostname,
		SeverityBreakdown: make([]model.SeverityCount, 0),
		TopMessages:       make([]model.TopMessage, 0),
		CriticalLogs:      make([]model.NetlogEvent, 0),
	}

	since := time.Now().UTC().Add(-7 * 24 * time.Hour)

	// Build critical logs query via squirrel for column list consistency.
	critQuery, critArgs, err := psq.
		Select(netlogColumns...).
		From("netlog_events").
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
		"SELECT MAX(received_at) FROM netlog_events WHERE hostname = $1",
		hostname,
	)

	// Q2: severity breakdown (7 days).
	batch.Queue(
		`SELECT severity, count(*) AS cnt
		 FROM netlog_events
		 WHERE hostname = $1 AND received_at >= $2
		 GROUP BY severity
		 ORDER BY severity`,
		hostname, since,
	)

	// Q3: top normalized messages (24h).
	msgSince := time.Now().UTC().Add(-24 * time.Hour)
	batch.Queue(
		`SELECT msg_pattern AS pattern,
		     min(message) AS sample,
		     count(*) AS cnt,
		     max(id) AS latest_id,
		     max(received_at) AS latest_at,
		     min(severity) AS severity
		 FROM netlog_events
		 WHERE hostname = $1 AND received_at >= $2 AND msg_pattern != ''
		 GROUP BY msg_pattern
		 ORDER BY cnt DESC, msg_pattern
		 LIMIT 10`,
		hostname, msgSince,
	)

	// Q4: recent critical logs.
	batch.Queue(critQuery, critArgs...)

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close() //nolint:errcheck // best-effort close

	// R1: last seen.
	var lastSeen *time.Time
	if err := results.QueryRow().Scan(&lastSeen); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return summary, fmt.Errorf("netlog device last seen: %w", err)
	}
	summary.LastSeenAt = lastSeen

	// R2: severity breakdown.
	sevRows, err := results.Query()
	if err != nil {
		return summary, fmt.Errorf("netlog device severity breakdown: %w", err)
	}

	for sevRows.Next() {
		var sev int
		var cnt int64
		if err := sevRows.Scan(&sev, &cnt); err != nil {
			return summary, fmt.Errorf("scan netlog severity: %w", err)
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
		return summary, fmt.Errorf("netlog device severity rows: %w", err)
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
		return summary, fmt.Errorf("netlog device top messages: %w", err)
	}

	for msgRows.Next() {
		var tm model.TopMessage
		if err := msgRows.Scan(&tm.Pattern, &tm.Sample, &tm.Count, &tm.LatestID, &tm.LatestAt, &tm.Severity); err != nil {
			return summary, fmt.Errorf("scan netlog top message: %w", err)
		}
		tm.SeverityLabel = model.SeverityLabel(tm.Severity)
		summary.TopMessages = append(summary.TopMessages, tm)
	}
	msgRows.Close()
	if err := msgRows.Err(); err != nil {
		return summary, fmt.Errorf("netlog device msg rows: %w", err)
	}

	// R4: critical logs.
	critRows, err := results.Query()
	if err != nil {
		return summary, fmt.Errorf("netlog device critical logs: %w", err)
	}

	critEvents, err := collectNetlogs(critRows)
	if err != nil {
		return summary, fmt.Errorf("collect netlog critical logs: %w", err)
	}
	if critEvents != nil {
		summary.CriticalLogs = critEvents
	}

	return summary, nil
}

func applyNetlogFilter(qb sq.SelectBuilder, f model.NetlogFilter) sq.SelectBuilder {
	if f.Hostname != "" {
		if strings.Contains(f.Hostname, "*") {
			pattern := strings.ReplaceAll(escapeLike(f.Hostname), "*", "%")
			qb = qb.Where("hostname ILIKE ?", pattern)
		} else {
			qb = qb.Where(sq.Eq{"hostname": f.Hostname})
		}
	}
	if f.FromhostIP != "" {
		qb = qb.Where("fromhost_ip = ?::inet", f.FromhostIP)
	}
	if f.Programname != "" {
		qb = qb.Where(sq.Eq{"programname": f.Programname})
	}
	if f.Severity != nil {
		qb = qb.Where(sq.Eq{"severity": *f.Severity})
	}
	if f.SeverityMax != nil {
		qb = qb.Where(sq.LtOrEq{"severity": *f.SeverityMax})
	}
	if f.Facility != nil {
		qb = qb.Where(sq.Eq{"facility": *f.Facility})
	}
	if f.SyslogTag != "" {
		qb = qb.Where(sq.Eq{"syslogtag": f.SyslogTag})
	}
	if f.MsgID != "" {
		qb = qb.Where(sq.Eq{"msgid": f.MsgID})
	}
	if f.Search != "" {
		escaped := escapeLike(f.Search)
		qb = qb.Where("message ILIKE ?", "%"+escaped+"%")
	}
	if f.From != nil {
		qb = qb.Where(sq.GtOrEq{"received_at": *f.From})
	}
	if f.To != nil {
		qb = qb.Where(sq.LtOrEq{"received_at": *f.To})
	}
	return qb
}

func collectNetlogs(rows pgx.Rows) ([]model.NetlogEvent, error) {
	events := make([]model.NetlogEvent, 0, 64)
	for rows.Next() {
		var e model.NetlogEvent
		if err := scanNetlog(rows, &e); err != nil {
			return nil, fmt.Errorf("scan netlog event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netlog rows iteration: %w", err)
	}
	return events, nil
}

func scanNetlog(row pgx.Row, e *model.NetlogEvent) error {
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
