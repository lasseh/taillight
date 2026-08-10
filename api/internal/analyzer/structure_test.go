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
				"## Needs Action\nstuff\n\n" +
				"## What Happened\n- 12:34 — x\n\n" +
				"## Watch\n_Nothing of concern this period._\n\n" +
				"*Baseline: sev≤3 12/day vs 7-day 10/day (+20%)*\n",
			want: []string{"TL;DR", "Needs Action", "What Happened", "Watch"},
		},
		{
			name: "ignores h3 and deeper",
			in:   "## TL;DR\n### sub\n#### deeper\n## Needs Action\n",
			want: []string{"TL;DR", "Needs Action"},
		},
		{
			name: "ignores headers inside fenced code blocks",
			in:   "## TL;DR\n```\n## fake header in fence\n```\n## Needs Action\n",
			want: []string{"TL;DR", "Needs Action"},
		},
		{
			name: "tolerates leading whitespace",
			in:   "  ## TL;DR\n## Needs Action\n",
			want: []string{"TL;DR", "Needs Action"},
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
			report:   body("TL;DR", "Needs Action", "What Happened", "Watch"),
			required: daily,
			wantErr:  false,
		},
		{
			name:      "missing section fails",
			report:    body("TL;DR", "Needs Action", "Watch"),
			required:  daily,
			wantErr:   true,
			errSubstr: "expected 4 H2 sections",
		},
		{
			name:      "reordered section fails",
			report:    body("TL;DR", "What Happened", "Needs Action", "Watch"),
			required:  daily,
			wantErr:   true,
			errSubstr: "section 2",
		},
		{
			name: "extra appendix fails",
			report: body("TL;DR", "Needs Action", "What Happened", "Watch",
				"Appendix A"),
			required:  daily,
			wantErr:   true,
			errSubstr: "expected 4 H2 sections",
		},
		{
			name:      "renamed section fails",
			report:    body("Summary", "Needs Action", "What Happened", "Watch"),
			required:  daily,
			wantErr:   true,
			errSubstr: "section 1",
		},
		{
			name:     "trailing colon tolerated",
			report:   body("TL;DR:", "Needs Action", "What Happened", "Watch"),
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
		"## Needs Action\n- one\n- two\n\n" +
		"## Watch\n_Nothing of concern this period._\n"

	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"tldr body", "TL;DR", "> **Status: WATCH** — bgp churn\n"},
		{"needs action body", "Needs Action", "- one\n- two\n"},
		{"trailing section to end", "Watch", "_Nothing of concern this period._\n"},
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

// dailyReportWithTLDR renders a 4-section daily report whose TL;DR body is
// supplied by the caller — used to isolate the first-section validator from
// the header-set validator. The trailing italic baseline footer mirrors what
// the prompt mandates, so these cases also prove the footer doesn't confuse
// header extraction.
func dailyReportWithTLDR(t *testing.T, tldr string) string {
	t.Helper()
	return "## TL;DR\n" + tldr + "\n\n" +
		"## Needs Action\n- foo\n\n" +
		"## What Happened\n_Nothing of concern this period._\n\n" +
		"## Watch\n_Nothing of concern this period._\n\n" +
		"*Baseline: sev≤3 12/day vs 7-day 10/day (+20%) · top error host `edge1-syd` (9 errors)*\n"
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

// TestValidateReportLength proves the daily line cap rejects a bloated reply
// (so the corrective retry regenerates it shorter), while blank lines stay
// free and uncapped modes (weekly) accept arbitrarily long reports.
func TestValidateReportLength(t *testing.T) {
	t.Parallel()

	pad := func(n int) string {
		return strings.Repeat("- padding bullet\n", n)
	}

	dailyCap := reportLineCap[modeDaily]
	if dailyCap == 0 {
		t.Fatal("reportLineCap[modeDaily] is unset — test cannot run")
	}

	tests := []struct {
		name      string
		mode      string
		report    string
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "compact daily passes",
			mode:    modeDaily,
			report:  dailyReportWithTLDR(t, "> **Status: NOMINAL** — quiet day."),
			wantErr: false,
		},
		{
			name: "over-cap daily fails",
			mode: modeDaily,
			report: "## TL;DR\n> **Status: WATCH** — noisy day.\n\n" +
				"## Needs Action\n" + pad(20) + "\n" +
				"## What Happened\n" + pad(30) + "\n" +
				"## Watch\n" + pad(20) + "\n",
			wantErr:   true,
			errSubstr: "70-line cap",
		},
		{
			name: "blank lines do not count toward the cap",
			mode: modeDaily,
			report: "## TL;DR\n> **Status: NOMINAL** — quiet.\n" + strings.Repeat("\n", 100) +
				"## Needs Action\n- foo\n\n" +
				"## What Happened\n- bar\n\n" +
				"## Watch\n- baz\n",
			wantErr: false,
		},
		{
			name: "weekly is uncapped",
			mode: modeWeekly,
			report: "## TL;DR\n> **Trend: STEADY** — typical week.\n\n" +
				"## Trend Movers\n" + pad(40) + "\n" +
				"## Chronic Hosts\n" + pad(40) + "\n" +
				"## New Surface Area\n- x\n\n" +
				"## Correlations Worth Naming\n- y\n\n" +
				"## Engineering Focus\n1. z\n",
			wantErr: false,
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
