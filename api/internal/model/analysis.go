package model

import "time"

// Analysis feed constants — the data sources an analysis run can target.
const (
	AnalysisFeedNetlog = "netlog"
	AnalysisFeedSrvlog = "srvlog"
	AnalysisFeedAll    = "all"
)

// Analysis report lifecycle statuses.
const (
	AnalysisStatusPending   = "pending"
	AnalysisStatusRunning   = "running"
	AnalysisStatusCompleted = "completed"
	AnalysisStatusFailed    = "failed"
)

// IsValidAnalysisFeed reports whether s is a recognized feed.
func IsValidAnalysisFeed(s string) bool {
	switch s {
	case AnalysisFeedNetlog, AnalysisFeedSrvlog, AnalysisFeedAll:
		return true
	}
	return false
}

// AnalysisReport represents a stored AI analysis report.
type AnalysisReport struct {
	ID               int64      `json:"id"`
	Slug             string     `json:"slug"`
	Feed             string     `json:"feed"`
	Model            string     `json:"model"`
	PeriodStart      time.Time  `json:"period_start"`
	PeriodEnd        time.Time  `json:"period_end"`
	Report           string     `json:"report,omitempty"`
	PromptTokens     int        `json:"prompt_tokens"`
	CompletionTokens int        `json:"completion_tokens"`
	Status           string     `json:"status"`
	Error            string     `json:"error,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

// AnalysisReportSummary is a lightweight variant for listing reports.
type AnalysisReportSummary struct {
	ID               int64      `json:"id"`
	Slug             string     `json:"slug"`
	Feed             string     `json:"feed"`
	Model            string     `json:"model"`
	PeriodStart      time.Time  `json:"period_start"`
	PeriodEnd        time.Time  `json:"period_end"`
	PromptTokens     int        `json:"prompt_tokens"`
	CompletionTokens int        `json:"completion_tokens"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"created_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

// AnalysisSchedule represents a configured recurring analysis run.
type AnalysisSchedule struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Enabled    bool       `json:"enabled"`
	Feed       string     `json:"feed"`
	Frequency  string     `json:"frequency"`              // "daily", "weekly", "monthly".
	DayOfWeek  *int       `json:"day_of_week,omitempty"`  // 0=Sun..6=Sat, nil for daily.
	DayOfMonth *int       `json:"day_of_month,omitempty"` // 1-28, nil for daily/weekly.
	TimeOfDay  string     `json:"time_of_day"`            // "HH:MM".
	Timezone   string     `json:"timezone"`               // IANA timezone.
	LastRunAt  *time.Time `json:"last_run_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// MsgIDCount holds a msgid with its total count and per-severity breakdown.
type MsgIDCount struct {
	MsgID          string        `json:"msgid"`
	Count          int64         `json:"count"`
	SeverityCounts map[int]int64 `json:"severity_counts"`
}

// HostErrorCount holds a hostname with its error count and top msgid.
type HostErrorCount struct {
	Hostname string `json:"hostname"`
	Count    int64  `json:"count"`
	TopMsgID string `json:"top_msgid"`
}

// SeverityLevelComparison compares current severity count to baseline average.
type SeverityLevelComparison struct {
	Severity    int     `json:"severity"`
	Label       string  `json:"label"`
	Current     int64   `json:"current"`
	BaselineAvg float64 `json:"baseline_avg"`
	ChangePct   float64 `json:"change_pct"`
}

// SeverityComparison wraps severity level comparisons.
type SeverityComparison struct {
	Levels []SeverityLevelComparison `json:"levels"`
}

// EventCluster represents a time window with correlated events across hosts.
type EventCluster struct {
	Bucket time.Time `json:"bucket"`
	Hosts  []string  `json:"hosts"`
	MsgIDs []string  `json:"msgids"`
	Total  int64     `json:"total"`
}
