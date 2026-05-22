package model

import (
	"reflect"
	"testing"
)

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

// TestNormalizeHosts pins the canonicalization behavior that the active-report
// uniqueness constraint depends on. The properties matter:
//   - same logical host set → same []string output, regardless of caller order
//   - empty / whitespace-only inputs → nil (so the "no scope" sentinel is one
//     value, not two)
//   - sorted output so the resulting text[] is byte-identical across requests
func TestNormalizeHosts(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, nil},
		{"empty", []string{}, nil},
		{"whitespace only", []string{"  ", "\t", ""}, nil},
		{"single", []string{"edge01"}, []string{"edge01"}},
		{"already sorted", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"out of order", []string{"c", "a", "b"}, []string{"a", "b", "c"}},
		{"dedup", []string{"a", "b", "a"}, []string{"a", "b"}},
		{"trim then dedup", []string{" a", "a", "  a  "}, []string{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeHosts(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("NormalizeHosts(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestAnalysisScopeIsAllHosts ensures the helper treats nil and empty
// identically — both mean "no host filter".
func TestAnalysisScopeIsAllHosts(t *testing.T) {
	if !(AnalysisScope{}).IsAllHosts() {
		t.Errorf("zero AnalysisScope should report IsAllHosts() = true")
	}
	if !(AnalysisScope{Hosts: nil}).IsAllHosts() {
		t.Errorf("nil Hosts should report IsAllHosts() = true")
	}
	if !(AnalysisScope{Hosts: []string{}}).IsAllHosts() {
		t.Errorf("empty Hosts should report IsAllHosts() = true")
	}
	if (AnalysisScope{Hosts: []string{"a"}}).IsAllHosts() {
		t.Errorf("non-empty Hosts should report IsAllHosts() = false")
	}
}
