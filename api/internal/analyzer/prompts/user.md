# {{ .FeedTitle }} Analysis Data — Last {{ .PeriodLabel }}
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
## Severity Comparison (current daily average vs 7-day daily average)
{{ range .SeverityComparison.Levels -}}
- {{ .Label }} ({{ .Severity }}): current={{ printf "%.1f" .Current }}/day, baseline_avg={{ printf "%.1f" .BaselineAvg }}/day, change={{ printf "%+.1f" .ChangePct }}%
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
{{- end }}