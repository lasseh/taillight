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

var syslogColumns = []string{
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

// GetSyslog returns a single event by ID.
func (s *Store) GetSyslog(ctx context.Context, id int64) (model.SyslogEvent, error) {
	query, args, err := psq.
		Select(syslogColumns...).
		From("syslog_events").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return model.SyslogEvent{}, fmt.Errorf("build query: %w", err)
	}

	var e model.SyslogEvent
	err = scanSyslog(s.pool.QueryRow(ctx, query, args...), &e)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.SyslogEvent{}, err
	}
	if err != nil {
		return model.SyslogEvent{}, fmt.Errorf("get event %d: %w", id, err)
	}
	return e, nil
}

// ListSyslogs returns events matching the filter with cursor-based pagination.
// It returns one extra row to determine if more pages exist.
func (s *Store) ListSyslogs(ctx context.Context, f model.SyslogFilter, cursor *model.Cursor, limit int) ([]model.SyslogEvent, *model.Cursor, error) {
	qb := psq.Select(syslogColumns...).From("syslog_events")
	qb = applySyslogFilter(qb, f)

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
	defer rows.Close()

	events, err := collectSyslogs(rows)
	if err != nil {
		return nil, nil, err
	}

	var nextCursor *model.Cursor
	if len(events) > limit {
		last := events[limit-1]
		nextCursor = &model.Cursor{
			ReceivedAt: last.ReceivedAt,
			ID:         last.ID,
		}
		events = events[:limit]
	}

	return events, nextCursor, nil
}

// ListSyslogsSince returns events with id > sinceID matching the filter,
// ordered chronologically (ASC). Used for SSE Last-Event-ID reconnect backfill.
func (s *Store) ListSyslogsSince(ctx context.Context, f model.SyslogFilter, sinceID int64, limit int) ([]model.SyslogEvent, error) {
	qb := psq.Select(syslogColumns...).From("syslog_events")
	qb = applySyslogFilter(qb, f)
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
	defer rows.Close()

	return collectSyslogs(rows)
}

// ListHosts returns distinct hostnames ordered alphabetically.
func (s *Store) ListHosts(ctx context.Context) ([]string, error) {
	return s.listDistinctStrings(ctx, "hostname")
}

// ListPrograms returns distinct program names ordered alphabetically.
func (s *Store) ListPrograms(ctx context.Context) ([]string, error) {
	return s.listDistinctStrings(ctx, "programname")
}

// ListTags returns distinct syslog tags ordered alphabetically.
func (s *Store) ListTags(ctx context.Context) ([]string, error) {
	return s.listDistinctStrings(ctx, "syslogtag")
}

// ListFacilities returns distinct facility codes ordered numerically.
func (s *Store) ListFacilities(ctx context.Context) ([]int, error) {
	rows, err := s.pool.Query(ctx, "SELECT facility FROM syslog_facility_cache ORDER BY facility LIMIT $1", metaLimit)
	if err != nil {
		return nil, fmt.Errorf("list facilities: %w", err)
	}
	defer rows.Close()

	var facilities []int
	for rows.Next() {
		var f int
		if err := rows.Scan(&f); err != nil {
			return nil, fmt.Errorf("scan facility: %w", err)
		}
		facilities = append(facilities, f)
	}
	return facilities, rows.Err()
}

