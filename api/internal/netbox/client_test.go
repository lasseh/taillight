package netbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewClientAuthScheme(t *testing.T) {
	cases := []struct {
		scheme  string
		want    string
		wantErr bool
	}{
		{"", "Token abc", false},
		{"token", "Token abc", false},
		{"TOKEN", "Token abc", false},
		{"bearer", "Bearer abc", false},
		{"Bearer", "Bearer abc", false},
		{"basic", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.scheme, func(t *testing.T) {
			c, err := NewClient(Config{URL: "http://example.com", Token: "abc", AuthScheme: tc.scheme})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NewClient(%q): want error, got nil", tc.scheme)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewClient(%q): unexpected error: %v", tc.scheme, err)
			}
			defer c.Close()
			if c.authHeader != tc.want {
				t.Fatalf("authHeader = %q, want %q", c.authHeader, tc.want)
			}
		})
	}
}

func TestNewClientRequiresURL(t *testing.T) {
	if _, err := NewClient(Config{Token: "x"}); err == nil {
		t.Fatal("NewClient with empty URL: want error, got nil")
	}
}

func TestClientLookupDevice_AuthSchemes(t *testing.T) {
	cases := []struct {
		scheme     string
		wantHeader string
	}{
		{"token", "Token secret"},
		{"bearer", "Bearer secret"},
	}
	for _, tc := range cases {
		t.Run(tc.scheme, func(t *testing.T) {
			var seenHeader string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seenHeader = r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"count":1,"results":[{"id":42,"name":"router1","display":"router1","status":{"value":"active","label":"Active"}}]}`)) //nolint:errcheck // test server.
			}))
			defer srv.Close()

			c, err := NewClient(Config{URL: srv.URL, Token: "secret", AuthScheme: tc.scheme, CacheTTL: time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()

			d, err := c.LookupDevice(context.Background(), "router1")
			if err != nil {
				t.Fatalf("LookupDevice: %v", err)
			}
			if d == nil || d.Name != "router1" {
				t.Fatalf("device = %+v", d)
			}
			if seenHeader != tc.wantHeader {
				t.Fatalf("Authorization header = %q, want %q", seenHeader, tc.wantHeader)
			}
		})
	}
}

func TestClientLookupCache(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"count":1,"results":[{"id":7,"name":"sw1","display":"sw1"}]}`)) //nolint:errcheck // test server.
	}))
	defer srv.Close()

	c, err := NewClient(Config{URL: srv.URL, Token: "x", CacheTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	for range 3 {
		if _, err := c.LookupDevice(context.Background(), "sw1"); err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("server hits = %d, want 1 (cache should dedupe repeats)", got)
	}
}

func TestClientLookupNegativeCache(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"count":0,"results":[]}`)) //nolint:errcheck // test server.
	}))
	defer srv.Close()

	c, err := NewClient(Config{URL: srv.URL, Token: "x", CacheTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	for range 2 {
		d, err := c.LookupDevice(context.Background(), "missing")
		if err != nil {
			t.Fatalf("LookupDevice: %v", err)
		}
		if d != nil {
			t.Fatalf("expected nil for missing device, got %+v", d)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("server hits = %d, want 1 (negative cache should dedupe)", got)
	}
}

func TestClientLookupNotFoundFromAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c, err := NewClient(Config{URL: srv.URL, Token: "x", CacheTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	d, err := c.LookupDevice(context.Background(), "x")
	if err != nil {
		t.Fatalf("404 should be silent: %v", err)
	}
	if d != nil {
		t.Fatalf("404 should give nil device, got %+v", d)
	}
}

func TestClientLookupServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, err := NewClient(Config{URL: srv.URL, Token: "x", CacheTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.LookupDevice(context.Background(), "x"); err == nil {
		t.Fatal("expected error for 500")
	} else if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error should mention 500, got %v", err)
	}
}

func TestClientLookupInterface(t *testing.T) {
	var seenQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"count":1,"results":[{"id":11,"name":"ge-0/0/1","device":{"id":1,"name":"router1","display":"router1"},"type":{"value":"1000base-t","label":"1000BASE-T"},"mtu":1500,"mac_address":"aa:bb:cc:dd:ee:ff","enabled":true}]}`)) //nolint:errcheck // test server.
	}))
	defer srv.Close()

	c, err := NewClient(Config{URL: srv.URL, Token: "x", CacheTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	iface, err := c.LookupInterface(context.Background(), "router1", "ge-0/0/1")
	if err != nil {
		t.Fatal(err)
	}
	if iface == nil {
		t.Fatal("expected interface, got nil")
	}
	if iface.Name != "ge-0/0/1" || iface.Device != "router1" || iface.MTU != 1500 || iface.MACAddress != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("interface = %+v", iface)
	}
	if !strings.Contains(seenQuery, "device=router1") || !strings.Contains(seenQuery, "name=ge-0%2F0%2F1") {
		t.Fatalf("query = %q (expected device + escaped interface name)", seenQuery)
	}
}
