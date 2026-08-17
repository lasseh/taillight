package analyzer

import (
	"regexp"
	"strings"
)

// listMarker matches the leading bullet or ordered-list marker of a list item.
var listMarker = regexp.MustCompile(`^([-*+]|\d{1,9}[.)])\s`)

// thematicBreak matches a markdown horizontal rule.
var thematicBreak = regexp.MustCompile(`^(-{3,}|\*{3,}|_{3,})$`)

// normalizeReportMarkdown gives every finding its own markdown block.
//
// The models we run emit one finding per line with no list marker and no blank
// line between them. Both renderers collapse that into a single block: goldmark
// (email + print) runs with WithHardWraps, so a whole section becomes one <p>
// with <br> between findings — the paragraph margin then fires once per
// *section* rather than once per finding. The frontend is worse: `marked`
// defaults to breaks:false, so the same lines join into one running paragraph
// with no break at all. Either way three separate [CRIT] items read as one wall
// of text.
//
// Separating the lines here rather than in a renderer means the stored markdown
// is well-formed once and all three surfaces — email, printed PDF, and the web
// report view — pick up the fix from a single implementation.
//
// Two shapes of the two-line "Needs Action" item are handled. When the model
// puts its action on a line of its own, that line stays glued to the finding
// above it and the finding gets a CommonMark hard break (two trailing spaces)
// so the action lands on its own line in every renderer regardless of the
// breaks/WithHardWraps setting. When the model instead folds the action into the
// finding line — which is what it does in practice — there is no continuation
// to glue and plain separation already yields the right blocks.
//
// Lines that already carry their own block semantics are left exactly as they
// are: fenced code, headings, table rows, blockquotes, thematic breaks, indented
// continuations, and existing list items. Leaving list items alone matters for
// more than tidiness — in CommonMark an unindented line after `- foo` is a lazy
// continuation of that item, so inserting a blank line there would change what
// the markdown means.
func normalizeReportMarkdown(report string) string {
	lines := strings.Split(strings.ReplaceAll(report, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines)*2)

	inFence := false
	seenSection := false
	// prevSeparable records whether the previously emitted line was one we
	// separate, which is the only case where the next one needs a blank line.
	prevSeparable := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(trimmed, "```"), strings.HasPrefix(trimmed, "~~~"):
			inFence = !inFence
			out = append(out, line)
			prevSeparable = false
			continue
		case inFence:
			out = append(out, line)
			continue
		case trimmed == "":
			out = append(out, line)
			prevSeparable = false
			continue
		case strings.HasPrefix(trimmed, "#"):
			// Section headers delimit the bodies we rewrite. Anything ahead of
			// the first one is not report prose and is passed through.
			seenSection = true
			out = append(out, line)
			prevSeparable = false
			continue
		}

		if !seenSection || !isSeparableLine(line, trimmed) {
			out = append(out, line)
			prevSeparable = false
			continue
		}

		if prevSeparable {
			if isActionContinuation(trimmed) {
				out[len(out)-1] = hardBreak(out[len(out)-1])
			} else {
				out = append(out, "")
			}
		}
		out = append(out, line)
		prevSeparable = true
	}

	return strings.Join(out, "\n")
}

// isSeparableLine reports whether line is ordinary report prose that should
// become its own block. Callers have already ruled out blank lines, headings,
// and fenced code.
func isSeparableLine(line, trimmed string) bool {
	switch {
	case listMarker.MatchString(trimmed):
		return false
	case thematicBreak.MatchString(trimmed):
		return false
	case strings.HasPrefix(trimmed, "|"), strings.HasPrefix(trimmed, ">"):
		return false
	case strings.HasPrefix(line, "  "), strings.HasPrefix(line, "\t"):
		// Indented: a continuation of whatever came before it.
		return false
	}
	return true
}

// isActionContinuation reports whether trimmed is the second line of the
// two-line Needs Action shape, which belongs with the finding above it rather
// than in a block of its own. Tolerates the model bolding the label.
func isActionContinuation(trimmed string) bool {
	s := strings.TrimLeft(trimmed, "*_")
	return strings.HasPrefix(strings.ToLower(s), "action:")
}

// hardBreak appends a CommonMark hard line break to s. Two trailing spaces are
// the one break marker both goldmark and the frontend's `marked` honour without
// depending on their respective hard-wrap settings.
func hardBreak(s string) string {
	if strings.HasSuffix(s, "  ") || strings.HasSuffix(s, `\`) {
		return s
	}
	return strings.TrimRight(s, " \t") + "  "
}
