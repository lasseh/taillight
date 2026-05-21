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
		if _, ok := firstSectionRule[mode]; !ok {
			t.Errorf("firstSectionRule missing entry for mode %q", mode)
		}
	}
}

func TestExtractSection(t *testing.T) {
	t.Parallel()

	report := "## TL;DR\n> **Status: WATCH** — bgp churn\n\n" +
		"## Top Incidents\n- one\n- two\n\n" +
		"## Anomalies\n_Nothing of concern this period._\n"

	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"tldr body", "TL;DR", "> **Status: WATCH** — bgp churn\n"},
		{"top incidents body", "Top Incidents", "- one\n- two\n"},
		{"trailing section to end", "Anomalies", "_Nothing of concern this period._\n"},
		{"missing section", "Nonexistent", ""},
		{"punctuation-tolerant header lookup", "TL;DR:", "> **Status: WATCH** — bgp churn\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractSection(report, tc.header)
			if got != tc.want {
				t.Errorf("extractSection(%q) = %q, want %q", tc.header, got, tc.want)
			}
		})
	}
}

// validBody renders a 5-section daily report whose TL;DR body is supplied
// by the caller — used to isolate the first-section validator from the
// header-set validator.
func dailyReportWithTLDR(t *testing.T, tldr string) string {
	t.Helper()
	return "## TL;DR\n" + tldr + "\n\n" +
		"## Top Incidents\n- foo\n\n" +
		"## Anomalies\n_Nothing of concern this period._\n\n" +
		"## Correlations\n_Nothing of concern this period._\n\n" +
		"## Action Queue\n_Nothing of concern this period._\n"
}

func TestValidateReportFirstSection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mode      string
		report    string
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "daily nominal passes",
			mode:    modeDaily,
			report:  dailyReportWithTLDR(t, "> **Status: NOMINAL** — quiet day."),
			wantErr: false,
		},
		{
			name:    "daily watch passes",
			mode:    modeDaily,
			report:  dailyReportWithTLDR(t, "> **Status: WATCH** — bgp churn."),
			wantErr: false,
		},
		{
			name:    "daily act now passes",
			mode:    modeDaily,
			report:  dailyReportWithTLDR(t, "> **Status: ACT NOW** — psu fail."),
			wantErr: false,
		},
		{
			name:      "daily bare placeholder fails",
			mode:      modeDaily,
			report:    dailyReportWithTLDR(t, "_Nothing of concern this period._"),
			wantErr:   true,
			errSubstr: "missing `**Status:",
		},
		{
			name:      "daily lowercase status fails",
			mode:      modeDaily,
			report:    dailyReportWithTLDR(t, "> **Status: nominal** — quiet."),
			wantErr:   true,
			errSubstr: "missing `**Status:",
		},
		{
			name:      "daily unknown status word fails",
			mode:      modeDaily,
			report:    dailyReportWithTLDR(t, "> **Status: FINE** — quiet."),
			wantErr:   true,
			errSubstr: "missing `**Status:",
		},
		{
			name: "weekly trend passes",
			mode: modeWeekly,
			report: "## TL;DR\n> **Trend: STEADY** — typical week.\n\n" +
				"## Trend Movers\n_Nothing notable this period._\n\n" +
				"## Chronic Hosts\n_Nothing notable this period._\n\n" +
				"## New Surface Area\n_Nothing notable this period._\n\n" +
				"## Correlations Worth Naming\n_Nothing notable this period._\n\n" +
				"## Engineering Focus\n_Nothing notable this period._\n",
			wantErr: false,
		},
		{
			name: "weekly bare placeholder fails",
			mode: modeWeekly,
			report: "## TL;DR\n_Nothing notable this period._\n\n" +
				"## Trend Movers\n_Nothing notable this period._\n\n" +
				"## Chronic Hosts\n_Nothing notable this period._\n\n" +
				"## New Surface Area\n_Nothing notable this period._\n\n" +
				"## Correlations Worth Naming\n_Nothing notable this period._\n\n" +
				"## Engineering Focus\n_Nothing notable this period._\n",
			wantErr:   true,
			errSubstr: "missing `**Trend:",
		},
		{
			name: "incident stand down passes",
			mode: modeIncident,
			report: "## Verdict\n> **STAND DOWN** — false alarm.\n\n" +
				"## What's Happening\n_No active anomaly visible in this window._\n\n" +
				"## Likely Cause\nbaseline noise\n\n" +
				"## Immediate Actions\n1. close ticket\n\n" +
				"## Standing Down\n_Verdict is STAND DOWN — no further action._\n",
			wantErr: false,
		},
		{
			name: "incident escalate passes",
			mode: modeIncident,
			report: "## Verdict\n> **ESCALATE** — multi-host fault.\n\n" +
				"## What's Happening\nN hosts firing\n\n" +
				"## Likely Cause\nshared upstream\n\n" +
				"## Immediate Actions\n1. page tier-2\n\n" +
				"## Standing Down\n_Verdict is ESCALATE — do not stand down without next-tier sign-off._\n",
			wantErr: false,
		},
		{
			name: "incident bare placeholder fails",
			mode: modeIncident,
			report: "## Verdict\n_No active anomaly visible in this window._\n\n" +
				"## What's Happening\n_No active anomaly visible in this window._\n\n" +
				"## Likely Cause\n_No active anomaly visible in this window._\n\n" +
				"## Immediate Actions\n_No active anomaly visible in this window._\n\n" +
				"## Standing Down\n_No active anomaly visible in this window._\n",
			wantErr:   true,
			errSubstr: "missing `**STAND DOWN",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateReport(tc.report, tc.mode)
			if tc.wantErr && err == nil {
				t.Fatalf("validateReport returned nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateReport returned %v, want nil", err)
			}
			if tc.wantErr && tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.errSubstr)
			}
		})
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
