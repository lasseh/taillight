package model

import (
	"net/http"
	"net/url"
	"testing"
)

// TestAppLogFilter_Matches_LevelFailClosed verifies that a level filter with an
// unrecognised level (e.g. an un-normalised alias from a notification rule,
// where levelMinRank is never set) matches nothing rather than silently
// matching every event (audit N6).
func TestAppLogFilter_Matches_LevelFailClosed(t *testing.T) {
	unknown := AppLogFilter{Level: "warning"} // alias, no levelMinRank
	if unknown.Matches(AppLogEvent{Level: "ERROR"}) {
		t.Error("filter with unrecognised level matched (should fail closed)")
	}

	// Canonical level still ranks correctly: WARN matches WARN+ and excludes INFO.
	warn := AppLogFilter{Level: "WARN"}
	if !warn.Matches(AppLogEvent{Level: "ERROR"}) {
		t.Error("WARN filter should match ERROR")
	}
	if warn.Matches(AppLogEvent{Level: "INFO"}) {
		t.Error("WARN filter should not match INFO")
	}
}

// TestAppLogFilter_Matches_Search verifies case-insensitive search on both Msg
// and Attrs, for parsed filters (precomputed searchLower) and directly
// constructed ones (notification rules, no searchLower).
func TestAppLogFilter_Matches_Search(t *testing.T) {
	r := &http.Request{URL: &url.URL{RawQuery: "search=Connection+REFUSED"}}
	parsed, err := ParseAppLogFilter(r)
	if err != nil {
		t.Fatalf("ParseAppLogFilter() error = %v", err)
	}
	direct := AppLogFilter{Search: "Connection REFUSED"}

	for name, f := range map[string]AppLogFilter{"parsed": parsed, "direct": direct} {
		t.Run(name, func(t *testing.T) {
			if !f.Matches(AppLogEvent{Msg: "dial tcp: connection refused"}) {
				t.Error("search should match Msg case-insensitively")
			}
			if !f.Matches(AppLogEvent{Msg: "request failed", Attrs: []byte(`{"err":"CONNECTION refused"}`)}) {
				t.Error("search should match Attrs case-insensitively")
			}
			if f.Matches(AppLogEvent{Msg: "request ok", Attrs: []byte(`{"status":200}`)}) {
				t.Error("search should not match unrelated event")
			}
		})
	}
}
