package report

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

func sampleReport() *model.AnalysisReport {
	completed := time.Date(2026, 5, 22, 13, 16, 0, 0, time.UTC)
	return &model.AnalysisReport{
		Slug:        "netlog-incident-2026-05-22-1315",
		Feed:        "netlog",
		PromptMode:  "incident",
		Hosts:       []string{"s-vts-ep-1", "s-vts-ep-2"},
		Model:       "gpt-oss:20b",
		PeriodStart: time.Date(2026, 5, 22, 10, 15, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 5, 22, 13, 15, 0, 0, time.UTC),
		Report: "# Incident Briefing — 2026-05-22 → 2026-05-22\n" +
			"_Period: 2026-05-22 10:15 UTC – 2026-05-22 13:15 UTC_\n\n" +
			"## Verdict\n\n" +
			"CONTAIN — `RTPERF_CPU_THRESHOLD_EXCEEDED` spike on s-vts-ep-1 at 12:05 and 12:35.\n\n" +
			"## Correlations\n\n" +
			"| Signature | Count | Hosts |\n|---|---|---|\n| cpu_threshold | 37 | s-vts-ep-1 |\n",
		Status:      "completed",
		CompletedAt: &completed,
		CreatedAt:   time.Date(2026, 5, 22, 13, 15, 30, 0, time.UTC),
	}
}

// TestRenderPrint covers the paper-friendly variant: an ink-light masthead,
// pagination CSS, the metadata strip, and the full rendered report body.
func TestRenderPrint(t *testing.T) {
	body := RenderHTML(sampleReport(), VariantPrint)

	checks := []string{
		"@page",                                      // page geometry for multi-page A4
		"@media print",                               // pagination block present
		"page-break-after: avoid",                    // headings stay with their content
		"May 22, 2026 13:16 UTC",                     // Generated row in colophon table
		"netlog-incident-2026-05-22-1315",            // slug in document <title>
		"gpt-oss:20b",                                // metadata strip
		"s-vts-ep-1, s-vts-ep-2",                     // host scope
		`<h1>Incident Briefing`,                      // analyzer-prepended title rendered
		`<h2>Verdict</h2>`,                           // section heading
		`<code>RTPERF_CPU_THRESHOLD_EXCEEDED</code>`, // inline code chip
		`<table>`,                                    // GFM pipe table rendered
	}
	for _, c := range checks {
		if !strings.Contains(body, c) {
			t.Errorf("print variant missing %q", c)
		}
	}

	// The print variant must not carry the email's dark masthead bar — that
	// background drops out when browsers print, which is the whole reason the
	// print variant exists.
	if strings.Contains(body, "background: #1f2937") {
		t.Error("print variant should not use the dark email masthead bar")
	}

	// Tall tables/pre are allowed to break across pages; forcing them whole
	// leaves blank gaps. Guard that we did not reintroduce the avoid rule.
	if strings.Contains(body, "page-break-inside: avoid") {
		t.Error("print variant should let tables/pre break across pages")
	}

	// Opt-in visual preview: write the rendered body to disk so a maintainer
	// can open it in a browser and eyeball pagination. Off by default.
	if path := os.Getenv("TAILLIGHT_PRINT_PREVIEW_PATH"); path != "" {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Logf("preview write failed: %v", err)
		} else {
			t.Logf("wrote print preview to %s", path)
		}
	}
}

// TestRenderEmail guards the email variant: dark masthead, metadata strip, and
// the full report body. Byte-for-byte email stability is additionally covered
// by the email backend's TestBuildEmailAnalysisReport, which calls through to
// this renderer.
func TestRenderEmail(t *testing.T) {
	body := RenderHTML(sampleReport(), VariantEmail)

	checks := []string{
		"background: #1f2937", // dark masthead bar retained for email
		"taillight-report",    // styled report container class
		"netlog-incident-2026-05-22-1315",
		"gpt-oss:20b",
		`<h2>Verdict</h2>`,
		`<table>`,
	}
	for _, c := range checks {
		if !strings.Contains(body, c) {
			t.Errorf("email variant missing %q", c)
		}
	}

	// The email variant must not pull in print-only page geometry.
	if strings.Contains(body, "@page") {
		t.Error("email variant should not carry @page print geometry")
	}
}

// TestScopeLabelEmpty falls back to a readable label when no hosts are scoped.
func TestScopeLabelEmpty(t *testing.T) {
	r := sampleReport()
	r.Hosts = nil
	if got := scopeLabel(r); got != "all hosts" {
		t.Errorf("scopeLabel with no hosts = %q, want %q", got, "all hosts")
	}
}
