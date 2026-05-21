package model

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// AppLogEvent represents a row from applog_events.
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
	// SourceIP is the resolved client IP captured by the ingest handler.
	// NULL on rows ingested before this field was added.
	SourceIP *string `json:"source_ip,omitempty"`
	// APIKeyID identifies the API key that ingested this row. Invalid (NULL)
	// when inserted via session auth or before this field was added.
	APIKeyID pgtype.UUID `json:"api_key_id"`
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
	Service    string
	Component  string
	Host       string
	Level      string // Minimum level: WARN returns WARN+ERROR.
	LevelExact string // Exact level match.
	Search     string
	From       *time.Time
	To         *time.Time

	levelMinRank *int // precomputed AppLogLevelRank(Level); nil means not set
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
	if f.Host != "" && !matchField(e.Host, f.Host) {
		return false
	}
	if f.LevelExact != "" {
		if !strings.EqualFold(e.Level, f.LevelExact) {
			return false
		}
	}
	if f.Level != "" {
		minRank := AppLogLevelRank(f.Level)
		if f.levelMinRank != nil {
			minRank = *f.levelMinRank
		}
		if minRank >= 0 {
			if eventRank := AppLogLevelRank(e.Level); eventRank < minRank {
				return false
			}
		}
	}
	if f.Search != "" {
		sl := strings.ToLower(f.Search)
		if !strings.Contains(strings.ToLower(e.Msg), sl) &&
			!strings.Contains(strings.ToLower(string(e.Attrs)), sl) {
			return false
		}
	}
	return true
}

// ParseAppLogFilter extracts a AppLogFilter from HTTP query parameters.
func ParseAppLogFilter(r *http.Request) (AppLogFilter, error) {
	p := newQueryParams(r)
	f := AppLogFilter{
		Service:   p.str("service"),
		Component: p.str("component"),
		Host:      p.str("host"),
		Search:    p.str("search"),
	}

	if v := r.URL.Query().Get("level"); v != "" {
		if normalized, ok := NormalizeLevel(v); !ok {
			p.fail("level: must be one of DEBUG, INFO, WARN, ERROR, FATAL")
		} else {
			f.Level = normalized
			rank := AppLogLevelRank(normalized)
			f.levelMinRank = &rank
		}
	}
	if v := r.URL.Query().Get("level_exact"); v != "" {
		if normalized, ok := NormalizeLevel(v); !ok {
			p.fail("level_exact: must be one of DEBUG, INFO, WARN, ERROR, FATAL")
		} else {
			f.LevelExact = normalized
		}
	}

	f.From = p.rfc3339("from")
	f.To = p.rfc3339("to")

	if err := p.err(); err != nil {
		return AppLogFilter{}, err
	}
	return f, nil
}
