// Package mute holds operator-created, time-boxed mute rules for the netlog
// and srvlog feeds, and the in-memory rule set that marks matching events.
// A muted event is still stored and streamed; it is only marked, and hidden
// or kept out of notifications by the consumers that read the mark.
package mute

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// Feed names the event feed a rule applies to.
type Feed string

// Feeds a mute rule can apply to.
const (
	FeedNetlog Feed = "netlog"
	FeedSrvlog Feed = "srvlog"
)

const (
	// DefaultDuration is the rule lifetime when a request omits duration_ms
	// or sends 0.
	DefaultDuration = 24 * time.Hour
	// MaxDuration caps a rule's lifetime; extending past it takes a
	// deliberate re-extension.
	MaxDuration = 7 * 24 * time.Hour
	// ExpiredListWindow is how long an ended rule stays in the list.
	ExpiredListWindow = 7 * 24 * time.Hour
)

// maxFieldLen is the byte cap on every match field and the reason, matching
// the feed filter's query-parameter cap.
const maxFieldLen = 500

// Rule mutes events of one feed that match every set field. An empty match
// field is not matched on. Only the expiry changes after creation.
type Rule struct {
	ID          int64     `json:"id"`
	Feed        Feed      `json:"feed"`
	Hostname    string    `json:"hostname"`
	Programname string    `json:"programname"`
	Search      string    `json:"search"`
	Reason      string    `json:"reason"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Validate checks the feed, that at least one match field is set, the field
// lengths, and that no field holds a NUL byte, which Postgres text cannot
// store. A hostname made only of '*' counts as unset: it would match every
// host, so a hostname-only "*" rule would mute the whole feed.
func (r Rule) Validate() error {
	switch r.Feed {
	case FeedNetlog, FeedSrvlog:
	default:
		return errors.New("feed must be netlog or srvlog")
	}
	hostSet := strings.Trim(strings.TrimSpace(r.Hostname), "*") != ""
	if !hostSet && strings.TrimSpace(r.Programname) == "" && strings.TrimSpace(r.Search) == "" {
		return errors.New("at least one of hostname, programname or search is required")
	}
	for _, f := range []struct{ name, value string }{
		{"hostname", r.Hostname},
		{"programname", r.Programname},
		{"search", r.Search},
		{"reason", r.Reason},
	} {
		if len(f.value) > maxFieldLen {
			return fmt.Errorf("%s must be at most %d bytes", f.name, maxFieldLen)
		}
		if strings.ContainsRune(f.value, 0) {
			return fmt.Errorf("%s must not contain a NUL byte", f.name)
		}
	}
	if strings.TrimSpace(r.Reason) == "" {
		return errors.New("reason is required")
	}
	return nil
}

// NetlogFilter converts the rule's match fields to a netlog filter. This and
// SrvlogFilter are the only rule-to-filter conversions, so marking and SQL
// exclusion share the feed filter's match semantics.
func (r Rule) NetlogFilter() model.NetlogFilter {
	return model.NetlogFilter{Hostname: r.Hostname, Programname: r.Programname, Search: r.Search}
}

// SrvlogFilter converts the rule's match fields to a srvlog filter.
func (r Rule) SrvlogFilter() model.SrvlogFilter {
	return model.SrvlogFilter{Hostname: r.Hostname, Programname: r.Programname, Search: r.Search}
}

// ExpiresAt returns now plus a duration in milliseconds. 0 means
// DefaultDuration; anything outside 1ms..MaxDuration is an error, never
// clamped.
func ExpiresAt(now time.Time, durationMS int64) (time.Time, error) {
	if durationMS == 0 {
		return now.Add(DefaultDuration), nil
	}
	if durationMS < 0 || durationMS > MaxDuration.Milliseconds() {
		return time.Time{}, fmt.Errorf("duration_ms must be between 1 and %d (7 days), or 0 for the 24h default", MaxDuration.Milliseconds())
	}
	return now.Add(time.Duration(durationMS) * time.Millisecond), nil
}
