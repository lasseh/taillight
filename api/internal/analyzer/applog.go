package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lasseh/taillight/internal/model"
)

// feedApplog is the applog feed name as the analyzer sees it.
const feedApplog = "applog"

// AppLogCaps bounds how much applog data reaches the prompt. The defaults
// are sized for the 32k context of the production Ollama box; the caps are
// bound to analysis.applog in config so a machine with more room can raise
// them. Zero fields fall back to the defaults, so a zero Config still runs.
type AppLogCaps struct {
	RankedServices           int // services covered in depth, in rank order
	ErrorTemplatesPerService int // ERROR/FATAL templates per ranked service
	WarnTemplatesPerService  int // WARN templates per ranked service
	NewTemplates             int // templates first seen in the window, across all services
	TemplateSamples          int // top templates that get a sample row, errors first
	SampleAttrsBytes         int // compacted attrs per sample
	SampleMsgChars           int // message text per sample
	SilentServices           int // silent services and new services listed, each
	SilentMinEventsPerDay    int // baseline rate a service needs for its silence to count
	LongTailServices         int // services in the long-tail table after the ranked ones
}

// DefaultAppLogCaps returns the caps sized for a 32k context window.
func DefaultAppLogCaps() AppLogCaps {
	return AppLogCaps{
		RankedServices:           12,
		ErrorTemplatesPerService: 5,
		WarnTemplatesPerService:  3,
		NewTemplates:             20,
		TemplateSamples:          15,
		SampleAttrsBytes:         400,
		SampleMsgChars:           300,
		SilentServices:           20,
		SilentMinEventsPerDay:    50,
		LongTailServices:         25,
	}
}

// withDefaults fills every zero or negative cap from DefaultAppLogCaps.
func (c AppLogCaps) withDefaults() AppLogCaps {
	d := DefaultAppLogCaps()
	c.RankedServices = orDefault(c.RankedServices, d.RankedServices)
	c.ErrorTemplatesPerService = orDefault(c.ErrorTemplatesPerService, d.ErrorTemplatesPerService)
	c.WarnTemplatesPerService = orDefault(c.WarnTemplatesPerService, d.WarnTemplatesPerService)
	c.NewTemplates = orDefault(c.NewTemplates, d.NewTemplates)
	c.TemplateSamples = orDefault(c.TemplateSamples, d.TemplateSamples)
	c.SampleAttrsBytes = orDefault(c.SampleAttrsBytes, d.SampleAttrsBytes)
	c.SampleMsgChars = orDefault(c.SampleMsgChars, d.SampleMsgChars)
	c.SilentServices = orDefault(c.SilentServices, d.SilentServices)
	c.SilentMinEventsPerDay = orDefault(c.SilentMinEventsPerDay, d.SilentMinEventsPerDay)
	c.LongTailServices = orDefault(c.LongTailServices, d.LongTailServices)
	return c
}

func orDefault(v, d int) int {
	if v > 0 {
		return v
	}
	return d
}

const (
	// applogBaseline is the comparison window before the period, matching
	// the syslog gather's 7-day baseline.
	applogBaseline     = 7 * 24 * time.Hour
	applogBaselineDays = 7.0

	// Hygiene thresholds are constants rather than caps: they define what
	// counts as a finding, not how much of it reaches the prompt.
	hygieneDominantShare     = 0.30
	hygieneDominantMinEvents = 100
	hygieneDominantLimit     = 5

	// Attrs compaction: a string value keeps this many characters and, when
	// it spans lines (a stack trace), this many lines.
	attrValueMaxChars = 120
	attrValueMaxLines = 3
)

// Section keys for applogData.Unavailable, referenced by name in
// prompts/applog/*/user.md.
const (
	unavailableNewTemplates = "new_templates"
	unavailableSamples      = "samples"
	unavailableVolume       = "volume"
	unavailableHygiene      = "hygiene"
)

// errorRank is the rank at and above which a level counts as an error for
// the two-list split.
var errorRank = model.AppLogLevelRank("ERROR")

// applogPerDay is a per-day rate breakdown, the unit the baseline is
// expressed in so window and baseline compare like for like.
type applogPerDay struct {
	Total float64
	Warn  float64
	Error float64
	Fatal float64
}

