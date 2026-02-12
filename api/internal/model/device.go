package model

import "time"

// DeviceSummary holds aggregated information for a single device (hostname).
type DeviceSummary struct {
	Hostname          string          `json:"hostname"`
	LastSeenAt        *time.Time      `json:"last_seen_at"`
	TotalCount        int64           `json:"total_count"`
	CriticalCount     int64           `json:"critical_count"`
	SeverityBreakdown []SeverityCount `json:"severity_breakdown"`
	TopPrograms       []TopSource     `json:"top_programs"`
	TopMessages       []TopMessage    `json:"top_messages"`
}

// TopMessage holds a normalized message pattern with its count and a sample.
type TopMessage struct {
	Pattern  string `json:"pattern"`
	Sample   string `json:"sample"`
	Count    int64  `json:"count"`
	LatestID int64  `json:"latest_id"`
}
