package model

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AppLogEvent represents a row from applog_events.
type AppLogEvent struct {
	ID         int64           `json:"id"`
	ReceivedAt time.Time       `json:"received_at"`
	Timestamp  time.Time       `json:"timestamp"`
	Level      string          `json:"level"`
	Service    string          `json:"service"`
	Component  string          `json:"component,omitempty"`
	Host       string          `json:"host,omitempty"`
	Msg        string          `json:"msg"`
	Source     string          `json:"source,omitempty"`
	Attrs      json.RawMessage `json:"attrs,omitempty"`
}

// ValidAppLogLevels is the set of canonical log levels and their ranks.
var ValidAppLogLevels = map[string]int{
	"DEBUG": 0,
	"INFO":  1,
	"WARN":  2,
	"ERROR": 3,
	"FATAL": 4,
}

// levelAliases maps common non-canonical level names to their canonical form.
// Checked after ValidAppLogLevels fails, so canonical names don't need entries.
var levelAliases = map[string]string{
	"TRACE":    "DEBUG",
	"WARNING":  "WARN",
	"CRITICAL": "FATAL",
	"PANIC":    "FATAL",
}

// NormalizeLevel maps a level string to its canonical form.
// Returns the canonical level and true, or "" and false if unrecognized.
func NormalizeLevel(level string) (string, bool) {
	upper := strings.ToUpper(level)
	if _, ok := ValidAppLogLevels[upper]; ok {
		return upper, true
	}
	if canon, ok := levelAliases[upper]; ok {
		return canon, true
	}
	return "", false
}

// AppLogLevelRank returns the numeric rank for a level string (higher = more severe).
func AppLogLevelRank(level string) int {
	if r, ok := ValidAppLogLevels[strings.ToUpper(level)]; ok {
		return r
	}
	return -1
}

// AppLogFilter holds optional filter criteria for querying log events.
type AppLogFilter struct {
	Service   string
	Component string
	Host      string
	Level     string // Minimum level: WARN returns WARN+ERROR.
	Search    string
	From      *time.Time
	To        *time.Time
}

// Matches returns true if the event satisfies all non-zero filter fields.
// Time filters are intentionally not checked here — live SSE clients
// should not filter by time range since they receive future events.
func (f AppLogFilter) Matches(e AppLogEvent) bool {
	if f.Service != "" && e.Service != f.Service {
		return false
	}
	if f.Component != "" && e.Component != f.Component {
		return false
	}
	if f.Host != "" && e.Host != f.Host {
		return false
	}
	if f.Level != "" {
		minRank := AppLogLevelRank(f.Level)
		eventRank := AppLogLevelRank(e.Level)
		if minRank >= 0 && eventRank < minRank {
			return false
		}
	}
	if f.Search != "" && !strings.Contains(strings.ToLower(e.Msg), strings.ToLower(f.Search)) {
		return false
	}
	return true
}

// ParseAppLogFilter extracts a AppLogFilter from HTTP query parameters.
func ParseAppLogFilter(r *http.Request) (AppLogFilter, error) {
	q := r.URL.Query()
	f := AppLogFilter{
		Service:   q.Get("service"),
		Component: q.Get("component"),
		Host:      q.Get("host"),
		Search:    q.Get("search"),
	}

	var errs []string

	for _, p := range []struct{ name, val string }{
		{"service", f.Service},
		{"component", f.Component},
		{"host", f.Host},
		{"search", f.Search},
	} {
		if len(p.val) > maxFilterStringLen {
			errs = append(errs, fmt.Sprintf("%s: exceeds max length %d", p.name, maxFilterStringLen))
		}
	}

	if v := q.Get("level"); v != "" {
		if normalized, ok := NormalizeLevel(v); !ok {
			errs = append(errs, "level: must be one of DEBUG, INFO, WARN, ERROR, FATAL")
		} else {
			f.Level = normalized
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
		return AppLogFilter{}, fmt.Errorf("invalid query parameters: %s", strings.Join(errs, "; "))
	}
	return f, nil
}
