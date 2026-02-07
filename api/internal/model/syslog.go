// Package model defines domain types for syslog events, filters, and cursors.
package model

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// maxFilterStringLen is the maximum length for string filter parameters.
const maxFilterStringLen = 500

// SyslogEvent represents a row from syslog_events.
type SyslogEvent struct {
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

// Severity codes per RFC 5424.
const (
	SeverityEmerg   = 0
	SeverityAlert   = 1
	SeverityCrit    = 2
	SeverityErr     = 3
	SeverityWarning = 4
	SeverityNotice  = 5
	SeverityInfo    = 6
	SeverityDebug   = 7
)

// Severity labels per RFC 5424.
var severityLabels = [8]string{
	"emerg",
	"alert",
	"crit",
	"err",
	"warning",
	"notice",
	"info",
	"debug",
}

// SeverityLabel returns the human-readable label for a syslog severity code.
func SeverityLabel(code int) string {
	if code >= 0 && code < len(severityLabels) {
		return severityLabels[code]
	}
	return fmt.Sprintf("unknown(%d)", code)
}

// Facility labels per RFC 5424.
var facilityLabels = [24]string{
	"kern",
	"user",
	"mail",
	"daemon",
	"auth",
	"syslog",
	"lpr",
	"news",
	"uucp",
	"cron",
	"authpriv",
	"ftp",
	"ntp",
	"security",
	"console",
	"clock",
	"local0",
	"local1",
	"local2",
	"local3",
	"local4",
	"local5",
	"local6",
	"local7",
}

// FacilityLabel returns the human-readable label for a syslog facility code.
func FacilityLabel(code int) string {
	if code >= 0 && code < len(facilityLabels) {
		return facilityLabels[code]
	}
	return fmt.Sprintf("unknown(%d)", code)
}

// SyslogFilter holds optional filter criteria for querying events.
type SyslogFilter struct {
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
func (f SyslogFilter) Matches(e SyslogEvent) bool {
	if f.Hostname != "" && e.Hostname != f.Hostname {
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
	if f.Search != "" && !strings.Contains(strings.ToLower(e.Message), strings.ToLower(f.Search)) {
		return false
	}
	return true
}

// Cursor represents a position for keyset pagination.
type Cursor struct {
	ReceivedAt time.Time
	ID         int64
}

// Encode returns a base64-encoded cursor string.
func (c Cursor) Encode() string {
	raw := fmt.Sprintf("%d,%d", c.ReceivedAt.UnixNano(), c.ID)
	return base64.URLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses a base64-encoded cursor string.
func DecodeCursor(s string) (Cursor, error) {
	raw, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("decode cursor: %w", err)
	}
	parts := strings.SplitN(string(raw), ",", 2)
	if len(parts) != 2 {
		return Cursor{}, fmt.Errorf("decode cursor: invalid format")
	}
	nanos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return Cursor{}, fmt.Errorf("decode cursor timestamp: %w", err)
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return Cursor{}, fmt.Errorf("decode cursor id: %w", err)
	}
	return Cursor{
		ReceivedAt: time.Unix(0, nanos),
		ID:         id,
	}, nil
}

// ParseSyslogFilter extracts an SyslogFilter from HTTP query parameters.
// Returns an error if any typed parameter has an invalid value.
func ParseSyslogFilter(r *http.Request) (SyslogFilter, error) {
	q := r.URL.Query()
	f := SyslogFilter{
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
		return SyslogFilter{}, fmt.Errorf("invalid query parameters: %s", strings.Join(errs, "; "))
	}
	return f, nil
}

// ParseCursor extracts a cursor from the "cursor" query parameter.
func ParseCursor(r *http.Request) *Cursor {
	v := r.URL.Query().Get("cursor")
	if v == "" {
		return nil
	}
	c, err := DecodeCursor(v)
	if err != nil {
		return nil
	}
	return &c
}

// ParseLimit extracts the limit from the "limit" query parameter.
// Returns defaultLimit if not specified, capped at maxLimit.
func ParseLimit(r *http.Request, defaultLimit, maxLimit int) int {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return defaultLimit
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}
