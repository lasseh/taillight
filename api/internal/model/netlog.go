package model

import (
	"net/http"
	"strings"
	"time"
)

// NetlogEvent represents a row from netlog_events.
type NetlogEvent struct {
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

// NetlogFilter holds optional filter criteria for querying netlog events.
type NetlogFilter struct {
	Hostname    string
	FromhostIP  string
	Programname string
	Severity    *int
	SeverityMax *int
	Facility    *int
	SyslogTag   string
	MsgID       string
	Search      string
	From        *time.Time
	To          *time.Time
}

// Matches returns true if the event satisfies all non-zero filter fields.
// Time filters (From/To) are intentionally not checked here — live SSE
// clients should not filter by time range since they receive future events.
func (f NetlogFilter) Matches(e NetlogEvent) bool {
	if f.Hostname != "" && !matchField(e.Hostname, f.Hostname) {
		return false
	}
	if f.FromhostIP != "" && e.FromhostIP != f.FromhostIP {
		return false
	}
	if f.Programname != "" && e.Programname != f.Programname {
		return false
	}
	if f.Severity != nil && e.Severity != *f.Severity {
		return false
	}
	if f.SeverityMax != nil && e.Severity > *f.SeverityMax {
		return false
	}
	if f.Facility != nil && e.Facility != *f.Facility {
		return false
	}
	if f.SyslogTag != "" && e.SyslogTag != f.SyslogTag {
		return false
	}
	if f.MsgID != "" && e.MsgID != f.MsgID {
		return false
	}
	if f.Search != "" {
		sl := strings.ToLower(f.Search)
		if !strings.Contains(strings.ToLower(e.Message), sl) {
			return false
		}
	}
	return true
}

// ParseNetlogFilter extracts a NetlogFilter from HTTP query parameters.
// Returns an error if any typed parameter has an invalid value.
func ParseNetlogFilter(r *http.Request) (NetlogFilter, error) {
	p := newQueryParams(r)
	f := NetlogFilter{
		Hostname:    p.str("hostname"),
		Programname: p.str("programname"),
		SyslogTag:   p.str("syslogtag"),
		MsgID:       p.str("msgid"),
		Search:      p.str("search"),
		FromhostIP:  p.ip("fromhost_ip"),
		Severity:    p.boundedInt("severity", 0, 7),
		SeverityMax: p.boundedInt("severity_max", 0, 7),
		Facility:    p.boundedInt("facility", 0, 23),
		From:        p.rfc3339("from"),
		To:          p.rfc3339("to"),
	}
	if err := p.err(); err != nil {
		return NetlogFilter{}, err
	}
	return f, nil
}

// NetlogDeviceSummary holds aggregated information for a single network device (hostname).
type NetlogDeviceSummary struct {
	Hostname          string          `json:"hostname"`
	FromhostIP        string          `json:"fromhost_ip"`
	LastSeenAt        *time.Time      `json:"last_seen_at"`
	TotalCount        int64           `json:"total_count"`
	CriticalCount     int64           `json:"critical_count"`
	SeverityBreakdown []SeverityCount `json:"severity_breakdown"`
	TopMessages       []TopMessage    `json:"top_messages"`
	CriticalLogs      []NetlogEvent   `json:"critical_logs"`
}
