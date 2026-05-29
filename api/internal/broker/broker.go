// Package broker provides SSE fan-out with per-client filtering.
package broker

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
)

const (
	// subscriptionBufferSize is the channel buffer size for each SSE client subscription.
	// A larger buffer allows clients to briefly fall behind without dropping events.
	subscriptionBufferSize = 512

	// maxSubscribers is the maximum number of concurrent SSE clients per broker.
	// Prevents memory exhaustion from too many open connections.
	maxSubscribers = 1000

	// maxSubscribersPerClient caps concurrent connections from a single client
	// key (typically the source IP). Prevents one client from exhausting the
	// global pool and denying SSE to everyone else.
	maxSubscribersPerClient = 20
)

// ErrTooManySubscribers is returned when the broker has reached its global
// connection limit.
var ErrTooManySubscribers = fmt.Errorf("too many SSE subscribers (max %d)", maxSubscribers)

// ErrTooManyClientSubscribers is returned when a single client key has reached
// its per-client connection limit.
var ErrTooManyClientSubscribers = fmt.Errorf("too many SSE subscribers for client (max %d)", maxSubscribersPerClient)

// Message carries a pre-marshaled event with its ID for SSE id: field support.
type Message struct {
	ID   int64
	Data []byte
}

// BrokerMetrics provides metric callbacks so the generic broker can update
// domain-specific Prometheus counters without importing the metrics package.
type BrokerMetrics struct {
	OnSubscribe   func()
	OnUnsubscribe func()
	OnBroadcast   func()
	OnDrop        func()
}

// Subscription holds a client's event channel and filter criteria.
type Subscription[F any] struct {
	ch        chan Message
	filter    F
	clientKey string
}

// Chan returns the event channel for reading.
func (s *Subscription[F]) Chan() <-chan Message {
	return s.ch
}

// Broker fans out events to connected SSE clients, applying per-client filters.
//
// E is the event type (e.g. model.SrvlogEvent).
// F is the filter type, which must implement Matches(E) bool.
type Broker[E any, F interface{ Matches(E) bool }] struct {
	mu          sync.RWMutex
	subscribers map[*Subscription[F]]struct{}
	byClient    map[string]int // per-client-key connection counts
	logger      *slog.Logger
	getID       func(E) int64
	label       string
	metrics     BrokerMetrics
}

// New creates a new Broker.
//
// label is used for log messages (e.g. "srvlog", "applog").
// getID extracts the event ID for the SSE id: field.
func New[E any, F interface{ Matches(E) bool }](
	logger *slog.Logger,
	label string,
	getID func(E) int64,
	m BrokerMetrics,
) *Broker[E, F] {
	return &Broker[E, F]{
		subscribers: make(map[*Subscription[F]]struct{}),
		byClient:    make(map[string]int),
		logger:      logger,
		getID:       getID,
		label:       label,
		metrics:     m,
	}
}

// Subscribe registers a new client with the given filter and returns its
// subscription. clientKey identifies the originating client (typically the
// source IP); an empty key disables the per-client cap (used in tests).
// Returns ErrTooManySubscribers if the broker has reached its global limit, or
// ErrTooManyClientSubscribers if the client key has reached its per-client limit.
func (b *Broker[E, F]) Subscribe(filter F, clientKey string) (*Subscription[F], error) {
	sub := &Subscription[F]{
		ch:        make(chan Message, subscriptionBufferSize),
		filter:    filter,
		clientKey: clientKey,
	}
	b.mu.Lock()
	if len(b.subscribers) >= maxSubscribers {
		b.mu.Unlock()
		return nil, ErrTooManySubscribers
	}
	if clientKey != "" && b.byClient[clientKey] >= maxSubscribersPerClient {
		b.mu.Unlock()
		return nil, ErrTooManyClientSubscribers
	}
	b.subscribers[sub] = struct{}{}
	if clientKey != "" {
		b.byClient[clientKey]++
	}
	b.mu.Unlock()
	b.metrics.OnSubscribe()
	b.logger.Debug(b.label+" client subscribed", "total", b.Len())
	return sub, nil
}

// Unsubscribe removes a client and closes its channel.
// Safe to call after Shutdown — if the subscription was already removed, this is a no-op.
func (b *Broker[E, F]) Unsubscribe(sub *Subscription[F]) {
	b.mu.Lock()
	if _, ok := b.subscribers[sub]; !ok {
		b.mu.Unlock()
		return
	}
	delete(b.subscribers, sub)
	close(sub.ch)
	b.decClientLocked(sub.clientKey)
	b.mu.Unlock()
	b.metrics.OnUnsubscribe()
	b.logger.Debug(b.label+" client unsubscribed", "total", b.Len())
}

// decClientLocked decrements the per-client connection count, removing the key
// when it reaches zero. Caller must hold b.mu.
func (b *Broker[E, F]) decClientLocked(clientKey string) {
	if clientKey == "" {
		return
	}
	if n := b.byClient[clientKey]; n <= 1 {
		delete(b.byClient, clientKey)
	} else {
		b.byClient[clientKey] = n - 1
	}
}

// Len returns the number of connected subscribers.
func (b *Broker[E, F]) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// Shutdown closes all subscriber channels, causing SSE handlers to return.
func (b *Broker[E, F]) Shutdown() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subscribers {
		close(sub.ch)
		delete(b.subscribers, sub)
	}
	clear(b.byClient)
	b.logger.Info(b.label + " broker shut down")
}

// Broadcast sends an event to all subscribers whose filter matches.
func (b *Broker[E, F]) Broadcast(event E) {
	data, err := json.Marshal(event)
	if err != nil {
		b.logger.Error("marshal "+b.label+" event", "err", err)
		return
	}

	msg := Message{ID: b.getID(event), Data: data}
	b.metrics.OnBroadcast()

	b.mu.RLock()
	defer b.mu.RUnlock()

	for sub := range b.subscribers {
		if !sub.filter.Matches(event) {
			continue
		}
		select {
		case sub.ch <- msg:
		default:
			b.metrics.OnDrop()
			b.logger.Warn("dropped "+b.label+" event for slow client", "event_id", msg.ID)
		}
	}
}
