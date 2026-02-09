package broker

import (
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/model"
)

// ApplogMessage carries a pre-marshaled log event with its ID for SSE id: field support.
type ApplogMessage struct {
	ID   int64
	Data []byte
}

// ApplogSubscription holds a client's event channel and filter criteria.
type ApplogSubscription struct {
	ch     chan ApplogMessage
	filter model.AppLogFilter
}

// Chan returns the event channel for reading.
func (s *ApplogSubscription) Chan() <-chan ApplogMessage {
	return s.ch
}

// ApplogBroker fans out log events to connected SSE clients, applying per-client filters.
type ApplogBroker struct {
	mu          sync.RWMutex
	subscribers map[*ApplogSubscription]struct{}
	logger      *slog.Logger
}

// NewApplogBroker creates a new ApplogBroker.
func NewApplogBroker(logger *slog.Logger) *ApplogBroker {
	return &ApplogBroker{
		subscribers: make(map[*ApplogSubscription]struct{}),
		logger:      logger,
	}
}

// Subscribe registers a new client with the given filter and returns its subscription.
// Returns ErrTooManySubscribers if the broker has reached its connection limit.
func (b *ApplogBroker) Subscribe(filter model.AppLogFilter) (*ApplogSubscription, error) {
	sub := &ApplogSubscription{
		ch:     make(chan ApplogMessage, subscriptionBufferSize),
		filter: filter,
	}
	b.mu.Lock()
	if len(b.subscribers) >= maxSubscribers {
		b.mu.Unlock()
		return nil, ErrTooManySubscribers
	}
	b.subscribers[sub] = struct{}{}
	b.mu.Unlock()
	metrics.ApplogSSEClientsActive.Inc()
	b.logger.Debug("applog client subscribed", "total", b.Len())
	return sub, nil
}

// Unsubscribe removes a client and closes its channel.
// Safe to call after Shutdown — if the subscription was already removed, this is a no-op.
func (b *ApplogBroker) Unsubscribe(sub *ApplogSubscription) {
	b.mu.Lock()
	if _, ok := b.subscribers[sub]; !ok {
		b.mu.Unlock()
		return
	}
	delete(b.subscribers, sub)
	close(sub.ch)
	b.mu.Unlock()
	metrics.ApplogSSEClientsActive.Dec()
	b.logger.Debug("applog client unsubscribed", "total", b.Len())
}

// Len returns the number of connected subscribers.
func (b *ApplogBroker) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// Shutdown closes all subscriber channels, causing SSE handlers to return.
func (b *ApplogBroker) Shutdown() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subscribers {
		close(sub.ch)
		delete(b.subscribers, sub)
	}
	b.logger.Info("applog broker shut down")
}

// Broadcast sends an event to all subscribers whose filter matches.
func (b *ApplogBroker) Broadcast(event model.AppLogEvent) {
	if b.Len() == 0 {
		return
	}

	data, err := json.Marshal(event)
	if err != nil {
		b.logger.Error("marshal applog event", "err", err)
		return
	}

	msg := ApplogMessage{ID: event.ID, Data: data}
	metrics.ApplogEventsBroadcastTotal.Inc()

	b.mu.RLock()
	defer b.mu.RUnlock()

	for sub := range b.subscribers {
		if !sub.filter.Matches(event) {
			continue
		}
		select {
		case sub.ch <- msg:
		default:
			metrics.ApplogEventsDroppedTotal.Inc()
			b.logger.Debug("dropped applog event for slow client", "event_id", event.ID)
		}
	}
}
