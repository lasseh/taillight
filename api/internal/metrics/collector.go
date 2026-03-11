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
		SSEClientsAppLog:      int(gaugeValue(AppLogSSEClientsActive)),
		DBPoolActive:          int(gaugeValue(DBPoolActiveConns)),
		DBPoolIdle:            int(gaugeValue(DBPoolIdleConns)),
		DBPoolTotal:           int(gaugeValue(DBPoolTotalConns)),
		EventsBroadcast:       int64(counterValue(EventsBroadcastTotal)),
		EventsDropped:         int64(counterValue(EventsDroppedTotal)),
		AppLogEventsBroadcast: int64(counterValue(AppLogEventsBroadcastTotal)),
		AppLogEventsDropped:   int64(counterValue(AppLogEventsDroppedTotal)),
		AppLogIngestTotal:     int64(counterValue(AppLogIngestTotal)),
		AppLogIngestErrors:    int64(counterValue(AppLogIngestErrorsTotal)),
		ListenerReconnects:    int64(counterValue(ListenerReconnectsTotal)),
	}
}
