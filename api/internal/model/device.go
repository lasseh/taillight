package model

import "time"

// SrvlogDeviceSummary holds aggregated information for a single device (hostname).
type SrvlogDeviceSummary struct {
	Hostname          string          `json:"hostname"`
	LastSeenAt        *time.Time      `json:"last_seen_at"`
	TotalCount        int64           `json:"total_count"`
	CriticalCount     int64           `json:"critical_count"`
	SeverityBreakdown []SeverityCount `json:"severity_breakdown"`
	TopMessages       []TopMessage    `json:"top_messages"`
	CriticalLogs      []SrvlogEvent   `json:"critical_logs"`
}

// TopMessage holds a normalized message pattern with its count and a sample.
// Severity is an int (RFC 5424 numeric level 0-7) because srvlog events use
// integer severity codes. Compare with AppLogTopMessage.Level which is a
// string ("DEBUG","INFO","WARN","ERROR","FATAL") because applog events use
// freeform text levels. This asymmetry reflects the different source protocols.
type TopMessage struct {
	Pattern       string    `json:"pattern"`
	Sample        string    `json:"sample"`
	Count         int64     `json:"count"`
	LatestID      int64     `json:"latest_id"`
	LatestAt      time.Time `json:"latest_at"`
	Severity      int       `json:"severity"`
	SeverityLabel string    `json:"severity_label"`
}
