package model

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func intPtr(n int) *int { return &n }

func TestSeverityLabel(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{0, "emerg"},
		{3, "err"},
		{6, "info"},
		{7, "debug"},
		{-1, "unknown(-1)"},
		{8, "unknown(8)"},
	}
	for _, tt := range tests {
		got := SeverityLabel(tt.code)
		if got != tt.want {
			t.Errorf("SeverityLabel(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestFacilityLabel(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{0, "kern"},
		{1, "user"},
		{4, "auth"},
		{16, "local0"},
		{23, "local7"},
		{-1, "unknown(-1)"},
		{24, "unknown(24)"},
	}
	for _, tt := range tests {
		got := FacilityLabel(tt.code)
		if got != tt.want {
			t.Errorf("FacilityLabel(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestSyslogFilter_Matches(t *testing.T) {
	base := SyslogEvent{
		ID:          1,
		Hostname:    "router1",
		FromhostIP:  "10.0.0.1",
		Programname: "rpd",
		Severity:    3,
		Facility:    23,
		SyslogTag:   "rpd[1234]:",
		MsgID:       "BGP_STATE",
		Message:     "BGP peer 10.0.0.2 state changed",
	}

	tests := []struct {
		name   string
		filter SyslogFilter
		want   bool
	}{
		{"empty filter matches all", SyslogFilter{}, true},
		{"hostname match", SyslogFilter{Hostname: "router1"}, true},
		{"hostname mismatch", SyslogFilter{Hostname: "router2"}, false},
		{"fromhost_ip match", SyslogFilter{FromhostIP: "10.0.0.1"}, true},
		{"fromhost_ip mismatch", SyslogFilter{FromhostIP: "10.0.0.2"}, false},
		{"programname match", SyslogFilter{Programname: "rpd"}, true},
		{"programname mismatch", SyslogFilter{Programname: "sshd"}, false},
		{"severity exact match", SyslogFilter{Severity: intPtr(3)}, true},
		{"severity exact mismatch", SyslogFilter{Severity: intPtr(4)}, false},
		{"severity_max includes", SyslogFilter{SeverityMax: intPtr(3)}, true},
		{"severity_max excludes", SyslogFilter{SeverityMax: intPtr(2)}, false},
		{"facility match", SyslogFilter{Facility: intPtr(23)}, true},
		{"facility mismatch", SyslogFilter{Facility: intPtr(0)}, false},
		{"syslogtag match", SyslogFilter{SyslogTag: "rpd[1234]:"}, true},
		{"syslogtag mismatch", SyslogFilter{SyslogTag: "sshd:"}, false},
		{"msgid match", SyslogFilter{MsgID: "BGP_STATE"}, true},
		{"msgid mismatch", SyslogFilter{MsgID: "OTHER"}, false},
		{"search match case-insensitive", SyslogFilter{Search: "bgp peer"}, true},
		{"search mismatch", SyslogFilter{Search: "ospf"}, false},
		{"combined hostname+severity", SyslogFilter{Hostname: "router1", Severity: intPtr(3)}, true},
		{"combined hostname mismatch+severity match", SyslogFilter{Hostname: "router2", Severity: intPtr(3)}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.Matches(base)
			if got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCursor_EncodeDecode(t *testing.T) {
	original := Cursor{
		ReceivedAt: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		ID:         42,
	}

	encoded := original.Encode()
	if encoded == "" {
		t.Fatal("Encode() returned empty string")
	}

	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}

	if !decoded.ReceivedAt.Equal(original.ReceivedAt) {
		t.Errorf("ReceivedAt = %v, want %v", decoded.ReceivedAt, original.ReceivedAt)
	}
	if decoded.ID != original.ID {
		t.Errorf("ID = %d, want %d", decoded.ID, original.ID)
	}
}

func TestDecodeCursor_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"not base64", "!!!invalid!!!"},
		{"missing comma", "MTIzNA=="},      // "1234" base64
		{"bad timestamp", "YWJjLDEyMw=="},  // "abc,123" base64
		{"bad id", "MTIzNDU2Nzg5MCxhYmM="}, // "1234567890,abc" base64
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeCursor(tt.input)
			if err == nil {
				t.Error("DecodeCursor() expected error, got nil")
			}
		})
	}
}

func TestParseLimit(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		defaultLimit int
		maxLimit     int
		want         int
	}{
		{"empty uses default", "", 100, 1000, 100},
		{"valid value", "50", 100, 1000, 50},
		{"exceeds max", "2000", 100, 1000, 1000},
		{"zero uses default", "0", 100, 1000, 100},
		{"negative uses default", "-5", 100, 1000, 100},
		{"non-numeric uses default", "abc", 100, 1000, 100},
		{"one is valid", "1", 100, 1000, 1},
		{"exactly max", "1000", 100, 1000, 1000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{URL: &url.URL{RawQuery: "limit=" + tt.query}}
			if tt.query == "" {
				r.URL.RawQuery = ""
			}
			got := ParseLimit(r, tt.defaultLimit, tt.maxLimit)
			if got != tt.want {
				t.Errorf("ParseLimit() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseSyslogFilter_Valid(t *testing.T) {
	tests := []struct {
		name  string
		query string
		check func(t *testing.T, f SyslogFilter)
	}{
		{
			"empty query",
			"",
			func(t *testing.T, f SyslogFilter) {
				if f.Hostname != "" || f.Severity != nil {
					t.Error("expected empty filter")
				}
			},
		},
		{
			"hostname only",
			"hostname=router1",
			func(t *testing.T, f SyslogFilter) {
				if f.Hostname != "router1" {
					t.Errorf("Hostname = %q, want %q", f.Hostname, "router1")
				}
			},
		},
		{
			"severity",
			"severity=3",
			func(t *testing.T, f SyslogFilter) {
				if f.Severity == nil || *f.Severity != 3 {
					t.Errorf("Severity = %v, want 3", f.Severity)
				}
			},
		},
		{
			"severity_max",
			"severity_max=5",
			func(t *testing.T, f SyslogFilter) {
				if f.SeverityMax == nil || *f.SeverityMax != 5 {
					t.Errorf("SeverityMax = %v, want 5", f.SeverityMax)
				}
			},
		},
		{
			"facility",
			"facility=23",
			func(t *testing.T, f SyslogFilter) {
				if f.Facility == nil || *f.Facility != 23 {
					t.Errorf("Facility = %v, want 23", f.Facility)
				}
			},
		},
		{
			"fromhost_ip IPv4",
			"fromhost_ip=10.0.0.1",
			func(t *testing.T, f SyslogFilter) {
				if f.FromhostIP != "10.0.0.1" {
					t.Errorf("FromhostIP = %q, want %q", f.FromhostIP, "10.0.0.1")
				}
			},
		},
		{
			"fromhost_ip IPv6",
			"fromhost_ip=::1",
			func(t *testing.T, f SyslogFilter) {
				if f.FromhostIP != "::1" {
					t.Errorf("FromhostIP = %q, want %q", f.FromhostIP, "::1")
				}
			},
		},
		{
			"time range",
			"from=2025-01-01T00:00:00Z&to=2025-01-02T00:00:00Z",
			func(t *testing.T, f SyslogFilter) {
				if f.From == nil {
					t.Fatal("From is nil")
				}
				if f.To == nil {
					t.Fatal("To is nil")
				}
				if f.From.Year() != 2025 || f.From.Month() != 1 || f.From.Day() != 1 {
					t.Errorf("From = %v", f.From)
				}
			},
		},
		{
			"search",
			"search=BGP+peer",
			func(t *testing.T, f SyslogFilter) {
				if f.Search != "BGP peer" {
					t.Errorf("Search = %q, want %q", f.Search, "BGP peer")
				}
			},
		},
		{
			"combined filters",
			"hostname=router1&severity=3&programname=rpd",
			func(t *testing.T, f SyslogFilter) {
				if f.Hostname != "router1" {
					t.Errorf("Hostname = %q", f.Hostname)
				}
				if f.Severity == nil || *f.Severity != 3 {
					t.Errorf("Severity = %v", f.Severity)
				}
				if f.Programname != "rpd" {
					t.Errorf("Programname = %q", f.Programname)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{URL: &url.URL{RawQuery: tt.query}}
			f, err := ParseSyslogFilter(r)
			if err != nil {
				t.Fatalf("ParseSyslogFilter() error = %v", err)
			}
			tt.check(t, f)
		})
	}
}

func TestParseSyslogFilter_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"bad severity", "severity=abc"},
		{"severity too high", "severity=8"},
		{"severity negative", "severity=-1"},
		{"bad severity_max", "severity_max=abc"},
		{"severity_max too high", "severity_max=8"},
		{"bad facility", "facility=abc"},
		{"facility too high", "facility=24"},
		{"bad fromhost_ip", "fromhost_ip=notanip"},
		{"bad from time", "from=not-a-date"},
		{"bad to time", "to=2025-13-01"},
		{"multiple errors", "severity=abc&facility=abc&fromhost_ip=bad"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{URL: &url.URL{RawQuery: tt.query}}
			_, err := ParseSyslogFilter(r)
			if err == nil {
				t.Error("ParseSyslogFilter() expected error, got nil")
			}
		})
	}
}

func TestParseCursor(t *testing.T) {
	// Valid cursor.
	c := Cursor{ReceivedAt: time.Now(), ID: 99}
	encoded := c.Encode()
	r := &http.Request{URL: &url.URL{RawQuery: "cursor=" + encoded}}
	got := ParseCursor(r)
	if got == nil {
		t.Fatal("ParseCursor() returned nil for valid cursor")
	}
	if got.ID != 99 {
		t.Errorf("ID = %d, want 99", got.ID)
	}

	// Missing cursor returns nil.
	r = &http.Request{URL: &url.URL{RawQuery: ""}}
	if ParseCursor(r) != nil {
		t.Error("ParseCursor() expected nil for empty cursor")
	}

	// Invalid cursor returns nil.
	r = &http.Request{URL: &url.URL{RawQuery: "cursor=invalid"}}
	if ParseCursor(r) != nil {
		t.Error("ParseCursor() expected nil for invalid cursor")
	}
}
