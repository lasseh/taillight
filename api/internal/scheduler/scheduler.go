// Package scheduler provides a simple daily scheduler for analysis runs.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Runner is the interface for triggering an analysis run.
type Runner interface {
	Run(ctx context.Context) (int64, error)
}

// Config holds scheduler configuration.
type Config struct {
	Enabled    bool
	ScheduleAt string // "HH:MM" in UTC.
}

// Scheduler runs the analyzer on a daily schedule.
type Scheduler struct {
	runner Runner
	cfg    Config
	logger *slog.Logger
}

// New creates a new Scheduler.
func New(runner Runner, cfg Config, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		runner: runner,
		cfg:    cfg,
		logger: logger,
	}
}

// Start blocks and runs the analyzer at the configured daily time.
// Returns when ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	hour, minute, err := parseTime(s.cfg.ScheduleAt)
	if err != nil {
		s.logger.Error("invalid schedule_at, scheduler disabled", "schedule_at", s.cfg.ScheduleAt, "err", err)
		return
	}

	s.logger.Info("analysis scheduler started", "schedule_at", s.cfg.ScheduleAt)

	for {
		next := nextRun(hour, minute)
		s.logger.Info("next analysis scheduled", "at", next.Format(time.RFC3339))

		select {
		case <-ctx.Done():
			s.logger.Info("analysis scheduler stopped")
			return
		case <-time.After(time.Until(next)):
			s.logger.Info("triggering scheduled analysis")
			if id, err := s.runner.Run(ctx); err != nil {
				s.logger.Error("scheduled analysis failed", "err", err)
			} else {
				s.logger.Info("scheduled analysis completed", "report_id", id)
			}
		}
	}
}

// nextRun returns the next occurrence of HH:MM UTC.
func nextRun(hour, minute int) time.Time {
	now := time.Now().UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

// parseTime parses "HH:MM" into hour and minute.
func parseTime(s string) (int, int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, fmt.Errorf("parse schedule time %q: %w", s, err)
	}
	return t.Hour(), t.Minute(), nil
}
