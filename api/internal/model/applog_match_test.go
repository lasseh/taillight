package model

import "testing"

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
