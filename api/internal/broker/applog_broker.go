package broker

import (
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/model"
)

// AppLogMessage carries a pre-marshaled log event with its ID for SSE id: field support.
type AppLogMessage struct {
	ID   int64
	Data []byte
}

// AppLogSubscription holds a client's event channel and filter criteria.
type AppLogSubscription struct {
	ch     chan AppLogMessage
	filter model.AppLogFilter
}

// Chan returns the event channel for reading.
func (s *AppLogSubscription) Chan() <-chan AppLogMessage {
	return s.ch
}

// AppLogBroker fans out log events to connected SSE clients, applying per-client filters.
type AppLogBroker struct {
	mu          sync.RWMutex
	subscribers map[*AppLogSubscription]struct{}
	logger      *slog.Logger
}

// NewAppLogBroker creates a new AppLogBroker.
func NewAppLogBroker(logger *slog.Logger) *AppLogBroker {
	return &AppLogBroker{
		subscribers: make(map[*AppLogSubscription]struct{}),
		logger:      logger,
	}
}

// Subscribe registers a new client with the given filter and returns its subscription.
// Returns ErrTooManySubscribers if the broker has reached its connection limit.
func (b *AppLogBroker) Subscribe(filter model.AppLogFilter) (*AppLogSubscription, error) {
	sub := &AppLogSubscription{
		ch:     make(chan AppLogMessage, subscriptionBufferSize),
		filter: filter,
	}
	b.mu.Lock()
	if len(b.subscribers) >= maxSubscribers {
		b.mu.Unlock()
		return nil, ErrTooManySubscribers
	}
	b.subscribers[sub] = struct{}{}
	b.mu.Unlock()
	metrics.AppLogSSEClientsActive.Inc()
	b.logger.Debug("applog client subscribed", "total", b.Len())
	return sub, nil
}

// Unsubscribe removes a client and closes its channel.
// Safe to call after Shutdown — if the subscription was already removed, this is a no-op.
func (b *AppLogBroker) Unsubscribe(sub *AppLogSubscription) {
	b.mu.Lock()
	if _, ok := b.subscribers[sub]; !ok {
		b.mu.Unlock()
		return
	}
	delete(b.subscribers, sub)
	close(sub.ch)
	b.mu.Unlock()
	metrics.AppLogSSEClientsActive.Dec()
	b.logger.Debug("applog client unsubscribed", "total", b.Len())
}

// Len returns the number of connected subscribers.
func (b *AppLogBroker) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// Shutdown closes all subscriber channels, causing SSE handlers to return.
func (b *AppLogBroker) Shutdown() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subscribers {
		close(sub.ch)
		delete(b.subscribers, sub)
	}
	b.logger.Info("applog broker shut down")
}

// Broadcast sends an event to all subscribers whose filter matches.
func (b *AppLogBroker) Broadcast(event model.AppLogEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		b.logger.Error("marshal applog event", "err", err)
		return
	}

	msg := AppLogMessage{ID: event.ID, Data: data}
	metrics.AppLogEventsBroadcastTotal.Inc()

	b.mu.RLock()
	defer b.mu.RUnlock()

	for sub := range b.subscribers {
		if !sub.filter.Matches(event) {
			continue
		}
		select {
		case sub.ch <- msg:
		default:
			metrics.AppLogEventsDroppedTotal.Inc()
			b.logger.Debug("dropped applog event for slow client", "event_id", event.ID)
		}
	}
}
