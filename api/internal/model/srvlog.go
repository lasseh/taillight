// Package model defines domain types for srvlog events, filters, and cursors.
package model

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxFilterStringLen is the maximum length for string filter parameters.
const maxFilterStringLen = 500

// SrvlogEvent represents a row from srvlog_events.
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

// Severity codes per RFC 5424.
const (
	severityEmerg   = 0 //nolint:unused // RFC 5424 completeness.
	severityAlert   = 1 //nolint:unused // RFC 5424 completeness.
	severityCrit    = 2 //nolint:unused // RFC 5424 completeness.
	SeverityErr     = 3
	SeverityWarning = 4
	severityNotice  = 5 //nolint:unused // RFC 5424 completeness.
	severityInfo    = 6 //nolint:unused // RFC 5424 completeness.
	severityDebug   = 7 //nolint:unused // RFC 5424 completeness.
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

// SeverityLabel returns the human-readable label for a srvlog severity code.
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

// SrvlogFilter holds optional filter criteria for querying events.
type SrvlogFilter struct {
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

// matchWildcard reports whether value matches a glob pattern where * matches
// any sequence of characters. The comparison is case-insensitive.
// If pattern contains no *, it falls back to a plain case-insensitive equal.
func matchWildcard(value, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return strings.EqualFold(value, pattern)
	}
	lower := strings.ToLower(value)
	parts := strings.Split(strings.ToLower(pattern), "*")
	pos := 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(lower[pos:], part)
		if idx < 0 {
			return false
		}
		// First segment must anchor at start.
		if i == 0 && idx != 0 {
			return false
		}
		pos += idx + len(part)
	}
	// Last segment must anchor at end.
	if last := parts[len(parts)-1]; last != "" && !strings.HasSuffix(lower, last) {
		return false
	}
	return true
}

// matchField compares value to pattern using wildcard if pattern contains *.
func matchField(value, pattern string) bool {
	if strings.Contains(pattern, "*") {
		return matchWildcard(value, pattern)
	}
	return value == pattern
}

// Matches returns true if the event satisfies all non-zero filter fields.
// Time filters (From/To) are intentionally not checked here — live SSE
// clients should not filter by time range since they receive future events.
func (f SrvlogFilter) Matches(e SrvlogEvent) bool {
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

// ParseSrvlogFilter extracts an SrvlogFilter from HTTP query parameters.
// Returns an error if any typed parameter has an invalid value.
func ParseSrvlogFilter(r *http.Request) (SrvlogFilter, error) {
	p := newQueryParams(r)
	f := SrvlogFilter{
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
		return SrvlogFilter{}, err
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