// ErrorPlus returns the error-and-above rate. Exported for the templates.
func (p applogPerDay) ErrorPlus() float64 { return p.Error + p.Fatal }

// applogServiceRow is one service as the prompt sees it: window counts, the
// same as per-day rates, the baseline per-day rates, and how many new
// templates the service produced.
type applogServiceRow struct {
	Service       string
	Current       model.AppLogLevelCounts
	CurrentPerDay applogPerDay
	Baseline      applogPerDay
	NewTemplates  int
}

// errorDelta is the change in error-and-above rate against the baseline.
func (r applogServiceRow) errorDelta() float64 {
	return r.CurrentPerDay.Error + r.CurrentPerDay.Fatal - r.Baseline.Error - r.Baseline.Fatal
}

// warnDelta is the change in WARN rate against the baseline.
func (r applogServiceRow) warnDelta() float64 {
	return r.CurrentPerDay.Warn - r.Baseline.Warn
}

// applogServiceReport is a ranked service with its two template lists.
type applogServiceReport struct {
	applogServiceRow
	ErrorTemplates []model.AppLogTemplate
	WarnTemplates  []model.AppLogTemplate
}

// applogLevelDrift compares one level's window rate to its baseline, fleet
// wide within the scope.
type applogLevelDrift struct {
	Label       string
	Current     float64 // per day
	BaselineAvg float64 // per day
	ChangePct   float64
}

// applogData holds everything the applog prompt renders.
type applogData struct {
	Feed        string
	Services    []string // empty when the run covers every service.
	Period      time.Duration
	PeriodLabel string
	PeriodStart time.Time
	PeriodEnd   time.Time

	// ActiveServices counts services with warn-and-above rows or a new
	// template in the window; only those are ranked.
	ActiveServices    int
	Drift             []applogLevelDrift // FATAL, ERROR, WARN, then all levels.
	Ranked            []applogServiceReport
	LongTail          []applogServiceRow
	Remainder         int   // active services beyond the ranked and long-tail lists.
	RemainderWarnPlus int64 // their warn-and-above rows, combined.
	NewTemplates      []model.AppLogTemplate
	Silent            []applogServiceRow
	NewServices       []applogServiceRow

	VolumeTimeline    []model.AnalysisVolumeBucket
	VolumeSparkline   string
	ErrorSparkline    string
	VolumePeaks       []string
	VolumeBucketLabel string

	Hygiene model.AppLogHygiene

	Caps        AppLogCaps
	Unavailable map[string]bool
}

