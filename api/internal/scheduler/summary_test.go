package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/notification"
)

// fakeSummaryStore implements SummaryStore in memory.
type fakeSummaryStore struct {
	schedules []notification.SummarySchedule
	topIssues []notification.TopIssue
	lastRuns  map[int64]time.Time
}

func (f *fakeSummaryStore) ListSummarySchedules(context.Context) ([]notification.SummarySchedule, error) {
	return f.schedules, nil
}

func (f *fakeSummaryStore) UpdateSummaryScheduleLastRun(_ context.Context, id int64, t time.Time) error {
	if f.lastRuns == nil {
		f.lastRuns = make(map[int64]time.Time)
	}
	f.lastRuns[id] = t
	return nil
}

func (f *fakeSummaryStore) GetSrvlogSummary(context.Context, time.Duration) (model.SyslogSummary, error) {
	return model.SyslogSummary{}, nil
}

func (f *fakeSummaryStore) GetNetlogSummary(context.Context, time.Duration) (model.SyslogSummary, error) {
	return model.SyslogSummary{}, nil
}

func (f *fakeSummaryStore) GetAppLogSummary(context.Context, time.Duration) (model.AppLogSummary, error) {
	return model.AppLogSummary{}, nil
}

func (f *fakeSummaryStore) GetTopIssues(context.Context, time.Time, []string, *int, string, int) ([]notification.TopIssue, error) {
	return f.topIssues, nil
}

// fakeSummarySender records dispatched reports.
type fakeSummarySender struct {
	reports  []notification.SummaryReport
	channels [][]int64
}

func (f *fakeSummarySender) SendSummary(_ context.Context, report notification.SummaryReport, channelIDs []int64) {
	f.reports = append(f.reports, report)
	f.channels = append(f.channels, channelIDs)
}

func newTestSummaryScheduler(store *fakeSummaryStore, sender *fakeSummarySender, now time.Time) *SummaryScheduler {
	s := NewSummaryScheduler(store, sender, testLogger())
	s.now = func() time.Time { return now }
	return s
}

func TestPeriodDuration(t *testing.T) {
	tests := []struct {
		frequency string
		want      time.Duration
	}{
		{freqDaily, 24 * time.Hour},
		{freqWeekly, 7 * 24 * time.Hour},
		{freqMonthly, 30 * 24 * time.Hour},
		{"bogus", 24 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.frequency, func(t *testing.T) {
			if got := periodDuration(tt.frequency); got != tt.want {
				t.Errorf("periodDuration(%q) = %v, want %v", tt.frequency, got, tt.want)
			}
		})
	}
}

func TestPeriodLabel(t *testing.T) {
	tests := []struct {
		frequency string
		want      string
	}{
		{freqDaily, "24 hours"},
		{freqWeekly, "7 days"},
		{freqMonthly, "30 days"},
		{"bogus", "24 hours"},
	}
	for _, tt := range tests {
		t.Run(tt.frequency, func(t *testing.T) {
			if got := periodLabel(tt.frequency); got != tt.want {
				t.Errorf("periodLabel(%q) = %q, want %q", tt.frequency, got, tt.want)
			}
		})
	}
}

