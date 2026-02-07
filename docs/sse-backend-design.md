# Syslog SSE Backend -- Design Reference

> **Note:** This document was the original design reference. The implementation has evolved significantly. See `api/` for the current code and `api/API.md` for the up-to-date API reference.
>
> **Key differences from this design:**
> - Listener now has automatic reconnection with exponential backoff and graceful shutdown
> - Broker supports per-client filtering (not just broadcast to all)
> - Applog support added (separate broker, SSE stream, and ingest endpoint)
> - Metrics, CORS configuration, and API key authentication added
> - Uses interfaces for testability (`internal/handler/store.go`)

Go service that streams filtered syslog events to browser clients via Server-Sent Events. Uses PostgreSQL LISTEN/NOTIFY for push-based delivery -- zero polling.

---

## Architecture

```
rsyslog (ompgsql) -> PostgreSQL -> LISTEN/NOTIFY -> Go SSE backend -> browser EventSource
```

1. rsyslog inserts filtered syslog events into `syslog_events`
2. A trigger fires `pg_notify('syslog_ingest', NEW.id::text)` on each INSERT
3. The Go backend holds a persistent `LISTEN syslog_ingest` connection
4. On notification, it fetches the new row and fans it out to all connected SSE clients

---

## Database Prerequisites

These must exist in the target PostgreSQL database before the backend connects.

### Table

```sql
CREATE TABLE syslog_events (
    id              BIGINT GENERATED ALWAYS AS IDENTITY,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    reported_at     TIMESTAMPTZ NOT NULL,
    hostname        TEXT        NOT NULL,
    fromhost_ip     INET        NOT NULL,
    programname     TEXT        NOT NULL DEFAULT '',
    msgid           TEXT        NOT NULL DEFAULT '',
    severity        SMALLINT    NOT NULL,
    facility        SMALLINT    NOT NULL,
    syslogtag       TEXT        NOT NULL DEFAULT '',
    structured_data TEXT,
    message         TEXT        NOT NULL,
    raw_message     TEXT
);

CREATE INDEX idx_syslog_host_received
    ON syslog_events (hostname, received_at DESC);

CREATE INDEX idx_syslog_severity_received
    ON syslog_events (severity, received_at DESC)
    WHERE severity <= 3;
```

### Trigger and function

```sql
CREATE OR REPLACE FUNCTION notify_syslog_insert()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('syslog_ingest', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_syslog_notify
    AFTER INSERT ON syslog_events
    FOR EACH ROW EXECUTE FUNCTION notify_syslog_insert();
```

### Notification payload

The trigger sends the new row's `id` as the payload on channel `syslog_ingest`. The backend uses this to fetch the full row.

```
channel: syslog_ingest
payload: "605"  (the BIGINT id as text)
```

---

## Go Project Layout

```
taillight/
├── cmd/
│   └── taillight/
│       └── main.go              # Entrypoint, config, wiring
├── internal/
│   ├── config/
│   │   └── config.go            # Environment/flag parsing
│   ├── postgres/
│   │   ├── listener.go          # LISTEN/NOTIFY connection
│   │   └── queries.go           # Row fetch queries
│   ├── broker/
│   │   └── broker.go            # SSE fan-out to connected clients
│   └── handler/
│       └── sse.go               # HTTP handler for /events SSE stream
├── go.mod
├── go.sum
└── Makefile
```

---

## Key Components

### 1. PostgreSQL listener

A dedicated connection (not from the pool) that runs `LISTEN syslog_ingest` and forwards notifications to a Go channel.

```go
// internal/postgres/listener.go

package postgres

import (
    "context"
    "log/slog"
    "strconv"

    "github.com/jackc/pgx/v5"
)

// SyslogEvent represents a row from syslog_events.
type SyslogEvent struct {
    ID             int64  `json:"id"`
    ReceivedAt     string `json:"received_at"`
    ReportedAt     string `json:"reported_at"`
    Hostname       string `json:"hostname"`
    FromhostIP     string `json:"fromhost_ip"`
    Programname    string `json:"programname"`
    MsgID          string `json:"msgid"`
    Severity       int    `json:"severity"`
    Facility       int    `json:"facility"`
    SyslogTag      string `json:"syslogtag"`
    StructuredData string `json:"structured_data,omitempty"`
    Message        string `json:"message"`
}

// Listener holds a dedicated LISTEN connection and publishes events.
type Listener struct {
    connStr string
    logger  *slog.Logger
}

func NewListener(connStr string, logger *slog.Logger) *Listener {
    return &Listener{connStr: connStr, logger: logger}
}

// Listen connects to PostgreSQL, runs LISTEN, and sends row IDs on the
// returned channel. Blocks until ctx is cancelled.
func (l *Listener) Listen(ctx context.Context) (<-chan int64, error) {
    conn, err := pgx.Connect(ctx, l.connStr)
    if err != nil {
        return nil, err
    }

    _, err = conn.Exec(ctx, "LISTEN syslog_ingest")
    if err != nil {
        conn.Close(ctx)
        return nil, err
    }

    ids := make(chan int64, 256)

    go func() {
        defer conn.Close(context.Background())
        defer close(ids)

        for {
            notification, err := conn.WaitForNotification(ctx)
            if err != nil {
                if ctx.Err() != nil {
                    return // clean shutdown
                }
                l.logger.Error("notification error", "err", err)
                return
            }

            id, err := strconv.ParseInt(notification.Payload, 10, 64)
            if err != nil {
                l.logger.Warn("invalid payload", "payload", notification.Payload)
                continue
            }

            select {
            case ids <- id:
            case <-ctx.Done():
                return
            }
        }
    }()

    l.logger.Info("listening for syslog notifications")
    return ids, nil
}
```

