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
	// AttrsTruncated is true when Attrs has been elided to control payload size
	// on list / SSE responses. Clients should fetch the event by ID to retrieve
	// the full attrs blob. Detail endpoints never set this.
	AttrsTruncated bool `json:"attrs_truncated,omitempty"`
	// SourceIP is the resolved client IP captured by the ingest handler.
	// NULL on rows ingested before this field was added.
	SourceIP *string `json:"source_ip,omitempty"`
	// APIKeyID identifies the API key that ingested this row. Invalid (NULL)
	// when inserted via session auth or before this field was added.
	APIKeyID pgtype.UUID `json:"api_key_id"`
}

// AttrsPreviewLimit caps the inline size of AppLogEvent.Attrs in list and SSE
// responses. Attrs blobs above this size are stripped and AttrsTruncated set;
// the full payload remains available via GET /api/v1/applog/{id}.
const AttrsPreviewLimit = 1024

// WithAttrsPreview returns a copy of e with Attrs stripped if it exceeds
// maxBytes. The detail endpoint should always use the original event.
func (e AppLogEvent) WithAttrsPreview(maxBytes int) AppLogEvent {
	if len(e.Attrs) <= maxBytes {
		return e
	}
	e.Attrs = nil
	e.AttrsTruncated = true
	return e
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

	levelMinRank *int   // precomputed AppLogLevelRank(Level); nil means not set
	searchLower  string // precomputed strings.ToLower(Search); empty means not set
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
		// Fail closed: a level filter was requested but the level is
		// unrecognised (e.g. an un-normalised alias from a notification rule).
		// Skipping the predicate would silently match everything (audit N6).
		if minRank < 0 {
			return false
		}
		if eventRank := AppLogLevelRank(e.Level); eventRank < minRank {
			return false
		}
	}
	if f.Search != "" {
		needle := searchNeedle(f.Search, f.searchLower)
		if !containsFold(e.Msg, needle) && !containsFold(e.Attrs, needle) {
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
	f.searchLower = strings.ToLower(f.Search)

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
