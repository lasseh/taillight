package analyzer

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// analysisData holds all aggregated data for prompt building.
type analysisData struct {
	Feed               string   // "srvlog", "netlog", or "all".
	Hosts              []string // empty when the run covers all hosts on the feed.
	Period             time.Duration
	PeriodLabel        string // e.g. "24 hours", "7 days".
	PeriodStart        time.Time
	PeriodEnd          time.Time
	TopMsgIDs          []model.MsgIDCount
	SeverityComparison model.SeverityComparison
	TopErrorHosts      []model.HostErrorCount
	NewMsgIDs          []string
	NewMsgIDSamples    map[string]model.SampleMessage // first observed example per new signature.
	EventClusters      []model.EventCluster
	TopPrograms        []model.ProgramCount
	TopFacilities      []model.FacilityCount
	VolumeTimeline     []model.AnalysisVolumeBucket
	VolumeSparkline    string   // total-events sparkline across the period.
	ErrorSparkline     string   // sev≤3 sparkline aligned to VolumeSparkline.
	VolumePeaks        []string // pre-formatted peak descriptions.
	VolumeBucketLabel  string   // e.g. "1 hour", "5 minutes" — describes one sparkline cell.
	JuniperRefs        map[string]model.JuniperNetlogRef
}

const (
	topMsgIDLimit    = 25
	topHostLimit     = 15
	topProgramLimit  = 10
	topFacilityLimit = 8
	clusterWindowMin = 5

	// topMsgIDSampleCount is the number of representative messages attached
	// per top event signature. Two samples give the model both freshness
	// and a hedge against an outlier first example without exploding the
	// prompt budget (25 signatures × 2 samples ≈ 50 lines).
	topMsgIDSampleCount = 2

	// Feed name constants.
	feedNetlog = "netlog"
	feedSrvlog = "srvlog"
	feedAll    = "all"
)

// sparkBlocks is the set of unicode block characters used to render
// sparklines, ordered low → high. Index 0 is reserved for "no data".
var sparkBlocks = [...]rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// pickBucketMinutes chooses a sparkline bucket size that keeps the output
// readable (target 12–48 cells across the period). Hourly granularity is
// the sweet spot for daily reports; weekly reports compress to 6h cells;
// monthly to day cells. Short incident windows drop to 5-minute cells.
func pickBucketMinutes(period time.Duration) int {
	switch {
	case period <= 6*time.Hour:
		return 5
	case period <= 36*time.Hour:
		return 60
	case period <= 8*24*time.Hour:
		return 360
	default:
		return 1440
	}
}

// bucketLabel formats a bucket-minutes value for prompt rendering.
func bucketLabel(minutes int) string {
	switch {
	case minutes%1440 == 0:
		return fmt.Sprintf("%d day", minutes/1440)
	case minutes%60 == 0:
		return fmt.Sprintf("%d hour", minutes/60)
	default:
		return fmt.Sprintf("%d minutes", minutes)
	}
}

// sparkline renders counts as a unicode sparkline scaled to the max value
// in the slice. Empty input returns "". Cells with count 0 render as a
// single space so the model can see the "gap" shape directly.
func sparkline(counts []int64) string {
	if len(counts) == 0 {
		return ""
	}
	var maxv int64
	for _, c := range counts {
		if c > maxv {
			maxv = c
		}
	}
	if maxv == 0 {
		// All zeros: render as blanks rather than a row of ▁ to avoid
		// implying activity.
		return strings.Repeat(" ", len(counts))
	}
	// Steps 1..len(sparkBlocks)-1 are real bars; 0 is the blank cell.
	steps := int64(len(sparkBlocks) - 1)
	var b strings.Builder
	b.Grow(len(counts) * 3) // unicode chars are 3 bytes.
	for _, c := range counts {
		if c <= 0 {
			b.WriteRune(sparkBlocks[0])
			continue
		}
		idx := (c*steps + maxv - 1) / maxv // ceil
		if idx < 1 {
			idx = 1
		}
		b.WriteRune(sparkBlocks[idx])
	}
	return b.String()
}