// gatherAppLog collects the applog data for the period ending at periodEnd.
// Service stats and the ranked services' templates are load-bearing; every
// other lookup is best-effort and marks its section unavailable on failure.
func (a *Analyzer) gatherAppLog(ctx context.Context, scope model.AnalysisScope, period time.Duration, periodEnd time.Time) (applogData, error) {
	caps := a.cfg.AppLog.withDefaults()
	periodStart := periodEnd.Add(-period)
	baselineStart := periodStart.Add(-applogBaseline)
	periodDays := period.Hours() / 24
	scoped := !scope.IsAllServices()

	data := applogData{
		Feed:        scope.Feed,
		Services:    scope.Services,
		Period:      period,
		PeriodLabel: periodLabel(period),
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		Caps:        caps,
		Unavailable: make(map[string]bool),
	}

	a.logger.Info("gathering applog service stats", "scoped", scoped)
	stats, err := a.store.GetAppLogServiceStats(ctx, scope, periodStart, baselineStart)
	if err != nil {
		return data, err
	}

	a.logger.Info("gathering applog new templates", "scoped", scoped)
	data.NewTemplates, err = bestEffort(ctx, a.logger, data.Unavailable, unavailableNewTemplates,
		func() ([]model.AppLogTemplate, error) {
			return a.store.GetAppLogNewTemplates(ctx, scope, periodStart, baselineStart, caps.NewTemplates)
		})
	if err != nil {
		return data, err
	}
	newCounts := make(map[string]int, len(data.NewTemplates))
	for _, t := range data.NewTemplates {
		newCounts[t.Service]++
	}

	rows := make([]applogServiceRow, 0, len(stats))
	for _, st := range stats {
		rows = append(rows, applogServiceRow{
			Service:       st.Service,
			Current:       st.Current,
			CurrentPerDay: perDay(st.Current, periodDays),
			Baseline:      perDay(st.Baseline, applogBaselineDays),
			NewTemplates:  newCounts[st.Service],
		})
	}
	data.Drift = levelDrift(stats, periodDays)

	// Rank the services that did something worth reading about, then cut
	// the list into the in-depth set, the long tail, and a remainder count.
	active := activeServices(rows)
	rankAppLogServices(active)
	data.ActiveServices = len(active)
	ranked, longTail, rest := cutRanked(active, caps)
	data.LongTail = longTail
	data.Remainder = len(rest)
	for _, r := range rest {
		data.RemainderWarnPlus += r.Current.WarnPlus()
	}

	rankedNames := make([]string, len(ranked))
	for i, r := range ranked {
		rankedNames[i] = r.Service
	}
	a.logger.Info("gathering applog top templates", "services", len(rankedNames))
	templates, err := a.store.GetAppLogTopTemplates(ctx, periodStart, rankedNames,
		caps.ErrorTemplatesPerService, caps.WarnTemplatesPerService)
	if err != nil {
		return data, err
	}
	data.Ranked = serviceReports(ranked, templates)

	// Samples: ranked error templates first, then warnings, until the cap;
	// every new template gets one regardless.
	keys := sampleKeys(data.Ranked, data.NewTemplates, caps.TemplateSamples)
	a.logger.Info("gathering applog template samples", "keys", len(keys))
	samples, err := bestEffort(ctx, a.logger, data.Unavailable, unavailableSamples,
		func() (map[model.AppLogTemplateKey]model.AppLogSample, error) {
			return a.store.GetAppLogTemplateSamples(ctx, periodStart, keys, caps.SampleMsgChars)
		})
	if err != nil {
		return data, err
	}
	attachSamples(&data, samples, caps.SampleAttrsBytes)

	// Volume comes from the hourly aggregate, so the sparkline never goes
	// finer than one cell per hour.
	bucketMinutes := max(pickBucketMinutes(period), 60)
	data.VolumeBucketLabel = bucketLabel(bucketMinutes)
	a.logger.Info("gathering applog volume timeline", "bucket_minutes", bucketMinutes)
	data.VolumeTimeline, err = bestEffort(ctx, a.logger, data.Unavailable, unavailableVolume,
		func() ([]model.AnalysisVolumeBucket, error) {
			return a.store.GetAppLogVolumeTimeline(ctx, scope, periodStart, periodEnd, bucketMinutes)
		})
	if err != nil {
		return data, err
	}
	if len(data.VolumeTimeline) > 0 {
		totals := make([]int64, len(data.VolumeTimeline))
		errs := make([]int64, len(data.VolumeTimeline))
		for i, b := range data.VolumeTimeline {
			totals[i] = b.Total
			errs[i] = b.ErrorCount
		}
		data.VolumeSparkline = sparkline(totals)
		data.ErrorSparkline = sparkline(errs)
		data.VolumePeaks = topPeakBuckets(data.VolumeTimeline, 3, peakTimeFormat(bucketMinutes))
	}

	a.logger.Info("gathering applog hygiene facts")
	data.Hygiene, err = bestEffort(ctx, a.logger, data.Unavailable, unavailableHygiene,
		func() (model.AppLogHygiene, error) {
			return a.store.GetAppLogHygiene(ctx, scope, periodStart,
				hygieneDominantShare, hygieneDominantMinEvents, hygieneDominantLimit)
		})
	if err != nil {
		return data, err
	}

	data.Silent, data.NewServices = silentAndNewServices(rows, caps)

	return data, nil
}

// activeServices keeps the services that logged at warn or above, or
// produced a new template, in the window.
func activeServices(rows []applogServiceRow) []applogServiceRow {
	active := make([]applogServiceRow, 0, len(rows))
	for _, r := range rows {
		if r.Current.WarnPlus() > 0 || r.NewTemplates > 0 {
			active = append(active, r)
		}
	}
	return active
}

// cutRanked splits an already ranked list into the in-depth set, the
// long-tail table, and whatever is left over.
func cutRanked(active []applogServiceRow, caps AppLogCaps) (ranked, longTail, rest []applogServiceRow) {
	ranked = active[:min(len(active), caps.RankedServices)]
	rest = active[len(ranked):]
	longTail = rest[:min(len(rest), caps.LongTailServices)]
	return ranked, longTail, rest[len(longTail):]
}

