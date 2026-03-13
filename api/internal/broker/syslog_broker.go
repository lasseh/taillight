package broker

import (
	"log/slog"

	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/model"
)

// SyslogMessage is the Message type used for syslog SSE events.
type SyslogMessage = Message

// SyslogSubscription is a Subscription parameterized with SyslogFilter.
type SyslogSubscription = Subscription[model.SyslogFilter]

// SyslogBroker fans out syslog events to connected SSE clients.
type SyslogBroker = Broker[model.SyslogEvent, model.SyslogFilter]

// NewSyslogBroker creates a new SyslogBroker.
func NewSyslogBroker(logger *slog.Logger) *SyslogBroker {
	return New[model.SyslogEvent, model.SyslogFilter](logger, "syslog",
		func(e model.SyslogEvent) int64 { return e.ID },
		BrokerMetrics{
			OnSubscribe:   func() { metrics.SSEClientsActive.Inc() },
			OnUnsubscribe: func() { metrics.SSEClientsActive.Dec() },
			OnBroadcast:   func() { metrics.EventsBroadcastTotal.Inc() },
			OnDrop:        func() { metrics.EventsDroppedTotal.Inc() },
		},
	)
}