func (s *Store) listDistinctStrings(ctx context.Context, column string) ([]string, error) {
	if _, ok := allowedMetaColumns[column]; !ok {
		return nil, fmt.Errorf("disallowed meta column: %s", column)
	}
	rows, err := s.pool.Query(ctx, "SELECT value FROM syslog_meta_cache WHERE column_name = $1 ORDER BY value LIMIT $2", column, metaLimit)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", column, err)
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan %s: %w", column, err)
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

func applySyslogFilter(qb sq.SelectBuilder, f model.SyslogFilter) sq.SelectBuilder {
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

// escapeLike escapes LIKE/ILIKE metacharacters so they are treated as literals.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func collectSyslogs(rows pgx.Rows) ([]model.SyslogEvent, error) {
	var events []model.SyslogEvent
	for rows.Next() {
		var e model.SyslogEvent
		if err := scanSyslog(rows, &e); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return events, nil
}

// GetVolume returns time-bucketed event counts grouped by hostname.
func (s *Store) GetVolume(ctx context.Context, interval model.VolumeInterval, rangeDur time.Duration) ([]model.VolumeBucket, error) {
	return s.getVolume(ctx, "syslog_events", "hostname", interval, rangeDur)
}

// GetAppLogVolume returns time-bucketed event counts grouped by service.
func (s *Store) GetAppLogVolume(ctx context.Context, interval model.VolumeInterval, rangeDur time.Duration) ([]model.VolumeBucket, error) {
	return s.getVolume(ctx, "applog_events", "service", interval, rangeDur)
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

func scanSyslog(row pgx.Row, e *model.SyslogEvent) error {
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

// GetSyslogSummary returns summary statistics for the given range.
func (s *Store) GetSyslogSummary(ctx context.Context, rangeDur time.Duration) (model.SyslogSummary, error) {
	since := time.Now().UTC().Add(-rangeDur)
	prevStart := since.Add(-rangeDur)

	// Get current 24h totals by severity
	query := `SELECT severity, count(*) AS cnt
	          FROM syslog_events
	          WHERE received_at >= $1
	          GROUP BY severity
	          ORDER BY severity`

	rows, err := s.pool.Query(ctx, query, since)
	if err != nil {
		return model.SyslogSummary{}, fmt.Errorf("syslog summary query: %w", err)
	}
	defer rows.Close()

	var summary model.SyslogSummary
	summary.SeverityBreakdown = make([]model.SeverityCount, 0)
	summary.TopHosts = make([]model.TopSource, 0)

	for rows.Next() {
		var sev int
		var cnt int64
		if err := rows.Scan(&sev, &cnt); err != nil {
			return model.SyslogSummary{}, fmt.Errorf("scan severity: %w", err)
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
		return model.SyslogSummary{}, fmt.Errorf("severity rows: %w", err)
	}

	// Get previous 24h total for trend calculation
	var prevTotal int64
	err = s.pool.QueryRow(ctx, "SELECT count(*) FROM syslog_events WHERE received_at >= $1 AND received_at < $2", prevStart, since).Scan(&prevTotal)
	if err != nil {
		return model.SyslogSummary{}, fmt.Errorf("prev total query: %w", err)
	}

	if prevTotal > 0 {
		summary.Trend = float64(summary.Total-prevTotal) / float64(prevTotal) * 100
	}

	// Get top hosts
	hostQuery := `SELECT hostname, count(*) AS cnt
	              FROM syslog_events
	              WHERE received_at >= $1
	              GROUP BY hostname
	              ORDER BY cnt DESC
	              LIMIT $2`

	hostRows, err := s.pool.Query(ctx, hostQuery, since, topSourcesLimit)
	if err != nil {
		return model.SyslogSummary{}, fmt.Errorf("top hosts query: %w", err)
	}
	defer hostRows.Close()

	for hostRows.Next() {
		var name string
		var cnt int64
		if err := hostRows.Scan(&name, &cnt); err != nil {
			return model.SyslogSummary{}, fmt.Errorf("scan host: %w", err)
		}
		summary.TopHosts = append(summary.TopHosts, model.TopSource{
			Name:  name,
			Count: cnt,
		})
	}
	if err := hostRows.Err(); err != nil {
		return model.SyslogSummary{}, fmt.Errorf("host rows: %w", err)
	}

	// Calculate percentages for top hosts
	for i := range summary.TopHosts {
		if summary.Total > 0 {
			summary.TopHosts[i].Pct = float64(summary.TopHosts[i].Count) / float64(summary.Total) * 100
		}
	}

	// Calculate percentages for severity breakdown
	for i := range summary.SeverityBreakdown {
		if summary.Total > 0 {
			summary.SeverityBreakdown[i].Pct = float64(summary.SeverityBreakdown[i].Count) / float64(summary.Total) * 100
		}
	}

	return summary, nil
}

// GetAppLogSummary returns summary statistics for the given range.
func (s *Store) GetAppLogSummary(ctx context.Context, rangeDur time.Duration) (model.AppLogSummary, error) {
	since := time.Now().UTC().Add(-rangeDur)
	prevStart := since.Add(-rangeDur)

	// Get current 24h totals by level
	query := `SELECT level, count(*) AS cnt
	          FROM applog_events
	          WHERE received_at >= $1
	          GROUP BY level
	          ORDER BY level`

	rows, err := s.pool.Query(ctx, query, since)
	if err != nil {
		return model.AppLogSummary{}, fmt.Errorf("applog summary query: %w", err)
	}
	defer rows.Close()

	var summary model.AppLogSummary
	summary.LevelBreakdown = make([]model.LevelCount, 0)
	summary.TopServices = make([]model.TopSource, 0)

	for rows.Next() {
		var level string
		var cnt int64
		if err := rows.Scan(&level, &cnt); err != nil {
			return model.AppLogSummary{}, fmt.Errorf("scan level: %w", err)
		}
		summary.Total += cnt
		levelUpper := strings.ToUpper(level)
		if levelUpper == "ERROR" || levelUpper == "FATAL" || levelUpper == "PANIC" {
			summary.Errors += cnt
		}
		if levelUpper == "WARN" || levelUpper == "WARNING" {
			summary.Warnings += cnt
		}
		summary.LevelBreakdown = append(summary.LevelBreakdown, model.LevelCount{
			Level: level,
			Count: cnt,
		})
	}
	if err := rows.Err(); err != nil {
		return model.AppLogSummary{}, fmt.Errorf("level rows: %w", err)
	}

	// Get previous 24h total for trend calculation
	var prevTotal int64
	err = s.pool.QueryRow(ctx, "SELECT count(*) FROM applog_events WHERE received_at >= $1 AND received_at < $2", prevStart, since).Scan(&prevTotal)
	if err != nil {
		return model.AppLogSummary{}, fmt.Errorf("prev total query: %w", err)
	}

	if prevTotal > 0 {
		summary.Trend = float64(summary.Total-prevTotal) / float64(prevTotal) * 100
	}

	// Get top services
	svcQuery := `SELECT service, count(*) AS cnt
	             FROM applog_events
	             WHERE received_at >= $1
	             GROUP BY service
	             ORDER BY cnt DESC
	             LIMIT $2`

	svcRows, err := s.pool.Query(ctx, svcQuery, since, topSourcesLimit)
	if err != nil {
		return model.AppLogSummary{}, fmt.Errorf("top services query: %w", err)
	}
	defer svcRows.Close()

	for svcRows.Next() {
		var name string
		var cnt int64
		if err := svcRows.Scan(&name, &cnt); err != nil {
			return model.AppLogSummary{}, fmt.Errorf("scan service: %w", err)
		}
		summary.TopServices = append(summary.TopServices, model.TopSource{
			Name:  name,
			Count: cnt,
		})
	}
	if err := svcRows.Err(); err != nil {
		return model.AppLogSummary{}, fmt.Errorf("service rows: %w", err)
	}

	// Calculate percentages for top services
	for i := range summary.TopServices {
		if summary.Total > 0 {
			summary.TopServices[i].Pct = float64(summary.TopServices[i].Count) / float64(summary.Total) * 100
		}
	}

	// Calculate percentages for level breakdown
	for i := range summary.LevelBreakdown {
		if summary.Total > 0 {
			summary.LevelBreakdown[i].Pct = float64(summary.LevelBreakdown[i].Count) / float64(summary.Total) * 100
		}
	}

	return summary, nil
}

// LookupJuniperRef returns all Juniper syslog reference entries matching the given name.
func (s *Store) LookupJuniperRef(ctx context.Context, name string) ([]model.JuniperSyslogRef, error) {
	query, args, err := psq.
		Select("id", "name", "message", "description", "type", "severity", "cause", "action", "os", "created_at").
		From("juniper_syslog_ref").
		Where(sq.Eq{"name": name}).
		OrderBy("os ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build juniper ref query: %w", err)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("lookup juniper ref %q: %w", name, err)
	}
	defer rows.Close()

	var refs []model.JuniperSyslogRef
	for rows.Next() {
		var r model.JuniperSyslogRef
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Message, &r.Description,
			&r.Type, &r.Severity, &r.Cause, &r.Action,
			&r.OS, &r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan juniper ref: %w", err)
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// UpsertJuniperRefs inserts or updates Juniper syslog reference entries.
// Returns the number of rows affected.
func (s *Store) UpsertJuniperRefs(ctx context.Context, refs []model.JuniperSyslogRef) (int64, error) {
	if len(refs) == 0 {
		return 0, nil
	}

	const batchSize = 500
	var total int64

	for i := 0; i < len(refs); i += batchSize {
		end := i + batchSize
		if end > len(refs) {
			end = len(refs)
		}
		batch := refs[i:end]

		qb := psq.Insert("juniper_syslog_ref").
			Columns("name", "message", "description", "type", "severity", "cause", "action", "os")

		for _, r := range batch {
			qb = qb.Values(r.Name, r.Message, r.Description, r.Type, r.Severity, r.Cause, r.Action, r.OS)
		}

		qb = qb.Suffix(`ON CONFLICT (name, os) DO UPDATE SET
			message = EXCLUDED.message,
			description = EXCLUDED.description,
			type = EXCLUDED.type,
			severity = EXCLUDED.severity,
			cause = EXCLUDED.cause,
			action = EXCLUDED.action`)

		query, args, err := qb.ToSql()
		if err != nil {
			return total, fmt.Errorf("build upsert query: %w", err)
		}

		tag, err := s.pool.Exec(ctx, query, args...)
		if err != nil {
			return total, fmt.Errorf("upsert juniper refs: %w", err)
		}
		total += tag.RowsAffected()
	}

	return total, nil
}
