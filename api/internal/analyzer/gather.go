package analyzer

import (
	"context"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// analysisData holds all aggregated data for prompt building.
type analysisData struct {
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
)

// gather collects all aggregated data for the analysis period.
func (a *Analyzer) gather(ctx context.Context) (analysisData, error) {
	now := time.Now().UTC()
	periodEnd := now
	periodStart := now.Add(-24 * time.Hour)
	baselineStart := now.Add(-8 * 24 * time.Hour) // 7 days before period start

	data := analysisData{
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
	}

	var err error

	a.logger.Info("gathering top msgids")
	data.TopMsgIDs, err = a.store.GetTopMsgIDs(ctx, periodStart, topMsgIDLimit)
	if err != nil {
		return data, err
	}

	a.logger.Info("gathering severity comparison")
	data.SeverityComparison, err = a.store.GetSeverityComparison(ctx, periodStart, baselineStart)
	if err != nil {
		return data, err
	}

	a.logger.Info("gathering top error hosts")
	data.TopErrorHosts, err = a.store.GetTopErrorHosts(ctx, periodStart, topHostLimit)
	if err != nil {
		return data, err
	}

	a.logger.Info("gathering new msgids")
	data.NewMsgIDs, err = a.store.GetNewMsgIDs(ctx, periodStart, baselineStart)
	if err != nil {
		return data, err
	}

	a.logger.Info("gathering event clusters")
	data.EventClusters, err = a.store.GetEventClusters(ctx, periodStart, clusterWindowMin)
	if err != nil {
		return data, err
	}

	// Juniper ref lookup is best-effort — warn and continue on failure.
	msgidNames := make([]string, 0, len(data.TopMsgIDs))
	for _, mc := range data.TopMsgIDs {
		msgidNames = append(msgidNames, mc.MsgID)
	}
	msgidNames = append(msgidNames, data.NewMsgIDs...)

	a.logger.Info("looking up juniper references", "count", len(msgidNames))
	data.JuniperRefs, err = a.store.LookupJuniperRefs(ctx, msgidNames)
	if err != nil {
		a.logger.Warn("juniper ref lookup failed, continuing without", "err", err)
		data.JuniperRefs = make(map[string]model.JuniperNetlogRef)
	}

	return data, nil
}
