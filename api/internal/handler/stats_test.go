package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lasseh/taillight/internal/model"
)

// recordingStatsStore records the queries the stats handlers pass on.
type recordingStatsStore struct {
	StatsStore // nil: any method not overridden below panics
	page       model.HostPage
	gotHosts   *model.HostQuery
	gotVolume  *model.VolumeParams
}

func (s *recordingStatsStore) ListHosts(_ context.Context, q model.HostQuery) (model.HostPage, error) {
	s.gotHosts = &q
	return s.page, nil
}

func (s *recordingStatsStore) GetVolume(_ context.Context, p model.VolumeParams) ([]model.VolumeBucket, error) {
	s.gotVolume = &p
	return nil, nil
}

func TestStatsHostsPassesQueryAndReturnsTotal(t *testing.T) {
	store := &recordingStatsStore{page: model.HostPage{
		Hosts: []model.HostEntry{{Hostname: "web01"}},
		Total: 1050,
	}}
	h := NewStatsHandler(store)

	rec := serve(t, "/hosts", "/hosts?search=web&status=warning&feed=srvlog&sort=hostname&limit=1&offset=10", h.Hosts)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	want := model.HostQuery{
		RangeDur: store.gotHosts.RangeDur, Search: "web", Status: model.HostStatusWarning,
		Feed: "srvlog", Sort: "hostname", Limit: 1, Offset: 10,
	}
	if *store.gotHosts != want {
		t.Errorf("store got %+v, want %+v", *store.gotHosts, want)
	}

	var resp struct {
		Data  []model.HostEntry `json:"data"`
		Total int               `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 || resp.Total != 1050 {
		t.Errorf("got %d hosts, total %d; want 1 host, total 1050", len(resp.Data), resp.Total)
	}
}

func TestStatsHostsEmptyPageIsArray(t *testing.T) {
	h := NewStatsHandler(&recordingStatsStore{})
	rec := serve(t, "/hosts", "/hosts", h.Hosts)
	if !strings.Contains(rec.Body.String(), `"data":[]`) {
		t.Errorf("body = %s, want data as []", rec.Body.String())
	}
}

func TestStatsHostsBadParamIs400(t *testing.T) {
	store := &recordingStatsStore{}
	h := NewStatsHandler(store)
	for _, q := range []string{"sort=random", "status=down", "feed=applog", "offset=-1"} {
		rec := serve(t, "/hosts", "/hosts?"+q, h.Hosts)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", q, rec.Code)
		}
	}
	if store.gotHosts != nil {
		t.Error("store was queried despite an invalid parameter")
	}
}

func TestStatsVolumePassesTotalsOnly(t *testing.T) {
	store := &recordingStatsStore{}
	h := NewStatsHandler(store)
	rec := serve(t, "/volume", "/volume?interval=30m&range=7d&totals_only=true", h.Volume)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if store.gotVolume == nil || !store.gotVolume.TotalsOnly {
		t.Errorf("store got %+v, want TotalsOnly", store.gotVolume)
	}
}
