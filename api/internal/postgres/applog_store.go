package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
)

var appLogColumns = []string{
	"id", "received_at", "timestamp", "level", "service",
	"component", "host", "msg", "source", "attrs",
}

// allowedAppLogMetaColumns is a whitelist of columns that can be used in log meta queries.
var allowedAppLogMetaColumns = map[string]struct{}{
	"service":   {},
	"component": {},
	"host":      {},
}

// InsertLogBatch inserts a batch of log events using pgx Batch API for efficiency.
// Returns the inserted events with populated ID and ReceivedAt.
func (s *Store) InsertLogBatch(ctx context.Context, events []model.AppLogEvent) ([]model.AppLogEvent, error) {
	if len(events) == 0 {
		return nil, nil
	}

	batch := &pgx.Batch{}
	const insertSQL = `INSERT INTO applog_events (timestamp, level, service, component, host, msg, source, attrs)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, received_at, timestamp, level, service, component, host, msg, source, attrs`

	for _, e := range events {
		var attrsBytes []byte
		if e.Attrs != nil {
			attrsBytes = []byte(e.Attrs)
		}
		batch.Queue(insertSQL, e.Timestamp, e.Level, e.Service, e.Component, e.Host, e.Msg, e.Source, attrsBytes)
	}

	results := s.pool.SendBatch(ctx, batch)

	inserted := make([]model.AppLogEvent, 0, len(events))
	for range events {
		var e model.AppLogEvent
		var attrs []byte
		err := results.QueryRow().Scan(
			&e.ID, &e.ReceivedAt, &e.Timestamp,
			&e.Level, &e.Service, &e.Component,
			&e.Host, &e.Msg, &e.Source, &attrs,
		)
		if err != nil {
			results.Close() //nolint:errcheck
			return nil, fmt.Errorf("insert log event: %w", err)
		}
		if attrs != nil {
			e.Attrs = json.RawMessage(attrs)
		}
		inserted = append(inserted, e)
	}

	if err := results.Close(); err != nil {
		return nil, fmt.Errorf("close batch results: %w", err)
	}

	return inserted, nil
}

// GetAppLog returns a single log event by ID.
func (s *Store) GetAppLog(ctx context.Context, id int64) (model.AppLogEvent, error) {
	query, args, err := psq.
		Select(appLogColumns...).
		From("applog_events").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return model.AppLogEvent{}, fmt.Errorf("build query: %w", err)
	}

	var e model.AppLogEvent
	err = scanAppLog(s.pool.QueryRow(ctx, query, args...), &e)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.AppLogEvent{}, err
	}
	if err != nil {
		return model.AppLogEvent{}, fmt.Errorf("get log event %d: %w", id, err)
	}
	return e, nil
}

// ListAppLogs returns log events matching the filter with cursor-based pagination.
func (s *Store) ListAppLogs(ctx context.Context, f model.AppLogFilter, cursor *model.Cursor, limit int) ([]model.AppLogEvent, *model.Cursor, error) {
	qb := psq.Select(appLogColumns...).From("applog_events")
	qb = applyAppLogFilter(qb, f)

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
		return nil, nil, fmt.Errorf("list log events: %w", err)
	}
	defer rows.Close()

	events, err := collectAppLogs(rows)
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

// ListAppLogsSince returns log events with id > sinceID matching the filter,
// ordered chronologically (ASC). Used for SSE Last-Event-ID reconnect backfill.
func (s *Store) ListAppLogsSince(ctx context.Context, f model.AppLogFilter, sinceID int64, limit int) ([]model.AppLogEvent, error) {
	qb := psq.Select(appLogColumns...).From("applog_events")
	qb = applyAppLogFilter(qb, f)
	qb = qb.Where(sq.Gt{"id": sinceID})
	qb = qb.OrderBy("id ASC").Limit(uint64(limit))

	query, args, err := qb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list log events since %d: %w", sinceID, err)
	}
	defer rows.Close()

	return collectAppLogs(rows)
}

// ListServices returns distinct service names ordered alphabetically.
func (s *Store) ListServices(ctx context.Context) ([]string, error) {
	return s.listAppLogDistinctStrings(ctx, "service")
}

