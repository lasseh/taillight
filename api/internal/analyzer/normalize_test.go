package analyzer

import (
	"strings"
	"testing"
)

// TestNormalizeReportMarkdown covers the shapes the models actually emit plus
// the markdown constructs the transform must not disturb.
func TestNormalizeReportMarkdown(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			// The observed shape: findings on bare consecutive lines with the
			// action folded into the same line. Each must become its own block.
			name: "one-line findings are separated",
			in: "## Needs Action\n" +
				"**[CRIT]** `BRCM_SALM` on `00a-leaf-d6e24-13` — 694 events — Action: check FPC0.\n" +
				"**[CRIT]** `EVO_PFEMAND` on `00a-hs-leaf-d6e32-02` — 92 events — Action: check next-hop.\n",
			want: "## Needs Action\n" +
				"**[CRIT]** `BRCM_SALM` on `00a-leaf-d6e24-13` — 694 events — Action: check FPC0.\n" +
				"\n" +
				"**[CRIT]** `EVO_PFEMAND` on `00a-hs-leaf-d6e32-02` — 92 events — Action: check next-hop.\n",
		},
		{
			// The shape the prompt asks for: the action on its own line stays
			// with its finding, joined by a hard break instead of a blank line.
			name: "two-line findings glue the action with a hard break",
			in: "## Needs Action\n" +
				"**[CRIT]** `BRCM_SALM` on `00a-leaf-d6e24-13` — 694 events.\n" +
				"Action: check FPC0 hardware.\n" +
				"**[WARN]** `RPD_OSPF_NBRDOWN` on `00a-core-3` — 226 events.\n" +
				"Action: check the QSFP alarms.\n",
			want: "## Needs Action\n" +
				"**[CRIT]** `BRCM_SALM` on `00a-leaf-d6e24-13` — 694 events.  \n" +
				"Action: check FPC0 hardware.\n" +
				"\n" +
				"**[WARN]** `RPD_OSPF_NBRDOWN` on `00a-core-3` — 226 events.  \n" +
				"Action: check the QSFP alarms.\n",
		},
		{
			name: "bolded action label is recognised",
			in: "## Needs Action\n" +
				"**[CRIT]** `BRCM_SALM` on `00a-leaf-d6e24-13`.\n" +
				"**Action:** check FPC0.\n",
			want: "## Needs Action\n" +
				"**[CRIT]** `BRCM_SALM` on `00a-leaf-d6e24-13`.  \n" +
				"**Action:** check FPC0.\n",
		},
		{
			// A tight list already renders as separate <li> blocks, and in
			// CommonMark a bare line after a list item is a lazy continuation
			// of it — separating would change the meaning.
			name: "existing list items are left alone",
			in: "## What Happened\n" +
				"- `BRCM_SALM` on `00a-leaf-d6e24-13` (694 events).\n" +
				"- `RPD_OSPF_NBRDOWN` on `00a-core-3` (226 events).\n" +
				"1. ordered item\n" +
				"2. another ordered item\n",
			want: "## What Happened\n" +
				"- `BRCM_SALM` on `00a-leaf-d6e24-13` (694 events).\n" +
				"- `RPD_OSPF_NBRDOWN` on `00a-core-3` (226 events).\n" +
				"1. ordered item\n" +
				"2. another ordered item\n",
		},
		{
			name: "fenced code is untouched",
			in: "## Watch\n" +
				"first line\n" +
				"```\n" +
				"show chassis fpc 0\n" +
				"show chassis environment\n" +
				"```\n" +
				"second line\n",
			want: "## Watch\n" +
				"first line\n" +
				"```\n" +
				"show chassis fpc 0\n" +
				"show chassis environment\n" +
				"```\n" +
				"second line\n",
		},
		{
			name: "table rows and blockquotes are untouched",
			in: "## Correlations\n" +
				"| Signature | Count |\n" +
				"|---|---|\n" +
				"| cpu_threshold | 37 |\n" +
				"> quoted status line\n",
			want: "## Correlations\n" +
				"| Signature | Count |\n" +
				"|---|---|\n" +
				"| cpu_threshold | 37 |\n" +
				"> quoted status line\n",
		},
		{
			name: "indented continuations stay with their parent",
			in: "## What Happened\n" +
				"- parent item\n" +
				"  continuation of the parent\n" +
				"next finding\n",
			want: "## What Happened\n" +
				"- parent item\n" +
				"  continuation of the parent\n" +
				"next finding\n",
		},
		{
			name: "headings reset separation and blank lines are preserved",
			in: "## TL;DR\n" +
				"**Status: ACT NOW** — critical routing failures.\n" +
				"\n" +
				"## Watch\n" +
				"err (sev 3) +29.1% vs baseline.\n" +
				"New `fpc<n> I<n>C Failed device` on `00a-core-3`.\n",
			want: "## TL;DR\n" +
				"**Status: ACT NOW** — critical routing failures.\n" +
				"\n" +
				"## Watch\n" +
				"err (sev 3) +29.1% vs baseline.\n" +
				"\n" +
				"New `fpc<n> I<n>C Failed device` on `00a-core-3`.\n",
		},
		{
			name: "content before the first section header is passed through",
			in:   "stray preamble line\nanother preamble line\n## TL;DR\n**Status: NOMINAL** — quiet.\n",
			want: "stray preamble line\nanother preamble line\n## TL;DR\n**Status: NOMINAL** — quiet.\n",
		},
		{
			name: "placeholder-only sections are unchanged",
			in: "## Needs Action\n" +
				"_Nothing of concern this period._\n" +
				"\n" +
				"## Watch\n" +
				"_Nothing of concern this period._\n",
			want: "## Needs Action\n" +
				"_Nothing of concern this period._\n" +
				"\n" +
				"## Watch\n" +
				"_Nothing of concern this period._\n",
		},
		{
			name: "empty input is unchanged",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeReportMarkdown(tt.in)
			if got != tt.want {
				t.Errorf("normalizeReportMarkdown()\ngot:\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

// TestNormalizeReportMarkdownIdempotent guards the property that matters when a
// report is re-processed: applying the transform to its own output must be a
// no-op, or repeated runs would keep inserting blank lines and stacking
// trailing spaces.
func TestNormalizeReportMarkdownIdempotent(t *testing.T) {
	in := "## TL;DR\n" +
		"**Status: ACT NOW** — critical routing failures.\n" +
		"\n" +
		"## Needs Action\n" +
		"**[CRIT]** `BRCM_SALM` on `00a-leaf-d6e24-13` — 694 events.\n" +
		"Action: check FPC0 hardware.\n" +
		"**[CRIT]** `EVO_PFEMAND` on `00a-hs-leaf-d6e32-02` — 92 events — Action: check next-hop.\n" +
		"\n" +
		"## What Happened\n" +
		"00:00–03:00 — 5+ hosts fired `qsfp Rx power low alarm set`.\n" +
		"`RPD_OSPF_NBRDOWN` on `00a-core-3` (226 events) — link flaps.\n"

	once := normalizeReportMarkdown(in)
	twice := normalizeReportMarkdown(once)
	if once != twice {
		t.Errorf("not idempotent\nonce:\n%q\ntwice:\n%q", once, twice)
	}
}

// TestNormalizeReportMarkdownEmptyDataBody covers the short-circuit body the
// analyzer emits when a window has no events. It never reaches the normalizer
// today, but it must survive the transform unchanged if that ever changes.
func TestNormalizeReportMarkdownEmptyDataBody(t *testing.T) {
	for _, body := range []string{
		"_No events recorded on this feed during this window._\n",
		"_No events recorded for the scoped host(s) during this window._\n",
	} {
		if got := normalizeReportMarkdown(body); got != body {
			t.Errorf("normalizeReportMarkdown(%q) = %q, want unchanged", body, got)
		}
	}
}

// TestNormalizeReportMarkdownNoTrailingSpaceStacking pins the hardBreak guard:
// a finding line that already ends in a hard break must not accumulate more.
func TestNormalizeReportMarkdownNoTrailingSpaceStacking(t *testing.T) {
	in := "## Needs Action\n" +
		"**[CRIT]** `BRCM_SALM` on `00a-leaf-d6e24-13`.  \n" +
		"Action: check FPC0.\n"

	got := normalizeReportMarkdown(in)
	if strings.Contains(got, "   \n") {
		t.Errorf("hard break stacked extra spaces: %q", got)
	}
	if got != in {
		t.Errorf("normalizeReportMarkdown() = %q, want unchanged %q", got, in)
	}
}
