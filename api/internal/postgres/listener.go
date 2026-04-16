// Package postgres provides PostgreSQL storage and LISTEN/NOTIFY support.
package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/taillight/internal/metrics"
)

const (
	// monitorInterval is how often we check notification channel utilization.
	monitorInterval = 30 * time.Second

	// channelUsageWarningThreshold triggers a warning when buffer is this full (0.8 = 80%).
	channelUsageWarningThreshold = 0.8

	// reconnectInitialBackoff is the starting delay between reconnection attempts.
	reconnectInitialBackoff = time.Second

	// reconnectMaxBackoff is the maximum delay between reconnection attempts.
	reconnectMaxBackoff = 30 * time.Second
)

// channelTable maps a NOTIFY channel name to its source table for gap fill queries.
var channelTable = map[string]string{
	"srvlog_ingest": "srvlog_events",
	"netlog_ingest": "netlog_events",
}

// Notification carries a row ID and the channel it arrived on.
type Notification struct {
	Channel string
	ID      int64
}

// Listener holds a dedicated LISTEN connection and publishes notifications.
type Listener struct {
	connStr    string
	pool       *pgxpool.Pool
	logger     *slog.Logger
	bufferSize int
	channels   []string // NOTIFY channels to LISTEN on.

	// Per-channel lastSeenID tracking for gap fill on reconnect.
	lastSeenSrvlogID atomic.Int64
	lastSeenNetlogID atomic.Int64

	mu     sync.Mutex
	conn   *pgx.Conn
	cancel context.CancelFunc
}

// NewListener creates a new Listener with the given notification buffer size.
// The pool is used to query missed events after reconnection.
// channels specifies which NOTIFY channels to LISTEN on (e.g. "srvlog_ingest", "netlog_ingest").
func NewListener(connStr string, pool *pgxpool.Pool, bufferSize int, logger *slog.Logger, channels []string) *Listener {
	return &Listener{
		connStr:    connStr,
		pool:       pool,
		bufferSize: bufferSize,
		logger:     logger,
		channels:   channels,
	}
}

// Listen connects to PostgreSQL, runs LISTEN on configured channels,
// and sends notifications on the returned channel.
// It reconnects automatically on connection loss.
func (l *Listener) Listen(ctx context.Context) (<-chan Notification, error) {
	conn, err := l.connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("initial connection: %w", err)
	}

	// Create a cancellable context for shutdown.
	listenCtx, cancel := context.WithCancel(ctx)
	l.mu.Lock()
	l.conn = conn
	l.cancel = cancel
	l.mu.Unlock()

	ch := make(chan Notification, l.bufferSize)

	go func() {
		defer close(ch)
		c := conn

		for {
			if err := l.recv(listenCtx, c, ch); err != nil {
				if listenCtx.Err() != nil {
					_ = c.Close(context.Background())
					return
				}
				l.logger.Error("listener connection lost", "err", err)
				_ = c.Close(context.Background())

				c = l.reconnect(listenCtx)
				if c == nil {
					return
				}
				l.mu.Lock()
				l.conn = c
				l.mu.Unlock()

				// Fill gap: push any events missed while disconnected.
				l.fillGap(listenCtx, ch)
			}
		}
	}()

	// Monitor channel utilization. Warn if the buffer exceeds 80% capacity,
	// which indicates event bursts are outpacing consumption.
	go func() {
		ticker := time.NewTicker(monitorInterval)
		defer ticker.Stop()
		for {
			select {
			case <-listenCtx.Done():
				return
			case <-ticker.C:
				usage := float64(len(ch)) / float64(cap(ch))
				if usage > channelUsageWarningThreshold {
					l.logger.Warn("notification channel near capacity", "usage_pct", int(usage*100), "len", len(ch), "cap", cap(ch))
				}
			}
		}
	}()

	l.logger.Info("listening for notifications", "channels", strings.Join(l.channels, ", "))
	return ch, nil
}

// Shutdown gracefully stops the listener and closes the connection.
func (l *Listener) Shutdown(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.cancel != nil {
		l.cancel()
	}
	if l.conn != nil {
		if err := l.conn.Close(ctx); err != nil {
			return fmt.Errorf("close listener connection: %w", err)
		}
		l.conn = nil
	}
	l.logger.Info("listener shut down")
	return nil
}