// ListComponents returns distinct component names ordered alphabetically.
func (s *Store) ListComponents(ctx context.Context) ([]string, error) {
	return s.listAppLogDistinctStrings(ctx, "component")
}

// ListAppLogHosts returns distinct host names ordered alphabetically.
func (s *Store) ListAppLogHosts(ctx context.Context) ([]string, error) {
	return s.listAppLogDistinctStrings(ctx, "host")
}

func (s *Store) listAppLogDistinctStrings(ctx context.Context, column string) ([]string, error) {
	if _, ok := allowedAppLogMetaColumns[column]; !ok {
		return nil, fmt.Errorf("disallowed log meta column: %s", column)
	}
	rows, err := s.pool.Query(ctx, "SELECT value FROM applog_meta_cache WHERE column_name = $1 ORDER BY value LIMIT $2", column, metaLimit)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", column, err)
	}
	defer rows.Close()

	values := make([]string, 0, 64)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan %s: %w", column, err)
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

func applyAppLogFilter(qb sq.SelectBuilder, f model.AppLogFilter) sq.SelectBuilder {
	if f.Service != "" {
		qb = qb.Where(sq.Eq{"service": f.Service})
	}
	if f.Component != "" {
		qb = qb.Where(sq.Eq{"component": f.Component})
	}
	if f.Host != "" {
		if strings.Contains(f.Host, "*") {
			pattern := strings.ReplaceAll(escapeLike(f.Host), "*", "%")
			qb = qb.Where("host ILIKE ?", pattern)
		} else {
			qb = qb.Where(sq.Eq{"host": f.Host})
		}
	}
	if f.LevelExact != "" {
		qb = qb.Where(sq.Eq{"level": f.LevelExact})
	}
	if f.Level != "" {
		// Level filter means "at or above this level".
		rank := model.AppLogLevelRank(f.Level)
		levels := appLogLevelsAtOrAbove(rank)
		if len(levels) > 0 {
			qb = qb.Where(sq.Eq{"level": levels})
		}
	}
	if f.Search != "" {
		qb = qb.Where("search_vector @@ plainto_tsquery('simple', ?)", f.Search)
	}
	if f.From != nil {
		qb = qb.Where(sq.GtOrEq{"received_at": *f.From})
	}
	if f.To != nil {
		qb = qb.Where(sq.LtOrEq{"received_at": *f.To})
	}
	return qb
}

// appLogLevelsAtOrAbove returns all level strings with rank >= minRank.
func appLogLevelsAtOrAbove(minRank int) []string {
	var levels []string
	for level, rank := range model.ValidAppLogLevels {
		if rank >= minRank {
			levels = append(levels, level)
		}
	}
	return levels
}

