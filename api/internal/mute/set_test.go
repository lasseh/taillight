package mute

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

var (
	t0      = time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	discard = slog.New(slog.NewTextHandler(io.Discard, nil))
)

// fixedSet returns a Set with no store whose clock reads *now.
func fixedSet(now *time.Time, rules ...Rule) *Set {
	s := NewSet(nil, func() time.Time { return *now }, discard)
	s.Replace(rules)
	return s
}

func netlogRule(id int64, host, prog, search string) Rule {
	return Rule{ID: id, Feed: FeedNetlog, Hostname: host, Programname: prog, Search: search, ExpiresAt: t0.Add(time.Hour)}
}

func idOf(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestSnapshotMatchNetlogFields(t *testing.T) {
	ev := model.NetlogEvent{Hostname: "Edge1-Oslo", Programname: "rpd", Message: "LACP bundle AE30 member down"}
	tests := []struct {
		name  string
		rule  Rule
		match bool
	}{
		{name: "exact hostname", rule: netlogRule(1, "Edge1-Oslo", "", ""), match: true},
		{name: "exact hostname is case-sensitive", rule: netlogRule(1, "edge1-oslo", "", ""), match: false},
		{name: "wildcard hostname is case-insensitive", rule: netlogRule(1, "edge*", "", ""), match: true},
		{name: "wildcard hostname mismatch", rule: netlogRule(1, "core*", "", ""), match: false},
		{name: "programname exact", rule: netlogRule(1, "", "rpd", ""), match: true},
		{name: "programname is not a prefix match", rule: netlogRule(1, "", "rp", ""), match: false},
		{name: "programname is case-sensitive", rule: netlogRule(1, "", "RPD", ""), match: false},
		{name: "search is a case-insensitive substring", rule: netlogRule(1, "", "", "bundle ae30"), match: true},
		{name: "search mismatch", rule: netlogRule(1, "", "", "ae31"), match: false},
		{name: "all fields ANDed, all match", rule: netlogRule(1, "edge*", "rpd", "ae30"), match: true},
		{name: "all fields ANDed, one fails", rule: netlogRule(1, "edge*", "sshd", "ae30"), match: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := t0
			got := fixedSet(&now, tt.rule).Active().MatchNetlog(ev)
			if tt.match && (got == nil || *got != tt.rule.ID) {
				t.Fatalf("MatchNetlog = %v, want %d", idOf(got), tt.rule.ID)
			}
			if !tt.match && got != nil {
				t.Fatalf("MatchNetlog = %d, want nil", *got)
			}
		})
	}
}

func TestSnapshotMatchSrvlog(t *testing.T) {
	now := t0
	rule := Rule{ID: 4, Feed: FeedSrvlog, Hostname: "web*", Search: "oom", ExpiresAt: t0.Add(time.Hour)}
	snap := fixedSet(&now, rule).Active()

	if got := snap.MatchSrvlog(model.SrvlogEvent{Hostname: "WEB01", Message: "kernel: OOM killer"}); got == nil || *got != 4 {
		t.Fatalf("MatchSrvlog = %v, want 4", idOf(got))
	}
	if got := snap.MatchSrvlog(model.SrvlogEvent{Hostname: "db01", Message: "kernel: OOM killer"}); got != nil {
		t.Fatalf("MatchSrvlog on other host = %d, want nil", *got)
	}
}

func TestSnapshotFeedScoping(t *testing.T) {
	now := t0
	netRule := Rule{ID: 1, Feed: FeedNetlog, Hostname: "h1", ExpiresAt: t0.Add(time.Hour)}
	srvRule := Rule{ID: 2, Feed: FeedSrvlog, Hostname: "h2", ExpiresAt: t0.Add(time.Hour)}
	snap := fixedSet(&now, netRule, srvRule).Active()

	if got := snap.MatchSrvlog(model.SrvlogEvent{Hostname: "h1"}); got != nil {
		t.Errorf("netlog rule matched a srvlog event: %d", *got)
	}
	if got := snap.MatchNetlog(model.NetlogEvent{Hostname: "h2"}); got != nil {
		t.Errorf("srvlog rule matched a netlog event: %d", *got)
	}
	if got := snap.MatchNetlog(model.NetlogEvent{Hostname: "h1"}); got == nil || *got != 1 {
		t.Errorf("MatchNetlog(h1) = %v, want 1", idOf(got))
	}
	if got := snap.MatchSrvlog(model.SrvlogEvent{Hostname: "h2"}); got == nil || *got != 2 {
		t.Errorf("MatchSrvlog(h2) = %v, want 2", idOf(got))
	}
}

func TestSnapshotExpiryBoundary(t *testing.T) {
	rule := Rule{ID: 1, Feed: FeedNetlog, Hostname: "h1", ExpiresAt: t0}
	ev := model.NetlogEvent{Hostname: "h1"}
	now := t0.Add(-time.Nanosecond)
	set := fixedSet(&now, rule)

	if got := set.Active().MatchNetlog(ev); got == nil {
		t.Fatal("rule did not match at expires_at-1ns")
	}
	// Expiry is checked at match time: no Replace or Reload in between.
	now = t0
	if got := set.Active().MatchNetlog(ev); got != nil {
		t.Fatalf("rule matched at expires_at: %d", *got)
	}
}

