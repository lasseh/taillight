package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/notification"
)

// Frequency constants.
const (
	freqDaily   = "daily"
	freqWeekly  = "weekly"
	freqMonthly = "monthly"
)

// SummaryStore defines the data access interface for the summary scheduler.
type SummaryStore interface {
	ListSummarySchedules(ctx context.Context) ([]notification.SummarySchedule, error)
	UpdateSummaryScheduleLastRun(ctx context.Context, id int64, t time.Time) error
	GetSrvlogSummary(ctx context.Context, rangeDur time.Duration) (model.SyslogSummary, error)
	GetNetlogSummary(ctx context.Context, rangeDur time.Duration) (model.SyslogSummary, error)
	GetAppLogSummary(ctx context.Context, rangeDur time.Duration) (model.AppLogSummary, error)
	GetTopIssues(ctx context.Context, since time.Time, kinds []string, severityMax *int, hostname string, topN int) ([]notification.TopIssue, error)
}

// SummarySender sends assembled summary reports to channels.
type SummarySender interface {
	SendSummary(ctx context.Context, report notification.SummaryReport, channelIDs []int64)
}

// SummaryScheduler checks for due summary schedules and dispatches reports.
type SummaryScheduler struct {
	store  SummaryStore
	sender SummarySender
	logger *slog.Logger
}

// NewSummaryScheduler creates a new SummaryScheduler.
func NewSummaryScheduler(store SummaryStore, sender SummarySender, logger *slog.Logger) *SummaryScheduler {
	return &SummaryScheduler{
		store:  store,
		sender: sender,
		logger: logger.With("component", "summary-scheduler"),
	}
}

// Start blocks and checks for due schedules every 60 seconds.
// Returns when ctx is cancelled.
func (s *SummaryScheduler) Start(ctx context.Context) {
	s.logger.Info("summary scheduler started")

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("summary scheduler stopped")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *SummaryScheduler) tick(ctx context.Context) {
	schedules, err := s.store.ListSummarySchedules(ctx)
	if err != nil {
		s.logger.Error("failed to list summary schedules", "err", err)
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

// isDue checks if a schedule should fire now.
func (s *SummaryScheduler) isDue(sched notification.SummarySchedule) bool {
	loc, err := time.LoadLocation(sched.Timezone)
	if err != nil {
		s.logger.Error("invalid timezone", "schedule", sched.Name, "timezone", sched.Timezone, "err", err)
		return false
	}

	now := time.Now().In(loc)
	hour, minute, err := parseTime(sched.TimeOfDay)
	if err != nil {
		s.logger.Error("invalid time_of_day", "schedule", sched.Name, "time_of_day", sched.TimeOfDay, "err", err)
		return false
	}

	// Check hour and minute match (within the 60s tick window).
	if now.Hour() != hour || now.Minute() != minute {
		return false
	}

	// Check frequency-specific conditions.
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

	// Prevent double-fire: last_run_at must be at least half the period ago.
	if sched.LastRunAt != nil {
		minInterval := periodDuration(sched.Frequency) / 2
		if time.Since(*sched.LastRunAt) < minInterval {
			return false
		}
	}

	return true
}

func (s *SummaryScheduler) runSchedule(ctx context.Context, sched notification.SummarySchedule) {
	period := periodDuration(sched.Frequency)
	now := time.Now().UTC()
	since := now.Add(-period)

	s.logger.Info("running summary schedule",
		"schedule", sched.Name,
		"frequency", sched.Frequency,
		"period", period,
		"kinds", sched.EventKinds,
	)

	report := notification.SummaryReport{
		Schedule:    sched,
		Period:      period,
		PeriodLabel: periodLabel(sched.Frequency),
		From:        since,
		To:          now,
	}

	// Gather summary data for each event kind.
	for _, kind := range sched.EventKinds {
		switch kind {
		case "srvlog": //nolint:goconst // Event kind strings match DB values.
			summary, err := s.store.GetSrvlogSummary(ctx, period)
			if err != nil {
				s.logger.Error("failed to get srvlog summary", "schedule", sched.Name, "err", err)
				continue
			}
			report.Srvlog = &summary
		case "netlog": //nolint:goconst // Event kind strings match DB values.
			summary, err := s.store.GetNetlogSummary(ctx, period)
			if err != nil {
				s.logger.Error("failed to get netlog summary", "schedule", sched.Name, "err", err)
				continue
			}
			report.Netlog = &summary
		case "applog": //nolint:goconst // Event kind strings match DB values.
			summary, err := s.store.GetAppLogSummary(ctx, period)
			if err != nil {
				s.logger.Error("failed to get applog summary", "schedule", sched.Name, "err", err)
				continue
			}
			report.AppLog = &summary
		}
	}

	// Get top issues across all requested kinds.
	topIssues, err := s.store.GetTopIssues(ctx, since, sched.EventKinds, sched.SeverityMax, sched.Hostname, sched.TopN)
	if err != nil {
		s.logger.Error("failed to get top issues", "schedule", sched.Name, "err", err)
	} else {
		report.TopIssues = topIssues
	}

	if report.TopIssues == nil {
		report.TopIssues = []notification.TopIssue{}
	}

	// Dispatch to channels.
	s.sender.SendSummary(ctx, report, sched.ChannelIDs)

	// Update last_run_at.
	if err := s.store.UpdateSummaryScheduleLastRun(ctx, sched.ID, now); err != nil {
		s.logger.Error("failed to update last_run_at", "schedule", sched.Name, "err", err)
	}

	s.logger.Info("summary schedule completed", "schedule", sched.Name)
}

func periodDuration(frequency string) time.Duration {
	switch frequency {
	case freqWeekly:
		return 7 * 24 * time.Hour
	case freqMonthly:
		return 30 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

func periodLabel(frequency string) string {
	switch frequency {
	case freqWeekly:
		return "7 days"
	case freqMonthly:
		return "30 days"
	default:
		return "24 hours"
	}
}

// TriggerSchedule runs a specific schedule immediately (for the "send now" feature).
func (s *SummaryScheduler) TriggerSchedule(ctx context.Context, id int64) error {
	schedules, err := s.store.ListSummarySchedules(ctx)
	if err != nil {
		return fmt.Errorf("list schedules: %w", err)
	}

	idx := slices.IndexFunc(schedules, func(ss notification.SummarySchedule) bool {
		return ss.ID == id
	})
	if idx < 0 {
		return fmt.Errorf("schedule %d not found", id)
	}

	s.runSchedule(ctx, schedules[idx])
	return nil
}
