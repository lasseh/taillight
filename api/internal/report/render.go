// Package report renders an analysis report into a complete, standalone HTML
// document. It is the single source of truth for report styling shared by two
// delivery paths: the notification email backend (VariantEmail) and the HTTP
// print endpoint the frontend prints to PDF (VariantPrint). Keeping both on one
// renderer means mail and the printed PDF read the same and never drift.
package report

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	ghtml "github.com/yuin/goldmark/renderer/html"

	"github.com/lasseh/taillight/internal/model"
)

// Variant selects the chrome wrapped around the shared report body.
type Variant int

const (
	// VariantEmail renders the email body: gray page, dark masthead bar, white
	// card, plus a dark palette for clients that honour prefers-color-scheme.
	VariantEmail Variant = iota
	// VariantPrint renders a paper-friendly document for browser print-to-PDF:
	// white page, ink-light masthead (browsers drop backgrounds when printing,
	// so a dark bar would vanish), and @page / page-break rules that let the
	// report flow across multiple A4 pages.
	VariantPrint
)

// RenderHTML returns a complete standalone HTML document for the report.
func RenderHTML(r *model.AnalysisReport, v Variant) string {
	if v == VariantPrint {
		return renderPrint(r)
	}
	return renderEmail(r)
}

// bodyCSS is the inline stylesheet for the rendered report body. Shared by both
// variants so mail and print share heading colors and inline-code treatment.
// Kept email-safe (no CSS variables, no flexbox, no oklch) so Gmail / Apple
// Mail render it cleanly; Outlook desktop degrades gracefully.
//
// Inline code carries no fill and no border on purpose. These reports name a
// hostname or a signature in nearly every clause, so a bordered chip per mention
// turns a paragraph into a mosaic of boxes — and a mail client that force-
// inverts the (light) email turns each of those boxes into a high-contrast
// rectangle. Monospace alone is enough to mark a token as literal, and a
// fill-less span has nothing for an inversion to make ugly. word-break keeps the
// long truncated Junos message templates from blowing out the line.
const bodyCSS = `
.taillight-report { color: #111827; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; font-size: 14px; line-height: 1.55; }
.taillight-report h1 { font-size: 20px; font-weight: 700; color: #111827; margin: 16px 0 12px; padding-bottom: 8px; border-bottom: 2px solid #d97706; }
.taillight-report h2 { font-size: 16px; font-weight: 600; color: #111827; margin: 24px 0 8px; padding-bottom: 4px; border-bottom: 1px solid #d1d5db; }
.taillight-report h3 { font-size: 14px; font-weight: 600; color: #1f2937; margin: 18px 0 6px; }
.taillight-report p { font-size: 13px; color: #111827; margin: 10px 0; }
.taillight-report li { font-size: 13px; color: #111827; margin: 6px 0; }
.taillight-report ul, .taillight-report ol { padding-left: 22px; }
.taillight-report em { color: #6b7280; font-style: italic; }
.taillight-report strong { color: #111827; font-weight: 600; }
.taillight-report code { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12px; color: #374151; word-break: break-word; }
.taillight-report pre { background: #f3f4f6; border: 1px solid #d1d5db; border-radius: 4px; padding: 10px 12px; overflow-x: auto; font-size: 12px; }
.taillight-report pre code { border: none; background: none; padding: 0; }
.taillight-report blockquote { border-left: 3px solid #6b7280; padding-left: 12px; color: #4b5563; margin: 10px 0; }
.taillight-report hr { border: none; border-top: 1px solid #d1d5db; margin: 18px 0; }
.taillight-report table { width: 100%; border-collapse: collapse; font-size: 12px; margin: 12px 0; border: 1px solid #9ca3af; }
.taillight-report th { background: #f3f4f6; color: #111827; font-weight: 600; text-align: left; padding: 6px 10px; border-bottom: 1px solid #9ca3af; }
.taillight-report td { padding: 5px 10px; border-bottom: 1px solid #e5e7eb; }
.taillight-report a { color: #1d4ed8; text-decoration: underline; }
.taillight-report details > summary { color: #1d4ed8; cursor: pointer; }
`

