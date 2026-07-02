package model

import (
	"sort"
	"strings"
	"time"
)

// Analysis feed constants — the data sources an analysis run can target.
// "all" means all syslog feeds (srvlog + netlog, a UNION ALL of the two
// syslog tables); applog is excluded by design (architecture review D3).
// The wire value stays "all" because it is persisted in analysis report
// and schedule rows — the UI labels it "all syslog" instead.
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

// Analysis prompt modes — which prompt set frames the report.
const (
	AnalysisModeDaily    = "daily"
	AnalysisModeWeekly   = "weekly"
	AnalysisModeIncident = "incident"
)

// IsValidAnalysisMode reports whether s is a recognized prompt mode.
func IsValidAnalysisMode(s string) bool {
	switch s {
	case AnalysisModeDaily, AnalysisModeWeekly, AnalysisModeIncident:
		return true
	}
	return false
}

// AnalysisModeForFrequency maps a schedule frequency to the prompt mode that
// scheduled runs use. Per the design decision to auto-derive mode from
// cadence: daily cadence uses the daily prompt; weekly and monthly both reuse
// the weekly prompt (no distinct monthly prompt exists today). Unknown
// frequencies fall back to daily so the worker never receives an empty mode.
func AnalysisModeForFrequency(frequency string) string {
	switch frequency {
	case "weekly", "monthly":
		return AnalysisModeWeekly
	default:
		return AnalysisModeDaily
	}
}

// AnalysisReport represents a stored AI analysis report.
//
// Hosts carries the report's host scope: an empty slice means "all hosts on
// the feed"; a non-empty slice restricts every aggregation (and the baseline
// comparison) to that exact set. The slice is normalized — sorted and deduped
// — before persistence so two requests with the same set collide on the
// active-report uniqueness constraint.
//
// Token-count contract: PromptTokens=0 && CompletionTokens=0 on a row with
// Status="completed" means the analyzer short-circuited because the gathered
// window had no current-window activity (see analyzer.isEmptyData). The
// body is a deterministic markdown stub; do not treat zero tokens as an
// anomaly signal in alerting on that combination.
type AnalysisReport struct {
	ID               int64      `json:"id"`
	Slug             string     `json:"slug"`
	Feed             string     `json:"feed"`
	PromptMode       string     `json:"prompt_mode"`
	Hosts            []string   `json:"hosts"`
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
	// NotifyChannelIDs are the email notification channel ids snapshotted from
	// the originating schedule at enqueue time; the worker mails the completed
	// report to them. Internal dispatch field — not exposed in API responses.
	NotifyChannelIDs []int64 `json:"-"`
}

