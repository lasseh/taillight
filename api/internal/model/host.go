package model

import (
	"math"
	"net/http"
	"slices"
	"time"
)

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

// Page size bounds for the hosts overview.
const (
	HostsDefaultLimit = 100
	HostsMaxLimit     = 500
)

// HostSorts are the accepted sort keys for the hosts overview. Each sorts
// descending except hostname, which sorts ascending.
var HostSorts = []string{"errors", "total", "hostname", "last_seen", "trend"}

// HostQuery selects one page of the hosts overview. The store applies the
// search, filters and sort before paging.
type HostQuery struct {
	RangeDur time.Duration
	Search   string     // case-insensitive hostname substring; "" matches all
	Status   HostStatus // "" matches all
	Feed     string     // "srvlog", "netlog", "both"; "" matches all
	Sort     string     // one of HostSorts
	Limit    int
	Offset   int
}

// HostPage is one page of the hosts overview. Total counts every host that
// matches the query, not only those in Hosts.
type HostPage struct {
	Hosts []HostEntry
	Total int
}

// ParseHostQuery extracts a HostQuery from the range, search, status, feed,
// sort, limit and offset query parameters.
func ParseHostQuery(r *http.Request) (HostQuery, error) {
	rangeDur, err := ParseRange(r)
	if err != nil {
		return HostQuery{}, err
	}
	p := newQueryParams(r)
	q := HostQuery{
		RangeDur: rangeDur,
		Search:   p.str("search"),
		Status:   HostStatus(p.q.Get("status")),
		Feed:     p.q.Get("feed"),
		Sort:     p.q.Get("sort"),
		Limit:    ParseLimit(r, HostsDefaultLimit, HostsMaxLimit),
	}
	switch q.Status {
	case "", HostStatusHealthy, HostStatusWarning, HostStatusCritical:
	default:
		p.fail("status: must be one of healthy, warning, critical")
	}
	switch q.Feed {
	case "", "srvlog", "netlog", "both":
	default:
		p.fail("feed: must be one of srvlog, netlog, both")
	}
	if q.Sort == "" {
		q.Sort = "errors"
	} else if !slices.Contains(HostSorts, q.Sort) {
		p.fail("sort: must be one of errors, total, hostname, last_seen, trend")
	}
	if off := p.boundedInt("offset", 0, math.MaxInt32); off != nil {
		q.Offset = *off
	}
	if err := p.err(); err != nil {
		return HostQuery{}, err
	}
	return q, nil
}