// printCSS adds page geometry and pagination control on top of bodyCSS for the
// print variant. Headings stay with the content that follows them; paragraphs
// and list items keep a 2-line minimum at page edges. Tables and <pre> are
// deliberately allowed to break across pages — forcing page-break-inside:avoid
// on a table taller than one page leaves an ugly full-page blank gap.
const printCSS = `
@media print {
  @page { size: A4; margin: 16mm 14mm 18mm 14mm; }
  html, body { background: #fff; }
  body { padding: 0; }
  .taillight-report h1, .taillight-report h2, .taillight-report h3 { page-break-after: avoid; }
  .taillight-report p, .taillight-report li { orphans: 2; widows: 2; }
  .taillight-report thead { display: table-header-group; }
  /* On screen long lines scroll (overflow-x:auto); on paper there is no
   * scrollbar, so wrap instead of clipping at the right margin. */
  .taillight-report pre { white-space: pre-wrap; word-wrap: break-word; }
  .taillight-report td { word-break: break-word; }
}
`

// emailDarkCSS is the dark palette for the email variant, and is deliberately
// not part of bodyCSS: the print variant must stay ink-on-paper.
//
// Without it a dark-mode client force-inverts the light design — which is how a
// #1f2937 masthead ends up rendering lighter than the card beneath it, and how
// every inline-code span becomes a high-contrast box. Declaring color-scheme
// support in the head plus shipping real dark styles is what stops that
// transform; clients that honour neither (Outlook, parts of Gmail) fall back to
// the light design, which is why the light design also has to survive inversion
// on its own.
//
// The wrapper elements carry inline backgrounds, so every override here needs
// !important to win. The class hooks exist purely for this block.
const emailDarkCSS = `
@media (prefers-color-scheme: dark) {
  .tl-page { background: #0f1216 !important; }
  .tl-card { background: #1a1e24 !important; box-shadow: none !important; }
  .tl-masthead { background: #272e37 !important; }
  .tl-footer { background: #151920 !important; color: #8b949e !important; }
  .tl-meta-label { color: #8b949e !important; }
  .tl-meta-value, .tl-openin { color: #e6edf3 !important; }
  .tl-openin { border-top-color: #30363d !important; }
  .taillight-report, .taillight-report p, .taillight-report li, .taillight-report strong { color: #e6edf3 !important; }
  .taillight-report h1, .taillight-report h2, .taillight-report h3 { color: #e6edf3 !important; }
  .taillight-report h2 { border-bottom-color: #30363d !important; }
  .taillight-report em { color: #8b949e !important; }
  .taillight-report code { color: #a5b6c7 !important; }
  .taillight-report pre { background: #12161c !important; border-color: #30363d !important; }
  .taillight-report blockquote { border-left-color: #6b7280 !important; color: #a5b6c7 !important; }
  .taillight-report hr { border-top-color: #30363d !important; }
  .taillight-report table { border-color: #30363d !important; }
  .taillight-report th { background: #21262d !important; color: #e6edf3 !important; border-bottom-color: #30363d !important; }
  .taillight-report td { border-bottom-color: #30363d !important; }
  .taillight-report a { color: #6ea8fe !important; }
  .tl-sev-crit { color: #f85149 !important; }
  .tl-sev-warn { color: #d29922 !important; }
  .tl-sev-ok { color: #3fb950 !important; }
}
`

// scopeLabel renders the host scope for the metadata strip.
func scopeLabel(r *model.AnalysisReport) string {
	if len(r.Hosts) > 0 {
		return strings.Join(r.Hosts, ", ")
	}
	return "all hosts"
}

// renderMarkdown converts the analyzer's markdown body to HTML. Uses goldmark's
// default extensions plus GFM tables (the Correlations section emits pipe tables
// that we want rendered, not shown as literal pipes). Output is treated as
// trusted because the analyzer prepends the title + period and the model output
// passes a structure validator before reaching this layer; we still avoid any
// extension that would parse raw HTML so a stray <script> in the markdown body
// can't reach the inbox or the printed page.
func renderMarkdown(md string) string {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	var buf bytes.Buffer
	parser := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(
			ghtml.WithHardWraps(),
		),
	)
	if err := parser.Convert([]byte(md), &buf); err != nil {
		// Fallback to escaped <pre> so the document still renders intact.
		return "<pre>" + html.EscapeString(md) + "</pre>"
	}
	return colorizeSeverity(buf.String())
}