// AnalysisReportSummary is a lightweight variant for listing reports.
type AnalysisReportSummary struct {
	ID               int64      `json:"id"`
	Slug             string     `json:"slug"`
	Feed             string     `json:"feed"`
	PromptMode       string     `json:"prompt_mode"`
	Hosts            []string   `json:"hosts"`
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

// AnalysisScope is the (feed, hosts) pair that selects which events an
// analyzer query reads from. Replacing a bare `feed string` parameter with
// this struct keeps the analyzer Store interface stable as new dimensions
// (today: hosts; later perhaps severity floor or program filter) get added.
//
// Hosts is canonical: empty means "all hosts on the feed," and non-empty
// must already be sorted + deduped (use NormalizeHosts before constructing).
type AnalysisScope struct {
	Feed  string
	Hosts []string
}

// IsAllHosts reports whether the scope applies to every host on the feed
// (i.e. no host filter). Callers use this both to choose query shape (skip
// `WHERE hostname = ANY(...)` clauses) and to decide whether to gather
// host-comparison aggregations that are degenerate under a narrow scope.
func (s AnalysisScope) IsAllHosts() bool {
	return len(s.Hosts) == 0
}

// NormalizeHosts returns a sorted, deduped, trimmed copy of hosts so that
// two requests with the same logical host set produce the same []string and
// therefore the same Postgres text[] value. The active-report uniqueness
// constraint relies on this — without it ["a","b"] and ["b","a"] would be
// stored as distinct keys.
//
// Empty input and an input that normalizes to zero entries both return nil
// (rather than an empty non-nil slice) so callers can use the zero value
// interchangeably with "no scope".
func NormalizeHosts(hosts []string) []string {
	if len(hosts) == 0 {
		return nil
	}
	out := make([]string, 0, len(hosts))
	seen := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// AnalysisSchedule represents a configured recurring analysis run.
type AnalysisSchedule struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Feed       string `json:"feed"`
	Frequency  string `json:"frequency"`              // "daily", "weekly", "monthly".
	DayOfWeek  *int   `json:"day_of_week,omitempty"`  // 0=Sun..6=Sat, nil for daily.
	DayOfMonth *int   `json:"day_of_month,omitempty"` // 1-28, nil for daily/weekly.
	TimeOfDay  string `json:"time_of_day"`            // "HH:MM".
	Timezone   string `json:"timezone"`               // IANA timezone.
	// NotifyChannelIDs references the email notification channels the completed
	// report is mailed to. Empty = no email. Snapshotted onto each enqueued
	// report so later schedule edits/deletes don't retarget a pending run; the
	// channel contents are resolved live at send time.
	NotifyChannelIDs []int64    `json:"notify_channel_ids"`
	LastRunAt        *time.Time `json:"last_run_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// AnalysisHostEntry is one row returned by the analysis hosts endpoint
// (GET /api/v1/analysis/hosts?feed=…). The picker uses this to populate its
// autocomplete suggestions; the report list does not.
//
// LastSeen is the most recent hour-aligned bucket from the continuous
// aggregate; it may be nil for hosts that have entered the meta cache but
// not yet produced an aggregated row (very freshly onboarded hosts).
type AnalysisHostEntry struct {
	Hostname string     `json:"hostname"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
}

// MsgIDCount holds an event signature with its total count, per-severity
// breakdown, and a handful of representative sample messages.
//
// "MsgID" is the stored field name for historical reasons; the value is the
// output of analyzer's event-key expression — the RFC 5424 MSGID when present
// and a normalized message pattern (msg_pattern) otherwise. The JSON tag is
// kept as "msgid" so any external consumers of the report API keep working.
type MsgIDCount struct {
	MsgID          string          `json:"msgid"`
	Count          int64           `json:"count"`
	HostCount      int             `json:"host_count"`
	TopHosts       []HostCount     `json:"top_hosts,omitempty"`
	SeverityCounts map[int]int64   `json:"severity_counts"`
	Samples        []SampleMessage `json:"samples,omitempty"`
}

// HostCount holds a hostname and how many events it contributed to a
// parent aggregation (e.g. one event signature). Used to surface
// "1 host vs 50 hosts" distinction on top msgids — same total volume
// reads as a wildly different incident depending on how concentrated it is.
type HostCount struct {
	Hostname string `json:"hostname"`
	Count    int64  `json:"count"`
}

// SampleMessage is one example log row attached to a top event signature so
// the analyzer prompt carries actual message text instead of just an ID.
// Message is truncated server-side to keep the prompt within context budget.
type SampleMessage struct {
	Hostname   string    `json:"hostname"`
	ReceivedAt time.Time `json:"received_at"`
	Severity   int       `json:"severity"`
	Message    string    `json:"message"`
}

// HostErrorCount holds a hostname with its error count and top msgid.
type HostErrorCount struct {
	Hostname string `json:"hostname"`
	Count    int64  `json:"count"`
	TopMsgID string `json:"top_msgid"`
}

// SeverityLevelComparison compares current severity rate to baseline average.
// Both Current and BaselineAvg are per-day rates regardless of window length:
// for a 24h run Current equals the raw count, for sub-24h incident windows it
// is extrapolated to a per-day-equivalent rate so percentage comparisons stay
// apples-to-apples.
type SeverityLevelComparison struct {
	Severity    int     `json:"severity"`
	Label       string  `json:"label"`
	Current     float64 `json:"current"`
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

// ProgramCount holds a srvlog programname (sshd, systemd, kernel, cron…)
// with its total event count and severity breakdown. Programname is the
// fastest way for a triage reader to distinguish auth events from cron
// noise from kernel faults, so the analyzer surfaces it alongside msgids.
type ProgramCount struct {
	Programname    string        `json:"programname"`
	Count          int64         `json:"count"`
	ErrorCount     int64         `json:"error_count"` // severity ≤ 3
	SeverityCounts map[int]int64 `json:"severity_counts"`
}

// AnalysisVolumeBucket is a single time bucket in the analyzer's per-period
// volume timeline. Distinct from the dashboard VolumeBucket type because
// the analyzer cares about total + errors (sev ≤ 3) per bucket, not
// per-host breakdown.
type AnalysisVolumeBucket struct {
	Bucket     time.Time `json:"bucket"`
	Total      int64     `json:"total"`
	ErrorCount int64     `json:"error_count"`
}

// FacilityCount holds a syslog facility (auth, kern, cron, daemon…) with
// its total event count. Facility movement on auth/authpriv is intrinsically
// a security signal worth its own line in the briefing.
type FacilityCount struct {
	Facility   int    `json:"facility"`
	Label      string `json:"label"`
	Count      int64  `json:"count"`
	ErrorCount int64  `json:"error_count"` // severity ≤ 3
}
