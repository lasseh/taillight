package analyzer

import (
	"fmt"
	"strings"
)

// requiredHeaders enumerates the H2 section headers each prompt mode must
// emit, in order. The validator forbids extra headers as well, so the model
// can't pad with "Appendix" or "Recommendations" sections.
var requiredHeaders = map[string][]string{
	modeDaily:    {"TL;DR", "Top Incidents", "Anomalies", "Correlations", "Action Queue"},
	modeWeekly:   {"TL;DR", "Trend Movers", "Chronic Hosts", "New Surface Area", "Correlations Worth Naming", "Engineering Focus"},
	modeIncident: {"Verdict", "What's Happening", "Likely Cause", "Immediate Actions", "Standing Down"},
}

// extractH2Headers returns the H2 ("## ") header titles in the order they
// appear in s. Headers inside fenced code blocks (``` … ```) are ignored
// so the literal skeleton we ship in the system prompt isn't confused with
// the model's own output if it ever gets echoed back.
func extractH2Headers(s string) []string {
	var headers []string
	inFence := false
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		// Match "## title" but not "### title" or "#### …".
		if strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "### ") {
			headers = append(headers, strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
		}
	}
	return headers
}

// normalizeHeader trims trailing punctuation and collapses whitespace so a
// minor stylistic divergence ("## TL;DR:") doesn't trigger a retry. Case is
// preserved otherwise — comparisons use strings.EqualFold.
func normalizeHeader(s string) string {
	s = strings.TrimRight(s, " \t.:")
	return strings.Join(strings.Fields(s), " ")
}

// structureCorrection composes the corrective user message sent to the model
// after a structure-validation failure. It names the specific deviation,
// re-states the required header sequence, and forbids the usual drift modes
// ("Appendix", "Recommendations", etc.).
func structureCorrection(cause error, required []string) string {
	var b strings.Builder
	b.WriteString("Your previous reply did not match the required section structure: ")
	b.WriteString(cause.Error())
	b.WriteString(".\n\nReply again from scratch. Start with `## ")
	b.WriteString(required[0])
	b.WriteString("` exactly — no title, no preamble. Use exactly these H2 headers, in this order, and no others:\n\n")
	for _, h := range required {
		b.WriteString("- ## ")
		b.WriteString(h)
		b.WriteString("\n")
	}
	b.WriteString("\nDo not add `Key Findings`, `Summary`, `Recommendations`, `Next Steps`, `Conclusion`, `Appendix`, or any other heading. If a section has nothing meaningful, fill it with a single short italic line as the system message describes; never leave a section empty.")
	return b.String()
}

// validateStructure checks that report contains exactly the required H2
// headers, in the required order, with no extras. Returns a human-readable
// error describing the first deviation, or nil if the structure is correct.
// Required is the slice returned by requiredHeaders[mode]; passing nil or an
// empty slice disables the check.
func validateStructure(report string, required []string) error {
	if len(required) == 0 {
		return nil
	}
	got := extractH2Headers(report)
	if len(got) == 0 {
		return fmt.Errorf("no H2 sections found; reply must start with `## %s`", required[0])
	}
	if len(got) != len(required) {
		return fmt.Errorf("expected %d H2 sections %v, got %d %v", len(required), required, len(got), got)
	}
	for i := range required {
		if !strings.EqualFold(normalizeHeader(got[i]), normalizeHeader(required[i])) {
			return fmt.Errorf("section %d: expected `## %s`, got `## %s`", i+1, required[i], got[i])
		}
	}
	return nil
}
