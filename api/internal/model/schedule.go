package model

import (
	"fmt"
	"time"
)

// Schedule cadence rules shared by analysis and summary schedules.

// MaxScheduleDayOfMonth is the last day a monthly schedule may fire on, so
// every month has that day.
const MaxScheduleDayOfMonth = 28

// SchedulePeriod is the window one firing of a schedule covers: 24h for
// daily, 7 days for weekly, 30 days for monthly. Unknown frequencies are
// treated as daily.
func SchedulePeriod(frequency string) time.Duration {
	switch frequency {
	case "weekly":
		return 7 * 24 * time.Hour
	case "monthly":
		return 30 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// ParseTimeOfDay parses a schedule's "HH:MM" time_of_day into 24-hour hour
// and minute components.
func ParseTimeOfDay(s string) (hour, minute int, err error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, fmt.Errorf("parse schedule time %q: %w", s, err)
	}
	return t.Hour(), t.Minute(), nil
}
