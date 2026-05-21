// Package scheduler runs periodic background tasks (notification summaries,
// recurring analysis reports) on a 60-second tick.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// parseTime parses an "HH:MM" string into 24-hour hour and minute components.
// Shared by all scheduler types that take a time_of_day field.
func parseTime(s string) (int, int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, fmt.Errorf("parse schedule time %q: %w", s, err)
	}
	return t.Hour(), t.Minute(), nil
}

// analysisFiringWindow is the lateness tolerance applied to an analysis
// schedule's scheduled_time. With a 60s tick and a 5min window, a queue-full
// or duplicate-active retry has roughly five opportunities to succeed before
// the period is missed. The summary scheduler uses an exact-minute match;
// analysis tolerates retry because enqueues can transiently fail.
const analysisFiringWindow = 5 * time.Minute

// AnalysisScheduleStore is the data access surface for the analysis scheduler.
type AnalysisScheduleStore interface {
	ListAnalysisSchedules(ctx context.Context) ([]model.AnalysisSchedule, error)
	GetAnalysisSchedule(ctx context.Context, id int64) (model.AnalysisSchedule, error)
	UpdateAnalysisScheduleLastRun(ctx context.Context, id int64, t time.Time) error
}

// AnalysisEnqueuer is the worker surface used to submit jobs.
type AnalysisEnqueuer interface {
	Enqueue(ctx context.Context, req model.AnalysisReport) (model.AnalysisReport, error)
}

// AnalysisScheduler ticks every 60s and fires due analysis schedules. Failed
// enqueues do not stamp last_run_at, so subsequent ticks within the firing
// window retry until either the enqueue succeeds or the window passes.
type AnalysisScheduler struct {
	store    AnalysisScheduleStore
	enqueuer AnalysisEnqueuer
	logger   *slog.Logger
}

// NewAnalysisScheduler constructs an analysis scheduler.
func NewAnalysisScheduler(store AnalysisScheduleStore, enqueuer AnalysisEnqueuer, logger *slog.Logger) *AnalysisScheduler {
	return &AnalysisScheduler{
		store:    store,
		enqueuer: enqueuer,
		logger:   logger.With("component", "analysis-scheduler"),
	}
}

// Start blocks until ctx is cancelled.
func (s *AnalysisScheduler) Start(ctx context.Context) {
	s.logger.Info("analysis scheduler started")
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("analysis scheduler stopped")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *AnalysisScheduler) tick(ctx context.Context) {
	schedules, err := s.store.ListAnalysisSchedules(ctx)
	if err != nil {
		s.logger.Error("list analysis schedules failed", "err", err)
		return
	}
	for _, sched := range schedules {
		if !sched.Enabled {
			continue
		}
		if !s.isDue(sched) {
			continue
		}
		s.runSchedule(ctx, sched)
	}
}

// isDue checks whether a schedule should fire on this tick. Returns true when:
//   - the current wall time is within [scheduled, scheduled+firingWindow] in the
//     schedule's timezone,
//   - the frequency-specific day check passes, and
//   - last_run_at is older than half the period (prevents double-fire).
func (s *AnalysisScheduler) isDue(sched model.AnalysisSchedule) bool {
	loc, err := time.LoadLocation(sched.Timezone)
	if err != nil {
		s.logger.Error("invalid timezone", "schedule", sched.Name, "timezone", sched.Timezone, "err", err)
		return false
	}
	hour, minute, err := parseTime(sched.TimeOfDay)
	if err != nil {
		s.logger.Error("invalid time_of_day", "schedule", sched.Name, "time_of_day", sched.TimeOfDay, "err", err)
		return false
	}

	now := time.Now().In(loc)
	scheduled := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc)
	if now.Before(scheduled) || now.Sub(scheduled) > analysisFiringWindow {
		return false
	}

	switch sched.Frequency {
	case freqWeekly:
		if sched.DayOfWeek == nil || int(now.Weekday()) != *sched.DayOfWeek {
			return false
		}
	case freqMonthly:
		if sched.DayOfMonth == nil || now.Day() != *sched.DayOfMonth {
			return false
		}
	}

	if sched.LastRunAt != nil {
		minInterval := periodDuration(sched.Frequency) / 2
		if time.Since(*sched.LastRunAt) < minInterval {
			return false
		}
	}

	return true
}

// scheduledPeriodEnd returns the period_end for a schedule firing today: the
// scheduled wall-clock time in the schedule's timezone, expressed in UTC and
// truncated to the minute. Used so retries within the firing window all land
// on the same period_end (and therefore the same slug + duplicate-active key).
func scheduledPeriodEnd(sched model.AnalysisSchedule) (time.Time, error) {
	loc, err := time.LoadLocation(sched.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %q: %w", sched.Timezone, err)
	}
	hour, minute, err := parseTime(sched.TimeOfDay)
	if err != nil {
		return time.Time{}, err
	}
	now := time.Now().In(loc)
	return time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc).UTC().Truncate(time.Minute), nil
}

func (s *AnalysisScheduler) runSchedule(ctx context.Context, sched model.AnalysisSchedule) {
	period := periodDuration(sched.Frequency)
	periodEnd, err := scheduledPeriodEnd(sched)
	if err != nil {
		s.logger.Error("scheduled period_end failed", "schedule", sched.Name, "err", err)
		return
	}
	periodStart := periodEnd.Add(-period)

	req := model.AnalysisReport{
		Feed:        sched.Feed,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
	}

	s.logger.Info("firing analysis schedule",
		"schedule", sched.Name, "feed", sched.Feed, "period", period)

	if _, err := s.enqueuer.Enqueue(ctx, req); err != nil {
		// Leave last_run_at unchanged so the schedule retries on the next tick
		// while still inside the firing window.
		s.logger.Warn("analysis schedule enqueue failed, will retry within firing window",
			"schedule", sched.Name, "err", err)
		return
	}

	if err := s.store.UpdateAnalysisScheduleLastRun(ctx, sched.ID, time.Now().UTC()); err != nil {
		s.logger.Error("failed to update analysis schedule last_run_at",
			"schedule", sched.Name, "err", err)
	}
}

// RunNow enqueues a one-off run for the schedule identified by id, used by the
// "run now" admin action. The period window matches what a scheduled tick
// would produce, so a same-minute manual click and scheduled fire collide on
// the duplicate-active index rather than running twice.
func (s *AnalysisScheduler) RunNow(ctx context.Context, id int64) error {
	sched, err := s.store.GetAnalysisSchedule(ctx, id)
	if err != nil {
		return fmt.Errorf("get schedule: %w", err)
	}

	periodEnd, err := scheduledPeriodEnd(sched)
	if err != nil {
		return err
	}
	periodStart := periodEnd.Add(-periodDuration(sched.Frequency))

	_, err = s.enqueuer.Enqueue(ctx, model.AnalysisReport{
		Feed:        sched.Feed,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
	})
	return err
}
