package analyzer

import (
	"context"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// analysisData holds all aggregated data for prompt building.
type analysisData struct {
	Feed               string // "srvlog", "netlog", or "all".
	Period             time.Duration
	PeriodLabel        string // e.g. "24 hours", "7 days".
	PeriodStart        time.Time
	PeriodEnd          time.Time
	TopMsgIDs          []model.MsgIDCount
	SeverityComparison model.SeverityComparison
	TopErrorHosts      []model.HostErrorCount
	NewMsgIDs          []string
	EventClusters      []model.EventCluster
	JuniperRefs        map[string]model.JuniperNetlogRef
}

const (
	topMsgIDLimit    = 25
	topHostLimit     = 15
	clusterWindowMin = 5

	// Feed name constants.
	feedNetlog = "netlog"
	feedSrvlog = "srvlog"
	feedAll    = "all"
)

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
func (a *Analyzer) gather(ctx context.Context, feed string, period time.Duration, periodEnd time.Time) (analysisData, error) {
	periodStart := periodEnd.Add(-period)
	// Baseline = 7 days immediately preceding the current period.
	baselineStart := periodStart.Add(-7 * 24 * time.Hour)

	data := analysisData{
		Feed:        feed,
		Period:      period,
		PeriodLabel: periodLabel(period),
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
	}

	var err error

	a.logger.Info("gathering top msgids", "feed", feed)
	data.TopMsgIDs, err = a.store.GetTopMsgIDs(ctx, feed, periodStart, topMsgIDLimit)
	if err != nil {
		return data, err
	}

	a.logger.Info("gathering severity comparison", "feed", feed)
	data.SeverityComparison, err = a.store.GetSeverityComparison(ctx, feed, periodStart, baselineStart)
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

	a.logger.Info("gathering top error hosts", "feed", feed)
	data.TopErrorHosts, err = a.store.GetTopErrorHosts(ctx, feed, periodStart, topHostLimit)
	if err != nil {
		return data, err
	}

	a.logger.Info("gathering new msgids", "feed", feed)
	data.NewMsgIDs, err = a.store.GetNewMsgIDs(ctx, feed, periodStart, baselineStart)
	if err != nil {
		return data, err
	}

	a.logger.Info("gathering event clusters", "feed", feed)
	data.EventClusters, err = a.store.GetEventClusters(ctx, feed, periodStart, clusterWindowMin)
	if err != nil {
		return data, err
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
