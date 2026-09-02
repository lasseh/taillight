package model

import "testing"

func TestAnalysisFeedRules(t *testing.T) {
	if !IsValidAnalysisFeed(AnalysisFeedApplog) {
		t.Error("applog should be a valid feed")
	}
	if IsValidAnalysisFeed("all") {
		t.Error("the combined all feed was removed")
	}
	modes := []struct {
		feed, mode string
		want       bool
	}{
		{AnalysisFeedApplog, AnalysisModeDaily, true},
		{AnalysisFeedApplog, AnalysisModeWeekly, false},
		{AnalysisFeedApplog, AnalysisModeIncident, false},
		{AnalysisFeedNetlog, AnalysisModeWeekly, true},
		{AnalysisFeedNetlog, "bogus", false},
	}
	for _, tc := range modes {
		if got := IsValidAnalysisModeForFeed(tc.feed, tc.mode); got != tc.want {
			t.Errorf("IsValidAnalysisModeForFeed(%s, %s) = %v, want %v", tc.feed, tc.mode, got, tc.want)
		}
	}
	freqs := []struct {
		feed, freq string
		want       bool
	}{
		{AnalysisFeedApplog, "daily", true},
		{AnalysisFeedApplog, "weekly", false},
		{AnalysisFeedApplog, "monthly", false},
		{AnalysisFeedSrvlog, "monthly", true},
	}
	for _, tc := range freqs {
		if got := IsValidAnalysisFrequencyForFeed(tc.feed, tc.freq); got != tc.want {
			t.Errorf("IsValidAnalysisFrequencyForFeed(%s, %s) = %v, want %v", tc.feed, tc.freq, got, tc.want)
		}
	}
	if scope := (AnalysisScope{Feed: AnalysisFeedApplog, Services: []string{"api"}}); scope.IsAllServices() || !scope.IsAllHosts() {
		t.Error("service-scoped applog scope misreported")
	}
	if got := AnalysisFrequenciesForFeed(AnalysisFeedApplog); len(got) != 1 || got[0] != "daily" {
		t.Errorf("AnalysisFrequenciesForFeed(applog) = %v, want [daily]", got)
	}
	if spec, ok := AnalysisFeedSpecFor(AnalysisFeedApplog); !ok || spec.ScopeKind != AnalysisScopeServices || spec.PromptFamily != "applog" {
		t.Errorf("applog spec = %+v, want services scope and applog prompt family", spec)
	}
	if _, ok := AnalysisFeedSpecFor("all"); ok {
		t.Error("the removed all feed has a spec")
	}
}