### 2. Row fetcher

Uses a connection pool (separate from the LISTEN connection) to fetch rows by ID.

```go
// internal/postgres/queries.go

package postgres

import (
    "context"
    "fmt"

    "github.com/jackc/pgx/v5/pgxpool"
)

type Queries struct {
    pool *pgxpool.Pool
}

func NewQueries(pool *pgxpool.Pool) *Queries {
    return &Queries{pool: pool}
}

func (q *Queries) GetEvent(ctx context.Context, id int64) (SyslogEvent, error) {
    var e SyslogEvent
    var sd *string

    err := q.pool.QueryRow(ctx, `
        SELECT id, received_at, reported_at, hostname, fromhost_ip,
               programname, msgid, severity, facility, syslogtag,
               structured_data, message
        FROM syslog_events
        WHERE id = $1
    `, id).Scan(
        &e.ID, &e.ReceivedAt, &e.ReportedAt, &e.Hostname, &e.FromhostIP,
        &e.Programname, &e.MsgID, &e.Severity, &e.Facility, &e.SyslogTag,
        &sd, &e.Message,
    )
    if err != nil {
        return e, fmt.Errorf("get event %d: %w", id, err)
    }
    if sd != nil {
        e.StructuredData = *sd
    }
    return e, nil
}

// Recent returns the last N events (for initial SSE backfill on connect).
func (q *Queries) Recent(ctx context.Context, limit int) ([]SyslogEvent, error) {
    rows, err := q.pool.Query(ctx, `
        SELECT id, received_at, reported_at, hostname, fromhost_ip,
               programname, msgid, severity, facility, syslogtag,
               structured_data, message
        FROM syslog_events
        ORDER BY received_at DESC
        LIMIT $1
    `, limit)
    if err != nil {
        return nil, fmt.Errorf("recent events: %w", err)
    }
    defer rows.Close()

    var events []SyslogEvent
    for rows.Next() {
        var e SyslogEvent
        var sd *string
        if err := rows.Scan(
            &e.ID, &e.ReceivedAt, &e.ReportedAt, &e.Hostname, &e.FromhostIP,
            &e.Programname, &e.MsgID, &e.Severity, &e.Facility, &e.SyslogTag,
            &sd, &e.Message,
        ); err != nil {
            return nil, fmt.Errorf("scan event: %w", err)
        }
        if sd != nil {
            e.StructuredData = *sd
        }
        events = append(events, e)
    }
    return events, nil
}
```

### 3. SSE broker

Fan-out hub that accepts subscriber channels and broadcasts events.

```go
// internal/broker/broker.go

package broker

import (
    "encoding/json"
    "log/slog"
    "sync"

    "your-module/internal/postgres"
)

type Broker struct {
    mu          sync.RWMutex
    subscribers map[chan []byte]struct{}
    logger      *slog.Logger
}

func New(logger *slog.Logger) *Broker {
    return &Broker{
        subscribers: make(map[chan []byte]struct{}),
        logger:      logger,
    }
}

// Subscribe returns a channel that receives JSON-encoded events.
// Call Unsubscribe when the client disconnects.
func (b *Broker) Subscribe() chan []byte {
    ch := make(chan []byte, 64)
    b.mu.Lock()
    b.subscribers[ch] = struct{}{}
    b.mu.Unlock()
    b.logger.Debug("client subscribed", "total", b.Len())
    return ch
}

func (b *Broker) Unsubscribe(ch chan []byte) {
    b.mu.Lock()
    delete(b.subscribers, ch)
    close(ch)
    b.mu.Unlock()
    b.logger.Debug("client unsubscribed", "total", b.Len())
}

func (b *Broker) Len() int {
    b.mu.RLock()
    defer b.mu.RUnlock()
    return len(b.subscribers)
}

// Broadcast sends an event to all connected clients.
// Slow clients that can't keep up have their message dropped.
func (b *Broker) Broadcast(event postgres.SyslogEvent) {
    data, err := json.Marshal(event)
    if err != nil {
        b.logger.Error("marshal event", "err", err)
        return
    }

    b.mu.RLock()
    defer b.mu.RUnlock()

    for ch := range b.subscribers {
        select {
        case ch <- data:
        default:
            // subscriber too slow, drop message
        }
    }
}
```

### 4. SSE HTTP handler

