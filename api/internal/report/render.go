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
	// card. Output is kept byte-for-byte stable so the email backend test
	// continues to guard it.
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
// variants so mail and print share heading colors and code-chip treatment.
// Kept email-safe (no CSS variables, no flexbox, no oklch) so Gmail / Apple
// Mail render it cleanly; Outlook desktop degrades gracefully.
const bodyCSS = `
.taillight-report { color: #111827; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; font-size: 14px; line-height: 1.55; }
.taillight-report h1 { font-size: 20px; font-weight: 700; color: #111827; margin: 16px 0 12px; padding-bottom: 8px; border-bottom: 2px solid #d97706; }
.taillight-report h2 { font-size: 16px; font-weight: 600; color: #111827; margin: 24px 0 8px; padding-bottom: 4px; border-bottom: 1px solid #d1d5db; }
.taillight-report h3 { font-size: 14px; font-weight: 600; color: #1f2937; margin: 18px 0 6px; }
.taillight-report p, .taillight-report li { font-size: 13px; color: #111827; margin: 6px 0; }
.taillight-report ul, .taillight-report ol { padding-left: 22px; }
.taillight-report em { color: #6b7280; font-style: italic; }
.taillight-report strong { color: #111827; font-weight: 600; }
.taillight-report code { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12px; padding: 1px 5px; border: 1px solid #d1d5db; border-radius: 3px; background: #f9fafb; color: #111827; }
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
	return buf.String()
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

// renderEmail renders the email body. Kept byte-for-byte identical to the
// previous backend.buildEmailAnalysisReport output (dark masthead, white card,
// gray page) so the email backend test continues to guard it.
func renderEmail(r *model.AnalysisReport) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <style>%s</style>
</head>
<body style="margin: 0; padding: 20px; background: #f5f5f5; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;">
  <div style="max-width: 760px; margin: 0 auto; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,0.1);">
    <div style="background: #1f2937; padding: 14px 20px; color: #fff;">
      <div style="font-size: 11px; letter-spacing: 0.08em; text-transform: uppercase; opacity: 0.7;">Taillight — Analysis Report</div>
      <div style="font-size: 12px; opacity: 0.85; margin-top: 4px; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;">%s</div>
    </div>
    <div style="padding: 18px 22px;">
      <table style="font-size: 12px; margin-bottom: 14px; border: none;">
        <tr><td style="padding: 2px 14px 2px 0; color: #6b7280; border: none;">Source</td><td style="font-weight: 600; border: none;">%s</td><td style="padding: 2px 14px 2px 22px; color: #6b7280; border: none;">Mode</td><td style="font-weight: 600; border: none;">%s</td></tr>
        <tr><td style="padding: 2px 14px 2px 0; color: #6b7280; border: none;">Scope</td><td style="font-weight: 600; border: none;">%s</td><td style="padding: 2px 14px 2px 22px; color: #6b7280; border: none;">Model</td><td style="font-weight: 600; border: none;">%s</td></tr>
      </table>
      <div class="taillight-report">%s</div>
      <div style="margin-top: 18px; font-size: 12px; color: #6b7280; border-top: 1px solid #e5e7eb; padding-top: 10px;">Open in Taillight: <code style="font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;">/analysis/reports/%s</code></div>
    </div>
    <div style="padding: 10px 22px; background: #f8f9fa; color: #888; font-size: 11px;">
      Generated %s
    </div>
  </div>
</body>
</html>`,
		bodyCSS,
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
      <div style="font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12px; color: #444;">generated %s</div>
      <table style="font-size: 12px; margin-top: 10px; border: none;">
        <tr><td style="padding: 2px 14px 2px 0; color: #6b7280; border: none;">Source</td><td style="font-weight: 600; border: none;">%s</td><td style="padding: 2px 14px 2px 22px; color: #6b7280; border: none;">Mode</td><td style="font-weight: 600; border: none;">%s</td></tr>
        <tr><td style="padding: 2px 14px 2px 0; color: #6b7280; border: none;">Scope</td><td style="font-weight: 600; border: none;">%s</td><td style="padding: 2px 14px 2px 22px; color: #6b7280; border: none;">Model</td><td style="font-weight: 600; border: none;">%s</td></tr>
      </table>
    </div>
  </div>
</body>
</html>`,
		html.EscapeString(r.Slug),
		bodyCSS,
		printCSS,
		renderMarkdown(r.Report),
		html.EscapeString(generatedAtHuman(r)),
		html.EscapeString(r.Feed),
		html.EscapeString(r.PromptMode),
		html.EscapeString(scopeLabel(r)),
		html.EscapeString(r.Model),
	)
}
