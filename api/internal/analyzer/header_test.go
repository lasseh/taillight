package analyzer

import (
	"strings"
	"testing"
	"time"
)

func TestBriefingTitle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		mode string
		want string
	}{
		{modeDaily, "Daily Operations Briefing"},
		{modeWeekly, "Weekly Operations Briefing"},
		{modeIncident, "Incident Briefing"},
		{"", "Operations Briefing"},
		{"unknown-mode", "Operations Briefing"},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			if got := briefingTitle(tc.mode); got != tc.want {
				t.Errorf("briefingTitle(%q) = %q, want %q", tc.mode, got, tc.want)
			}
		})
	}
}

// TestRenderReportHeader pins the exact byte layout — frontend rendering and
// printed PDFs both inherit this format, so a typo here surfaces immediately.
func TestRenderReportHeader(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 5, 20, 19, 54, 0, 0, time.UTC)
	end := time.Date(2026, 5, 21, 19, 54, 0, 0, time.UTC)
	got := renderReportHeader(modeDaily, start, end)
	want := "# Daily Operations Briefing — 2026-05-20 → 2026-05-21\n" +
		"_Period: 2026-05-20 19:54 UTC – 2026-05-21 19:54 UTC_\n\n"
	if got != want {
		t.Errorf("renderReportHeader =\n%q\nwant\n%q", got, want)
	}
}

// TestRenderReportHeaderUsesUTC ensures the formatter normalizes to UTC even
// when the caller passes a non-UTC time — otherwise a report run on a host
// with a local clock would render with a TZ offset in the heading.
func TestRenderReportHeaderUsesUTC(t *testing.T) {
	t.Parallel()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("America/New_York tzdata unavailable: %v", err)
	}
	start := time.Date(2026, 5, 20, 15, 54, 0, 0, loc) // 19:54 UTC
	end := time.Date(2026, 5, 21, 15, 54, 0, 0, loc)   // 19:54 UTC next day
	got := renderReportHeader(modeDaily, start, end)
	if !strings.Contains(got, "19:54 UTC") {
		t.Errorf("renderReportHeader did not normalize to UTC: %q", got)
	}
}

// TestPrependReportHeader makes sure the model reply is preserved verbatim
// after the header, with no extra blank lines stacking up between them.
func TestPrependReportHeader(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 5, 20, 19, 54, 0, 0, time.UTC)
	end := time.Date(2026, 5, 21, 19, 54, 0, 0, time.UTC)
	body := "\n\n## TL;DR\n**Status: NOMINAL** — quiet period."
	got := prependReportHeader(body, modeDaily, start, end)

	// Header lands first, body follows without preserving the leading
	// newlines from the model reply.
	if !strings.HasPrefix(got, "# Daily Operations Briefing — ") {
		t.Errorf("output does not start with H1 header: %q", got)
	}
	if !strings.Contains(got, "\n\n## TL;DR\n") {
		t.Errorf("body section not preserved: %q", got)
	}
	// The header ends with \n\n and the body had leading \n\n — they
	// should not stack into three or four blank lines.
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("stacked blank lines between header and body: %q", got)
	}
}
