package metrics

import (
	"github.com/lasseh/taillight/internal/model"
	dto "github.com/prometheus/client_model/go"
)

// gaugeValue extracts the current value from a Gauge metric.
func gaugeValue(g interface{ Write(*dto.Metric) error }) float64 {
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		return 0
	}
	if m.Gauge != nil {
		return *m.Gauge.Value
	}
	return 0
}

// counterValue extracts the current value from a Counter metric.
func counterValue(c interface{ Write(*dto.Metric) error }) float64 {
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		return 0
	}
	if m.Counter != nil {
		return *m.Counter.Value
	}
	return 0
}

// Snapshot reads all Prometheus metrics and returns a MetricsSnapshot.
func Snapshot() model.MetricsSnapshot {
	return model.MetricsSnapshot{
		SSEClientsSyslog:      int(gaugeValue(SSEClientsActive)),
		SSEClientsApplog:      int(gaugeValue(ApplogSSEClientsActive)),
		DBPoolActive:          int(gaugeValue(DBPoolActiveConns)),
		DBPoolIdle:            int(gaugeValue(DBPoolIdleConns)),
		DBPoolTotal:           int(gaugeValue(DBPoolTotalConns)),
		EventsBroadcast:       int64(counterValue(EventsBroadcastTotal)),
		EventsDropped:         int64(counterValue(EventsDroppedTotal)),
		ApplogEventsBroadcast: int64(counterValue(ApplogEventsBroadcastTotal)),
		ApplogEventsDropped:   int64(counterValue(ApplogEventsDroppedTotal)),
		ApplogIngestTotal:     int64(counterValue(ApplogIngestTotal)),
		ApplogIngestErrors:    int64(counterValue(ApplogIngestErrorsTotal)),
		ListenerReconnects:    int64(counterValue(ListenerReconnectsTotal)),
	}
}
