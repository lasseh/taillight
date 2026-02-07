// Package broker provides SSE fan-out with per-client filtering.
package broker

import (
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/model"
)

// subscriptionBufferSize is the channel buffer size for each SSE client subscription.
// A larger buffer allows clients to briefly fall behind without dropping events.
const subscriptionBufferSize = 64

// SyslogMessage carries a pre-marshaled event with its ID for SSE id: field support.
type SyslogMessage struct {
	ID   int64
	Data []byte
}

// SyslogSubscription holds a client's event channel and filter criteria.
type SyslogSubscription struct {
	ch     chan SyslogMessage
	filter model.SyslogFilter
}

// Chan returns the event channel for reading.
func (s *SyslogSubscription) Chan() <-chan SyslogMessage {
	return s.ch
}

// SyslogBroker fans out syslog events to connected SSE clients, applying per-client filters.
type SyslogBroker struct {
	mu          sync.RWMutex
	subscribers map[*SyslogSubscription]struct{}
	logger      *slog.Logger
}

// NewSyslogBroker creates a new SyslogBroker.
func NewSyslogBroker(logger *slog.Logger) *SyslogBroker {
	return &SyslogBroker{
		subscribers: make(map[*SyslogSubscription]struct{}),
		logger:      logger,
	}
}

// Subscribe registers a new client with the given filter and returns its subscription.
func (b *SyslogBroker) Subscribe(filter model.SyslogFilter) *SyslogSubscription {
	sub := &SyslogSubscription{
		ch:     make(chan SyslogMessage, subscriptionBufferSize),
		filter: filter,
	}
	b.mu.Lock()
	b.subscribers[sub] = struct{}{}
	b.mu.Unlock()
	metrics.SSEClientsActive.Inc()
	b.logger.Debug("client subscribed", "total", b.Len())
	return sub
}

// Unsubscribe removes a client and closes its channel.
// Safe to call after Shutdown — if the subscription was already removed, this is a no-op.
func (b *SyslogBroker) Unsubscribe(sub *SyslogSubscription) {
	b.mu.Lock()
	if _, ok := b.subscribers[sub]; !ok {
		b.mu.Unlock()
		return
	}
	delete(b.subscribers, sub)
	close(sub.ch)
	b.mu.Unlock()
	metrics.SSEClientsActive.Dec()
	b.logger.Debug("client unsubscribed", "total", b.Len())
}

// Len returns the number of connected subscribers.
func (b *SyslogBroker) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// Shutdown closes all subscriber channels, causing SSE handlers to return.
func (b *SyslogBroker) Shutdown() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subscribers {
		close(sub.ch)
		delete(b.subscribers, sub)
	}
	b.logger.Info("syslog broker shut down")
}

// Broadcast sends an event to all subscribers whose filter matches.
func (b *SyslogBroker) Broadcast(event model.SyslogEvent) {
	if b.Len() == 0 {
		return
	}

	data, err := json.Marshal(event)
	if err != nil {
		b.logger.Error("marshal event", "err", err)
		return
	}

	msg := SyslogMessage{ID: event.ID, Data: data}
	metrics.EventsBroadcastTotal.Inc()

	b.mu.RLock()
	defer b.mu.RUnlock()

	for sub := range b.subscribers {
		if !sub.filter.Matches(event) {
			continue
		}
		select {
		case sub.ch <- msg:
		default:
			metrics.EventsDroppedTotal.Inc()
			b.logger.Warn("dropped event for slow client", "event_id", event.ID)
		}
	}
}
