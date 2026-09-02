package model

import "time"

// AppLogTemplateKey identifies a recurring applog message: the service and
// component that logged it, the trigger-computed msg_pattern (numbers and
// IPs replaced, cut at 200 chars), and the canonical level the rows
// carried. Applog has no msgid, so the pattern is the whole signature; a
// pattern that logs at two levels is two keys, each with its own sample.
type AppLogTemplateKey struct {
	Service   string `json:"service"`
	Component string `json:"component"`
	Pattern   string `json:"pattern"`
	Level     string `json:"level"`
}

// AppLogTemplate is one message template with its warn-and-above activity in
// the analysis window.
type AppLogTemplate struct {
	AppLogTemplateKey
	Count     int64         `json:"count"`
	HostCount int           `json:"host_count"`
	FirstSeen time.Time     `json:"first_seen"`
	LastSeen  time.Time     `json:"last_seen"`
	Sample    *AppLogSample `json:"sample,omitempty"`
}

// AppLogSample is the most recent row for a template. Msg is cut by the
// store; Attrs is the raw JSON text as stored, which the analyzer compacts
// to its prompt budget.
type AppLogSample struct {
	Host       string    `json:"host"`
	Level      string    `json:"level"`
	ReceivedAt time.Time `json:"received_at"`
	Msg        string    `json:"msg"`
	Attrs      string    `json:"attrs"`
}

// AppLogLevelCounts breaks a row count down by canonical level. Total covers
// every level, including INFO and DEBUG, which have no field of their own.
type AppLogLevelCounts struct {
	Total int64 `json:"total"`
	Warn  int64 `json:"warn"`
	Error int64 `json:"error"`
	Fatal int64 `json:"fatal"`
}

// WarnPlus returns the warn-and-above count.
func (c AppLogLevelCounts) WarnPlus() int64 { return c.Warn + c.Error + c.Fatal }

// ErrorPlus returns the error-and-above count.
func (c AppLogLevelCounts) ErrorPlus() int64 { return c.Error + c.Fatal }

// AppLogServiceStats is one service's activity from the hourly aggregate:
// raw counts for the analysis window and for the baseline period before it.
// The analyzer converts both to per-day rates.
type AppLogServiceStats struct {
	Service  string            `json:"service"`
	Current  AppLogLevelCounts `json:"current"`
	Baseline AppLogLevelCounts `json:"baseline"`
}

// AppLogHygiene holds the log-hygiene facts computed over warn-and-above
// rows in the window. Every number in the report's hygiene note traces back
// to one of these fields.
type AppLogHygiene struct {
	WarnPlusRows   int64                    `json:"warn_plus_rows"`
	EmptyComponent int64                    `json:"empty_component"`
	OversizeAttrs  int64                    `json:"oversize_attrs"` // attrs larger than AttrsPreviewLimit bytes
	Dominant       []AppLogDominantTemplate `json:"dominant"`
}

// AppLogDominantTemplate is a WARN template that accounts for at least the
// analyzer's configured share of its service's WARN volume.
type AppLogDominantTemplate struct {
	AppLogTemplateKey
	Count        int64 `json:"count"`
	ServiceTotal int64 `json:"service_total"`
}
