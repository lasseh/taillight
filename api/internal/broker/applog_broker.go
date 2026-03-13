package broker

import (
	"log/slog"

	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/model"
)

// AppLogMessage is the Message type used for applog SSE events.
type AppLogMessage = Message

// AppLogSubscription is a Subscription parameterized with AppLogFilter.
type AppLogSubscription = Subscription[model.AppLogFilter]

// AppLogBroker fans out log events to connected SSE clients.
type AppLogBroker = Broker[model.AppLogEvent, model.AppLogFilter]

// NewAppLogBroker creates a new AppLogBroker.
func NewAppLogBroker(logger *slog.Logger) *AppLogBroker {
	return New[model.AppLogEvent, model.AppLogFilter](logger, "applog",
		func(e model.AppLogEvent) int64 { return e.ID },
		BrokerMetrics{
			OnSubscribe:   func() { metrics.AppLogSSEClientsActive.Inc() },
			OnUnsubscribe: func() { metrics.AppLogSSEClientsActive.Dec() },
			OnBroadcast:   func() { metrics.AppLogEventsBroadcastTotal.Inc() },
			OnDrop:        func() { metrics.AppLogEventsDroppedTotal.Inc() },
		},
	)
}
