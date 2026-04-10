package notification

import (
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// SummarySchedule represents a configured periodic summary digest.
type SummarySchedule struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Enabled     bool       `json:"enabled"`
	Frequency   string     `json:"frequency"`              // "daily", "weekly", "monthly".
	DayOfWeek   *int       `json:"day_of_week,omitempty"`  // 0=Sun..6=Sat, nil for daily.
	DayOfMonth  *int       `json:"day_of_month,omitempty"` // 1-28, nil for daily/weekly.
	TimeOfDay   string     `json:"time_of_day"`            // "HH:MM".
	Timezone    string     `json:"timezone"`               // IANA timezone.
	EventKinds  []string   `json:"event_kinds"`            // e.g. ["srvlog","netlog","applog"].
	SeverityMax *int       `json:"severity_max,omitempty"`
	Hostname    string     `json:"hostname,omitempty"`
	TopN        int        `json:"top_n"`
	ChannelIDs  []int64    `json:"channel_ids"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// SummaryReport is the assembled data for one summary run.
type SummaryReport struct {
	Schedule    SummarySchedule      `json:"schedule"`
	Period      time.Duration        `json:"period"`
	PeriodLabel string               `json:"period_label"` // "24 hours", "7 days".
	From        time.Time            `json:"from"`
	To          time.Time            `json:"to"`
	Srvlog      *model.SyslogSummary `json:"srvlog,omitempty"`
	Netlog      *model.SyslogSummary `json:"netlog,omitempty"`
	AppLog      *model.AppLogSummary `json:"applog,omitempty"`
	TopIssues   []TopIssue           `json:"top_issues"`
}

// TopIssue is a single high-frequency/high-severity log pattern.
type TopIssue struct {
	Kind     string `json:"kind"`     // "srvlog", "netlog", "applog".
	Severity int    `json:"severity"` // Numeric (0-7 for syslog, mapped for applog).
	Label    string `json:"label"`    // "err", "warn", "ERROR", etc.
	Source   string `json:"source"`   // Hostname or service.
	Program  string `json:"program"`  // Programname or component.
	Message  string `json:"message"`  // Truncated message/pattern.
	Count    int64  `json:"count"`
}
