package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/notification"
)

// Event kind constants for top issues queries.
const (
	kindSrvlog = "srvlog"
	kindNetlog = "netlog"
	kindApplog = "applog"
)

// AppLog level constants.
const (
	appLevelFatal = "FATAL"
	appLevelError = "ERROR"
	appLevelWarn  = "WARN"
)

// --- Summary Schedules ---

// ListSummarySchedules returns all summary schedules with their channel IDs.
func (s *Store) ListSummarySchedules(ctx context.Context) ([]notification.SummarySchedule, error) {
	const query = `
		SELECT s.id, s.name, s.enabled, s.frequency,
		       s.day_of_week, s.day_of_month,
		       s.time_of_day, s.timezone,
		       s.event_kinds, s.severity_max, s.hostname, s.top_n,
		       s.last_run_at, s.created_at, s.updated_at,
		       COALESCE(array_agg(sc.channel_id ORDER BY sc.channel_id) FILTER (WHERE sc.channel_id IS NOT NULL), '{}')
		FROM summary_schedules s
		LEFT JOIN summary_schedule_channels sc ON sc.schedule_id = s.id
		GROUP BY s.id
		ORDER BY s.id ASC`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list summary schedules: %w", err)
	}
	defer rows.Close()

	var schedules []notification.SummarySchedule
	for rows.Next() {
		ss, err := scanSummarySchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, ss)
	}
	return schedules, rows.Err()
}

// GetSummarySchedule returns a single summary schedule by ID.
func (s *Store) GetSummarySchedule(ctx context.Context, id int64) (notification.SummarySchedule, error) {
	const query = `
		SELECT s.id, s.name, s.enabled, s.frequency,
		       s.day_of_week, s.day_of_month,
		       s.time_of_day, s.timezone,
		       s.event_kinds, s.severity_max, s.hostname, s.top_n,
		       s.last_run_at, s.created_at, s.updated_at,
		       COALESCE(array_agg(sc.channel_id ORDER BY sc.channel_id) FILTER (WHERE sc.channel_id IS NOT NULL), '{}')
		FROM summary_schedules s
		LEFT JOIN summary_schedule_channels sc ON sc.schedule_id = s.id
		WHERE s.id = $1
		GROUP BY s.id`

	row := s.pool.QueryRow(ctx, query, id)
	ss, err := scanSummaryScheduleRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return notification.SummarySchedule{}, err
	}
	if err != nil {
		return notification.SummarySchedule{}, fmt.Errorf("get summary schedule %d: %w", id, err)
	}
	return ss, nil
}

