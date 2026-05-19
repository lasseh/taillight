package model

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func req(rawQuery string) *http.Request {
	return &http.Request{URL: &url.URL{RawQuery: rawQuery}}
}

func TestQueryParams_BoundedInt(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		min, max int
		wantPtr  bool
		wantVal  int
		wantErr  bool
	}{
		{"absent", "", 0, 7, false, 0, false},
		{"valid low", "x=0", 0, 7, true, 0, false},
		{"valid high", "x=7", 0, 7, true, 7, false},
		{"below min", "x=-1", 0, 7, false, 0, true},
		{"above max", "x=8", 0, 7, false, 0, true},
		{"not an int", "x=abc", 0, 7, false, 0, true},
		{"facility range ok", "x=23", 0, 23, true, 23, false},
		{"facility out of range", "x=24", 0, 23, false, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newQueryParams(req(tt.query))
			got := p.boundedInt("x", tt.min, tt.max)
			if tt.wantPtr {
				if got == nil || *got != tt.wantVal {
					t.Fatalf("boundedInt = %v, want %d", got, tt.wantVal)
				}
			} else if got != nil {
				t.Fatalf("boundedInt = %d, want nil", *got)
			}
			if hasErr := p.err() != nil; hasErr != tt.wantErr {
				t.Fatalf("err() presence = %v, want %v", hasErr, tt.wantErr)
			}
		})
	}
}

func TestQueryParams_IP(t *testing.T) {
	p := newQueryParams(req("ip=192.168.1.1"))
	if got := p.ip("ip"); got != "192.168.1.1" {
		t.Fatalf("ip = %q, want 192.168.1.1", got)
	}
	if p.err() != nil {
		t.Fatalf("unexpected error: %v", p.err())
	}

	bad := newQueryParams(req("ip=notanip"))
	if got := bad.ip("ip"); got != "" {
		t.Fatalf("ip = %q, want empty", got)
	}
	if bad.err() == nil {
		t.Fatal("expected error for invalid IP")
	}
}

func TestQueryParams_RFC3339(t *testing.T) {
	p := newQueryParams(req("from=2025-01-02T15:04:05Z"))
	if got := p.rfc3339("from"); got == nil {
		t.Fatal("rfc3339 = nil, want parsed time")
	}
	if p.err() != nil {
		t.Fatalf("unexpected error: %v", p.err())
	}

	bad := newQueryParams(req("from=not-a-date"))
	if got := bad.rfc3339("from"); got != nil {
		t.Fatalf("rfc3339 = %v, want nil", got)
	}
	if bad.err() == nil {
		t.Fatal("expected error for invalid RFC3339")
	}
}

func TestQueryParams_StrMaxLen(t *testing.T) {
	long := strings.Repeat("a", maxFilterStringLen+1)
	p := newQueryParams(req("s=" + long))
	if got := p.str("s"); got != "" {
		t.Fatalf("str = %q, want empty on overflow", got)
	}
	if p.err() == nil {
		t.Fatal("expected error for over-length string")
	}

	ok := newQueryParams(req("s=hello"))
	if got := ok.str("s"); got != "hello" {
		t.Fatalf("str = %q, want hello", got)
	}
}

func TestQueryParams_ErrAccumulates(t *testing.T) {
	p := newQueryParams(req("a=bad&b=bad"))
	p.boundedInt("a", 0, 7)
	p.boundedInt("b", 0, 7)
	err := p.err()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "a:") || !strings.Contains(err.Error(), "b:") {
		t.Fatalf("error should mention both params: %v", err)
	}
}

// ParseNetlogFilter was previously only exercised through handler integration
// tests; cover its validation directly.
func TestParseNetlogFilter_Valid(t *testing.T) {
	r := req("hostname=router1&severity=3&facility=23&fromhost_ip=10.0.0.1&from=2025-01-02T15:04:05Z")
	f, err := ParseNetlogFilter(r)
	if err != nil {
		t.Fatalf("ParseNetlogFilter() error = %v", err)
	}
	if f.Hostname != "router1" {
		t.Errorf("Hostname = %q, want router1", f.Hostname)
	}
	if f.Severity == nil || *f.Severity != 3 {
		t.Errorf("Severity = %v, want 3", f.Severity)
	}
	if f.Facility == nil || *f.Facility != 23 {
		t.Errorf("Facility = %v, want 23", f.Facility)
	}
	if f.FromhostIP != "10.0.0.1" {
		t.Errorf("FromhostIP = %q, want 10.0.0.1", f.FromhostIP)
	}
	if f.From == nil {
		t.Error("From = nil, want parsed time")
	}
}

func TestParseNetlogFilter_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"bad severity", "severity=abc"},
		{"severity too high", "severity=8"},
		{"facility too high", "facility=24"},
		{"bad fromhost_ip", "fromhost_ip=notanip"},
		{"bad from time", "from=not-a-date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseNetlogFilter(req(tt.query)); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

// ParseAppLogFilter was previously only exercised through handler integration
// tests; cover its validation and level normalization directly.
func TestParseAppLogFilter_Valid(t *testing.T) {
	r := req("service=api&component=auth&host=node1&level=warning&level_exact=error&from=2025-01-02T15:04:05Z")
	f, err := ParseAppLogFilter(r)
	if err != nil {
		t.Fatalf("ParseAppLogFilter() error = %v", err)
	}
	if f.Service != "api" || f.Component != "auth" || f.Host != "node1" {
		t.Errorf("string fields = %q/%q/%q", f.Service, f.Component, f.Host)
	}
	if f.Level != "WARN" { // "warning" normalizes to WARN
		t.Errorf("Level = %q, want WARN", f.Level)
	}
	if f.LevelExact != "ERROR" {
		t.Errorf("LevelExact = %q, want ERROR", f.LevelExact)
	}
	if f.From == nil {
		t.Error("From = nil, want parsed time")
	}
}

func TestParseAppLogFilter_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"bad level", "level=verbose"},
		{"bad level_exact", "level_exact=loud"},
		{"bad from time", "from=not-a-date"},
		{"bad to time", "to=2025-13-01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseAppLogFilter(req(tt.query)); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}
