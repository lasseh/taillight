package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeAnalysisStore implements AnalysisScheduleStore in memory.
type fakeAnalysisStore struct {
	schedules []model.AnalysisSchedule
	lastRuns  map[int64]time.Time
}

func (f *fakeAnalysisStore) ListAnalysisSchedules(context.Context) ([]model.AnalysisSchedule, error) {
	return f.schedules, nil
}

func (f *fakeAnalysisStore) GetAnalysisSchedule(_ context.Context, id int64) (model.AnalysisSchedule, error) {
	for _, s := range f.schedules {
		if s.ID == id {
			return s, nil
		}
	}
	return model.AnalysisSchedule{}, errors.New("schedule not found")
}

func (f *fakeAnalysisStore) UpdateAnalysisScheduleLastRun(_ context.Context, id int64, t time.Time) error {
	if f.lastRuns == nil {
		f.lastRuns = make(map[int64]time.Time)
	}
	f.lastRuns[id] = t
	return nil
}

// fakeEnqueuer records enqueue attempts and can fail on demand.
type fakeEnqueuer struct {
	err  error
	reqs []model.AnalysisReport
}

func (f *fakeEnqueuer) Enqueue(_ context.Context, req model.AnalysisReport) (model.AnalysisReport, error) {
	f.reqs = append(f.reqs, req)
	return req, f.err
}

func newTestAnalysisScheduler(store *fakeAnalysisStore, enq *fakeEnqueuer, now time.Time) *AnalysisScheduler {
	s := NewAnalysisScheduler(store, enq, testLogger())
	s.now = func() time.Time { return now }
	return s
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		hour    int
		minute  int
		wantErr bool
	}{
		{name: "midnight", in: "00:00", hour: 0, minute: 0},
		{name: "morning", in: "09:30", hour: 9, minute: 30},
		{name: "end of day", in: "23:59", hour: 23, minute: 59},
		{name: "empty", in: "", wantErr: true},
		{name: "garbage", in: "banana", wantErr: true},
		{name: "hour out of range", in: "25:00", wantErr: true},
		{name: "minute out of range", in: "12:61", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hour, minute, err := parseTime(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseTime(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if err == nil && (hour != tt.hour || minute != tt.minute) {
				t.Errorf("parseTime(%q) = %d:%d, want %d:%d", tt.in, hour, minute, tt.hour, tt.minute)
			}
		})
	}
}

func TestScheduledPeriodEnd(t *testing.T) {
	tests := []struct {
		name      string
		timezone  string
		timeOfDay string
		now       time.Time
		want      time.Time
		wantErr   bool
	}{
		{
			name:      "utc truncated to scheduled minute",
			timezone:  "UTC",
			timeOfDay: "09:00",
			now:       time.Date(2026, time.June, 17, 9, 3, 42, 123456, time.UTC),
			want:      time.Date(2026, time.June, 17, 9, 0, 0, 0, time.UTC),
		},
		{
			name:      "oslo summer time is UTC+2",
			timezone:  "Europe/Oslo",
			timeOfDay: "09:00",
			now:       time.Date(2026, time.June, 17, 7, 3, 0, 0, time.UTC), // 09:03 CEST.
			want:      time.Date(2026, time.June, 17, 7, 0, 0, 0, time.UTC),
		},
		{
			name:      "oslo winter time is UTC+1",
			timezone:  "Europe/Oslo",
			timeOfDay: "09:00",
			now:       time.Date(2026, time.January, 14, 8, 3, 0, 0, time.UTC), // 09:03 CET.
			want:      time.Date(2026, time.January, 14, 8, 0, 0, 0, time.UTC),
		},
		{
			name:      "invalid timezone",
			timezone:  "Not/AZone",
			timeOfDay: "09:00",
			now:       time.Date(2026, time.June, 17, 9, 0, 0, 0, time.UTC),
			wantErr:   true,
		},
		{
			name:      "invalid time_of_day",
			timezone:  "UTC",
			timeOfDay: "banana",
			now:       time.Date(2026, time.June, 17, 9, 0, 0, 0, time.UTC),
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched := model.AnalysisSchedule{Timezone: tt.timezone, TimeOfDay: tt.timeOfDay}
			got, err := scheduledPeriodEnd(sched, tt.now)
			if (err != nil) != tt.wantErr {
				t.Fatalf("scheduledPeriodEnd() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if !got.Equal(tt.want) {
				t.Errorf("scheduledPeriodEnd() = %v, want %v", got, tt.want)
			}
			if got.Location() != time.UTC {
				t.Errorf("scheduledPeriodEnd() location = %v, want UTC", got.Location())
			}
		})
	}
}