// topPeakBuckets returns up to n bucket descriptions sorted by error count
// descending (then total descending as tiebreaker). Peaks are formatted as
// e.g. "03:00 (240 err / 1200 total)" using bucketTimeFormat to keep the
// label compact for the rendered prompt.
func topPeakBuckets(buckets []model.AnalysisVolumeBucket, n int, bucketTimeFormat string) []string {
	if len(buckets) == 0 || n <= 0 {
		return nil
	}
	type idxScore struct {
		i           int
		errs, total int64
	}
	scored := make([]idxScore, len(buckets))
	for i, b := range buckets {
		scored[i] = idxScore{i: i, errs: b.ErrorCount, total: b.Total}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].errs != scored[j].errs {
			return scored[i].errs > scored[j].errs
		}
		return scored[i].total > scored[j].total
	})
	if n > len(scored) {
		n = len(scored)
	}
	out := make([]string, 0, n)
	for _, s := range scored[:n] {
		if s.errs == 0 && s.total == 0 {
			continue
		}
		b := buckets[s.i]
		out = append(out, fmt.Sprintf("%s (%d err / %d total)",
			b.Bucket.Format(bucketTimeFormat), s.errs, s.total))
	}
	return out
}

// peakTimeFormat returns the time format string appropriate for a bucket
// granularity — finer buckets need more precision in the peak label.
func peakTimeFormat(bucketMinutes int) string {
	switch {
	case bucketMinutes < 60:
		return "01-02 15:04"
	case bucketMinutes < 1440:
		return "01-02 15:00"
	default:
		return "2006-01-02"
	}
}

// periodLabel returns a short human label for a Run period.
func periodLabel(d time.Duration) string {
	days := int(d.Hours() / 24)
	switch {
	case days >= 30:
		return "30 days"
	case days >= 7:
		return "7 days"
	case d >= 24*time.Hour:
		return "24 hours"
	default:
		return d.String()
	}
}

