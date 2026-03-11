package analyzer

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/lasseh/taillight/internal/model"
)

const systemPrompt = `You are a senior network operations analyst reviewing syslog data from a Juniper-based network infrastructure. Produce a concise daily operations briefing in markdown format.

Your report MUST include these sections:

## Executive Summary
A 2-3 sentence overview of the last 24 hours highlighting the most important findings.

## Incident Analysis
For each significant event type (msgid), analyze:
- What the event means (use Juniper reference data when available)
- Volume and severity distribution
- Which hosts are affected
- Recommended operator action

## Anomaly Detection
- Severity level spikes compared to 7-day baseline
- New/previously unseen event types
- Unusual patterns

## Event Correlation
- Events that occurred simultaneously across multiple hosts
- Potential cascading failures or related incidents

## Priority Actions
A numbered list of recommended actions for the ops team, ordered by urgency.

Guidelines:
- Be specific — reference actual hostnames, msgids, and counts
- Flag anything with severity 0-3 (emerg/alert/crit/err) as requiring attention
- Note percentage changes vs baseline that exceed ±50%
- Keep the report actionable — tell operators what to DO, not just what happened
- If there is little activity, say so briefly — do not fabricate issues`

var userPromptTemplate = template.Must(template.New("user").Funcs(template.FuncMap{
	"severityLabel": model.SeverityLabel,
	"join":          strings.Join,
}).Parse(`# Syslog Analysis Data — Last 24 Hours
Period: {{ .PeriodStart.Format "2006-01-02 15:04 UTC" }} to {{ .PeriodEnd.Format "2006-01-02 15:04 UTC" }}

## Top Event Types (by volume)
{{ range .TopMsgIDs -}}
- **{{ .MsgID }}**: {{ .Count }} events {{ range $sev, $cnt := .SeverityCounts }}[{{ severityLabel $sev }}={{ $cnt }}] {{ end }}
{{- if index $.JuniperRefs .MsgID }}
  Juniper ref: {{ (index $.JuniperRefs .MsgID).Description }}
  {{- if (index $.JuniperRefs .MsgID).Cause }}  | Cause: {{ (index $.JuniperRefs .MsgID).Cause }}{{ end }}
  {{- if (index $.JuniperRefs .MsgID).Action }}  | Action: {{ (index $.JuniperRefs .MsgID).Action }}{{ end }}
{{- end }}
{{ end }}
## Severity Comparison (current 24h vs 7-day daily average)
{{ range .SeverityComparison.Levels -}}
- {{ .Label }} ({{ .Severity }}): current={{ .Current }}, baseline_avg={{ printf "%.0f" .BaselineAvg }}, change={{ printf "%+.1f" .ChangePct }}%
{{ end }}
## Hosts with Most Errors (severity <= 3)
{{ range .TopErrorHosts -}}
- **{{ .Hostname }}**: {{ .Count }} errors, top msgid={{ .TopMsgID }}
{{ end }}
{{- if .NewMsgIDs }}
## New Event Types (not seen in prior 7 days)
{{ range .NewMsgIDs -}}
- {{ . }}{{ if index $.JuniperRefs . }} — {{ (index $.JuniperRefs .).Description }}{{ end }}
{{ end }}
{{- end }}
{{- if .EventClusters }}
## Cross-Host Event Clusters (5-min windows)
{{ range .EventClusters -}}
- {{ .Bucket.Format "15:04 UTC" }}: {{ .Total }} events across [{{ join .Hosts ", " }}] — msgids: [{{ join .MsgIDs ", " }}]
{{ end }}
{{- end }}`))

// buildPrompt renders the system and user prompts from gathered data.
func buildPrompt(data analysisData) (string, string, error) {
	var buf bytes.Buffer
	if err := userPromptTemplate.Execute(&buf, data); err != nil {
		return "", "", fmt.Errorf("render user prompt: %w", err)
	}
	return systemPrompt, buf.String(), nil
}
