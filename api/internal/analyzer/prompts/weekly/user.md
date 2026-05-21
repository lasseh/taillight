# {{ .FeedTitle }} — {{ .PeriodLabel }} trend data block
Period: {{ .PeriodStart.Format "2006-01-02 15:04 UTC" }} → {{ .PeriodEnd.Format "2006-01-02 15:04 UTC" }}

Severity legend: 0=emerg 1=alert 2=crit 3=err 4=warn 5=notice 6=info 7=debug.
Rates in the Severity Drift block are per-day; counts elsewhere are raw totals across the full period.

## Top Event Types (by volume, max 25)
{{ range .TopMsgIDs -}}
- `{{ .MsgID }}` — {{ .Count }} events · severity mix: {{ range $sev, $cnt := .SeverityCounts }}{{ severityLabel $sev }}={{ $cnt }} {{ end }}
{{- if index $.JuniperRefs .MsgID }}
  - **Description:** {{ (index $.JuniperRefs .MsgID).Description }}
  {{- if (index $.JuniperRefs .MsgID).Cause }}
  - **Cause:** {{ (index $.JuniperRefs .MsgID).Cause }}
  {{- end }}
  {{- if (index $.JuniperRefs .MsgID).Action }}
  - **Action:** {{ (index $.JuniperRefs .MsgID).Action }}
  {{- end }}
{{- end }}
{{ end }}
## Severity Drift (current daily average vs 7-day daily average prior to this period)
{{ range .SeverityComparison.Levels -}}
- {{ .Label }} (sev {{ .Severity }}): current={{ printf "%.1f" .Current }}/day · baseline={{ printf "%.1f" .BaselineAvg }}/day · change={{ printf "%+.1f" .ChangePct }}%
{{ end }}
## Hosts with Most Errors (severity ≤ 3, max 15)
{{ range .TopErrorHosts -}}
- `{{ .Hostname }}` — {{ .Count }} errors · top msgid: `{{ .TopMsgID }}`
{{ end }}
{{- if .NewMsgIDs }}
## New Event Types (not seen in the 7 days prior to this period)
{{ range .NewMsgIDs -}}
- `{{ . }}`{{ if index $.JuniperRefs . }} — {{ (index $.JuniperRefs .).Description }}{{ if (index $.JuniperRefs .).Cause }} · Cause: {{ (index $.JuniperRefs .).Cause }}{{ end }}{{ end }}
{{ end }}
{{- else }}
## New Event Types
_None._
{{- end }}
{{ if .EventClusters }}
## Cross-Host Event Clusters (5-minute windows; ≥2 hosts firing the same msgid)
{{ range .EventClusters -}}
- {{ .Bucket.Format "2006-01-02 15:04 UTC" }} — {{ .Total }} events across [{{ join .Hosts ", " }}]; msgids: [{{ join .MsgIDs ", " }}]
{{ end }}
{{- else }}
## Cross-Host Event Clusters
_None in this period._
{{- end }}

---
Write the trend review now, following the section order and rules from the system message. Do not echo this data block. Do not include any preamble before the TL;DR. Remember: trend, not incident — at week scale, isolated single-day spikes are noise.
