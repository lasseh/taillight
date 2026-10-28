package ingestbridge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/mute"
)

type fakeFetcher struct {
	srvlog    model.SrvlogEvent
	netlog    model.NetlogEvent
	srvlogErr error
	netlogErr error
	srvCalls  int
	netCalls  int
}

func (f *fakeFetcher) GetSrvlog(_ context.Context, id int64) (model.SrvlogEvent, error) {
	f.srvCalls++
	if f.srvlogErr != nil {
		return model.SrvlogEvent{}, f.srvlogErr
	}
	f.srvlog.ID = id
	return f.srvlog, nil
}

func (f *fakeFetcher) GetNetlog(_ context.Context, id int64) (model.NetlogEvent, error) {
	f.netCalls++
	if f.netlogErr != nil {
		return model.NetlogEvent{}, f.netlogErr
	}
	f.netlog.ID = id
	return f.netlog, nil
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestDispatch_RoutesSrvlog(t *testing.T) {
	f := &fakeFetcher{}
	var gotSrv []int64
	var gotNet []int64
	Dispatch(context.Background(), Notification{ChannelSrvlog, 7}, f, nil, Sinks{
		BroadcastSrvlog: func(e model.SrvlogEvent) { gotSrv = append(gotSrv, e.ID) },
		BroadcastNetlog: func(e model.NetlogEvent) { gotNet = append(gotNet, e.ID) },
	}, discard(), time.Second)

	if len(gotSrv) != 1 || gotSrv[0] != 7 {
		t.Fatalf("srvlog sink = %v, want [7]", gotSrv)
	}
	if len(gotNet) != 0 {
		t.Fatalf("netlog sink should not fire, got %v", gotNet)
	}
	if f.netCalls != 0 {
		t.Fatalf("GetNetlog should not be called, got %d", f.netCalls)
	}
}

func TestDispatch_RoutesNetlog(t *testing.T) {
	f := &fakeFetcher{}
	var gotNet []int64
	Dispatch(context.Background(), Notification{ChannelNetlog, 9}, f, nil, Sinks{
		BroadcastSrvlog: func(model.SrvlogEvent) { t.Fatal("srvlog sink must not fire") },
		BroadcastNetlog: func(e model.NetlogEvent) { gotNet = append(gotNet, e.ID) },
	}, discard(), time.Second)

	if len(gotNet) != 1 || gotNet[0] != 9 {
		t.Fatalf("netlog sink = %v, want [9]", gotNet)
	}
}

func TestDispatch_FetchErrorIsSwallowed(t *testing.T) {
	f := &fakeFetcher{srvlogErr: errors.New("db down")}
	Dispatch(context.Background(), Notification{ChannelSrvlog, 1}, f, nil, Sinks{
		BroadcastSrvlog: func(model.SrvlogEvent) { t.Fatal("sink must not fire on fetch error") },
		NotifySrvlog:    func(model.SrvlogEvent) { t.Fatal("notify must not fire on fetch error") },
	}, discard(), time.Second)
	if f.srvCalls != 1 {
		t.Fatalf("GetSrvlog calls = %d, want 1", f.srvCalls)
	}
	// No panic, no sink call — the worker loop survives a bad row.
}

func TestDispatch_UnknownChannelIgnored(t *testing.T) {
	f := &fakeFetcher{}
	Dispatch(context.Background(), Notification{"mystery_ingest", 1}, f, nil, Sinks{
		BroadcastSrvlog: func(model.SrvlogEvent) { t.Fatal("must not fire") },
		BroadcastNetlog: func(model.NetlogEvent) { t.Fatal("must not fire") },
	}, discard(), time.Second)
	if f.srvCalls != 0 || f.netCalls != 0 {
		t.Fatalf("no fetch expected, got srv=%d net=%d", f.srvCalls, f.netCalls)
	}
}

func TestDispatch_NilBroadcastSkipsFetch(t *testing.T) {
	f := &fakeFetcher{}
	// Both feeds disabled (nil broadcast) — must not even fetch, even when
	// a notify sink is set.
	s := Sinks{
		NotifySrvlog: func(model.SrvlogEvent) { t.Fatal("notify must not fire without broadcast") },
		NotifyNetlog: func(model.NetlogEvent) { t.Fatal("notify must not fire without broadcast") },
	}
	Dispatch(context.Background(), Notification{ChannelNetlog, 1}, f, nil, s, discard(), time.Second)
	Dispatch(context.Background(), Notification{ChannelSrvlog, 1}, f, nil, s, discard(), time.Second)
	if f.netCalls != 0 || f.srvCalls != 0 {
		t.Fatalf("fetch should be skipped when broadcast is nil, got net=%d srv=%d", f.netCalls, f.srvCalls)
	}
}

// Muting: a real rule set on a fixed clock decides which events are marked.

var muteNow = time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)

