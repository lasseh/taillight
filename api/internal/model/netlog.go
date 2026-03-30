package model

import (
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
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
	q := r.URL.Query()
	f := NetlogFilter{
		Hostname:    q.Get("hostname"),
		Programname: q.Get("programname"),
		SyslogTag:   q.Get("syslogtag"),
		MsgID:       q.Get("msgid"),
		Search:      q.Get("search"),
	}

	var errs []string

	for _, p := range []struct{ name, val string }{
		{"hostname", f.Hostname},
		{"programname", f.Programname},
		{"syslogtag", f.SyslogTag},
		{"msgid", f.MsgID},
		{"search", f.Search},
	} {
		if len(p.val) > maxFilterStringLen {
			errs = append(errs, fmt.Sprintf("%s: exceeds max length %d", p.name, maxFilterStringLen))
		}
	}

	if v := q.Get("fromhost_ip"); v != "" {
		addr, err := netip.ParseAddr(v)
		if err != nil {
			errs = append(errs, "fromhost_ip: must be a valid IP address")
		} else {
			f.FromhostIP = addr.String()
		}
	}
	if v := q.Get("severity"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 7 {
			errs = append(errs, "severity: must be an integer 0-7")
		} else {
			f.Severity = &n
		}
	}
	if v := q.Get("severity_max"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 7 {
			errs = append(errs, "severity_max: must be an integer 0-7")
		} else {
			f.SeverityMax = &n
		}
	}
	if v := q.Get("facility"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 23 {
			errs = append(errs, "facility: must be an integer 0-23")
		} else {
			f.Facility = &n
		}
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			errs = append(errs, "from: must be RFC3339 format")
		} else {
			f.From = &t
		}
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			errs = append(errs, "to: must be RFC3339 format")
		} else {
			f.To = &t
		}
	}

	if len(errs) > 0 {
		return NetlogFilter{}, fmt.Errorf("invalid query parameters: %s", strings.Join(errs, "; "))
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
