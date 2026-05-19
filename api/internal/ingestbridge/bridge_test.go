package ingestbridge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
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
	Dispatch(context.Background(), Notification{ChannelSrvlog, 7}, f,
		func(e model.SrvlogEvent) { gotSrv = append(gotSrv, e.ID) },
		func(e model.NetlogEvent) { gotNet = append(gotNet, e.ID) },
		discard(), time.Second)

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
	Dispatch(context.Background(), Notification{ChannelNetlog, 9}, f,
		func(model.SrvlogEvent) { t.Fatal("srvlog sink must not fire") },
		func(e model.NetlogEvent) { gotNet = append(gotNet, e.ID) },
		discard(), time.Second)

	if len(gotNet) != 1 || gotNet[0] != 9 {
		t.Fatalf("netlog sink = %v, want [9]", gotNet)
	}
}

func TestDispatch_FetchErrorIsSwallowed(t *testing.T) {
	f := &fakeFetcher{srvlogErr: errors.New("db down")}
	Dispatch(context.Background(), Notification{ChannelSrvlog, 1}, f,
		func(model.SrvlogEvent) { t.Fatal("sink must not fire on fetch error") },
		nil, discard(), time.Second)
	if f.srvCalls != 1 {
		t.Fatalf("GetSrvlog calls = %d, want 1", f.srvCalls)
	}
	// No panic, no sink call — the worker loop survives a bad row.
}

func TestDispatch_UnknownChannelIgnored(t *testing.T) {
	f := &fakeFetcher{}
	Dispatch(context.Background(), Notification{"mystery_ingest", 1}, f,
		func(model.SrvlogEvent) { t.Fatal("must not fire") },
		func(model.NetlogEvent) { t.Fatal("must not fire") },
		discard(), time.Second)
	if f.srvCalls != 0 || f.netCalls != 0 {
		t.Fatalf("no fetch expected, got srv=%d net=%d", f.srvCalls, f.netCalls)
	}
}

func TestDispatch_NilSinkSkipsFetch(t *testing.T) {
	f := &fakeFetcher{}
	// netlog disabled (nil sink) — must not even fetch.
	Dispatch(context.Background(), Notification{ChannelNetlog, 1}, f,
		nil, nil, discard(), time.Second)
	if f.netCalls != 0 {
		t.Fatalf("GetNetlog should be skipped when sink is nil, got %d calls", f.netCalls)
	}
}
