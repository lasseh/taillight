package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
)

// analysisScheduleColumns lists the columns selected for schedule reads.
const analysisScheduleColumns = "id, name, enabled, feed, frequency, " +
	"day_of_week, day_of_month, time_of_day, timezone, " +
	"last_run_at, created_at, updated_at"

// ListAnalysisSchedules returns all analysis schedules ordered by id.
func (s *Store) ListAnalysisSchedules(ctx context.Context) ([]model.AnalysisSchedule, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+analysisScheduleColumns+` FROM analysis_schedules ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list analysis schedules: %w", err)
	}
	defer rows.Close()

	var schedules []model.AnalysisSchedule
	for rows.Next() {
		sched, err := scanAnalysisSchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, sched)
	}
	return schedules, rows.Err()
}

// GetAnalysisSchedule returns a single analysis schedule by ID.
func (s *Store) GetAnalysisSchedule(ctx context.Context, id int64) (model.AnalysisSchedule, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+analysisScheduleColumns+` FROM analysis_schedules WHERE id=$1`, id)
	sched, err := scanAnalysisSchedule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.AnalysisSchedule{}, err
	}
	if err != nil {
		return model.AnalysisSchedule{}, fmt.Errorf("get analysis schedule %d: %w", id, err)
	}
	return sched, nil
}

// CreateAnalysisSchedule inserts a new analysis schedule.
func (s *Store) CreateAnalysisSchedule(ctx context.Context, sched model.AnalysisSchedule) (model.AnalysisSchedule, error) {
	query, args, err := psq.
		Insert("analysis_schedules").
		Columns("name", "enabled", "feed", "frequency",
			"day_of_week", "day_of_month", "time_of_day", "timezone").
		Values(sched.Name, sched.Enabled, sched.Feed, sched.Frequency,
			sched.DayOfWeek, sched.DayOfMonth, sched.TimeOfDay, sched.Timezone).
		Suffix("RETURNING id, created_at, updated_at").
		ToSql()
	if err != nil {
		return model.AnalysisSchedule{}, fmt.Errorf("build insert analysis schedule: %w", err)
	}

	err = s.pool.QueryRow(ctx, query, args...).Scan(&sched.ID, &sched.CreatedAt, &sched.UpdatedAt)
	if err != nil {
		return model.AnalysisSchedule{}, fmt.Errorf("insert analysis schedule: %w", err)
	}
	return sched, nil
}

// UpdateAnalysisSchedule updates an existing analysis schedule by ID.
func (s *Store) UpdateAnalysisSchedule(ctx context.Context, id int64, sched model.AnalysisSchedule) (model.AnalysisSchedule, error) {
	query, args, err := psq.
		Update("analysis_schedules").
		Set("name", sched.Name).
		Set("enabled", sched.Enabled).
		Set("feed", sched.Feed).
		Set("frequency", sched.Frequency).
		Set("day_of_week", sched.DayOfWeek).
		Set("day_of_month", sched.DayOfMonth).
		Set("time_of_day", sched.TimeOfDay).
		Set("timezone", sched.Timezone).
		Set("updated_at", time.Now()).
		Where(sq.Eq{"id": id}).
		Suffix("RETURNING " + analysisScheduleColumns).
		ToSql()
	if err != nil {
		return model.AnalysisSchedule{}, fmt.Errorf("build update analysis schedule: %w", err)
	}

	row := s.pool.QueryRow(ctx, query, args...)
	updated, err := scanAnalysisSchedule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.AnalysisSchedule{}, err
	}
	if err != nil {
		return model.AnalysisSchedule{}, fmt.Errorf("update analysis schedule %d: %w", id, err)
	}
	return updated, nil
}

// DeleteAnalysisSchedule removes a schedule by ID.
func (s *Store) DeleteAnalysisSchedule(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM analysis_schedules WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete analysis schedule %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// UpdateAnalysisScheduleLastRun stamps last_run_at after a successful enqueue.
func (s *Store) UpdateAnalysisScheduleLastRun(ctx context.Context, id int64, t time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE analysis_schedules SET last_run_at=$2 WHERE id=$1`, id, t)
	if err != nil {
		return fmt.Errorf("update analysis schedule %d last_run_at: %w", id, err)
	}
	return nil
}

// scanAnalysisSchedule reads a row into AnalysisSchedule. pgx.Rows satisfies
// pgx.Row, so this works for both Query (loop) and QueryRow (single) call sites.
func scanAnalysisSchedule(row pgx.Row) (model.AnalysisSchedule, error) {
	var sched model.AnalysisSchedule
	var timeOfDay time.Time
	if err := row.Scan(
		&sched.ID, &sched.Name, &sched.Enabled, &sched.Feed, &sched.Frequency,
		&sched.DayOfWeek, &sched.DayOfMonth, &timeOfDay, &sched.Timezone,
		&sched.LastRunAt, &sched.CreatedAt, &sched.UpdatedAt,
	); err != nil {
		return model.AnalysisSchedule{}, err
	}
	sched.TimeOfDay = timeOfDay.Format("15:04")
	return sched, nil
}