func (l *Listener) connect(ctx context.Context) (*pgx.Conn, error) {
	conn, err := pgx.Connect(ctx, l.connStr)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	for _, ch := range l.channels {
		if _, err := conn.Exec(ctx, "LISTEN "+ch); err != nil {
			_ = conn.Close(ctx)
			return nil, fmt.Errorf("listen %s: %w", ch, err)
		}
	}
	return conn, nil
}

func (l *Listener) reconnect(ctx context.Context) *pgx.Conn {
	backoff := reconnectInitialBackoff
	maxBackoff := reconnectMaxBackoff

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}

		metrics.ListenerReconnectsTotal.Inc()
		conn, err := l.connect(ctx)
		if err != nil {
			l.logger.Warn("reconnect failed", "err", err, "retry_in", backoff)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			// Add jitter to avoid thundering herd on simultaneous reconnects.
			jitter := time.Duration(rand.Int64N(int64(backoff / 2)))
			backoff += jitter
			continue
		}

		l.logger.Info("listener reconnected")
		return conn
	}
}

func (l *Listener) recv(ctx context.Context, conn *pgx.Conn, ch chan<- Notification) error {
	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}

		id, err := strconv.ParseInt(notification.Payload, 10, 64)
		if err != nil {
			metrics.ListenerPayloadParseErrorsTotal.WithLabelValues(notification.Channel).Inc()
			l.logger.Warn("invalid notification payload", "channel", notification.Channel, "payload", notification.Payload)
			continue
		}

		select {
		case ch <- Notification{Channel: notification.Channel, ID: id}:
			l.storeLastSeenID(notification.Channel, id)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// storeLastSeenID updates the correct per-channel atomic based on the notification channel.
func (l *Listener) storeLastSeenID(channel string, id int64) {
	switch channel {
	case "srvlog_ingest":
		l.lastSeenSrvlogID.Store(id)
	case "netlog_ingest":
		l.lastSeenNetlogID.Store(id)
	}
}

// lastSeenIDForChannel returns the last seen ID for the given notification channel.
func (l *Listener) lastSeenIDForChannel(channel string) int64 {
	switch channel {
	case "srvlog_ingest":
		return l.lastSeenSrvlogID.Load()
	case "netlog_ingest":
		return l.lastSeenNetlogID.Load()
	default:
		return 0
	}
}

// fillGap queries for events inserted while the listener was disconnected
// and pushes them into the notification channel so the brokers don't miss any.
// Runs per-channel gap fill for each configured channel.
func (l *Listener) fillGap(ctx context.Context, ch chan<- Notification) {
	for _, notifyCh := range l.channels {
		l.fillGapForChannel(ctx, ch, notifyCh)
	}
}

// fillGapForChannel runs gap fill for a single notification channel.
func (l *Listener) fillGapForChannel(ctx context.Context, ch chan<- Notification, notifyCh string) {
	lastID := l.lastSeenIDForChannel(notifyCh)
	if lastID == 0 {
		return // no baseline — nothing to fill
	}

	table, ok := channelTable[notifyCh]
	if !ok {
		l.logger.Warn("no table mapping for channel", "channel", notifyCh)
		return
	}

	start := time.Now()
	defer func() {
		metrics.ListenerGapFillDuration.WithLabelValues(notifyCh).Observe(time.Since(start).Seconds())
	}()

	//nolint:gosec // table name comes from a hardcoded map, not user input
	query := fmt.Sprintf("SELECT id FROM %s WHERE id > $1 ORDER BY id ASC LIMIT 10000", table)
	rows, err := l.pool.Query(ctx, query, lastID)
	if err != nil {
		if ctx.Err() != nil {
			return // shutting down
		}
		l.logger.Error("gap fill query failed", "channel", notifyCh, "table", table, "last_seen_id", lastID, "err", err)
		return
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			if ctx.Err() != nil {
				return // shutting down
			}
			l.logger.Error("gap fill scan failed", "channel", notifyCh, "err", err)
			return
		}
		select {
		case ch <- Notification{Channel: notifyCh, ID: id}:
			l.storeLastSeenID(notifyCh, id)
			count++
		case <-ctx.Done():
			return
		}
	}
	if err := rows.Err(); err != nil {
		if ctx.Err() != nil {
			return // shutting down
		}
		l.logger.Error("gap fill rows error", "channel", notifyCh, "err", err)
		return
	}
	if count > 0 {
		metrics.ListenerGapFillEventsTotal.WithLabelValues(notifyCh).Add(float64(count))
		l.logger.Info("gap fill complete", "channel", notifyCh, "events", count, "from_id", lastID)
	}
}