// codeSpan matches a rendered inline-code or fenced-code element.
var codeSpan = regexp.MustCompile(`(?s)<code[^>]*>.*?</code>`)

// severityToken matches the bracketed severity tags the Needs Action items lead
// with, and the status word the TL;DR commits to.
var severityToken = regexp.MustCompile(`\[(?:CRIT|WARN)\]|\b(?:ACT NOW|NOMINAL|WATCH)\b`)

// colorizeSeverity tints the severity tags and the TL;DR status word so a
// reader scanning the brief lands on the [CRIT] items first.
//
// Colored text rather than a filled badge, for two reasons: a fill is what a
// dark-mode mail client inverts into a shouting rectangle, and background colors
// are dropped when a browser prints, so a badge would vanish from the PDF.
//
// Runs on goldmark's output rather than on the markdown, so it only ever sees
// already-escaped text and cannot promote model output into markup. Matches
// inside a code element are skipped — a signature or a quoted sample message may
// legitimately contain one of these tokens, and tinting it mid-chip looks wrong.
func colorizeSeverity(body string) string {
	var b strings.Builder
	b.Grow(len(body))
	last := 0
	for _, m := range codeSpan.FindAllStringIndex(body, -1) {
		b.WriteString(severityToken.ReplaceAllStringFunc(body[last:m[0]], severitySpan))
		b.WriteString(body[m[0]:m[1]])
		last = m[1]
	}
	b.WriteString(severityToken.ReplaceAllStringFunc(body[last:], severitySpan))
	return b.String()
}

// severitySpan wraps a matched severity token in its color. The class carries
// no styling of its own — it exists so emailDarkCSS can re-tint these to their
// dark-background equivalents, which the inline style alone would win against.
func severitySpan(token string) string {
	var class, color string
	switch token {
	case "[CRIT]", "ACT NOW":
		class, color = "tl-sev-crit", "#b91c1c"
	case "[WARN]", "WATCH":
		class, color = "tl-sev-warn", "#b45309"
	case "NOMINAL":
		class, color = "tl-sev-ok", "#15803d"
	default:
		return token
	}
	return `<span class="` + class + `" style="color: ` + color + `; font-weight: 600;">` + token + `</span>`
}

// generatedAt prefers completed_at over created_at so a finished report stamps
// the actual finish time, not the queue time.
func generatedAt(r *model.AnalysisReport) string {
	if r.CompletedAt != nil {
		return r.CompletedAt.UTC().Format(time.RFC3339)
	}
	return r.CreatedAt.UTC().Format(time.RFC3339)
}

// generatedAtHuman is the masthead-friendly stamp for the printed PDF.
func generatedAtHuman(r *model.AnalysisReport) string {
	ts := r.CreatedAt
	if r.CompletedAt != nil {
		ts = *r.CompletedAt
	}
	return ts.UTC().Format("Jan 2, 2006 15:04 UTC")
}

// renderEmail renders the email body: dark masthead, white card, gray page,
// with emailDarkCSS layered on for dark-mode clients. Guarded by TestRenderEmail
// here and TestBuildEmailAnalysisReport in the notification backend — both are
// substring checks, not byte-equality, so structural edits are free as long as
// the asserted markers survive.
func renderEmail(r *model.AnalysisReport) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <meta name="color-scheme" content="light dark">
  <meta name="supported-color-schemes" content="light dark">
  <style>%s%s</style>
