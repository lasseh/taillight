package model

import "testing"

// TestAnalysisModeForFrequency locks in the cadence → prompt mode mapping that
// the scheduler depends on (and that the schedule form's UI hint advertises).
// daily cadence uses the daily prompt; weekly and monthly both reuse the
// weekly trend prompt since no monthly-specific prompt exists yet; unknown
// or empty frequencies must not return an empty mode, otherwise the worker
// would receive a blank PromptMode and the analyzer's buildPrompt back-compat
// path would silently render daily instead of surfacing the misconfiguration.
func TestAnalysisModeForFrequency(t *testing.T) {
	cases := []struct {
		freq string
		want string
	}{
		{"daily", AnalysisModeDaily},
		{"weekly", AnalysisModeWeekly},
		{"monthly", AnalysisModeWeekly},
		{"", AnalysisModeDaily},
		{"hourly-experimental", AnalysisModeDaily},
	}

	for _, tc := range cases {
		t.Run(tc.freq, func(t *testing.T) {
			got := AnalysisModeForFrequency(tc.freq)
			if got != tc.want {
				t.Errorf("AnalysisModeForFrequency(%q) = %q, want %q", tc.freq, got, tc.want)
			}
		})
	}
}

// TestIsValidAnalysisMode covers the validation contract used by the handler.
func TestIsValidAnalysisMode(t *testing.T) {
	valid := []string{AnalysisModeDaily, AnalysisModeWeekly, AnalysisModeIncident}
	for _, m := range valid {
		if !IsValidAnalysisMode(m) {
			t.Errorf("IsValidAnalysisMode(%q) = false, want true", m)
		}
	}
	invalid := []string{"", "monthly", "DAILY", "incident ", "weekly-trend"}
	for _, m := range invalid {
		if IsValidAnalysisMode(m) {
			t.Errorf("IsValidAnalysisMode(%q) = true, want false", m)
		}
	}
}