// gather collects all aggregated data for the analysis period ending at periodEnd.
func (a *Analyzer) gather(ctx context.Context, scope model.AnalysisScope, period time.Duration, periodEnd time.Time) (analysisData, error) {
	periodStart := periodEnd.Add(-period)
	// Baseline = 7 days immediately preceding the current period.
	baselineStart := periodStart.Add(-7 * 24 * time.Hour)

	feed := scope.Feed
	data := analysisData{
		Feed:        feed,
		Hosts:       scope.Hosts,
		Period:      period,
		PeriodLabel: periodLabel(period),
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
	}

	var err error

	a.logger.Info("gathering top msgids", "feed", feed, "scoped", !scope.IsAllHosts())
	data.TopMsgIDs, err = a.store.GetTopMsgIDs(ctx, scope, periodStart, topMsgIDLimit)
	if err != nil {
		return data, err
	}

	a.logger.Info("gathering severity comparison", "feed", feed, "scoped", !scope.IsAllHosts())
	data.SeverityComparison, err = a.store.GetSeverityComparison(ctx, scope, periodStart, baselineStart)
	if err != nil {
		return data, err
	}

	// Normalize current counts to a per-day rate regardless of window length so
	// the percentage-change comparison against the always-daily baseline stays
	// apples-to-apples. Baseline divisor inside the store already yields daily
	// average; for a 24h window this division is a no-op, but for sub-24h
	// incident windows (e.g. 1h) it converts the raw count to a per-day-
	// equivalent rate — otherwise 10 events in the last hour would look
	// "quieter" than a 50/day baseline when it's actually a 5× spike.
	periodDays := period.Hours() / 24
	if periodDays > 0 {
		for i := range data.SeverityComparison.Levels {
			lvl := &data.SeverityComparison.Levels[i]
			lvl.Current /= periodDays
			if lvl.BaselineAvg > 0 {
				lvl.ChangePct = (lvl.Current - lvl.BaselineAvg) / lvl.BaselineAvg * 100
			} else {
				lvl.ChangePct = 0
			}
		}
	}

	// Top error hosts and event clusters are skipped when the run is scoped
	// to specific hosts: the user already told us which hosts to look at, so
	// "which hosts have the most errors" is tautological, and "events
	// correlated across hosts" is degraded to "correlated across the small
	// set you picked." Skipping saves DB roundtrips and avoids feeding the
	// prompt with sections the model would either skip or narrate emptily.
	if scope.IsAllHosts() {
		a.logger.Info("gathering top error hosts", "feed", feed)
		data.TopErrorHosts, err = a.store.GetTopErrorHosts(ctx, scope, periodStart, topHostLimit)
		if err != nil {
			return data, err
		}

		a.logger.Info("gathering event clusters", "feed", feed)
		data.EventClusters, err = a.store.GetEventClusters(ctx, scope, periodStart, clusterWindowMin)
		if err != nil {
			return data, err
		}
	} else {
		a.logger.Info("skipping top error hosts and event clusters under host scope",
			"feed", feed, "hosts", len(scope.Hosts))
	}

	a.logger.Info("gathering new msgids", "feed", feed)
	data.NewMsgIDs, err = a.store.GetNewMsgIDs(ctx, scope, periodStart, baselineStart)
	if err != nil {
		return data, err
	}

	// Attach representative sample messages to every top signature so the
	// prompt carries actual log text — without this the model only sees an
	// event ID and a severity histogram, which for srvlog is rarely enough
	// to reason about what happened.
	if len(data.TopMsgIDs) > 0 {
		topKeys := make([]string, len(data.TopMsgIDs))
		for i, mc := range data.TopMsgIDs {
			topKeys[i] = mc.MsgID
		}
		a.logger.Info("gathering samples for top msgids", "feed", feed, "keys", len(topKeys))
		samples, sampErr := a.store.GetMsgIDSamples(ctx, scope, periodStart, topKeys, topMsgIDSampleCount)
		if sampErr != nil {
			// Best-effort — warn and continue without samples.
			a.logger.Warn("top msgid sample lookup failed, continuing without", "err", sampErr)
		} else {
			for i := range data.TopMsgIDs {
				if s, ok := samples[data.TopMsgIDs[i].MsgID]; ok {
					data.TopMsgIDs[i].Samples = s
				}
			}
		}
	}

	// Volume timeline + sparkline. Bucket size is derived from period
	// length to keep the sparkline within 12–48 cells (readable for the
	// model and human reviewers).
	bucketMinutes := pickBucketMinutes(period)
	data.VolumeBucketLabel = bucketLabel(bucketMinutes)
	a.logger.Info("gathering volume timeline", "feed", feed, "bucket_minutes", bucketMinutes)
	timeline, volErr := a.store.GetVolumeTimeline(ctx, scope, periodStart, periodEnd, bucketMinutes)
	if volErr != nil {
		// Best-effort — the sparkline is a nice-to-have, not load-bearing
		// for the rest of the analysis.
		a.logger.Warn("volume timeline lookup failed, continuing without", "err", volErr)
	} else {
		data.VolumeTimeline = timeline
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

	// Program + facility breakdowns are srvlog-only signals. The store
	// returns nil for netlog so calling unconditionally would also work,
	// but skipping here keeps the log lines truthful about what was
	// queried. Both are best-effort — they enrich the prompt but the
	// report is still useful without them, so a slow or failing query
	// shouldn't kill the whole run.
	if feed != feedNetlog {
		a.logger.Info("gathering top programs", "feed", feed)
		programs, progErr := a.store.GetTopPrograms(ctx, scope, periodStart, topProgramLimit)
		if progErr != nil {
			a.logger.Warn("top programs lookup failed, continuing without", "err", progErr)
		} else {
			data.TopPrograms = programs
		}

		a.logger.Info("gathering top facilities", "feed", feed)
		facilities, facErr := a.store.GetTopFacilities(ctx, scope, periodStart, topFacilityLimit)
		if facErr != nil {
			a.logger.Warn("top facilities lookup failed, continuing without", "err", facErr)
		} else {
			data.TopFacilities = facilities
		}
	}

	// One sample per new signature — "first observed" context turns a
	// dangling event ID into something the model can interpret.
	data.NewMsgIDSamples = make(map[string]model.SampleMessage)
	if len(data.NewMsgIDs) > 0 {
		a.logger.Info("gathering samples for new msgids", "feed", feed, "keys", len(data.NewMsgIDs))
		newSamples, sampErr := a.store.GetMsgIDSamples(ctx, scope, periodStart, data.NewMsgIDs, 1)
		if sampErr != nil {
			a.logger.Warn("new msgid sample lookup failed, continuing without", "err", sampErr)
		} else {
			for k, list := range newSamples {
				if len(list) > 0 {
					data.NewMsgIDSamples[k] = list[0]
				}
			}
		}
	}

	data.JuniperRefs = make(map[string]model.JuniperNetlogRef)

	// Juniper reference data only applies to netlog msgids; skip the lookup
	// entirely for srvlog feeds (the table would return zero matches anyway,
	// but skipping saves a DB roundtrip and clarifies intent).
	if feed == feedNetlog || feed == feedAll {
		msgidNames := make([]string, 0, len(data.TopMsgIDs)+len(data.NewMsgIDs))
		for _, mc := range data.TopMsgIDs {
			msgidNames = append(msgidNames, mc.MsgID)
		}
		msgidNames = append(msgidNames, data.NewMsgIDs...)

		a.logger.Info("looking up juniper references", "count", len(msgidNames))
		refs, lookupErr := a.store.LookupJuniperRefs(ctx, msgidNames)
		if lookupErr != nil {
			// Best-effort — warn and continue.
			a.logger.Warn("juniper ref lookup failed, continuing without", "err", lookupErr)
		} else {
			data.JuniperRefs = refs
		}
	}

	return data, nil
}
