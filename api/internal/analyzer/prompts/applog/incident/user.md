{{ .LogDataBegin }}
# Applog — {{ .PeriodLabel }} incident window data block
Period: {{ .PeriodStart.Format "2006-01-02 15:04 UTC" }} → {{ .PeriodEnd.Format "2006-01-02 15:04 UTC" }}
{{- if .IsScoped }}
Scope: {{ .ScopeLabel }}
{{- end }}

The window starts on the hour, so it may run up to 59 minutes longer than requested. Rows in this block are application logs at level WARN, ERROR, or FATAL unless a section says otherwise. A template is a message with numbers → `<n>` and IP addresses → `<ip>`, identified by service / component / pattern; a template that logs at two levels appears once per level. Counts are raw counts within the period unless labeled per-day. Baseline = the 7 days before the period, as a per-day rate.

## Level drift (current per-day vs 7-day per-day; the all-levels row includes INFO and DEBUG)
{{ range .Drift -}}
- {{ .Label }}: current={{ printf "%.1f" .Current }}/day · baseline={{ printf "%.1f" .BaselineAvg }}/day · change={{ printf "%+.1f" .ChangePct }}%
{{ end }}
{{- if index .Unavailable "volume" }}
## Volume timeline
_Unavailable — this lookup failed for this run. Do not read it as "no volume."_
{{ else if .VolumeSparkline }}
## Volume timeline ({{ .VolumeBucketLabel }} per cell; all levels)
- Total: `{{ .VolumeSparkline }}`
- Errors (ERROR + FATAL): `{{ .ErrorSparkline }}`
{{- if .VolumePeaks }}
- Peaks: {{ join .VolumePeaks "; " }}
{{- end }}
{{ end }}
## Ranked services ({{ len .Ranked }} of {{ .ActiveServices }} active; ranked by new templates, then error-rate change vs baseline, then warning-rate change)
{{- if not .Ranked }}
_None — no service logged at WARN or above this period._
{{- end }}
{{ range $i, $s := .Ranked -}}
### {{ add $i 1 }}. `{{ sanitize $s.Service }}` — errors {{ $s.Current.ErrorPlus }} (baseline {{ printf "%.1f" $s.Baseline.ErrorPlus }}/day) · warnings {{ $s.Current.Warn }} (baseline {{ printf "%.1f" $s.Baseline.Warn }}/day) · new templates {{ $s.NewTemplates }}
{{- range $s.ErrorTemplates }}
- {{ .Level }} {{ if .Component }}`{{ sanitize .Component }}`{{ else }}(no component){{ end }} `{{ truncate (sanitize .Pattern) 120 }}` — {{ .Count }} events · {{ .HostCount }} host(s) · {{ .FirstSeen.Format "15:04" }}–{{ .LastSeen.Format "15:04" }}
{{- if .Sample }}
  - sample {{ .Sample.ReceivedAt.Format "15:04" }} `{{ sanitize .Sample.Host }}`: `{{ sanitize .Sample.Msg }}`{{ if .Sample.Attrs }} · attrs: `{{ sanitize .Sample.Attrs }}`{{ end }}
{{- end }}
{{- end }}
{{- range $s.WarnTemplates }}
- {{ .Level }} {{ if .Component }}`{{ sanitize .Component }}`{{ else }}(no component){{ end }} `{{ truncate (sanitize .Pattern) 120 }}` — {{ .Count }} events · {{ .HostCount }} host(s) · {{ .FirstSeen.Format "15:04" }}–{{ .LastSeen.Format "15:04" }}
{{- if .Sample }}
  - sample {{ .Sample.ReceivedAt.Format "15:04" }} `{{ sanitize .Sample.Host }}`: `{{ sanitize .Sample.Msg }}`{{ if .Sample.Attrs }} · attrs: `{{ sanitize .Sample.Attrs }}`{{ end }}
{{- end }}
{{- end }}
{{ end }}
{{- if .LongTail }}
## Long tail (next {{ len .LongTail }} active services)
{{ range .LongTail -}}
- `{{ sanitize .Service }}` — {{ .Current.ErrorPlus }} errors / {{ .Current.Warn }} warnings{{ if .NewTemplates }} · {{ .NewTemplates }} new template(s){{ end }}
{{ end }}
{{- end }}
{{- if .Remainder }}
- (+{{ .Remainder }} more active services, {{ .RemainderWarnPlus }} warn+ rows combined)
{{ end }}
{{- if index .Unavailable "new_templates" }}
## New signatures
_Unavailable — this lookup failed for this run. Do not read it as "no new signatures."_
{{- else if .NewTemplates }}
## New signatures (not seen in the 7 days before this period; errors first; max {{ .Caps.NewTemplates }})
{{ range .NewTemplates -}}
- {{ .Level }} `{{ sanitize .Service }}` / {{ if .Component }}`{{ sanitize .Component }}`{{ else }}(no component){{ end }} `{{ truncate (sanitize .Pattern) 120 }}` — {{ .Count }} events · {{ .HostCount }} host(s) · first seen {{ .FirstSeen.Format "2006-01-02 15:04 UTC" }}
{{- if .Sample }}
  - sample {{ .Sample.ReceivedAt.Format "15:04" }} `{{ sanitize .Sample.Host }}`: `{{ sanitize .Sample.Msg }}`{{ if .Sample.Attrs }} · attrs: `{{ sanitize .Sample.Attrs }}`{{ end }}
{{- end }}
{{ end }}
{{- else }}
## New signatures
_None._
{{- end }}
## Silent services (baseline rate predicts at least {{ .Caps.SilentMinEventsPerDay }} rows in this window; none logged, at any level)
{{- if .Silent }}
{{ range .Silent -}}
- `{{ sanitize .Service }}` — baseline {{ printf "%.0f" .Baseline.Total }}/day
{{ end }}
{{- else }}
_None._
{{- end }}
## New services (no rows in the 7-day baseline)
{{- if .NewServices }}
{{ range .NewServices -}}
- `{{ sanitize .Service }}` — {{ .Current.Total }} events this period ({{ .Current.ErrorPlus }} errors, {{ .Current.Warn }} warnings)
{{ end }}
{{- else }}
_None._
{{- end }}
{{- if index .Unavailable "hygiene" }}
## Hygiene
_Unavailable — this lookup failed for this run. Do not read it as "nothing to flag."_
{{- else }}
## Hygiene (over WARN-and-above rows; dominant templates over WARN rows only)
- Rows: {{ .Hygiene.WarnPlusRows }} · empty component: {{ .Hygiene.EmptyComponent }} · attrs over {{ .AttrsLimit }} bytes: {{ .Hygiene.OversizeAttrs }}
{{- range .Hygiene.Dominant }}
- Dominant: `{{ sanitize .Service }}` {{ if .Component }}`{{ sanitize .Component }}`{{ else }}(no component){{ end }} `{{ truncate (sanitize .Pattern) 100 }}` — {{ .Count }} of {{ .ServiceTotal }} WARN rows
{{- end }}
{{- end }}
{{- if index .Unavailable "samples" }}
- Samples: unavailable for this run — templates carry no sample lines.
{{- end }}
{{ .LogDataEnd }}

---
Write the briefing now, following the section order and rules from the system message. Do not echo this data block. Do not include any preamble before the first header.
