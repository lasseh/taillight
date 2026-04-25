package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/netbox"
)

type fakeNetboxStore struct {
	event model.NetlogEvent
	err   error
}

func (f fakeNetboxStore) GetNetlog(_ context.Context, _ int64) (model.NetlogEvent, error) {
	return f.event, f.err
}

// netboxResponseEnvelope mirrors the shape produced by EnrichNetlog.
type netboxResponseEnvelope struct {
	Data struct {
		Entities []netbox.Entity `json:"entities"`
		Lookups  []netbox.Lookup `json:"lookups"`
	} `json:"data"`
}

func newTestNetboxClient(t *testing.T, h http.HandlerFunc) (*netbox.Client, func()) {
	t.Helper()
	srv := httptest.NewServer(h)
	c, err := netbox.NewClient(netbox.Config{URL: srv.URL, Token: "x", CacheTTL: time.Minute})
	if err != nil {
		srv.Close()
		t.Fatal(err)
	}
	return c, func() {
		c.Close()
		srv.Close()
	}
}

func doEnrich(t *testing.T, h *NetboxHandler, id string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/api/v1/netlog/{id}/netbox", h.EnrichNetlog)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/netlog/"+id+"/netbox", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestNetboxHandler_BadID(t *testing.T) {
	client, cleanup := newTestNetboxClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("netbox should not be called for a bad id")
	})
	defer cleanup()

	h := NewNetboxHandler(client, fakeNetboxStore{}, nil)
	rr := doEnrich(t, h, "abc")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestNetboxHandler_MissingLog(t *testing.T) {
	client, cleanup := newTestNetboxClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("netbox should not be called when the log is missing")
	})
	defer cleanup()

	h := NewNetboxHandler(client, fakeNetboxStore{err: pgx.ErrNoRows}, nil)
	rr := doEnrich(t, h, "999")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

func TestNetboxHandler_HappyPath(t *testing.T) {
	// Server returns a device for "router1", IP not found, interface found.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/dcim/devices"):
			w.Write([]byte(`{"count":1,"results":[{"id":1,"name":"router1","display":"router1","status":{"value":"active","label":"Active"}}]}`)) //nolint:errcheck // test server.
		case strings.HasPrefix(r.URL.Path, "/api/ipam/ip-addresses"):
			w.Write([]byte(`{"count":0,"results":[]}`)) //nolint:errcheck // test server.
		case strings.HasPrefix(r.URL.Path, "/api/dcim/interfaces"):
			w.Write([]byte(`{"count":1,"results":[{"id":2,"name":"ge-0/0/1","device":{"id":1,"name":"router1","display":"router1"},"mtu":1500}]}`)) //nolint:errcheck // test server.
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c, err := netbox.NewClient(netbox.Config{URL: srv.URL, Token: "x", CacheTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	store := fakeNetboxStore{event: model.NetlogEvent{
		ID:       42,
		Hostname: "router1",
		Message:  "BGP peer 10.0.0.5 on ge-0/0/1 is down",
	}}
	h := NewNetboxHandler(c, store, nil)
	rr := doEnrich(t, h, "42")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var env netboxResponseEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v\nbody=%s", err, rr.Body.String())
	}

	// We expect at least: device(router1), ip(10.0.0.5), interface(ge-0/0/1).
	wantTypes := map[string]bool{netbox.EntityDevice: false, netbox.EntityIP: false, netbox.EntityInterface: false}
	for _, e := range env.Data.Entities {
		if _, ok := wantTypes[e.Type]; ok {
			wantTypes[e.Type] = true
		}
	}
	for k, ok := range wantTypes {
		if !ok {
			t.Fatalf("missing extracted entity %q in %+v", k, env.Data.Entities)
		}
	}

	// Lookup invariants: never fail the request even when individual lookups miss.
	gotFound := map[string]bool{}
	for _, lk := range env.Data.Lookups {
		if lk.Found {
			gotFound[lk.Entity.Type] = true
		}
		if lk.Error != "" {
			t.Errorf("unexpected error on %s/%s: %s", lk.Entity.Type, lk.Entity.Value, lk.Error)
		}
	}
	if !gotFound[netbox.EntityDevice] || !gotFound[netbox.EntityInterface] {
		t.Fatalf("expected device and interface to be found, got %+v", env.Data.Lookups)
	}
	// IP should be present but not found.
	for _, lk := range env.Data.Lookups {
		if lk.Entity.Type == netbox.EntityIP && lk.Found {
			t.Fatalf("expected ip lookup to be not-found, got %+v", lk)
		}
	}
}

func TestNetboxHandler_PerEntityErrorsDontFailRequest(t *testing.T) {
	// All Netbox calls 500 — request still 200, individual lookups carry Error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, err := netbox.NewClient(netbox.Config{URL: srv.URL, Token: "x", CacheTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	store := fakeNetboxStore{event: model.NetlogEvent{
		ID:       1,
		Hostname: "router1",
		Message:  "10.0.0.1 logged",
	}}
	h := NewNetboxHandler(c, store, nil)
	rr := doEnrich(t, h, "1")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var env netboxResponseEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Lookups) == 0 {
		t.Fatal("want lookups for extracted entities, got none")
	}
	for _, lk := range env.Data.Lookups {
		if lk.Found {
			t.Errorf("upstream 500 should not produce found=true: %+v", lk)
		}
		if lk.Error == "" {
			t.Errorf("expected per-entity Error, got none for %+v", lk)
		}
	}
}