func TestSnapshotLowestIDWins(t *testing.T) {
	now := t0
	ev := model.NetlogEvent{Hostname: "h1", Programname: "rpd", Message: "x"}
	set := fixedSet(&now,
		netlogRule(9, "h1", "", ""),
		netlogRule(3, "", "rpd", ""),
		netlogRule(5, "h*", "", ""),
	)
	if got := set.Active().MatchNetlog(ev); got == nil || *got != 3 {
		t.Fatalf("MatchNetlog = %v, want 3", idOf(got))
	}
	// Set.MatchNetlog delegates to the same snapshot logic.
	if got := set.MatchNetlog(ev); got == nil || *got != 3 {
		t.Fatalf("Set.MatchNetlog = %v, want 3", idOf(got))
	}
}

func TestSnapshotMatchReturnsFreshPointer(t *testing.T) {
	now := t0
	set := fixedSet(&now, netlogRule(1, "h1", "", ""))
	ev := model.NetlogEvent{Hostname: "h1"}
	a := set.Active().MatchNetlog(ev)
	*a = 99
	if b := set.Active().MatchNetlog(ev); b == nil || *b != 1 {
		t.Fatalf("second match = %v, want 1 (pointer must not alias the rule)", idOf(b))
	}
}

func TestReplaceDoesNotAliasInput(t *testing.T) {
	now := t0
	rules := []Rule{netlogRule(2, "h1", "", ""), netlogRule(1, "h2", "", "")}
	set := fixedSet(&now, rules...)
	rules[0].Hostname = "changed"
	if got := set.Active().MatchNetlog(model.NetlogEvent{Hostname: "h1"}); got == nil || *got != 2 {
		t.Fatalf("MatchNetlog(h1) = %v after mutating the input slice, want 2", idOf(got))
	}
}

func TestSnapshotExclusions(t *testing.T) {
	now := t0
	set := fixedSet(&now,
		netlogRule(4, "h4", "", ""),
		Rule{ID: 2, Feed: FeedNetlog, Hostname: "expired", ExpiresAt: t0},
		Rule{ID: 3, Feed: FeedSrvlog, Hostname: "srv", Search: "oom", ExpiresAt: t0.Add(time.Hour)},
		netlogRule(1, "", "rpd", "ae30"),
	)
	snap := set.Active()

	net := snap.NetlogExclusions()
	if len(net) != 2 {
		t.Fatalf("NetlogExclusions len = %d, want 2: %+v", len(net), net)
	}
	// Ordered by rule id.
	if net[0].Hostname != "" || net[0].Programname != "rpd" || net[0].Search != "ae30" {
		t.Errorf("NetlogExclusions[0] = %+v, want rule 1's fields", net[0])
	}
	if net[1].Hostname != "h4" || net[1].Programname != "" || net[1].Search != "" {
		t.Errorf("NetlogExclusions[1] = %+v, want rule 4's fields", net[1])
	}

	srv := snap.SrvlogExclusions()
	if len(srv) != 1 || srv[0].Hostname != "srv" || srv[0].Search != "oom" {
		t.Fatalf("SrvlogExclusions = %+v, want only rule 3", srv)
	}
}

func TestSnapshotEmpty(t *testing.T) {
	now := t0
	snap := fixedSet(&now).Active()
	if got := snap.MatchNetlog(model.NetlogEvent{Hostname: "h1"}); got != nil {
		t.Fatalf("empty set matched: %d", *got)
	}
	if ex := snap.NetlogExclusions(); len(ex) != 0 {
		t.Fatalf("empty set exclusions = %+v", ex)
	}
}

type fakeStore struct {
	rules        []Rule
	err          error
	expiresAfter time.Time
}

func (f *fakeStore) ListMuteRules(_ context.Context, expiresAfter time.Time) ([]Rule, error) {
	f.expiresAfter = expiresAfter
	return f.rules, f.err
}

func TestSetReload(t *testing.T) {
	now := t0
	store := &fakeStore{rules: []Rule{netlogRule(1, "h1", "", "")}}
	set := NewSet(store, func() time.Time { return now }, discard)

	if err := set.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !store.expiresAfter.Equal(t0) {
		t.Errorf("Reload listed rules expiring after %v, want now (%v)", store.expiresAfter, t0)
	}
	ev := model.NetlogEvent{Hostname: "h1"}
	if got := set.MatchNetlog(ev); got == nil || *got != 1 {
		t.Fatalf("MatchNetlog after Reload = %v, want 1", idOf(got))
	}

	// A failed reload keeps the last good rules.
	store.err = errors.New("db down")
	store.rules = nil
	if err := set.Reload(context.Background()); err == nil {
		t.Fatal("Reload with store error returned nil")
	}
	if got := set.MatchNetlog(ev); got == nil || *got != 1 {
		t.Fatalf("MatchNetlog after failed Reload = %v, want 1 (last good rules)", idOf(got))
	}
}
