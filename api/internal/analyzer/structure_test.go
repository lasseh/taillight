package analyzer

import (
	"strings"
	"testing"
)

func TestExtractH2Headers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "well-formed daily report",
			in: "## TL;DR\n> **Status: WATCH** — foo\n\n" +
				"## Top Incidents\nstuff\n\n" +
				"## Anomalies\n_Nothing of concern this period._\n\n" +
				"## Correlations\n- 12:34 UTC — x\n\n" +
				"## Action Queue\n1. do thing\n",
			want: []string{"TL;DR", "Top Incidents", "Anomalies", "Correlations", "Action Queue"},
		},
		{
			name: "ignores h3 and deeper",
			in:   "## TL;DR\n### sub\n#### deeper\n## Top Incidents\n",
			want: []string{"TL;DR", "Top Incidents"},
		},
		{
			name: "ignores headers inside fenced code blocks",
			in:   "## TL;DR\n```\n## fake header in fence\n```\n## Top Incidents\n",
			want: []string{"TL;DR", "Top Incidents"},
		},
		{
			name: "tolerates leading whitespace",
			in:   "  ## TL;DR\n## Top Incidents\n",
			want: []string{"TL;DR", "Top Incidents"},
		},
		{
			name: "empty input",
			in:   "",
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractH2Headers(tc.in)
			if !equalSlices(got, tc.want) {
				t.Errorf("extractH2Headers = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidateStructure(t *testing.T) {
	t.Parallel()

	daily := requiredHeaders[modeDaily]
	if len(daily) == 0 {
		t.Fatal("requiredHeaders[modeDaily] is empty — test cannot run")
	}
	body := func(headers ...string) string {
		var b strings.Builder
		for _, h := range headers {
			b.WriteString("## ")
			b.WriteString(h)
			b.WriteString("\nbody\n\n")
		}
		return b.String()
	}

	tests := []struct {
		name      string
		report    string
		required  []string
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "well-formed daily passes",
			report:   body("TL;DR", "Top Incidents", "Anomalies", "Correlations", "Action Queue"),
			required: daily,
			wantErr:  false,
		},
		{
			name:      "missing section fails",
			report:    body("TL;DR", "Top Incidents", "Anomalies", "Action Queue"),
			required:  daily,
			wantErr:   true,
			errSubstr: "expected 5 H2 sections",
		},
		{
			name:      "reordered section fails",
			report:    body("TL;DR", "Anomalies", "Top Incidents", "Correlations", "Action Queue"),
			required:  daily,
			wantErr:   true,
			errSubstr: "section 2",
		},
		{
			name: "extra appendix fails",
			report: body("TL;DR", "Top Incidents", "Anomalies", "Correlations", "Action Queue",
				"Appendix A"),
			required:  daily,
			wantErr:   true,
			errSubstr: "expected 5 H2 sections",
		},
		{
			name:      "renamed section fails",
			report:    body("Summary", "Top Incidents", "Anomalies", "Correlations", "Action Queue"),
			required:  daily,
			wantErr:   true,
			errSubstr: "section 1",
		},
		{
			name:     "trailing colon tolerated",
			report:   body("TL;DR:", "Top Incidents", "Anomalies", "Correlations", "Action Queue"),
			required: daily,
			wantErr:  false,
		},
		{
			name:      "no headers at all",
			report:    "Status: watch. Just prose, no structure.",
			required:  daily,
			wantErr:   true,
			errSubstr: "no H2 sections found",
		},
		{
			name:     "weekly headers pass on weekly required list",
			report:   body("TL;DR", "Trend Movers", "Chronic Hosts", "New Surface Area", "Correlations Worth Naming", "Engineering Focus"),
			required: requiredHeaders[modeWeekly],
			wantErr:  false,
		},
		{
			name:     "incident headers pass on incident required list",
			report:   body("Verdict", "What's Happening", "Likely Cause", "Immediate Actions", "Standing Down"),
			required: requiredHeaders[modeIncident],
			wantErr:  false,
		},
		{
			name:     "empty required disables check",
			report:   "anything goes",
			required: nil,
			wantErr:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateStructure(tc.report, tc.required)
			if tc.wantErr && err == nil {
				t.Fatalf("validateStructure returned nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateStructure returned %v, want nil", err)
			}
			if tc.wantErr && tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.errSubstr)
			}
		})
	}
}

func TestRequiredHeadersMatchPrompts(t *testing.T) {
	t.Parallel()
	// Belt-and-braces: every prompt mode the analyzer accepts must have a
	// requiredHeaders entry, otherwise the validator silently no-ops for
	// that mode and structural drift goes unnoticed.
	for mode := range validModes {
		if _, ok := requiredHeaders[mode]; !ok {
			t.Errorf("requiredHeaders missing entry for mode %q", mode)
		}
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