// CreateSummarySchedule inserts a new summary schedule and its channel associations.
func (s *Store) CreateSummarySchedule(ctx context.Context, ss notification.SummarySchedule) (notification.SummarySchedule, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return notification.SummarySchedule{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Rollback after commit is a no-op.

	query, args, err := psq.
		Insert("summary_schedules").
		Columns(
			"name", "enabled", "frequency",
			"day_of_week", "day_of_month",
			"time_of_day", "timezone",
			"event_kinds", "severity_max", "hostname", "top_n",
		).
		Values(
			ss.Name, ss.Enabled, ss.Frequency,
			ss.DayOfWeek, ss.DayOfMonth,
			ss.TimeOfDay, ss.Timezone,
			ss.EventKinds, ss.SeverityMax, ss.Hostname, ss.TopN,
		).
		Suffix("RETURNING id, created_at, updated_at").
		ToSql()
	if err != nil {
		return notification.SummarySchedule{}, fmt.Errorf("build query: %w", err)
	}

	err = tx.QueryRow(ctx, query, args...).Scan(&ss.ID, &ss.CreatedAt, &ss.UpdatedAt)
	if err != nil {
		return notification.SummarySchedule{}, fmt.Errorf("insert summary schedule: %w", err)
	}

	if err := insertScheduleChannels(ctx, tx, ss.ID, ss.ChannelIDs); err != nil {
		return notification.SummarySchedule{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return notification.SummarySchedule{}, fmt.Errorf("commit tx: %w", err)
	}
	return ss, nil
}

// UpdateSummarySchedule updates a schedule and replaces its channel associations.
func (s *Store) UpdateSummarySchedule(ctx context.Context, id int64, ss notification.SummarySchedule) (notification.SummarySchedule, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return notification.SummarySchedule{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Rollback after commit is a no-op.

	query, args, err := psq.
		Update("summary_schedules").
		Set("name", ss.Name).
		Set("enabled", ss.Enabled).
		Set("frequency", ss.Frequency).
		Set("day_of_week", ss.DayOfWeek).
		Set("day_of_month", ss.DayOfMonth).
		Set("time_of_day", ss.TimeOfDay).
		Set("timezone", ss.Timezone).
		Set("event_kinds", ss.EventKinds).
		Set("severity_max", ss.SeverityMax).
		Set("hostname", ss.Hostname).
		Set("top_n", ss.TopN).
		Set("updated_at", time.Now()).
		Where(sq.Eq{"id": id}).
		Suffix("RETURNING id, name, enabled, frequency, day_of_week, day_of_month, time_of_day, timezone, event_kinds, severity_max, hostname, top_n, last_run_at, created_at, updated_at").
		ToSql()
	if err != nil {
		return notification.SummarySchedule{}, fmt.Errorf("build query: %w", err)
	}

	err = tx.QueryRow(ctx, query, args...).Scan(
		&ss.ID, &ss.Name, &ss.Enabled, &ss.Frequency,
		&ss.DayOfWeek, &ss.DayOfMonth,
		&ss.TimeOfDay, &ss.Timezone,
		&ss.EventKinds, &ss.SeverityMax, &ss.Hostname, &ss.TopN,
		&ss.LastRunAt, &ss.CreatedAt, &ss.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return notification.SummarySchedule{}, err
	}
	if err != nil {
		return notification.SummarySchedule{}, fmt.Errorf("update summary schedule %d: %w", id, err)
	}

	// Replace channel associations.
	delQuery, delArgs, err := psq.Delete("summary_schedule_channels").Where(sq.Eq{"schedule_id": id}).ToSql()
	if err != nil {
		return notification.SummarySchedule{}, fmt.Errorf("build delete query: %w", err)
	}
	if _, err := tx.Exec(ctx, delQuery, delArgs...); err != nil {
		return notification.SummarySchedule{}, fmt.Errorf("delete schedule channels: %w", err)
	}

	if err := insertScheduleChannels(ctx, tx, id, ss.ChannelIDs); err != nil {
		return notification.SummarySchedule{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return notification.SummarySchedule{}, fmt.Errorf("commit tx: %w", err)
	}
	return ss, nil
}

// DeleteSummarySchedule deletes a summary schedule by ID.
func (s *Store) DeleteSummarySchedule(ctx context.Context, id int64) error {
	query, args, err := psq.
		Delete("summary_schedules").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	tag, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete summary schedule %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// UpdateSummaryScheduleLastRun updates the last_run_at timestamp.
func (s *Store) UpdateSummaryScheduleLastRun(ctx context.Context, id int64, t time.Time) error {
	query, args, err := psq.
		Update("summary_schedules").
		Set("last_run_at", t).
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	_, err = s.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update summary schedule last_run_at %d: %w", id, err)
	}
	return nil
}

// --- Top Issues Query ---

// GetTopIssues returns the highest-severity, highest-frequency log messages
// across the requested event kinds for a given time range.
func (s *Store) GetTopIssues(ctx context.Context, since time.Time, kinds []string, severityMax *int, hostname string, topN int) ([]notification.TopIssue, error) {
	var allIssues []notification.TopIssue

	for _, kind := range kinds {
		switch kind {
		case kindSrvlog:
			issues, err := s.getTopSyslogIssues(ctx, "srvlog_events", kindSrvlog, since, severityMax, hostname, topN)
			if err != nil {
				return nil, fmt.Errorf("srvlog top issues: %w", err)
			}
			allIssues = append(allIssues, issues...)
		case kindNetlog:
			issues, err := s.getTopSyslogIssues(ctx, "netlog_events", kindNetlog, since, severityMax, hostname, topN)
			if err != nil {
				return nil, fmt.Errorf("netlog top issues: %w", err)
			}
			allIssues = append(allIssues, issues...)
		case kindApplog:
			issues, err := s.getTopAppLogIssues(ctx, since, hostname, topN)
			if err != nil {
				return nil, fmt.Errorf("applog top issues: %w", err)
			}
			allIssues = append(allIssues, issues...)
		}
	}

	// Sort by severity ASC (most critical first), then by count DESC.
	sort.Slice(allIssues, func(i, j int) bool {
		if allIssues[i].Severity != allIssues[j].Severity {
			return allIssues[i].Severity < allIssues[j].Severity
		}
		return allIssues[i].Count > allIssues[j].Count
	})

	if len(allIssues) > topN {
		allIssues = allIssues[:topN]
	}
	return allIssues, nil
}

func (s *Store) getTopSyslogIssues(ctx context.Context, table, kind string, since time.Time, severityMax *int, hostname string, topN int) ([]notification.TopIssue, error) {
	qb := psq.
		Select("severity", "hostname", "programname", "left(message, 200) AS message", "COUNT(*) AS cnt").
		From(table).
		Where(sq.GtOrEq{"received_at": since}).
		GroupBy("severity", "hostname", "programname", "left(message, 200)").
		OrderBy("severity ASC", "cnt DESC").
		Limit(uint64(topN))

	if severityMax != nil {
		qb = qb.Where(sq.LtOrEq{"severity": *severityMax})
	}
	if hostname != "" {
		qb = qb.Where(sq.Eq{"hostname": hostname})
	}

	query, args, err := qb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", table, err)
	}
	defer rows.Close()

	var issues []notification.TopIssue
	for rows.Next() {
		var ti notification.TopIssue
		if err := rows.Scan(&ti.Severity, &ti.Source, &ti.Program, &ti.Message, &ti.Count); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		ti.Kind = kind
		ti.Label = model.SeverityLabel(ti.Severity)
		issues = append(issues, ti)
	}
	return issues, rows.Err()
}

func (s *Store) getTopAppLogIssues(ctx context.Context, since time.Time, hostname string, topN int) ([]notification.TopIssue, error) {
	qb := psq.
		Select("level", "host", "service", "left(msg, 200) AS message", "COUNT(*) AS cnt").
		From("applog_events").
		Where(sq.GtOrEq{"received_at": since}).
		Where(sq.NotEq{"level": "DEBUG"}).
		Where(sq.NotEq{"level": "INFO"}).
		GroupBy("level", "host", "service", "left(msg, 200)").
		OrderBy("CASE level WHEN 'FATAL' THEN 0 WHEN 'ERROR' THEN 1 WHEN 'WARN' THEN 2 ELSE 3 END ASC", "cnt DESC").
		Limit(uint64(topN))

	if hostname != "" {
		qb = qb.Where(sq.Eq{"host": hostname})
	}

	query, args, err := qb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query applog_events: %w", err)
	}
	defer rows.Close()

	var issues []notification.TopIssue
	for rows.Next() {
		var ti notification.TopIssue
		if err := rows.Scan(&ti.Label, &ti.Source, &ti.Program, &ti.Message, &ti.Count); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		ti.Kind = "applog"
		ti.Severity = appLogLevelToSeverity(ti.Label)
		issues = append(issues, ti)
	}
	return issues, rows.Err()
}

// appLogLevelToSeverity maps applog text levels to numeric severity for sorting.
func appLogLevelToSeverity(level string) int {
	switch level {
	case appLevelFatal:
		return 0
	case appLevelError:
		return 3
	case appLevelWarn:
		return 4
	default:
		return 6
	}
}

// --- Scan helpers ---

func scanSummarySchedule(rows pgx.Rows) (notification.SummarySchedule, error) {
	var ss notification.SummarySchedule
	var timeOfDay time.Time
	if err := rows.Scan(
		&ss.ID, &ss.Name, &ss.Enabled, &ss.Frequency,
		&ss.DayOfWeek, &ss.DayOfMonth,
		&timeOfDay, &ss.Timezone,
		&ss.EventKinds, &ss.SeverityMax, &ss.Hostname, &ss.TopN,
		&ss.LastRunAt, &ss.CreatedAt, &ss.UpdatedAt,
		&ss.ChannelIDs,
	); err != nil {
		return notification.SummarySchedule{}, fmt.Errorf("scan summary schedule: %w", err)
	}
	ss.TimeOfDay = timeOfDay.Format("15:04")
	if ss.EventKinds == nil {
		ss.EventKinds = []string{}
	}
	if ss.ChannelIDs == nil {
		ss.ChannelIDs = []int64{}
	}
	return ss, nil
}

func scanSummaryScheduleRow(row pgx.Row) (notification.SummarySchedule, error) {
	var ss notification.SummarySchedule
	var timeOfDay time.Time
	if err := row.Scan(
		&ss.ID, &ss.Name, &ss.Enabled, &ss.Frequency,
		&ss.DayOfWeek, &ss.DayOfMonth,
		&timeOfDay, &ss.Timezone,
		&ss.EventKinds, &ss.SeverityMax, &ss.Hostname, &ss.TopN,
		&ss.LastRunAt, &ss.CreatedAt, &ss.UpdatedAt,
		&ss.ChannelIDs,
	); err != nil {
		return notification.SummarySchedule{}, err
	}
	ss.TimeOfDay = timeOfDay.Format("15:04")
	if ss.EventKinds == nil {
		ss.EventKinds = []string{}
	}
	if ss.ChannelIDs == nil {
		ss.ChannelIDs = []int64{}
	}
	return ss, nil
}

// insertScheduleChannels inserts channel associations for a schedule within a transaction.
func insertScheduleChannels(ctx context.Context, tx pgx.Tx, scheduleID int64, channelIDs []int64) error {
	for _, chID := range channelIDs {
		query, args, err := psq.
			Insert("summary_schedule_channels").
			Columns("schedule_id", "channel_id").
			Values(scheduleID, chID).
			ToSql()
		if err != nil {
			return fmt.Errorf("build schedule channel query: %w", err)
		}
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("insert schedule channel (%d, %d): %w", scheduleID, chID, err)
		}
	}
	return nil
}