// ruleSet returns a mute rule set whose clock is fixed at muteNow.
func ruleSet(rules ...mute.Rule) *mute.Set {
	s := mute.NewSet(nil, func() time.Time { return muteNow }, discard())
	s.Replace(rules)
	return s
}

// flapRule mutes the known bundle flap on one host for feed, with the given
// expiry.
func flapRule(id int64, feed mute.Feed, expiresAt time.Time) mute.Rule {
	return mute.Rule{ID: id, Feed: feed, Hostname: "leaf-02", Search: "bundle ae30", ExpiresAt: expiresAt}
}

func flapFetcher() *fakeFetcher {
	return &fakeFetcher{
		netlog: model.NetlogEvent{Hostname: "leaf-02", Message: "lag_bundlestate_ifd_change: bundle ae30 minimum links not met"},
		srvlog: model.SrvlogEvent{Hostname: "leaf-02", Message: "lag_bundlestate_ifd_change: bundle ae30 minimum links not met"},
	}
}

// netlogRecorder captures what Dispatch hands to each netlog sink.
type netlogRecorder struct {
	broadcast []model.NetlogEvent
	notified  []model.NetlogEvent
}

func (r *netlogRecorder) sinks() Sinks {
	return Sinks{
		BroadcastNetlog: func(e model.NetlogEvent) { r.broadcast = append(r.broadcast, e) },
		NotifyNetlog:    func(e model.NetlogEvent) { r.notified = append(r.notified, e) },
	}
}

// srvlogRecorder captures what Dispatch hands to each srvlog sink.
type srvlogRecorder struct {
	broadcast []model.SrvlogEvent
	notified  []model.SrvlogEvent
}

func (r *srvlogRecorder) sinks() Sinks {
	return Sinks{
		BroadcastSrvlog: func(e model.SrvlogEvent) { r.broadcast = append(r.broadcast, e) },
		NotifySrvlog:    func(e model.SrvlogEvent) { r.notified = append(r.notified, e) },
	}
}

