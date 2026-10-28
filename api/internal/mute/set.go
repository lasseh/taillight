package mute

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// refreshInterval is how often Run reloads the rules from the store. Writes
// through the API reload immediately; the tick covers anything else.
const refreshInterval = 30 * time.Second

// Store loads mute rules. *postgres.Store satisfies it.
type Store interface {
	ListMuteRules(ctx context.Context, expiresAfter time.Time) ([]Rule, error)
}

// Set is the in-memory mute rule set. Rules are swapped in whole by Replace;
// readers take a Snapshot and never see a partial update.
type Set struct {
	store  Store
	now    func() time.Time
	logger *slog.Logger

	reloadMu sync.Mutex // serializes Reload: the tick against write-triggered reloads

	mu    sync.RWMutex
	rules []Rule // sorted by ID ascending; never mutated after Replace
}

// NewSet returns an empty Set. store may be nil when the caller only uses
// Replace.
func NewSet(store Store, now func() time.Time, logger *slog.Logger) *Set {
	return &Set{store: store, now: now, logger: logger}
}

// Replace swaps in a copy of rules sorted by ID, so the lowest id wins when
// several rules match.
func (s *Set) Replace(rules []Rule) {
	sorted := slices.Clone(rules)
	slices.SortFunc(sorted, func(a, b Rule) int { return cmp.Compare(a.ID, b.ID) })
	s.mu.Lock()
	s.rules = sorted
	s.mu.Unlock()
}

// Reload loads the rules that are active now and replaces the set. On error
// the current rules stay in place.
func (s *Set) Reload(ctx context.Context) error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	rules, err := s.store.ListMuteRules(ctx, s.now())
	if err != nil {
		return fmt.Errorf("list mute rules: %w", err)
	}
	s.Replace(rules)
	return nil
}

// Run reloads the rules every 30s until ctx is done.
func (s *Set) Run(ctx context.Context) {
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Reload(ctx); err != nil {
				s.logger.Warn("mute rule reload failed", "err", err)
			}
		}
	}
}

// Active returns a snapshot of the current rules and clock. It copies only
// the slice header, so it does not allocate.
func (s *Set) Active() Snapshot {
	s.mu.RLock()
	rules := s.rules
	s.mu.RUnlock()
	return Snapshot{rules: rules, now: s.now()}
}

// MatchNetlog returns the id of the lowest-id active netlog rule matching e,
// or nil.
func (s *Set) MatchNetlog(e model.NetlogEvent) *int64 { return s.Active().MatchNetlog(e) }

// MatchSrvlog returns the id of the lowest-id active srvlog rule matching e,
// or nil.
func (s *Set) MatchSrvlog(e model.SrvlogEvent) *int64 { return s.Active().MatchSrvlog(e) }

// Snapshot is the rule set at one instant. Use one snapshot per request so
// SQL exclusion and per-row marking agree.
type Snapshot struct {
	rules []Rule
	now   time.Time
}

// active reports whether r applies to feed at the snapshot's time. A rule is
// active iff now is before its expiry, so expiry needs no reload.
func (s Snapshot) active(r *Rule, feed Feed) bool {
	return r.Feed == feed && s.now.Before(r.ExpiresAt)
}

// MatchNetlog returns a fresh pointer to the id of the lowest-id active
// netlog rule matching e, or nil.
func (s Snapshot) MatchNetlog(e model.NetlogEvent) *int64 {
	for i := range s.rules {
		r := &s.rules[i]
		if s.active(r, FeedNetlog) && r.NetlogFilter().Matches(e) {
			id := r.ID
			return &id
		}
	}
	return nil
}

// MatchSrvlog returns a fresh pointer to the id of the lowest-id active
// srvlog rule matching e, or nil.
func (s Snapshot) MatchSrvlog(e model.SrvlogEvent) *int64 {
	for i := range s.rules {
		r := &s.rules[i]
		if s.active(r, FeedSrvlog) && r.SrvlogFilter().Matches(e) {
			id := r.ID
			return &id
		}
	}
	return nil
}

// NetlogExclusions returns the filters of the active netlog rules, in id
// order, or nil when there are none.
func (s Snapshot) NetlogExclusions() []model.NetlogFilter {
	var out []model.NetlogFilter
	for i := range s.rules {
		if r := &s.rules[i]; s.active(r, FeedNetlog) {
			out = append(out, r.NetlogFilter())
		}
	}
	return out
}

// SrvlogExclusions returns the filters of the active srvlog rules, in id
// order, or nil when there are none.
func (s Snapshot) SrvlogExclusions() []model.SrvlogFilter {
	var out []model.SrvlogFilter
	for i := range s.rules {
		if r := &s.rules[i]; s.active(r, FeedSrvlog) {
			out = append(out, r.SrvlogFilter())
		}
	}
	return out
}
