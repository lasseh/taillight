package model

import "time"

// HostStatus classifies a host's health based on its error ratio.
type HostStatus string

const (
	HostStatusHealthy  HostStatus = "healthy"
	HostStatusWarning  HostStatus = "warning"
	HostStatusCritical HostStatus = "critical"
)

// HostEntry is a single row in the hosts overview page.
type HostEntry struct {
	Hostname          string          `json:"hostname"`
	Feed              string          `json:"feed"`   // "srvlog", "netlog", "both"
	Status            HostStatus      `json:"status"` // computed from error ratio
	LastSeenAt        *time.Time      `json:"last_seen_at"`
	TotalCount        int64           `json:"total_count"`
	ErrorCount        int64           `json:"error_count"`
	Trend             float64         `json:"trend"`
	SeverityBreakdown []SeverityCount `json:"severity_breakdown"`
	HourlyBuckets     []HourlyBucket  `json:"hourly_buckets"`
	TopErrors         []TopSource     `json:"top_errors"`
}

// HourlyBucket is a single hour of activity for a host.
type HourlyBucket struct {
	Bucket     time.Time `json:"bucket"`
	Count      int64     `json:"count"`
	ErrorCount int64     `json:"error_count"`
}
