package model

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

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

func TestSrvlogFilter_Matches(t *testing.T) {
	base := SrvlogEvent{
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
		filter SrvlogFilter
		want   bool
	}{
		{"empty filter matches all", SrvlogFilter{}, true},
		{"hostname match", SrvlogFilter{Hostname: "router1"}, true},
		{"hostname mismatch", SrvlogFilter{Hostname: "router2"}, false},
		{"fromhost_ip match", SrvlogFilter{FromhostIP: "10.0.0.1"}, true},
		{"fromhost_ip mismatch", SrvlogFilter{FromhostIP: "10.0.0.2"}, false},
		{"programname match", SrvlogFilter{Programname: "rpd"}, true},
		{"programname mismatch", SrvlogFilter{Programname: "sshd"}, false},
		{"severity exact match", SrvlogFilter{Severity: new(3)}, true},
		{"severity exact mismatch", SrvlogFilter{Severity: new(4)}, false},
		{"severity_max includes", SrvlogFilter{SeverityMax: new(3)}, true},
		{"severity_max excludes", SrvlogFilter{SeverityMax: new(2)}, false},
		{"facility match", SrvlogFilter{Facility: new(23)}, true},
		{"facility mismatch", SrvlogFilter{Facility: new(0)}, false},
		{"syslogtag match", SrvlogFilter{SyslogTag: "rpd[1234]:"}, true},
		{"syslogtag mismatch", SrvlogFilter{SyslogTag: "sshd:"}, false},
		{"msgid match", SrvlogFilter{MsgID: "BGP_STATE"}, true},
		{"msgid mismatch", SrvlogFilter{MsgID: "OTHER"}, false},
		{"search match case-insensitive", SrvlogFilter{Search: "bgp peer"}, true},
		{"search mismatch", SrvlogFilter{Search: "ospf"}, false},
		{"combined hostname+severity", SrvlogFilter{Hostname: "router1", Severity: new(3)}, true},
		{"combined hostname mismatch+severity match", SrvlogFilter{Hostname: "router2", Severity: new(3)}, false},
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

func TestParseHideMuted(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    bool
		wantErr bool
	}{
		{"absent is false", "", false, false},
		{"true", "hide_muted=true", true, false},
		{"1", "hide_muted=1", true, false},
		{"false", "hide_muted=false", false, false},
		{"0", "hide_muted=0", false, false},
		{"garbage is an error", "hide_muted=yes", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHideMuted(&http.Request{URL: &url.URL{RawQuery: tt.query}})
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseHideMuted() err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseHideMuted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseSrvlogFilter_Valid(t *testing.T) {
	tests := []struct {
		name  string
		query string
		check func(t *testing.T, f SrvlogFilter)
	}{
		{
			"empty query",
			"",
			func(t *testing.T, f SrvlogFilter) {
				if f.Hostname != "" || f.Severity != nil {
					t.Error("expected empty filter")
				}
			},
		},
		{
			"hostname only",
			"hostname=router1",
			func(t *testing.T, f SrvlogFilter) {
				if f.Hostname != "router1" {
					t.Errorf("Hostname = %q, want %q", f.Hostname, "router1")
				}
			},
		},
		{
			"severity",
			"severity=3",
			func(t *testing.T, f SrvlogFilter) {
				if f.Severity == nil || *f.Severity != 3 {
					t.Errorf("Severity = %v, want 3", f.Severity)
				}
			},
		},
		{
			"severity_max",
			"severity_max=5",
			func(t *testing.T, f SrvlogFilter) {
				if f.SeverityMax == nil || *f.SeverityMax != 5 {
					t.Errorf("SeverityMax = %v, want 5", f.SeverityMax)
				}
			},
		},
		{
			"facility",
			"facility=23",
			func(t *testing.T, f SrvlogFilter) {
				if f.Facility == nil || *f.Facility != 23 {
					t.Errorf("Facility = %v, want 23", f.Facility)
				}
			},
		},
		{
			"fromhost_ip IPv4",
			"fromhost_ip=10.0.0.1",
			func(t *testing.T, f SrvlogFilter) {
				if f.FromhostIP != "10.0.0.1" {
					t.Errorf("FromhostIP = %q, want %q", f.FromhostIP, "10.0.0.1")
				}
			},
		},
		{
			"fromhost_ip IPv6",
			"fromhost_ip=::1",
			func(t *testing.T, f SrvlogFilter) {
				if f.FromhostIP != "::1" {
					t.Errorf("FromhostIP = %q, want %q", f.FromhostIP, "::1")
				}
			},
		},
		{
			"time range",
			"from=2025-01-01T00:00:00Z&to=2025-01-02T00:00:00Z",
			func(t *testing.T, f SrvlogFilter) {
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
			func(t *testing.T, f SrvlogFilter) {
				if f.Search != "BGP peer" {
					t.Errorf("Search = %q, want %q", f.Search, "BGP peer")
				}
			},
		},
		{
			"combined filters",
			"hostname=router1&severity=3&programname=rpd",
			func(t *testing.T, f SrvlogFilter) {
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
			f, err := ParseSrvlogFilter(r)
			if err != nil {
				t.Fatalf("ParseSrvlogFilter() error = %v", err)
			}
			tt.check(t, f)
		})
	}
}

func TestParseSrvlogFilter_Invalid(t *testing.T) {
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
			_, err := ParseSrvlogFilter(r)
			if err == nil {
				t.Error("ParseSrvlogFilter() expected error, got nil")
			}
		})
	}
}

func Test_matchWildcard(t *testing.T) {
	tests := []struct {
		value   string
		pattern string
		want    bool
	}{
		// Prefix glob.
		{"c-lab-sw01", "c-lab-*", true},
		{"c-lab-rtr02", "c-lab-*", true},
		{"d-lab-sw01", "c-lab-*", false},
		// Suffix glob.
		{"core-sw01", "*-sw01", true},
		{"edge-sw01", "*-sw01", true},
		{"core-rtr01", "*-sw01", false},
		// Middle glob.
		{"c-lab-sw01", "c-*-sw01", true},
		{"c-prod-sw01", "c-*-sw01", true},
		{"c-lab-rtr01", "c-*-sw01", false},
		// No wildcard — exact match.
		{"router1", "router1", true},
		{"router2", "router1", false},
		// Case-insensitive.
		{"Router1", "router1", true},
		{"ROUTER1", "router*", true},
		// Match-all.
		{"anything", "*", true},
		{"", "*", true},
		// Multiple wildcards.
		{"a-b-c-d", "a-*-*-d", true},
		{"a-x-y-d", "a-*-*-d", true},
		{"a-x-y-e", "a-*-*-d", false},
	}
	for _, tt := range tests {
		name := fmt.Sprintf("%s~%s", tt.value, tt.pattern)
		t.Run(name, func(t *testing.T) {
			got := matchWildcard(tt.value, tt.pattern)
			if got != tt.want {
				t.Errorf("matchWildcard(%q, %q) = %v, want %v", tt.value, tt.pattern, got, tt.want)
			}
		})
	}
}

func Test_containsFold(t *testing.T) {
	tests := []struct {
		s      string
		substr string // must be lowercase, as produced by strings.ToLower
		want   bool
	}{
		{"BGP peer 10.0.0.2 state changed", "bgp peer", true},
		{"BGP PEER DOWN", "bgp peer", true},
		{"bgp peer down", "bgp peer", true},
		{"BGP peer down", "ospf", false},
		{"", "bgp", false},
		{"bgp", "bgp peer longer than haystack", false},
		{"anything", "", true},
		{"", "", true},
		// Unicode case folding (multi-byte runes).
		{"Grønn ØL", "grønn øl", true},
		{"GRØNN ØL", "øl", true},
		{"Grønn ØL", "gronn", false},
		// Match at start and end.
		{"error: disk full", "error", true},
		{"error: disk full", "full", true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s~%s", tt.s, tt.substr), func(t *testing.T) {
			if got := containsFold(tt.s, tt.substr); got != tt.want {
				t.Errorf("containsFold(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
			}
			// []byte form must behave identically (applog Attrs path).
			if got := containsFold([]byte(tt.s), tt.substr); got != tt.want {
				t.Errorf("containsFold([]byte(%q), %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
			}
		})
	}
}

// containsFoldPerRune is the per-rune scan containsFold used before its match
// became linear, kept as the reference its results must equal. It tries a
// match at every rune boundary of s, lowering one rune at a time.
func containsFoldPerRune(s, substrLower string) bool {
	if substrLower == "" {
		return true
	}
	for start := range len(s) {
		if s[start]&0xC0 == 0x80 {
			continue // UTF-8 continuation byte: matches only start on rune boundaries
		}
		i, matched := start, true
		for _, pr := range substrLower {
			if i >= len(s) {
				matched = false
				break
			}
			sr, size := utf8.DecodeRuneInString(s[i:])
			if unicode.ToLower(sr) != pr {
				matched = false
				break
			}
			i += size
		}
		if matched {
			return true
		}
	}
	return false
}

// TestContainsFold_SameResultsAsPerRuneScan proves the linear match finds the
// same events as the per-rune scan it replaced, for uppercase, non-ASCII and
// length-changing case mappings. search is lowered the way filters lower it.
//
// The one input where the two differ is left out: invalid UTF-8 whose match
// would start on a stray continuation byte, against a search starting with
// U+FFFD. The old scan never started there; strings.ToLower turns the byte
// into U+FFFD, so the linear match does, as containsFold documents. Message
// text read back from Postgres is always valid UTF-8.
func TestContainsFold_SameResultsAsPerRuneScan(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		search string
		want   bool
	}{
		{"lowercase ascii", "bgp peer 10.0.0.2 state changed", "BGP peer", true},
		{"uppercase ascii", "BGP PEER DOWN", "bgp Peer", true},
		{"uppercase ascii absent", "INTERFACE XE-0/0/0 DOWN", "xe-0/0/1", false},
		{"match at start", "ERROR: disk full", "error", true},
		{"match at end", "error: DISK FULL", "full", true},
		{"repeated prefix near miss", strings.Repeat("A", 600), strings.Repeat("a", 499) + "b", false},
		{"repeated prefix match at end", strings.Repeat("A", 600) + "B", strings.Repeat("a", 499) + "b", true},
		{"norwegian letters", "Grønn ØL på ÅSEN", "øl PÅ", true},
		{"norwegian uppercase", "GRØNN", "grønn", true},
		{"accent does not fold", "Grønn", "gronn", false},
		{"cyrillic", "ОШИБКА диска", "ошибка", true},
		{"greek", "ΟΔΥΣΣΕΥΣ", "οδυσσευσ", true},
		{"greek final sigma does not fold", "ΣΊΣΥΦΟΣ", "σίσυφος", false},
		{"dotted capital i shrinks", "İSTANBUL", "istanbul", true},
		{"kelvin sign shrinks", "temp 300K reached", "300k", true},
		{"kelvin sign in search", "temp 300k reached", "300K", true},
		{"capital sharp s", "STRAẞE", "straße", true},
		{"no case", "错误: 磁盘已满", "磁盘", true},
		{"emoji", "deploy 🚀 DONE", "🚀 done", true},
		{"invalid byte matches replacement", "bad \xff BYTE", "\xff byte", true},
		{"invalid lead byte", "\xc3( OK", "\xc3( ok", true},
		{"truncated rune", "\xf0\x9f\x98 X", "\xf0\x9f\x98 x", true},
		{"literal replacement char", "x�Y", "�y", true},
		{"empty search", "anything", "", true},
		{"empty message", "", "bgp", false},
		{"both empty", "", "", true},
		{"search longer than message", "bgp", "bgp peer longer than message", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			needle := strings.ToLower(tt.search)
			want := containsFoldPerRune(tt.s, needle)
			if want != tt.want {
				t.Fatalf("reference containsFoldPerRune(%q, %q) = %v, table says %v", tt.s, needle, want, tt.want)
			}
			if got := containsFold(tt.s, needle); got != want {
				t.Errorf("containsFold(%q, %q) = %v, per-rune scan = %v", tt.s, needle, got, want)
			}
			if got := containsFold([]byte(tt.s), needle); got != want {
				t.Errorf("containsFold([]byte(%q), %q) = %v, per-rune scan = %v", tt.s, needle, got, want)
			}
		})
	}
}

// TestSrvlogFilter_Matches_SearchParsed proves case-insensitive search still
// matches when the filter comes from ParseSrvlogFilter, i.e. via the
// precomputed searchLower hot path used by SSE broadcast.
func TestSrvlogFilter_Matches_SearchParsed(t *testing.T) {
	r := &http.Request{URL: &url.URL{RawQuery: "search=BGP+PEER"}}
	f, err := ParseSrvlogFilter(r)
	if err != nil {
		t.Fatalf("ParseSrvlogFilter() error = %v", err)
	}
	if !f.Matches(SrvlogEvent{Message: "bgp peer 10.0.0.2 state changed"}) {
		t.Error("parsed search filter should match case-insensitively")
	}
	if f.Matches(SrvlogEvent{Message: "ospf adjacency down"}) {
		t.Error("parsed search filter should not match unrelated message")
	}
}

func TestNetlogFilter_Matches_Search(t *testing.T) {
	r := &http.Request{URL: &url.URL{RawQuery: "search=Link+Down"}}
	f, err := ParseNetlogFilter(r)
	if err != nil {
		t.Fatalf("ParseNetlogFilter() error = %v", err)
	}
	if !f.Matches(NetlogEvent{Message: "SNMP_TRAP_LINK_DOWN: link down on xe-0/0/0"}) {
		t.Error("parsed search filter should match case-insensitively")
	}
	// Directly-constructed filter (no precomputed searchLower) still works.
	direct := NetlogFilter{Search: "LINK DOWN"}
	if !direct.Matches(NetlogEvent{Message: "snmp link down"}) {
		t.Error("direct search filter should match case-insensitively")
	}
}

// BenchmarkSrvlogFilter_Matches_Search tracks the SSE broadcast hot path, where
// Matches runs once per subscriber. Matching is linear in the message length
// for any search term, and lowers the message on each call: a message that
// lowering changes (uppercase or non-ASCII letters) costs one allocation of
// its size per match.
func BenchmarkSrvlogFilter_Matches_Search(b *testing.B) {
	longNeedle := strings.Repeat("a", maxFilterStringLen-1) + "b"
	tests := []struct {
		name    string
		search  string
		message string
		want    bool
	}{
		{"lowercase ascii", "BGP+Peer", "interface xe-0/0/0 flapped repeatedly before the bgp peer 10.0.0.2 state changed", true},
		{"mixed case ascii", "BGP+Peer", "interface xe-0/0/0 flapped repeatedly before the BGP peer 10.0.0.2 state changed", true},
		{"uppercase ascii", "BGP+Peer", "INTERFACE XE-0/0/0 FLAPPED REPEATEDLY BEFORE THE BGP PEER 10.0.0.2 STATE CHANGED", true},
		{"non-ascii", "BGP+Peer", "Grensesnitt xe-0/0/0 på Østre ruter flagret før BGP-peer 10.0.0.2 endret tilstand: BGP peer nede", true},
		{"64KiB lowercase near miss", longNeedle, strings.Repeat("a", 64<<10), false},
		{"64KiB uppercase near miss", longNeedle, strings.Repeat("A", 64<<10), false},
	}
	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			r := &http.Request{URL: &url.URL{RawQuery: "search=" + tt.search}}
			f, err := ParseSrvlogFilter(r)
			if err != nil {
				b.Fatalf("ParseSrvlogFilter() error = %v", err)
			}
			e := SrvlogEvent{Message: tt.message}
			b.ReportAllocs()
			for b.Loop() {
				if f.Matches(e) != tt.want {
					b.Fatalf("Matches() = %v, want %v", !tt.want, tt.want)
				}
			}
		})
	}
}

func TestSrvlogFilter_Matches_Wildcard(t *testing.T) {
	base := SrvlogEvent{
		Hostname: "c-lab-sw01",
		Severity: 3,
		Message:  "test",
	}
	tests := []struct {
		name   string
		filter SrvlogFilter
		want   bool
	}{
		{"wildcard hostname match", SrvlogFilter{Hostname: "c-lab-*"}, true},
		{"wildcard hostname mismatch", SrvlogFilter{Hostname: "d-lab-*"}, false},
		{"exact hostname still works", SrvlogFilter{Hostname: "c-lab-sw01"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filter.Matches(base); got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAppLogFilter_Matches_Wildcard(t *testing.T) {
	base := AppLogEvent{
		Host:    "c-lab-sw01",
		Level:   "INFO",
		Service: "myapp",
		Msg:     "test",
	}
	tests := []struct {
		name   string
		filter AppLogFilter
		want   bool
	}{
		{"wildcard host match", AppLogFilter{Host: "c-lab-*"}, true},
		{"wildcard host mismatch", AppLogFilter{Host: "d-lab-*"}, false},
		{"exact host still works", AppLogFilter{Host: "c-lab-sw01"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filter.Matches(base); got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
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
