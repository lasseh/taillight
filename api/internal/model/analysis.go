package model

import "time"

// AnalysisReport represents a stored AI analysis report.
type AnalysisReport struct {
	ID               int64     `json:"id"`
	GeneratedAt      time.Time `json:"generated_at"`
	Model            string    `json:"model"`
	PeriodStart      time.Time `json:"period_start"`
	PeriodEnd        time.Time `json:"period_end"`
	Report           string    `json:"report"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	DurationMS       int64     `json:"duration_ms"`
	Status           string    `json:"status"`
}

// AnalysisReportSummary is a lightweight variant for listing reports.
type AnalysisReportSummary struct {
	ID               int64     `json:"id"`
	GeneratedAt      time.Time `json:"generated_at"`
	Model            string    `json:"model"`
	PeriodStart      time.Time `json:"period_start"`
	PeriodEnd        time.Time `json:"period_end"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	DurationMS       int64     `json:"duration_ms"`
	Status           string    `json:"status"`
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
