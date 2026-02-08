package model

import "time"

// MetricsSnapshot represents a single row in the taillight_metrics hypertable.
type MetricsSnapshot struct {
	CollectedAt           time.Time `json:"collected_at"`
	SSEClientsSyslog      int       `json:"sse_clients_syslog"`
	SSEClientsApplog      int       `json:"sse_clients_applog"`
	DBPoolActive          int       `json:"db_pool_active"`
	DBPoolIdle            int       `json:"db_pool_idle"`
	DBPoolTotal           int       `json:"db_pool_total"`
	EventsBroadcast       int64     `json:"events_broadcast"`
	EventsDropped         int64     `json:"events_dropped"`
	ApplogEventsBroadcast int64     `json:"applog_events_broadcast"`
	ApplogEventsDropped   int64     `json:"applog_events_dropped"`
	ApplogIngestTotal     int64     `json:"applog_ingest_total"`
	ApplogIngestErrors    int64     `json:"applog_ingest_errors"`
	ListenerReconnects    int64     `json:"listener_reconnects"`
}

// MetricsSummary contains aggregated KPIs for a time range.
type MetricsSummary struct {
	// Latest gauge values.
	SSEClientsSyslog int `json:"sse_clients_syslog"`
	SSEClientsApplog int `json:"sse_clients_applog"`
	DBPoolActive     int `json:"db_pool_active"`
	DBPoolIdle       int `json:"db_pool_idle"`
	DBPoolTotal      int `json:"db_pool_total"`

	// Counter deltas over the range (max - min).
	EventsBroadcast       int64 `json:"events_broadcast"`
	EventsDropped         int64 `json:"events_dropped"`
	ApplogEventsBroadcast int64 `json:"applog_events_broadcast"`
	ApplogEventsDropped   int64 `json:"applog_events_dropped"`
	ApplogIngestTotal     int64 `json:"applog_ingest_total"`
	ApplogIngestErrors    int64 `json:"applog_ingest_errors"`
	ListenerReconnects    int64 `json:"listener_reconnects"`

	// Computed rates (per minute).
	EventsRate float64 `json:"events_rate"`
	IngestRate float64 `json:"ingest_rate"`
}

// MetricsTimeSeries is one time-bucketed data point.
type MetricsTimeSeries struct {
	Time  time.Time `json:"time"`
	Value float64   `json:"value"`
}
