package analyzer

import (
	"fmt"
	"regexp"
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

// firstSectionRule defines the regex each mode's first section (TL;DR or
// Verdict) body must match. The pattern targets the bolded status/trend/
// verdict token so the model can't get away with emitting only the bare
// placeholder line `_Nothing of concern this period._` in the headline
// section — a quiet period is still a Status: NOMINAL decision, not a
// non-answer.
//
// Patterns use case-sensitive matching for the status words because the
// prompts ask for them in UPPERCASE; if the model emits them in mixed case
// we want the validator to flag that so the corrective follow-up can fix
// it. (?m) so ^/$ match line ends inside the section body.
var firstSectionRule = map[string]struct {
	pattern *regexp.Regexp
	desc    string
}{
	modeDaily: {
		pattern: regexp.MustCompile(`\*\*Status:\s+(NOMINAL|WATCH|ACT NOW)\*\*`),
		desc:    "`**Status: NOMINAL|WATCH|ACT NOW**` line",
	},
	modeWeekly: {
		pattern: regexp.MustCompile(`\*\*Trend:\s+(IMPROVING|STEADY|DEGRADING|MIXED)\*\*`),
		desc:    "`**Trend: IMPROVING|STEADY|DEGRADING|MIXED**` line",
	},
	modeIncident: {
		pattern: regexp.MustCompile(`\*\*(STAND DOWN|INVESTIGATE|CONTAIN|ESCALATE)\*\*`),
		desc:    "`**STAND DOWN|INVESTIGATE|CONTAIN|ESCALATE**` token",
	},
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

// extractSection returns the body of the H2 section whose title matches
// header (case-insensitive, punctuation-tolerant). Returns the empty string
// if the section isn't found. The body runs from the line after the header
// up to (but not including) the next H2 line or end-of-input. Fenced code
// blocks inside the body are returned intact.
func extractSection(report, header string) string {
	want := normalizeHeader(header)
	lines := strings.Split(report, "\n")
	var body []string
	inFence := false
	capturing := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			if capturing {
				body = append(body, line)
			}
			continue
		}
		isH2 := !inFence && strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "### ")
		if isH2 {
			if capturing {
				// Hit the next section — stop.
				break
			}
			got := normalizeHeader(strings.TrimPrefix(trimmed, "## "))
			if strings.EqualFold(got, want) {
				capturing = true
				continue
			}
			continue
		}
		if capturing {
			body = append(body, line)
		}
	}
	return strings.Join(body, "\n")
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
	b.WriteString("Your previous reply did not match the required output rules: ")
	b.WriteString(cause.Error())
	b.WriteString(".\n\nReply again from scratch. Start with `## ")
	b.WriteString(required[0])
	b.WriteString("` exactly — no title, no preamble. Use exactly these H2 headers, in this order, and no others:\n\n")
	for _, h := range required {
		b.WriteString("- ## ")
		b.WriteString(h)
		b.WriteString("\n")
	}
	b.WriteString("\nDo not add `Key Findings`, `Summary`, `Recommendations`, `Next Steps`, `Conclusion`, `Appendix`, or any other heading. The first section must contain a bolded status/trend/verdict line as described in the system message — never just the placeholder.")
	return b.String()
}

// validateReport runs every output rule for the given mode and returns the
// first violation found, or nil if the report is well-formed. Order:
// headers (must be exact set + order) → first-section content (must contain
// the mode's status/trend/verdict token).
func validateReport(report, mode string) error {
	required := requiredHeaders[mode]
	if len(required) == 0 {
		return nil
	}
	if err := validateStructure(report, required); err != nil {
		return err
	}
	if err := validateFirstSection(report, mode, required[0]); err != nil {
		return err
	}
	return nil
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

// validateFirstSection checks that the body of the first section contains
// the mode's required status/trend/verdict token. A bare placeholder line
// (`_Nothing of concern this period._` or similar) is explicitly not enough
// for the headline section — the model must commit to a status word.
func validateFirstSection(report, mode, firstHeader string) error {
	rule, ok := firstSectionRule[mode]
	if !ok {
		return nil
	}
	body := strings.TrimSpace(extractSection(report, firstHeader))
	if body == "" {
		return fmt.Errorf("section `## %s` is empty; expected %s", firstHeader, rule.desc)
	}
	if !rule.pattern.MatchString(body) {
		return fmt.Errorf("section `## %s` is missing %s (got: %q)", firstHeader, rule.desc, truncateForError(body, 120))
	}
	return nil
}

// truncateForError returns s clipped to at most n runes, with an ellipsis
// when truncated. Used to keep validator error messages readable when the
// model's body is long.
func truncateForError(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
