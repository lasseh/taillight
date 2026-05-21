# {{ .FeedTitle }} — {{ .PeriodLabel }} data block
Period: {{ .PeriodStart.Format "2006-01-02 15:04 UTC" }} → {{ .PeriodEnd.Format "2006-01-02 15:04 UTC" }}

Severity legend: 0=emerg 1=alert 2=crit 3=err 4=warn 5=notice 6=info 7=debug.
All counts below are raw event counts within the period unless explicitly labeled per-day.

## Top Event Signatures (by volume, max 25)
Each signature is the RFC 5424 MSGID when present, otherwise a normalized message template (numbers → `<n>`, IPs → `<ip>`). Sample messages are verbatim log text — use them to ground your interpretation; do not invent details that aren't in them.
{{ range .TopMsgIDs -}}
- `{{ .MsgID }}` — {{ .Count }} events{{ if .HostCount }} · {{ .HostCount }} host{{ if gt .HostCount 1 }}s{{ end }}{{ if .TopHosts }} (top: {{ range $i, $h := .TopHosts }}{{ if $i }}, {{ end }}`{{ $h.Hostname }}` ({{ $h.Count }}){{ end }}){{ end }}{{ end }} · severity mix: {{ range $sev, $cnt := .SeverityCounts }}{{ severityLabel $sev }}={{ $cnt }} {{ end }}
{{- if index $.JuniperRefs .MsgID }}
  - **Description:** {{ (index $.JuniperRefs .MsgID).Description }}
  {{- if (index $.JuniperRefs .MsgID).Cause }}
  - **Cause:** {{ (index $.JuniperRefs .MsgID).Cause }}
  {{- end }}
  {{- if (index $.JuniperRefs .MsgID).Action }}
  - **Action:** {{ (index $.JuniperRefs .MsgID).Action }}
  {{- end }}
{{- end }}
{{- if .Samples }}
  - **Samples:**
  {{- range .Samples }}
    - {{ .ReceivedAt.Format "15:04" }} `{{ .Hostname }}` ({{ severityLabel .Severity }}): `{{ .Message }}`
  {{- end }}
{{- end }}
{{ end }}
{{- if .VolumeSparkline }}
## Volume Timeline ({{ .VolumeBucketLabel }} per cell)
- Total: `{{ .VolumeSparkline }}`
- Errors (sev ≤ 3): `{{ .ErrorSparkline }}`
{{- if .VolumePeaks }}
- Peaks: {{ join .VolumePeaks "; " }}
{{- end }}
{{ end }}
## Severity Drift (current daily average vs 7-day daily average)
{{ range .SeverityComparison.Levels -}}
- {{ .Label }} (sev {{ .Severity }}): current={{ printf "%.1f" .Current }}/day · baseline={{ printf "%.1f" .BaselineAvg }}/day · change={{ printf "%+.1f" .ChangePct }}%
{{ end }}
{{- if .TopPrograms }}
## Top Programs (srvlog programname; max 10)
{{ range .TopPrograms -}}
- `{{ .Programname }}` — {{ .Count }} events ({{ .ErrorCount }} severity ≤ 3) · severity mix: {{ range $sev, $cnt := .SeverityCounts }}{{ severityLabel $sev }}={{ $cnt }} {{ end }}
{{ end }}
{{- end }}
{{- if .TopFacilities }}
## Top Facilities (syslog facility; max 8)
{{ range .TopFacilities -}}
- `{{ .Label }}` (facility {{ .Facility }}) — {{ .Count }} events ({{ .ErrorCount }} severity ≤ 3)
{{ end }}
{{- end }}
## Hosts with Most Errors (severity ≤ 3, max 15)
{{ range .TopErrorHosts -}}
- `{{ .Hostname }}` — {{ .Count }} errors · top msgid: `{{ .TopMsgID }}`
{{ end }}
{{- if .NewMsgIDs }}
## New Event Signatures (not seen in the 7 days prior to this period)
{{ range .NewMsgIDs -}}
- `{{ . }}`{{ if index $.JuniperRefs . }} — {{ (index $.JuniperRefs .).Description }}{{ if (index $.JuniperRefs .).Cause }} · Cause: {{ (index $.JuniperRefs .).Cause }}{{ end }}{{ end }}
{{- if index $.NewMsgIDSamples . }}
  - **First observed:** {{ (index $.NewMsgIDSamples .).ReceivedAt.Format "2006-01-02 15:04 UTC" }} on `{{ (index $.NewMsgIDSamples .).Hostname }}` ({{ severityLabel (index $.NewMsgIDSamples .).Severity }}): `{{ (index $.NewMsgIDSamples .).Message }}`
{{- end }}
{{ end }}
{{- else }}
## New Event Signatures
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
Write the briefing now, following the section order and rules from the system message. Do not echo this data block. Do not include any preamble before the TL;DR.
