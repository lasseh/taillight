package client

import (
	"encoding/json"
	"time"
)

// SrvlogEvent mirrors model.SrvlogEvent with pure Go types.
type SrvlogEvent struct {
	ID             int64     `json:"id"`
	ReceivedAt     time.Time `json:"received_at"`
	ReportedAt     time.Time `json:"reported_at"`
	Hostname       string    `json:"hostname"`
	FromhostIP     string    `json:"fromhost_ip"`
	Programname    string    `json:"programname"`
	MsgID          string    `json:"msgid"`
	Severity       int       `json:"severity"`
	SeverityLabel  string    `json:"severity_label"`
	Facility       int       `json:"facility"`
	FacilityLabel  string    `json:"facility_label"`
	SyslogTag      string    `json:"syslogtag"`
	StructuredData *string   `json:"structured_data,omitempty"`
	Message        string    `json:"message"`
	RawMessage     *string   `json:"raw_message,omitempty"`
}

// AppLogEvent mirrors model.AppLogEvent with pure Go types.
type AppLogEvent struct {
	ID         int64           `json:"id"`
	ReceivedAt time.Time       `json:"received_at"`
	Timestamp  time.Time       `json:"timestamp"`
	Level      string          `json:"level"`
	Service    string          `json:"service"`
	Component  string          `json:"component"`
	Host       string          `json:"host"`
	Msg        string          `json:"msg"`
	Source     string          `json:"source"`
	Attrs      json.RawMessage `json:"attrs"`
}

// NetlogEvent is identical to SrvlogEvent for network log entries.
type NetlogEvent = SrvlogEvent

// ListResponse is the standard paginated list envelope.
type ListResponse[T any] struct {
	Data    []T     `json:"data"`
	Cursor  *string `json:"cursor,omitempty"`
	HasMore bool    `json:"has_more"`
}

// ItemResponse is the standard single-item envelope.
type ItemResponse[T any] struct {
	Data T `json:"data"`
}

// UserInfo represents the authenticated user.
type UserInfo struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	IsAdmin  bool   `json:"is_admin"`
}

// SrvlogDeviceSummary represents a device-level summary.
type SrvlogDeviceSummary struct {
	Hostname          string          `json:"hostname"`
	FromhostIP        string          `json:"fromhost_ip"`
	LastSeenAt        *time.Time      `json:"last_seen_at"`
	TotalCount        int64           `json:"total_count"`
	CriticalCount     int64           `json:"critical_count"`
	SeverityBreakdown []SeverityCount `json:"severity_breakdown"`
	TopMessages       []TopMessage    `json:"top_messages"`
	CriticalLogs      []SrvlogEvent   `json:"critical_logs"`
}

// SeverityCount is a severity bucket in a breakdown.
type SeverityCount struct {
	Severity int     `json:"severity"`
	Label    string  `json:"label"`
	Count    int64   `json:"count"`
	Pct      float64 `json:"pct"`
}

// TopMessage is a frequently recurring message pattern.
type TopMessage struct {
	Pattern       string    `json:"pattern"`
	Sample        string    `json:"sample"`
	Count         int64     `json:"count"`
	LatestID      int64     `json:"latest_id"`
	LatestAt      time.Time `json:"latest_at"`
	Severity      int       `json:"severity"`
	SeverityLabel string    `json:"severity_label"`
}

// VolumeBucket is a time-bucketed event count.
type VolumeBucket struct {
	Time   time.Time        `json:"time"`
	Total  int64            `json:"total"`
	ByHost map[string]int64 `json:"by_host"`
}

// StatsSummary is the aggregated summary response for srvlog/netlog.
type StatsSummary struct {
	Total             int64           `json:"total"`
	Trend             float64         `json:"trend"`
	Errors            int64           `json:"errors"`
	Warnings          int64           `json:"warnings"`
	SeverityBreakdown []SeverityCount `json:"severity_breakdown"`
	TopHosts          []HostCount     `json:"top_hosts"`
}

// AppLogStatsSummary is the aggregated summary response for applog.
type AppLogStatsSummary struct {
	Total          int64        `json:"total"`
	Trend          float64      `json:"trend"`
	Errors         int64        `json:"errors"`
	Warnings       int64        `json:"warnings"`
	LevelBreakdown []LevelCount `json:"level_breakdown"`
	TopServices    []HostCount  `json:"top_services"` // reuse HostCount (same shape)
}

// HostCount is a host with its event count.
type HostCount struct {
	Name  string  `json:"name"`
	Count int64   `json:"count"`
	Pct   float64 `json:"pct"`
}

// HostEntry represents a host in the hosts inventory.
type HostEntry struct {
	Hostname   string     `json:"hostname"`
	FromhostIP string     `json:"fromhost_ip"`
	Feed       string     `json:"feed"`
	TotalCount int64      `json:"total_count"`
	ErrorCount int64      `json:"error_count"`
	ErrorRatio float64    `json:"error_ratio"`
	Trend      float64    `json:"trend"`
	LastSeenAt *time.Time `json:"last_seen_at"`
}
