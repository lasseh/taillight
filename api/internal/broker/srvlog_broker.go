package broker

import (
	"log/slog"

	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/model"
)

// SrvlogMessage is the Message type used for srvlog SSE events.
type SrvlogMessage = Message

// SrvlogSubscription is a Subscription parameterized with SrvlogFilter.
type SrvlogSubscription = Subscription[model.SrvlogFilter]

// SrvlogBroker fans out srvlog events to connected SSE clients.
type SrvlogBroker = Broker[model.SrvlogEvent, model.SrvlogFilter]

// NewSrvlogBroker creates a new SrvlogBroker.
func NewSrvlogBroker(logger *slog.Logger) *SrvlogBroker {
	return New[model.SrvlogEvent, model.SrvlogFilter](logger, "srvlog",
		func(e model.SrvlogEvent) int64 { return e.ID },
		BrokerMetrics{
			OnSubscribe:   func() { metrics.SSEClientsActive.Inc() },
			OnUnsubscribe: func() { metrics.SSEClientsActive.Dec() },
			OnBroadcast:   func() { metrics.EventsBroadcastTotal.Inc() },
			OnDrop:        func() { metrics.EventsDroppedTotal.Inc() },
		},
	)
}
