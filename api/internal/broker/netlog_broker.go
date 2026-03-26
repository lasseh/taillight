package broker

import (
	"log/slog"

	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/model"
)

// NetlogMessage is the Message type used for netlog SSE events.
type NetlogMessage = Message

// NetlogSubscription is a Subscription parameterized with NetlogFilter.
type NetlogSubscription = Subscription[model.NetlogFilter]

// NetlogBroker fans out netlog events to connected SSE clients.
type NetlogBroker = Broker[model.NetlogEvent, model.NetlogFilter]

// NewNetlogBroker creates a new NetlogBroker.
func NewNetlogBroker(logger *slog.Logger) *NetlogBroker {
	return New[model.NetlogEvent, model.NetlogFilter](logger, "netlog",
		func(e model.NetlogEvent) int64 { return e.ID },
		BrokerMetrics{
			OnSubscribe:   func() { metrics.NetlogSSEClientsActive.Inc() },
			OnUnsubscribe: func() { metrics.NetlogSSEClientsActive.Dec() },
			OnBroadcast:   func() { metrics.NetlogEventsBroadcastTotal.Inc() },
			OnDrop:        func() { metrics.NetlogEventsDroppedTotal.Inc() },
		},
	)
}
