// Package metrics provides Prometheus metrics for the taillight backend.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTPRequestsTotal counts HTTP requests by method, path pattern, and status code.
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "taillight",
		Name:      "http_requests_total",
		Help:      "Total number of HTTP requests.",
	}, []string{"method", "path", "status"})

	// HTTPRequestDuration tracks HTTP request latency by method and path pattern.
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "taillight",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path"})

	// SSEClientsActive tracks the current number of connected SSE clients.
	SSEClientsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "taillight",
		Name:      "sse_clients_active",
		Help:      "Number of currently connected SSE clients.",
	})

	// EventsBroadcastTotal counts events broadcast to SSE clients.
	EventsBroadcastTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "taillight",
		Name:      "events_broadcast_total",
		Help:      "Total number of events broadcast.",
	})

	// EventsDroppedTotal counts events dropped due to slow clients.
	EventsDroppedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "taillight",
		Name:      "events_dropped_total",
		Help:      "Total number of events dropped for slow clients.",
	})

	// ListenerReconnectsTotal counts LISTEN/NOTIFY reconnection attempts.
	ListenerReconnectsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "taillight",
		Name:      "listener_reconnects_total",
		Help:      "Total number of listener reconnection attempts.",
	})

	// Applog metrics.

	// ApplogSSEClientsActive tracks the current number of connected applog SSE clients.
	ApplogSSEClientsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "taillight",
		Name:      "applog_sse_clients_active",
		Help:      "Number of currently connected applog SSE clients.",
	})

	// ApplogEventsBroadcastTotal counts applog events broadcast to SSE clients.
	ApplogEventsBroadcastTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "taillight",
		Name:      "applog_events_broadcast_total",
		Help:      "Total number of applog events broadcast.",
	})

	// ApplogEventsDroppedTotal counts applog events dropped due to slow clients.
	ApplogEventsDroppedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "taillight",
		Name:      "applog_events_dropped_total",
		Help:      "Total number of applog events dropped for slow clients.",
	})

	// ApplogIngestTotal counts total log entries ingested.
	ApplogIngestTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "taillight",
		Name:      "applog_ingest_total",
		Help:      "Total number of log entries ingested.",
	})

	// ApplogIngestBatchesTotal counts ingest POST requests.
	ApplogIngestBatchesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "taillight",
		Name:      "applog_ingest_batches_total",
		Help:      "Total number of ingest batch requests.",
	})

	// ApplogIngestErrorsTotal counts failed ingest requests.
	ApplogIngestErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "taillight",
		Name:      "applog_ingest_errors_total",
		Help:      "Total number of failed ingest requests.",
	})

	// NotificationsReceivedTotal counts LISTEN/NOTIFY notifications received.
	NotificationsReceivedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "taillight",
		Name:      "notifications_received_total",
		Help:      "Total number of LISTEN/NOTIFY notifications received.",
	}, []string{"channel"})

	// DB connection pool metrics.

	// DBPoolActiveConns tracks active connections in the pool.
	DBPoolActiveConns = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "taillight",
		Name:      "db_pool_active_conns",
		Help:      "Number of active connections in the database pool.",
	})

	// DBPoolIdleConns tracks idle connections in the pool.
	DBPoolIdleConns = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "taillight",
		Name:      "db_pool_idle_conns",
		Help:      "Number of idle connections in the database pool.",
	})

	// DBPoolTotalConns tracks total connections in the pool.
	DBPoolTotalConns = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "taillight",
		Name:      "db_pool_total_conns",
		Help:      "Total number of connections in the database pool.",
	})

	// Analysis metrics.

	// AnalysisRunsTotal counts analysis runs by status.
	AnalysisRunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "taillight",
		Name:      "analysis_runs_total",
		Help:      "Total number of analysis runs by status.",
	}, []string{"status"})

	// AnalysisDurationSeconds tracks analysis run duration.
	AnalysisDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "taillight",
		Name:      "analysis_duration_seconds",
		Help:      "Duration of analysis runs in seconds.",
		Buckets:   []float64{30, 60, 120, 300, 600},
	})

	// Notification metrics.

	// NotifRulesEvaluatedTotal counts events × rules evaluated.
	NotifRulesEvaluatedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "taillight",
		Name:      "notification_rules_evaluated_total",
		Help:      "Total number of rule evaluations against events.",
	})

	// NotifRulesMatchedTotal counts rule matches.
	NotifRulesMatchedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "taillight",
		Name:      "notification_rules_matched_total",
		Help:      "Total number of rule matches.",
	})

	// NotifDispatchedTotal counts notifications sent to the dispatch queue.
	NotifDispatchedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "taillight",
		Name:      "notification_dispatched_total",
		Help:      "Total number of notifications dispatched.",
	})

	// NotifSentTotal counts delivery outcomes by channel type and status.
	NotifSentTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "taillight",
		Name:      "notification_sent_total",
		Help:      "Total number of notification send attempts by outcome.",
	}, []string{"channel_type", "status"})

	// NotifSuppressedTotal counts suppressed notifications by reason.
	NotifSuppressedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "taillight",
		Name:      "notification_suppressed_total",
		Help:      "Total number of suppressed notifications by reason.",
	}, []string{"reason"})

	// NotifSendDuration tracks notification send latency.
	NotifSendDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "taillight",
		Name:      "notification_send_duration_seconds",
		Help:      "Duration of notification send operations in seconds.",
		Buckets:   prometheus.DefBuckets,
	})

	// NotifDispatchQueueLen tracks current dispatch queue depth.
	NotifDispatchQueueLen = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "taillight",
		Name:      "notification_dispatch_queue_length",
		Help:      "Current number of notifications in the dispatch queue.",
	})
)