// serviceReports pairs each ranked service with its templates, split into
// the error-and-above list and the WARN list.
func serviceReports(ranked []applogServiceRow, templates []model.AppLogTemplate) []applogServiceReport {
	reports := make([]applogServiceReport, len(ranked))
	byService := make(map[string]*applogServiceReport, len(ranked))
	for i, r := range ranked {
		reports[i] = applogServiceReport{applogServiceRow: r}
		byService[r.Service] = &reports[i]
	}
	for _, t := range templates {
		rep, ok := byService[t.Service]
		if !ok {
			continue
		}
		if model.AppLogLevelRank(t.Level) >= errorRank {
			rep.ErrorTemplates = append(rep.ErrorTemplates, t)
		} else {
			rep.WarnTemplates = append(rep.WarnTemplates, t)
		}
	}
	return reports
}

// attachSamples binds each fetched sample to its template, compacting the
// attrs to the prompt budget on the way.
func attachSamples(data *applogData, samples map[model.AppLogTemplateKey]model.AppLogSample, attrsBytes int) {
	attach := func(t *model.AppLogTemplate) {
		s, ok := samples[t.AppLogTemplateKey]
		if !ok {
			return
		}
		s.Attrs = compactAttrs(s.Attrs, attrsBytes)
		t.Sample = &s
	}
	for i := range data.Ranked {
		for j := range data.Ranked[i].ErrorTemplates {
			attach(&data.Ranked[i].ErrorTemplates[j])
		}
		for j := range data.Ranked[i].WarnTemplates {
			attach(&data.Ranked[i].WarnTemplates[j])
		}
	}
	for i := range data.NewTemplates {
		attach(&data.NewTemplates[i])
	}
}

// silentAndNewServices derives both lists from the stats already in hand.
// Silent means a baseline of at least the configured rate and nothing in
// the window; new means absent from the 7-day baseline, so a service
// returning after a longer gap reads as new too. Each list is capped at
// caps.SilentServices.
func silentAndNewServices(rows []applogServiceRow, caps AppLogCaps) (silent, fresh []applogServiceRow) {
	minSilent := float64(caps.SilentMinEventsPerDay)
	for _, r := range rows {
		switch {
		case r.Current.Total == 0 && r.Baseline.Total >= minSilent:
			silent = append(silent, r)
		case r.Current.Total > 0 && r.Baseline.Total == 0:
			fresh = append(fresh, r)
		}
	}
	sort.SliceStable(silent, func(i, j int) bool {
		if silent[i].Baseline.Total != silent[j].Baseline.Total {
			return silent[i].Baseline.Total > silent[j].Baseline.Total
		}
		return silent[i].Service < silent[j].Service
	})
	sort.SliceStable(fresh, func(i, j int) bool {
		if fresh[i].Current.Total != fresh[j].Current.Total {
			return fresh[i].Current.Total > fresh[j].Current.Total
		}
		return fresh[i].Service < fresh[j].Service
	})
	return silent[:min(len(silent), caps.SilentServices)], fresh[:min(len(fresh), caps.SilentServices)]
}

// perDay converts raw counts to per-day rates over days.
func perDay(c model.AppLogLevelCounts, days float64) applogPerDay {
	if days <= 0 {
		days = 1
	}
	return applogPerDay{
		Total: float64(c.Total) / days,
		Warn:  float64(c.Warn) / days,
		Error: float64(c.Error) / days,
		Fatal: float64(c.Fatal) / days,
	}
}

// levelDrift sums every service's counts and compares the window's per-day
// rate to the baseline's, one entry per level plus one for all levels.
func levelDrift(stats []model.AppLogServiceStats, periodDays float64) []applogLevelDrift {
	var cur, base model.AppLogLevelCounts
	for _, st := range stats {
		cur.Total += st.Current.Total
		cur.Warn += st.Current.Warn
		cur.Error += st.Current.Error
		cur.Fatal += st.Current.Fatal
		base.Total += st.Baseline.Total
		base.Warn += st.Baseline.Warn
		base.Error += st.Baseline.Error
		base.Fatal += st.Baseline.Fatal
	}
	c := perDay(cur, periodDays)
	b := perDay(base, applogBaselineDays)
	mk := func(label string, current, baseline float64) applogLevelDrift {
		d := applogLevelDrift{Label: label, Current: current, BaselineAvg: baseline}
		if baseline > 0 {
			d.ChangePct = (current - baseline) / baseline * 100
		}
		return d
	}
	return []applogLevelDrift{
		mk("FATAL", c.Fatal, b.Fatal),
		mk("ERROR", c.Error, b.Error),
		mk("WARN", c.Warn, b.Warn),
		mk("all levels", c.Total, b.Total),
	}
}

