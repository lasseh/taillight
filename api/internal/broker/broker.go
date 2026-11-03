// Package broker provides SSE fan-out with per-client filtering.
package broker

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
)

const (
	// subscriptionBufferSize is the channel buffer size for each SSE client subscription.
	// A larger buffer allows clients to briefly fall behind without dropping events.
	subscriptionBufferSize = 512

	// subscriptionByteBudget caps the marshalled bytes queued for one
	// subscription. A subscriber whose backlog would pass it is evicted, so a
	// client that reads slower than its events arrive holds a bounded share of
	// heap however large the events are.
	subscriptionByteBudget = 16 << 20

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
	// queued counts the Data bytes sent on ch and not yet released by
	// Consumed. Every subscription counts a shared event's full size, so it is
	// an upper bound on the heap its queue holds.
	queued atomic.Int64
}

// Chan returns the event channel for reading. The reader passes each message
// it receives to Consumed. The channel is closed when the subscription ends:
// on Unsubscribe, on Shutdown, or when the broker evicts a reader that fell
// too far behind.
func (s *Subscription[F]) Chan() <-chan Message {
	return s.ch
}

// Consumed releases msg from the subscription's byte budget. Call it once for
// each message received from Chan.
func (s *Subscription[F]) Consumed(msg Message) {
	s.queued.Add(-int64(len(msg.Data)))
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
	byteBudget  int64 // subscriptionByteBudget (lowered in tests)
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
		byteBudget:  subscriptionByteBudget,
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

// Unsubscribe removes a client, discards its queued events and closes its
// channel. Safe to call after Shutdown — if the subscription was already
// removed, this is a no-op.
func (b *Broker[E, F]) Unsubscribe(sub *Subscription[F]) {
	b.mu.Lock()
	if _, ok := b.subscribers[sub]; !ok {
		b.mu.Unlock()
		return
	}
	delete(b.subscribers, sub)
	close(sub.ch)
	// An evicted reader's backlog is released now, and its handler sees the
	// close on its next receive instead of writing the backlog first.
	for len(sub.ch) > 0 {
		<-sub.ch
	}
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

// Broadcast sends an event to all subscribers whose filter matches. A
// subscriber whose queue is full loses the event. One whose queued bytes would
// pass the byte budget is evicted, which ends its stream.
func (b *Broker[E, F]) Broadcast(event E) {
	data, err := json.Marshal(event)
	if err != nil {
		b.logger.Error("marshal "+b.label+" event", "err", err)
		return
	}

	msg := Message{ID: b.getID(event), Data: data}
	size := int64(len(data))
	b.metrics.OnBroadcast()

	var evict []*Subscription[F]
	b.mu.RLock()
	for sub := range b.subscribers {
		if !sub.filter.Matches(event) {
			continue
		}
		// Reserve the bytes before sending, so concurrent broadcasts cannot
		// pass the budget together.
		if sub.queued.Add(size) > b.byteBudget {
			sub.queued.Add(-size)
			b.metrics.OnDrop()
			evict = append(evict, sub)
			continue
		}
		select {
		case sub.ch <- msg:
		default:
			sub.queued.Add(-size)
			b.metrics.OnDrop()
			b.logger.Warn("dropped "+b.label+" event for slow client", "event_id", msg.ID)
		}
	}
	b.mu.RUnlock()

	// Unsubscribe takes the write lock, so evict after releasing the read lock.
	for _, sub := range evict {
		b.logger.Warn("evicted "+b.label+" client over its queued byte budget",
			"event_id", msg.ID, "client", sub.clientKey, "queued_bytes", sub.queued.Load())
		b.Unsubscribe(sub)
	}
}
