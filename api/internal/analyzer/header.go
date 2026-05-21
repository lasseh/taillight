package analyzer

import (
	"fmt"
	"strings"
	"time"
)

// briefingTitle returns the long-form title used at the top of a rendered
// report. It mirrors the frontend briefingTitle() label so an operator
// reading the markdown directly (curl, PDF export, copy-paste) sees the
// same heading the UI shows.
func briefingTitle(mode string) string {
	switch mode {
	case modeDaily:
		return "Daily Operations Briefing"
	case modeWeekly:
		return "Weekly Operations Briefing"
	case modeIncident:
		return "Incident Briefing"
	default:
		return "Operations Briefing"
	}
}

// renderReportHeader builds the title + period block prepended to the
// model's reply. Format:
//
//	# Daily Operations Briefing — 2026-05-20 → 2026-05-21
//	_Period: 2026-05-20 19:54 UTC – 2026-05-21 19:54 UTC_
//
// We prepend in code rather than asking the model to render the title:
//
//   - dates are deterministic, so the model would just be wasting tokens
//     restating period_start / period_end;
//   - a fixed format means the H1 + period sub-line are byte-identical
//     across reports, which the frontend renders cleanly via the same
//     marked → DOMPurify path it already uses;
//   - the H2 structure validator only inspects `## ` headers so an H1
//     above `## TL;DR` does not interfere.
func renderReportHeader(mode string, periodStart, periodEnd time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — %s → %s\n",
		briefingTitle(mode),
		periodStart.UTC().Format("2006-01-02"),
		periodEnd.UTC().Format("2006-01-02"),
	)
	fmt.Fprintf(&b, "_Period: %s – %s_\n\n",
		periodStart.UTC().Format("2006-01-02 15:04 UTC"),
		periodEnd.UTC().Format("2006-01-02 15:04 UTC"),
	)
	return b.String()
}

// prependReportHeader returns the model's reply with the briefing header
// inserted at the top. A leading newline in the reply is collapsed so the
// header sits flush against the rest of the report.
func prependReportHeader(report, mode string, periodStart, periodEnd time.Time) string {
	return renderReportHeader(mode, periodStart, periodEnd) + strings.TrimLeft(report, "\n")
}