func collectAppLogs(rows pgx.Rows) ([]model.AppLogEvent, error) {
	events := make([]model.AppLogEvent, 0, 64)
	for rows.Next() {
		var e model.AppLogEvent
		if err := scanAppLog(rows, &e); err != nil {
			return nil, fmt.Errorf("scan log event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return events, nil
}

func scanAppLog(row pgx.Row, e *model.AppLogEvent) error {
	var attrs []byte
	err := row.Scan(
		&e.ID, &e.ReceivedAt, &e.Timestamp, &e.Level, &e.Service,
		&e.Component, &e.Host, &e.Msg, &e.Source, &attrs,
	)
	if err != nil {
		return err
	}
	if attrs != nil {
		e.Attrs = json.RawMessage(attrs)
	}
	return nil
}

// GetAppLogDeviceSummary returns aggregated applog information for the given host.
// It fetches last-seen time, level breakdown (7d), top normalized messages (7d),
// and recent error/fatal logs using a single pgx.Batch round-trip.
func (s *Store) GetAppLogDeviceSummary(ctx context.Context, host string) (model.AppLogDeviceSummary, error) {
	summary := model.AppLogDeviceSummary{
		Host:           host,
		LevelBreakdown: make([]model.LevelCount, 0),
		TopMessages:    make([]model.AppLogTopMessage, 0),
		ErrorLogs:      make([]model.AppLogEvent, 0),
	}

	since := time.Now().UTC().Add(-7 * 24 * time.Hour)

	// Build error logs query via squirrel for column list consistency.
	errQuery, errArgs, err := psq.
		Select(appLogColumns...).
		From("applog_events").
		Where(sq.Eq{"host": host}).
		Where(sq.GtOrEq{"received_at": since}).
		Where(sq.Eq{"level": []string{"ERROR", "FATAL"}}).
		OrderBy("received_at DESC", "id DESC").
		Limit(50).
		ToSql()
	if err != nil {
		return summary, fmt.Errorf("build error logs query: %w", err)
	}

	// Send all 4 queries in a single round-trip.
	batch := &pgx.Batch{}

	// Q1: last seen (most recent event for this host).
	// Use received_at (not timestamp) so the idx_applog_host_received index
	// can serve this as an index-only scan.
	batch.Queue(
		"SELECT MAX(received_at) FROM applog_events WHERE host = $1",
		host,
	)

	// Q2: level breakdown (7 days).
	batch.Queue(
		`SELECT level, count(*) AS cnt
		 FROM applog_events
		 WHERE host = $1 AND received_at >= $2
		 GROUP BY level
		 ORDER BY level`,
		host, since,
	)

	// Q3: top normalized messages (24h).
	// Uses the pre-computed msg_pattern column (populated by trigger on INSERT)
	// so no regex runs at query time. Rows with empty msg_pattern (pre-migration
	// data) are excluded — fully populated within 24h of deploying migration 000012.
	msgSince := time.Now().UTC().Add(-24 * time.Hour)
	batch.Queue(
		`SELECT msg_pattern AS pattern,
		     min(msg) AS sample,
		     count(*) AS cnt,
		     max(id) AS latest_id,
		     max(received_at) AS latest_at,
		     min(level) AS level
		 FROM applog_events
		 WHERE host = $1 AND received_at >= $2 AND msg_pattern != ''
		 GROUP BY msg_pattern
		 ORDER BY cnt DESC, msg_pattern
		 LIMIT 10`,
		host, msgSince,
	)

	// Q4: recent error logs.
	batch.Queue(errQuery, errArgs...)

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close() //nolint:errcheck // best-effort close

	// R1: last seen.
	var lastSeen *time.Time
	if err := results.QueryRow().Scan(&lastSeen); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return summary, fmt.Errorf("applog device last seen: %w", err)
	}
	summary.LastSeenAt = lastSeen

	// R2: level breakdown.
	levelRows, err := results.Query()
	if err != nil {
		return summary, fmt.Errorf("applog device level breakdown: %w", err)
	}

	var total int64
	for levelRows.Next() {
		var level string
		var cnt int64
		if err := levelRows.Scan(&level, &cnt); err != nil {
			return summary, fmt.Errorf("scan level: %w", err)
		}
		total += cnt
		upper := strings.ToUpper(level)
		if upper == "ERROR" || upper == "FATAL" {
			summary.ErrorCount += cnt
		}
		summary.LevelBreakdown = append(summary.LevelBreakdown, model.LevelCount{
			Level: level,
			Count: cnt,
		})
	}
	levelRows.Close()

	summary.TotalCount = total
	for i := range summary.LevelBreakdown {
		if total > 0 {
			summary.LevelBreakdown[i].Pct = float64(summary.LevelBreakdown[i].Count) / float64(total) * 100
		}
	}

	// R3: top messages.
	msgRows, err := results.Query()
	if err != nil {
		return summary, fmt.Errorf("applog device top messages: %w", err)
	}

	for msgRows.Next() {
		var tm model.AppLogTopMessage
		if err := msgRows.Scan(&tm.Pattern, &tm.Sample, &tm.Count, &tm.LatestID, &tm.LatestAt, &tm.Level); err != nil {
			return summary, fmt.Errorf("scan top message: %w", err)
		}
		summary.TopMessages = append(summary.TopMessages, tm)
	}
	msgRows.Close()

	// R4: error logs.
	errRows, err := results.Query()
	if err != nil {
		return summary, fmt.Errorf("applog device error logs: %w", err)
	}

	errEvents, err := collectAppLogs(errRows)
	if err != nil {
		return summary, fmt.Errorf("collect error logs: %w", err)
	}
	if errEvents != nil {
		summary.ErrorLogs = errEvents
	}

	return summary, nil
}