```go
// internal/handler/sse.go

package handler

import (
    "fmt"
    "net/http"

    "your-module/internal/broker"
    "your-module/internal/postgres"
)

type SSEHandler struct {
    broker  *broker.Broker
    queries *postgres.Queries
}

func NewSSE(b *broker.Broker, q *postgres.Queries) *SSEHandler {
    return &SSEHandler{broker: b, queries: q}
}

func (h *SSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "streaming unsupported", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("Access-Control-Allow-Origin", "*")

    // Backfill: send recent events so the client has context
    recent, err := h.queries.Recent(r.Context(), 50)
    if err == nil {
        for i := len(recent) - 1; i >= 0; i-- {
            data, _ := json.Marshal(recent[i])
            fmt.Fprintf(w, "event: syslog\ndata: %s\n\n", data)
        }
        flusher.Flush()
    }

    // Subscribe to live events
    ch := h.broker.Subscribe()
    defer h.broker.Unsubscribe(ch)

    for {
        select {
        case data, ok := <-ch:
            if !ok {
                return
            }
            fmt.Fprintf(w, "event: syslog\ndata: %s\n\n", data)
            flusher.Flush()
        case <-r.Context().Done():
            return
        }
    }
}
```

### 5. Wiring (main.go)

```go
package main

import (
    "context"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
    "github.com/jackc/pgx/v5/pgxpool"

    "your-module/internal/broker"
    "your-module/internal/handler"
    "your-module/internal/postgres"
)

func main() {
    logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    connStr := envOr("DATABASE_URL", "postgres://taillight@localhost:5432/taillight")

    // Connection pool for queries
    pool, err := pgxpool.New(ctx, connStr)
    if err != nil {
        logger.Error("connect to database", "err", err)
        os.Exit(1)
    }
    defer pool.Close()

    queries := postgres.NewQueries(pool)

    // Dedicated LISTEN connection
    listener := postgres.NewListener(connStr, logger)
    ids, err := listener.Listen(ctx)
    if err != nil {
        logger.Error("start listener", "err", err)
        os.Exit(1)
    }

    // SSE broker
    b := broker.New(logger)

    // Bridge: notification -> fetch row -> broadcast
    go func() {
        for id := range ids {
            event, err := queries.GetEvent(ctx, id)
            if err != nil {
                logger.Warn("fetch event", "id", id, "err", err)
                continue
            }
            b.Broadcast(event)
        }
    }()

    // HTTP server
    r := chi.NewRouter()
    r.Use(middleware.RequestID)
    r.Use(middleware.RealIP)
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)
    r.Use(middleware.Timeout(60 * time.Second))

    r.Get("/events", handler.NewSSE(b, queries).ServeHTTP)

    // Health check
    r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
        if err := pool.Ping(r.Context()); err != nil {
            http.Error(w, "db unreachable", http.StatusServiceUnavailable)
            return
        }
        w.WriteHeader(http.StatusOK)
    })

    addr := envOr("LISTEN_ADDR", ":8080")
    srv := &http.Server{Addr: addr, Handler: r}

    go func() {
        logger.Info("starting SSE server", "addr", addr)
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            logger.Error("server error", "err", err)
            cancel()
        }
    }()

    <-ctx.Done()
    logger.Info("shutting down")

    shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer shutdownCancel()
    srv.Shutdown(shutdownCtx)
}

func envOr(key, fallback string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return fallback
}
```

---

## SSE Wire Format

Each event is sent as a named SSE event with JSON data:

```
event: syslog
data: {"id":605,"received_at":"2025-01-31T12:00:01Z","reported_at":"2025-01-31T12:00:00.123Z","hostname":"juniper-mx1","fromhost_ip":"10.0.1.1","programname":"rpd","msgid":"RPD_BGP_NEIGHBOR_STATE_CHANGED","severity":5,"facility":23,"syslogtag":"rpd[1234]:","message":"BGP peer 10.0.0.2 Down - hold timer expired"}

```

### Browser client

```javascript
const source = new EventSource("http://localhost:8080/events");

source.addEventListener("syslog", (e) => {
    const event = JSON.parse(e.data);
    console.log(event.hostname, event.msgid, event.message);
});

source.onerror = () => {
    console.warn("SSE connection lost, reconnecting...");
};
```

`EventSource` automatically reconnects on disconnect.

---

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | `postgres://taillight@localhost:5432/taillight` | PostgreSQL connection string |
| `LISTEN_ADDR` | `:8080` | HTTP listen address |

---

## Dependencies

```
go get github.com/jackc/pgx/v5
go get github.com/go-chi/chi/v5
```

---

## Data Flow Summary

```
1. rsyslog INSERT -> syslog_events table
2. PostgreSQL trigger -> pg_notify('syslog_ingest', id)
3. Go listener (pgx WaitForNotification) -> receives id
4. Go fetches full row by id from pool
5. Broker broadcasts JSON to all subscriber channels
6. SSE handler writes "event: syslog\ndata: {...}\n\n" + flush
7. Browser EventSource receives and parses JSON
```
