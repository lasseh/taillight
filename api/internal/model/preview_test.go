package model

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func strPtr(s string) *string { return &s }

func TestSyslogWithPreview(t *testing.T) {
	long := strings.Repeat("<", 65441)
	// "é" is two bytes, so this message's preview cut falls inside a rune.
	runeSplit := strings.Repeat("a", MessagePreviewLimit-1) + "é" + "tail"

	tests := []struct {
		name      string
		msg       string
		raw, sd   *string
		wantMsg   int // byte length after the cut
		wantRaw   int // -1: nil
		wantSD    int // -1: nil
		truncated bool
	}{
		{"short text is kept", "link down", strPtr("<11>raw"), strPtr("[sd]"), 9, 7, 4, false},
		{"nil optional fields stay nil", "x", nil, nil, 1, -1, -1, false},
		{"text at the limits is kept", strings.Repeat("m", MessagePreviewLimit), strPtr(strings.Repeat("r", RawPreviewLimit)), nil, MessagePreviewLimit, RawPreviewLimit, -1, false},
		{"long message is cut", long, nil, nil, MessagePreviewLimit, -1, -1, true},
		{"long raw_message alone sets the flag", "x", strPtr(long), nil, 1, RawPreviewLimit, -1, true},
		{"long structured_data alone sets the flag", "x", nil, strPtr(long), 1, -1, RawPreviewLimit, true},
		{"cut backs off to a rune boundary", runeSplit, nil, nil, MessagePreviewLimit - 1, -1, -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := func(feed, msg string, raw, sd *string, truncated bool) {
				t.Helper()
				if len(msg) != tt.wantMsg || !utf8.ValidString(msg) {
					t.Errorf("%s: message is %d bytes (valid UTF-8 %v), want %d", feed, len(msg), utf8.ValidString(msg), tt.wantMsg)
				}
				for _, f := range []struct {
					name string
					got  *string
					want int
				}{{"raw_message", raw, tt.wantRaw}, {"structured_data", sd, tt.wantSD}} {
					switch {
					case f.want < 0 && f.got != nil:
						t.Errorf("%s: %s = %q, want nil", feed, f.name, *f.got)
					case f.want >= 0 && (f.got == nil || len(*f.got) != f.want):
						t.Errorf("%s: %s = %v, want %d bytes", feed, f.name, f.got, f.want)
					}
				}
				if truncated != tt.truncated {
					t.Errorf("%s: Truncated = %v, want %v", feed, truncated, tt.truncated)
				}
			}

			var rawBefore string
			if tt.raw != nil {
				rawBefore = *tt.raw
			}

			srv := SrvlogEvent{Message: tt.msg, RawMessage: tt.raw, StructuredData: tt.sd}.WithPreview()
			check("srvlog", srv.Message, srv.RawMessage, srv.StructuredData, srv.Truncated)
			net := NetlogEvent{Message: tt.msg, RawMessage: tt.raw, StructuredData: tt.sd}.WithPreview()
			check("netlog", net.Message, net.RawMessage, net.StructuredData, net.Truncated)

			// The caller's strings are left whole.
			if tt.raw != nil && *tt.raw != rawBefore {
				t.Error("WithPreview changed the caller's raw_message")
			}
		})
	}
}