func mutedID(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestDispatch_Muting(t *testing.T) {
	active := muteNow.Add(time.Hour)
	expired := muteNow
	otherHost := mute.Rule{ID: 3, Hostname: "leaf-99", ExpiresAt: active}

	tests := []struct {
		name         string
		muter        func(feed mute.Feed) Muter
		wantMutedBy  any
		wantNotified int
	}{
		{
			name:         "matching rule marks and skips notify",
			muter:        func(feed mute.Feed) Muter { return ruleSet(flapRule(7, feed, active)) },
			wantMutedBy:  int64(7),
			wantNotified: 0,
		},
		{
			name: "non-matching rule leaves the event unmarked and notifies once",
			muter: func(feed mute.Feed) Muter {
				r := otherHost
				r.Feed = feed
				return ruleSet(r)
			},
			wantMutedBy:  nil,
			wantNotified: 1,
		},
		{
			name:         "expired rule does not mark",
			muter:        func(feed mute.Feed) Muter { return ruleSet(flapRule(7, feed, expired)) },
			wantMutedBy:  nil,
			wantNotified: 1,
		},
		{
			// A literal nil interface: a nil *mute.Set would not be a nil Muter.
			name:         "nil muter marks nothing",
			muter:        func(mute.Feed) Muter { return nil },
			wantMutedBy:  nil,
			wantNotified: 1,
		},
	}

	for _, tt := range tests {
		t.Run("netlog/"+tt.name, func(t *testing.T) {
			var rec netlogRecorder
			Dispatch(context.Background(), Notification{ChannelNetlog, 11}, flapFetcher(),
				tt.muter(mute.FeedNetlog), rec.sinks(), discard(), time.Second)

			if len(rec.broadcast) != 1 {
				t.Fatalf("broadcast %d events, want 1", len(rec.broadcast))
			}
			if got := mutedID(rec.broadcast[0].MutedBy); got != tt.wantMutedBy {
				t.Errorf("broadcast muted_by = %v, want %v", got, tt.wantMutedBy)
			}
			if len(rec.notified) != tt.wantNotified {
				t.Fatalf("notified %d events, want %d", len(rec.notified), tt.wantNotified)
			}
			if tt.wantNotified == 1 && rec.notified[0].ID != 11 {
				t.Errorf("notified id = %d, want 11", rec.notified[0].ID)
			}
		})

		t.Run("srvlog/"+tt.name, func(t *testing.T) {
			var rec srvlogRecorder
			Dispatch(context.Background(), Notification{ChannelSrvlog, 12}, flapFetcher(),
				tt.muter(mute.FeedSrvlog), rec.sinks(), discard(), time.Second)

			if len(rec.broadcast) != 1 {
				t.Fatalf("broadcast %d events, want 1", len(rec.broadcast))
			}
			if got := mutedID(rec.broadcast[0].MutedBy); got != tt.wantMutedBy {
				t.Errorf("broadcast muted_by = %v, want %v", got, tt.wantMutedBy)
			}
			if len(rec.notified) != tt.wantNotified {
				t.Fatalf("notified %d events, want %d", len(rec.notified), tt.wantNotified)
			}
			if tt.wantNotified == 1 && rec.notified[0].ID != 12 {
				t.Errorf("notified id = %d, want 12", rec.notified[0].ID)
			}
		})
	}
}

func TestDispatch_RuleForOtherFeedDoesNotMark(t *testing.T) {
	// A netlog rule never mutes a srvlog event with the same host and text.
	set := ruleSet(flapRule(7, mute.FeedNetlog, muteNow.Add(time.Hour)))
	var rec srvlogRecorder
	Dispatch(context.Background(), Notification{ChannelSrvlog, 1}, flapFetcher(), set, rec.sinks(), discard(), time.Second)
	if len(rec.broadcast) != 1 || rec.broadcast[0].MutedBy != nil {
		t.Fatalf("srvlog event marked by a netlog rule: %+v", rec.broadcast)
	}
	if len(rec.notified) != 1 {
		t.Fatalf("notified %d events, want 1", len(rec.notified))
	}
}

func TestDispatch_NilNotifyStillBroadcasts(t *testing.T) {
	set := ruleSet()
	var gotNet []model.NetlogEvent
	var gotSrv []model.SrvlogEvent
	s := Sinks{
		BroadcastNetlog: func(e model.NetlogEvent) { gotNet = append(gotNet, e) },
		BroadcastSrvlog: func(e model.SrvlogEvent) { gotSrv = append(gotSrv, e) },
	}
	f := flapFetcher()
	Dispatch(context.Background(), Notification{ChannelNetlog, 5}, f, set, s, discard(), time.Second)
	Dispatch(context.Background(), Notification{ChannelSrvlog, 6}, f, set, s, discard(), time.Second)

	if f.netCalls != 1 || f.srvCalls != 1 {
		t.Fatalf("fetches = net %d, srv %d; want 1 each", f.netCalls, f.srvCalls)
	}
	if len(gotNet) != 1 || gotNet[0].ID != 5 {
		t.Fatalf("netlog broadcast = %+v, want one event with id 5", gotNet)
	}
	if len(gotSrv) != 1 || gotSrv[0].ID != 6 {
		t.Fatalf("srvlog broadcast = %+v, want one event with id 6", gotSrv)
	}
}
