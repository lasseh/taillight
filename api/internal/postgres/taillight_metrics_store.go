package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// allowedMetricsFields is a whitelist of columns that can be queried for time series.
var allowedMetricsFields = map[string]struct{}{
	"sse_clients_syslog":      {},
	"sse_clients_applog":      {},
	"db_pool_active":          {},
	"db_pool_idle":            {},
	"db_pool_total":           {},
	"events_broadcast":        {},
	"events_dropped":          {},
	"applog_events_broadcast": {},
	"applog_events_dropped":   {},
	"applog_ingest_total":     {},
	"applog_ingest_errors":    {},
	"listener_reconnects":     {},
}

// counterMetricsFields are cumulative counters — time series uses MAX()-MIN() per bucket.
var counterMetricsFields = map[string]struct{}{
	"events_broadcast":        {},
	"events_dropped":          {},
	"applog_events_broadcast": {},
	"applog_events_dropped":   {},
	"applog_ingest_total":     {},
	"applog_ingest_errors":    {},
	"listener_reconnects":     {},
}

// InsertMetricsSnapshot writes a single metrics snapshot to the hypertable.
func (s *Store) InsertMetricsSnapshot(ctx context.Context, snap model.MetricsSnapshot) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO taillight_metrics (
			sse_clients_syslog, sse_clients_applog,
			db_pool_active, db_pool_idle, db_pool_total,
			events_broadcast, events_dropped,
			applog_events_broadcast, applog_events_dropped,
			applog_ingest_total, applog_ingest_errors,
			listener_reconnects
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		snap.SSEClientsSyslog, snap.SSEClientsApplog,
		snap.DBPoolActive, snap.DBPoolIdle, snap.DBPoolTotal,
		snap.EventsBroadcast, snap.EventsDropped,
		snap.ApplogEventsBroadcast, snap.ApplogEventsDropped,
		snap.ApplogIngestTotal, snap.ApplogIngestErrors,
		snap.ListenerReconnects,
	)
	if err != nil {
		return fmt.Errorf("insert metrics snapshot: %w", err)
	}
	return nil
}

// GetMetricsSummary returns aggregated KPIs for the given time range.
// Gauges use the latest snapshot; counters use MAX()-MIN() for deltas.
func (s *Store) GetMetricsSummary(ctx context.Context, rangeDur time.Duration) (model.MetricsSummary, error) {
	since := time.Now().UTC().Add(-rangeDur)

	query := `SELECT
		-- Latest gauge values (from most recent snapshot).
		(SELECT sse_clients_syslog FROM taillight_metrics WHERE collected_at >= $1 ORDER BY collected_at DESC LIMIT 1),
		(SELECT sse_clients_applog FROM taillight_metrics WHERE collected_at >= $1 ORDER BY collected_at DESC LIMIT 1),
		(SELECT db_pool_active FROM taillight_metrics WHERE collected_at >= $1 ORDER BY collected_at DESC LIMIT 1),
		(SELECT db_pool_idle FROM taillight_metrics WHERE collected_at >= $1 ORDER BY collected_at DESC LIMIT 1),
		(SELECT db_pool_total FROM taillight_metrics WHERE collected_at >= $1 ORDER BY collected_at DESC LIMIT 1),
		-- Counter deltas (max - min over range).
		COALESCE(MAX(events_broadcast) - MIN(events_broadcast), 0),
		COALESCE(MAX(events_dropped) - MIN(events_dropped), 0),
		COALESCE(MAX(applog_events_broadcast) - MIN(applog_events_broadcast), 0),
		COALESCE(MAX(applog_events_dropped) - MIN(applog_events_dropped), 0),
		COALESCE(MAX(applog_ingest_total) - MIN(applog_ingest_total), 0),
		COALESCE(MAX(applog_ingest_errors) - MIN(applog_ingest_errors), 0),
		COALESCE(MAX(listener_reconnects) - MIN(listener_reconnects), 0)
	FROM taillight_metrics
	WHERE collected_at >= $1`

	var summary model.MetricsSummary
	err := s.pool.QueryRow(ctx, query, since).Scan(
		&summary.SSEClientsSyslog,
		&summary.SSEClientsApplog,
		&summary.DBPoolActive,
		&summary.DBPoolIdle,
		&summary.DBPoolTotal,
		&summary.EventsBroadcast,
		&summary.EventsDropped,
		&summary.ApplogEventsBroadcast,
		&summary.ApplogEventsDropped,
		&summary.ApplogIngestTotal,
		&summary.ApplogIngestErrors,
		&summary.ListenerReconnects,
	)
	if err != nil {
		return model.MetricsSummary{}, fmt.Errorf("metrics summary query: %w", err)
	}

	// Compute rates (per minute).
	minutes := rangeDur.Minutes()
	if minutes > 0 {
		if summary.EventsBroadcast > 0 {
			summary.EventsRate = float64(summary.EventsBroadcast) / minutes
		}
		if summary.ApplogIngestTotal > 0 {
			summary.IngestRate = float64(summary.ApplogIngestTotal) / minutes
		}
	}

	return summary, nil
}

// GetMetricsTimeSeries returns time-bucketed values for a single metric field.
// Gauge fields return the AVG per bucket; counter fields return MAX()-MIN() per bucket.
func (s *Store) GetMetricsTimeSeries(ctx context.Context, field string, interval model.VolumeInterval, rangeDur time.Duration) ([]model.MetricsTimeSeries, error) {
	if _, ok := allowedMetricsFields[field]; !ok {
		return nil, fmt.Errorf("disallowed metrics field: %s", field)
	}
	if !interval.IsValid() {
		return nil, fmt.Errorf("invalid volume interval: %s", interval)
	}

	since := time.Now().UTC().Add(-rangeDur)

	// Counter fields: delta per bucket (max - min).
	// Gauge fields: average per bucket.
	var agg string
	if _, isCounter := counterMetricsFields[field]; isCounter {
		agg = fmt.Sprintf("MAX(%s) - MIN(%s)", field, field)
	} else {
		agg = fmt.Sprintf("AVG(%s)", field)
	}

	query := fmt.Sprintf(
		`SELECT time_bucket($1::interval, collected_at) AS bucket,
		        %s AS val
		 FROM taillight_metrics
		 WHERE collected_at >= $2
		 GROUP BY bucket
		 ORDER BY bucket ASC`, agg)

	rows, err := s.pool.Query(ctx, query, interval.String(), since)
	if err != nil {
		return nil, fmt.Errorf("metrics time series query: %w", err)
	}
	defer rows.Close()

	var series []model.MetricsTimeSeries
	for rows.Next() {
		var ts model.MetricsTimeSeries
		if err := rows.Scan(&ts.Time, &ts.Value); err != nil {
			return nil, fmt.Errorf("scan metrics time series: %w", err)
		}
		series = append(series, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metrics time series rows: %w", err)
	}

	return series, nil
}