</head>
<body class="tl-page" style="margin: 0; padding: 20px; background: #f5f5f5; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;">
  <div class="tl-card" style="max-width: 760px; margin: 0 auto; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,0.1);">
    <div class="tl-masthead" style="background: #1f2937; padding: 14px 20px; color: #fff;">
      <div style="font-size: 11px; letter-spacing: 0.08em; text-transform: uppercase; opacity: 0.7;">Taillight — Analysis Report</div>
      <div style="font-size: 12px; opacity: 0.85; margin-top: 4px; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;">%s</div>
    </div>
    <div style="padding: 18px 22px;">
      <table style="font-size: 12px; margin-bottom: 14px; border: none;">
        <tr><td class="tl-meta-label" style="padding: 2px 14px 2px 0; color: #6b7280; border: none;">Source</td><td class="tl-meta-value" style="font-weight: 600; border: none;">%s</td><td class="tl-meta-label" style="padding: 2px 14px 2px 22px; color: #6b7280; border: none;">Mode</td><td class="tl-meta-value" style="font-weight: 600; border: none;">%s</td></tr>
        <tr><td class="tl-meta-label" style="padding: 2px 14px 2px 0; color: #6b7280; border: none;">Scope</td><td class="tl-meta-value" style="font-weight: 600; border: none;">%s</td><td class="tl-meta-label" style="padding: 2px 14px 2px 22px; color: #6b7280; border: none;">Model</td><td class="tl-meta-value" style="font-weight: 600; border: none;">%s</td></tr>
      </table>
      <div class="taillight-report">%s</div>
      <div class="tl-openin" style="margin-top: 18px; font-size: 12px; color: #6b7280; border-top: 1px solid #e5e7eb; padding-top: 10px;">Open in Taillight: <code style="font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;">/analysis/reports/%s</code></div>
    </div>
    <div class="tl-footer" style="padding: 10px 22px; background: #f8f9fa; color: #888; font-size: 11px;">
      Generated %s
    </div>
  </div>
</body>
</html>`,
		bodyCSS,
		emailDarkCSS,
		html.EscapeString(r.Slug),
		html.EscapeString(r.Feed),
		html.EscapeString(r.PromptMode),
		html.EscapeString(scopeLabel(r)),
		html.EscapeString(r.Model),
		renderMarkdown(r.Report),
		html.EscapeString(r.Slug),
		generatedAt(r),
	)
}

// renderPrint renders the paper-friendly document the frontend loads into a
// hidden iframe and prints. The report's own H1 title (prepended by the
// analyzer) leads the document; the Taillight masthead + provenance metadata
// (source / mode / scope / model / generated) sits at the bottom as a colophon.
// Uses an ink-light palette (browsers drop background colors when printing, so
// the email's dark bar would print as invisible white-on-white) and printCSS
// for clean multi-page A4 pagination.
func renderPrint(r *model.AnalysisReport) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <title>%s</title>
  <style>%s%s</style>
</head>
<body style="margin: 0; padding: 24px; background: #fff; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;">
  <div style="max-width: 820px; margin: 0 auto;">
    <div class="taillight-report">%s</div>
    <div style="border-top: 1px solid #999; margin-top: 24px; padding-top: 10px;">
      <table style="font-size: 12px; border: none;">
        <tr><td style="padding: 2px 14px 2px 0; color: #6b7280; border: none;">Source</td><td style="font-weight: 600; border: none;">%s</td><td style="padding: 2px 14px 2px 22px; color: #6b7280; border: none;">Mode</td><td style="font-weight: 600; border: none;">%s</td></tr>
        <tr><td style="padding: 2px 14px 2px 0; color: #6b7280; border: none;">Scope</td><td style="font-weight: 600; border: none;">%s</td><td style="padding: 2px 14px 2px 22px; color: #6b7280; border: none;">Model</td><td style="font-weight: 600; border: none;">%s</td></tr>
        <tr><td style="padding: 2px 14px 2px 0; color: #6b7280; border: none;">Generated</td><td style="font-weight: 600; border: none;" colspan="3">%s</td></tr>
      </table>
    </div>
  </div>
</body>
</html>`,
		html.EscapeString(r.Slug),
		bodyCSS,
		printCSS,
		renderMarkdown(r.Report),
		html.EscapeString(r.Feed),
		html.EscapeString(r.PromptMode),
		html.EscapeString(scopeLabel(r)),
		html.EscapeString(r.Model),
		html.EscapeString(generatedAtHuman(r)),
	)
}