// rankAppLogServices orders services by new templates, then error-and-above
// delta against baseline, then WARN delta, then name. Change ranks above
// chronic volume on purpose: the long-tail table still carries the counts.
func rankAppLogServices(rows []applogServiceRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		x, y := rows[i], rows[j]
		if x.NewTemplates != y.NewTemplates {
			return x.NewTemplates > y.NewTemplates
		}
		if dx, dy := x.errorDelta(), y.errorDelta(); dx != dy {
			return dx > dy
		}
		if dx, dy := x.warnDelta(), y.warnDelta(); dx != dy {
			return dx > dy
		}
		return x.Service < y.Service
	})
}

// sampleKeys picks which templates get a sample row: ranked services' error
// templates in rank order, then their warn templates, up to budget, plus
// every new template. Duplicates collapse.
func sampleKeys(ranked []applogServiceReport, newTemplates []model.AppLogTemplate, budget int) []model.AppLogTemplateKey {
	var keys []model.AppLogTemplateKey
	seen := make(map[model.AppLogTemplateKey]bool)
	add := func(k model.AppLogTemplateKey) {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for _, rep := range ranked {
		for _, t := range rep.ErrorTemplates {
			if budget <= 0 {
				break
			}
			add(t.AppLogTemplateKey)
			budget--
		}
	}
	for _, rep := range ranked {
		for _, t := range rep.WarnTemplates {
			if budget <= 0 {
				break
			}
			add(t.AppLogTemplateKey)
			budget--
		}
	}
	for _, t := range newTemplates {
		add(t.AppLogTemplateKey)
	}
	return keys
}

// isEmptyAppLogData reports whether the window has nothing to narrate: no
// service logged at warn or above, no template is new, and no service went
// silent or appeared.
func isEmptyAppLogData(d applogData) bool {
	return d.ActiveServices == 0 && len(d.NewTemplates) == 0 && len(d.Silent) == 0 && len(d.NewServices) == 0
}

// emptyAppLogBody is the deterministic body for an empty applog window.
func emptyAppLogBody(scope model.AnalysisScope) string {
	if scope.IsAllServices() {
		return "_No warnings or errors recorded on the applog feed during this window._\n"
	}
	return "_No warnings or errors recorded for the scoped service(s) during this window._\n"
}

// compactAttrs rewrites a sample's attrs JSON for the prompt: string values
// are cut to attrValueMaxChars and, when multi-line, to attrValueMaxLines
// (a stack trace keeps its first frames); nested values get the same
// treatment; the serialised result is cut at maxBytes. Text that is not
// valid JSON is cut as-is.
func compactAttrs(raw string, maxBytes int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || maxBytes <= 0 {
		return ""
	}
	out := raw
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(compactValue(v)); err == nil {
			out = strings.TrimRight(buf.String(), "\n")
		}
	}
	return truncateBytes(out, maxBytes)
}

func compactValue(v any) any {
	switch t := v.(type) {
	case string:
		return compactString(t)
	case map[string]any:
		for k, val := range t {
			t[k] = compactValue(val)
		}
		return t
	case []any:
		for i := range t {
			t[i] = compactValue(t[i])
		}
		return t
	default:
		return v
	}
}

func compactString(s string) string {
	if strings.Contains(s, "\n") {
		lines := strings.SplitN(s, "\n", attrValueMaxLines+1)
		if len(lines) > attrValueMaxLines {
			s = strings.Join(lines[:attrValueMaxLines], "\n") + " …"
		}
	}
	return truncatePromptString(s, attrValueMaxChars)
}

// truncateBytes cuts s to at most n bytes on a rune boundary, appending an
// ellipsis when it cut anything.
func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