func TestAnalysisSchedulerIsDue(t *testing.T) {
	// Wednesday 2026-06-17 09:00:00 UTC.
	base := time.Date(2026, time.June, 17, 9, 0, 0, 0, time.UTC)
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
		{name: "daily fires at scheduled time", frequency: freqDaily, now: base, want: true},
		{name: "daily fires within firing window", frequency: freqDaily, now: base.Add(3 * time.Minute), want: true},
		{name: "daily fires at window edge", frequency: freqDaily, now: base.Add(analysisFiringWindow), want: true},
		{name: "daily not due just past window", frequency: freqDaily, now: base.Add(analysisFiringWindow + time.Second), want: false},
		{name: "daily not due before scheduled time", frequency: freqDaily, now: base.Add(-time.Minute), want: false},
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
		{name: "oslo summer schedule fires at UTC+2", frequency: freqDaily, timezone: "Europe/Oslo", now: time.Date(2026, time.June, 17, 7, 2, 0, 0, time.UTC), want: true},
		{name: "oslo winter schedule fires at UTC+1", frequency: freqDaily, timezone: "Europe/Oslo", now: time.Date(2026, time.January, 14, 8, 2, 0, 0, time.UTC), want: true},
		{name: "oslo summer schedule misses at winter offset", frequency: freqDaily, timezone: "Europe/Oslo", now: time.Date(2026, time.June, 17, 8, 2, 0, 0, time.UTC), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched := model.AnalysisSchedule{
				ID:         1,
				Name:       tt.name,
				Enabled:    true,
				Feed:       "srvlog",
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
			s := newTestAnalysisScheduler(&fakeAnalysisStore{}, &fakeEnqueuer{}, tt.now)
			if got := s.isDue(sched); got != tt.want {
				t.Errorf("isDue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnalysisSchedulerTick(t *testing.T) {
	base := time.Date(2026, time.June, 17, 9, 0, 0, 0, time.UTC)
	due := model.AnalysisSchedule{ID: 1, Name: "due", Enabled: true, Feed: "srvlog", Frequency: freqDaily, TimeOfDay: "09:00", Timezone: "UTC"}
	disabled := due
	disabled.ID = 2
	disabled.Name = "disabled"
	disabled.Enabled = false
	notDue := due
	notDue.ID = 3
	notDue.Name = "not-due"
	notDue.TimeOfDay = "15:00"

	t.Run("only enabled due schedules fire", func(t *testing.T) {
		store := &fakeAnalysisStore{schedules: []model.AnalysisSchedule{due, disabled, notDue}}
		enq := &fakeEnqueuer{}
		s := newTestAnalysisScheduler(store, enq, base)

		s.tick(context.Background())

		if len(enq.reqs) != 1 {
			t.Fatalf("enqueued requests = %d, want 1", len(enq.reqs))
		}
		req := enq.reqs[0]
		if !req.PeriodEnd.Equal(base) {
			t.Errorf("PeriodEnd = %v, want %v", req.PeriodEnd, base)
		}
		if !req.PeriodStart.Equal(base.Add(-24 * time.Hour)) {
			t.Errorf("PeriodStart = %v, want %v", req.PeriodStart, base.Add(-24*time.Hour))
		}
		if len(store.lastRuns) != 1 {
			t.Fatalf("last_run stamps = %d, want 1", len(store.lastRuns))
		}
		if _, ok := store.lastRuns[due.ID]; !ok {
			t.Errorf("last_run_at not stamped for due schedule %d", due.ID)
		}
	})

	t.Run("failed enqueue does not stamp last_run", func(t *testing.T) {
		store := &fakeAnalysisStore{schedules: []model.AnalysisSchedule{due}}
		enq := &fakeEnqueuer{err: errors.New("queue full")}
		s := newTestAnalysisScheduler(store, enq, base)

		s.tick(context.Background())

		if len(enq.reqs) != 1 {
			t.Fatalf("enqueue attempts = %d, want 1", len(enq.reqs))
		}
		if len(store.lastRuns) != 0 {
			t.Errorf("last_run_at stamped after failed enqueue: %v", store.lastRuns)
		}
	})
}