func TestSummarySchedulerIsDue(t *testing.T) {
	// Wednesday 2026-06-17 09:00:30 UTC — mid-minute, as a real tick would land.
	base := time.Date(2026, time.June, 17, 9, 0, 30, 0, time.UTC)
	weekday := int(base.Weekday())

	tests := []struct {
		name       string
		frequency  string
		timeOfDay  string
		timezone   string
		dayOfWeek  *int
		dayOfMonth *int
		lastRunAt  *time.Time
		now        time.Time
		want       bool
	}{
		{name: "daily fires within scheduled minute", frequency: freqDaily, now: base, want: true},
		{name: "daily not due one minute early", frequency: freqDaily, now: base.Add(-time.Minute), want: false},
		{name: "daily not due one minute late", frequency: freqDaily, now: base.Add(time.Minute), want: false},
		{name: "daily has no firing window unlike analysis", frequency: freqDaily, now: base.Add(3 * time.Minute), want: false},
		{name: "weekly fires on matching day", frequency: freqWeekly, dayOfWeek: new(weekday), now: base, want: true},
		{name: "weekly not due on other day", frequency: freqWeekly, dayOfWeek: new((weekday + 1) % 7), now: base, want: false},
		{name: "weekly never fires with nil day_of_week", frequency: freqWeekly, now: base, want: false},
		{name: "monthly fires on matching day", frequency: freqMonthly, dayOfMonth: new(base.Day()), now: base, want: true},
		{name: "monthly not due on other day", frequency: freqMonthly, dayOfMonth: new(base.Day() + 1), now: base, want: false},
		{name: "monthly never fires with nil day_of_month", frequency: freqMonthly, now: base, want: false},
		{name: "double-fire guard blocks recent run", frequency: freqDaily, lastRunAt: new(base.Add(-10 * time.Minute)), now: base, want: false},
		{name: "double-fire guard blocks just under half period", frequency: freqDaily, lastRunAt: new(base.Add(-11 * time.Hour)), now: base, want: false},
		{name: "fires when last run is older than half period", frequency: freqDaily, lastRunAt: new(base.Add(-13 * time.Hour)), now: base, want: true},
		{name: "invalid timezone never fires", frequency: freqDaily, timezone: "Not/AZone", now: base, want: false},
		{name: "invalid time_of_day never fires", frequency: freqDaily, timeOfDay: "banana", now: base, want: false},
		{name: "oslo summer schedule fires at UTC+2", frequency: freqDaily, timezone: "Europe/Oslo", now: time.Date(2026, time.June, 17, 7, 0, 30, 0, time.UTC), want: true},
		{name: "oslo winter schedule fires at UTC+1", frequency: freqDaily, timezone: "Europe/Oslo", now: time.Date(2026, time.January, 14, 8, 0, 30, 0, time.UTC), want: true},
		{name: "oslo summer schedule misses at winter offset", frequency: freqDaily, timezone: "Europe/Oslo", now: time.Date(2026, time.June, 17, 8, 0, 30, 0, time.UTC), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched := notification.SummarySchedule{
				ID:         1,
				Name:       tt.name,
				Enabled:    true,
				Frequency:  tt.frequency,
				TimeOfDay:  "09:00",
				Timezone:   "UTC",
				DayOfWeek:  tt.dayOfWeek,
				DayOfMonth: tt.dayOfMonth,
				LastRunAt:  tt.lastRunAt,
			}
			if tt.timeOfDay != "" {
				sched.TimeOfDay = tt.timeOfDay
			}
			if tt.timezone != "" {
				sched.Timezone = tt.timezone
			}
			s := newTestSummaryScheduler(&fakeSummaryStore{}, &fakeSummarySender{}, tt.now)
			if got := s.isDue(sched); got != tt.want {
				t.Errorf("isDue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSummarySchedulerTick(t *testing.T) {
	base := time.Date(2026, time.June, 17, 9, 0, 30, 0, time.UTC)
	due := notification.SummarySchedule{
		ID: 1, Name: "due", Enabled: true, Frequency: freqDaily,
		TimeOfDay: "09:00", Timezone: "UTC",
		EventKinds: []string{"srvlog"}, ChannelIDs: []int64{7},
	}
	disabled := due
	disabled.ID = 2
	disabled.Name = "disabled"
	disabled.Enabled = false
	notDue := due
	notDue.ID = 3
	notDue.Name = "not-due"
	notDue.TimeOfDay = "15:00"

	store := &fakeSummaryStore{schedules: []notification.SummarySchedule{due, disabled, notDue}}
	sender := &fakeSummarySender{}
	s := newTestSummaryScheduler(store, sender, base)

	s.tick(context.Background())

	if len(sender.reports) != 1 {
		t.Fatalf("sent reports = %d, want 1", len(sender.reports))
	}
	report := sender.reports[0]
	if report.Schedule.ID != due.ID {
		t.Errorf("report schedule ID = %d, want %d", report.Schedule.ID, due.ID)
	}
	if report.PeriodLabel != "24 hours" {
		t.Errorf("PeriodLabel = %q, want %q", report.PeriodLabel, "24 hours")
	}
	if !report.To.Equal(base) {
		t.Errorf("To = %v, want %v", report.To, base)
	}
	if !report.From.Equal(base.Add(-24 * time.Hour)) {
		t.Errorf("From = %v, want %v", report.From, base.Add(-24*time.Hour))
	}
	if report.Srvlog == nil {
		t.Errorf("Srvlog summary = nil, want populated for requested kind")
	}
	if report.TopIssues == nil {
		t.Errorf("TopIssues = nil, want empty slice")
	}
	if len(sender.channels) != 1 || len(sender.channels[0]) != 1 || sender.channels[0][0] != 7 {
		t.Errorf("channels = %v, want [[7]]", sender.channels)
	}
	if len(store.lastRuns) != 1 {
		t.Fatalf("last_run stamps = %d, want 1", len(store.lastRuns))
	}
	if got, ok := store.lastRuns[due.ID]; !ok || !got.Equal(base) {
		t.Errorf("last_run_at = %v (ok=%v), want %v", got, ok, base)
	}
}
