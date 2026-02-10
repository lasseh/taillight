// Package postgres provides PostgreSQL storage and LISTEN/NOTIFY support.
package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

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

// Notification carries a row ID and the channel it arrived on.
type Notification struct {
	Channel string
	ID      int64
}

// Listener holds a dedicated LISTEN connection and publishes notifications.
type Listener struct {
	connStr    string
	logger     *slog.Logger
	bufferSize int

	mu     sync.Mutex
	conn   *pgx.Conn
	cancel context.CancelFunc
}

// NewListener creates a new Listener with the given notification buffer size.
func NewListener(connStr string, bufferSize int, logger *slog.Logger) *Listener {
	return &Listener{connStr: connStr, bufferSize: bufferSize, logger: logger}
}

// Listen connects to PostgreSQL, runs LISTEN on syslog_ingest,
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

	l.logger.Info("listening for notifications", "channel", "syslog_ingest")
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
	if _, err := conn.Exec(ctx, "LISTEN syslog_ingest"); err != nil {
		_ = conn.Close(ctx)
		return nil, fmt.Errorf("listen syslog_ingest: %w", err)
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
			l.logger.Warn("invalid notification payload", "channel", notification.Channel, "payload", notification.Payload)
			continue
		}

		select {
		case ch <- Notification{Channel: notification.Channel, ID: id}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
