# Internals

This document covers the internal implementation details of the taillight backend. It is aimed at contributors and developers who want to understand how the code works, why specific design decisions were made, and how the major subsystems fit together.

All file paths are relative to `api/` unless otherwise noted.

---

## Handler Pattern

Every HTTP handler in taillight follows the same structure. A handler struct holds a store interface and exposes methods with the standard `http.HandlerFunc` signature.

### Store interfaces

Each handler domain defines its own store interface in a dedicated file. This keeps handler packages testable without pulling in the full `postgres.Store`:

```go
// internal/handler/store.go
type SrvlogStore interface {
    GetSrvlog(ctx context.Context, id int64) (model.SrvlogEvent, error)
    ListSrvlogs(ctx context.Context, f model.SrvlogFilter, cursor *model.Cursor, limit int) ([]model.SrvlogEvent, *model.Cursor, error)
    ListSrvlogsSince(ctx context.Context, f model.SrvlogFilter, sinceID int64, limit int) ([]model.SrvlogEvent, error)
    // ...
}

type AppLogStore interface {
    GetAppLog(ctx context.Context, id int64) (model.AppLogEvent, error)
    ListAppLogs(ctx context.Context, f model.AppLogFilter, cursor *model.Cursor, limit int) ([]model.AppLogEvent, *model.Cursor, error)
    // ...
}
```

A separate `StatsStore` interface in `internal/handler/stats.go` covers volume and summary queries. The concrete `postgres.Store` satisfies all of these interfaces.

### Constructor pattern

Handlers are created with a `New*Handler(store)` constructor that accepts only the store interface:

```go
// internal/handler/srvlog.go
type SrvlogHandler struct {
    store SrvlogStore
}

func NewSrvlogHandler(store SrvlogStore) *SrvlogHandler {
    return &SrvlogHandler{store: store}
}
```

SSE handlers additionally take the broker and a logger:

```go
func NewSrvlogSSEHandler(b *broker.SrvlogBroker, s SrvlogStore, l *slog.Logger) *SrvlogSSEHandler
```

### Request-scoped logging

The `RequestLogger` middleware (`internal/handler/request_id.go`) creates a logger enriched with the chi request ID and stores it in the request context:

```go
func RequestLogger(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        reqID := middleware.GetReqID(r.Context())
        logger := slog.Default().With("request_id", reqID)
        ctx := context.WithValue(r.Context(), ctxKeyLogger{}, logger)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

Handlers retrieve it with `LoggerFromContext(r.Context())`, which falls back to `slog.Default()` if no logger is in the context.

### Response helpers

All JSON responses go through helpers in `internal/handler/response.go`:

| Helper | Purpose |
|--------|---------|
| `writeJSON(w, v)` | 200 OK + JSON body |
| `writeJSONStatus(w, status, v)` | Custom status + JSON body |
| `writeError(w, status, code, msg)` | Error envelope via `httputil.WriteError` |
| `emptySlice[T](s)` | Ensures nil slices serialize as `[]` instead of `null` |
| `mustJSON(v)` | Marshal or return `nil, false` (used in SSE handlers) |

List endpoints wrap results in a `listResponse` envelope with `data`, `cursor`, and `has_more` fields. Single-item endpoints use `itemResponse` with just `data`.

### Filter parsing

Filters are parsed from HTTP query parameters in the model package:

- `model.ParseSrvlogFilter(r)` -- extracts hostname, programname, severity, facility, search, time range, etc.
- `model.ParseAppLogFilter(r)` -- extracts service, component, host, level, search, time range
- `model.ParseCursor(r)` -- decodes the `cursor` query param
- `model.ParseLimit(r, default, max)` -- extracts and clamps the `limit` param

All string filter parameters are capped at 500 characters. Typed parameters (severity, facility, level) are validated and return errors for invalid values.

---

## SSE Broker System

The broker system fans out database events to connected SSE clients with per-client filtering. There are two parallel implementations: `SrvlogBroker` and `AppLogBroker` (`internal/broker/`). They share the same architecture.

### Data structures

```
SrvlogBroker
  mu          sync.RWMutex
  subscribers map[*SrvlogSubscription]struct{}
  logger      *slog.Logger

SrvlogSubscription
  ch     chan SrvlogMessage    // buffered, cap=64
  filter model.SrvlogFilter
```

Subscribers are tracked in a map keyed by pointer. This gives O(1) subscribe/unsubscribe and avoids index management.

### Subscribe / Unsubscribe lifecycle

1. **Subscribe**: Creates a `SrvlogSubscription` with a buffered channel (capacity 64) and the client's filter. Acquires write lock, checks the subscriber count against the 1000-client limit, adds the subscription, releases the lock, and increments `metrics.SSEClientsActive`.

2. **Unsubscribe**: Acquires write lock, checks if the subscription exists (idempotent), removes it, closes the channel, releases the lock, and decrements the gauge. The close signals the SSE handler's event loop to exit.

### Per-client filter matching

Each filter type implements a `Matches(event)` method that checks all non-zero fields against the event. The srvlog filter checks hostname (with wildcard support), fromhost_ip, programname, severity, severity_max, facility, syslogtag, msgid, and search (case-insensitive substring). The applog filter checks service, component, host (with wildcard), level (rank-based: "WARN" matches WARN, ERROR, FATAL), and search (against both msg and attrs).

Time filters (`From`/`To`) are intentionally excluded from `Matches()` -- live SSE clients receive future events, so filtering by time range would be wrong.

Wildcard matching (`matchWildcard` in `internal/model/srvlog.go`) supports `*` as a glob character with case-insensitive comparison. The first segment anchors at the start, the last segment anchors at the end, and `*` matches any sequence in between.

### Broadcasting

```go
func (b *SrvlogBroker) Broadcast(event model.SrvlogEvent) {
    if b.Len() == 0 {
        return                          // early exit: no subscribers
    }
    data, err := json.Marshal(event)    // marshal once for all clients
    msg := SrvlogMessage{ID: event.ID, Data: data}
    metrics.EventsBroadcastTotal.Inc()

    b.mu.RLock()                        // read lock: concurrent reads OK
    defer b.mu.RUnlock()
    for sub := range b.subscribers {
        if !sub.filter.Matches(event) {
            continue                    // skip non-matching clients
        }
        select {
        case sub.ch <- msg:             // non-blocking send
        default:
            metrics.EventsDroppedTotal.Inc()
        }
    }
}
```

Key details:
- The event is JSON-marshaled once, then the same `[]byte` is shared across all subscriptions.
- A read lock (`RLock`) is held during broadcast so subscribes/unsubscribes block but multiple broadcasts can proceed concurrently.
- The send is non-blocking: if a client's 64-message buffer is full, the event is dropped and a metric is incremented. This prevents one slow client from blocking all others.

### Limits and metrics

| Constant | Value | Purpose |
|----------|-------|---------|
| `subscriptionBufferSize` | 64 | Per-client channel buffer |
| `maxSubscribers` | 1000 | Maximum concurrent SSE clients per broker |

Metrics tracked: `SSEClientsActive` (gauge), `EventsBroadcastTotal` (counter), `EventsDroppedTotal` (counter), with parallel `AppLog*` variants.

---

## LISTEN/NOTIFY Pipeline

Events flow from PostgreSQL to Go through the `Listener` (`internal/postgres/listener.go`), which uses PostgreSQL's LISTEN/NOTIFY mechanism to receive real-time notifications when new rows are inserted.

### Why a dedicated connection

The Listener uses a bare `pgx.Conn` -- not a connection from the pool. This is because `WaitForNotification` is a blocking call that holds the connection indefinitely. Using a pool connection would tie up a slot and eventually starve other queries. The dedicated connection is created via `pgx.Connect()` and managed separately from the `pgxpool.Pool`.

### Architecture

```
PostgreSQL                        Go Listener                    Brokers
  INSERT → trigger →            ┌──────────────┐
  pg_notify('srvlog_ingest',    │  pgx.Conn    │
            new_id)  ─────────► │  LISTEN      │──► Notification{channel, id}
                                │  channel     │         │
                                └──────────────┘         ▼
                                                   store.GetSrvlog(id)
                                                         │
                                                         ▼
                                                  broker.Broadcast(event)
```

### Notification flow

1. **Receive**: `conn.WaitForNotification(ctx)` blocks until a notification arrives. The payload is the row ID as a string.

2. **Parse**: The payload is parsed to `int64`. Invalid payloads are logged and skipped.

3. **Track**: `lastSeenID` is updated atomically for gap-fill on reconnection.

4. **Dispatch**: A `Notification{Channel, ID}` is sent on a buffered channel. The consumer (in `serve.go`) fetches the full event by ID from the store and broadcasts it to the appropriate broker.

### Reconnection with gap fill

When the connection drops:

1. The old connection is closed.
2. `reconnect()` attempts to re-establish the connection with exponential backoff (1s initial, 30s max) plus random jitter to avoid thundering herd.
3. After reconnecting, `fillGap()` queries for all event IDs greater than `lastSeenID` (up to 10,000) and pushes them into the notification channel. This ensures no events are lost during the disconnect window.
4. `metrics.ListenerReconnectsTotal` is incremented on each attempt.

### Channel monitoring

A background goroutine checks the notification channel utilization every 30 seconds. If the buffer exceeds 80% capacity, a warning is logged. This indicates event bursts are outpacing consumption and the buffer size may need to be increased.

---

## SSE Handler Lifecycle

Both `SrvlogSSEHandler.Stream` and `AppLogSSEHandler.Stream` (`internal/handler/srvlog_sse.go`, `applog_sse.go`) follow the same lifecycle:

### 1. Setup

```go
flusher, ok := w.(http.Flusher)     // verify streaming support
filter, err := model.ParseSrvlogFilter(r)

w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("Connection", "keep-alive")
w.Header().Set("X-Accel-Buffering", "no")  // disable nginx buffering
```

### 2. Subscribe before backfill

```go
sub, err := h.broker.Subscribe(filter)
defer h.broker.Unsubscribe(sub)
```

The subscription is created **before** the backfill query. This is critical: if we queried first and subscribed second, events arriving between the query and the subscribe would be lost. Subscribing first means those events land in the channel buffer and are delivered after the backfill.

### 3. Backfill

The backfill logic handles two cases:

**With `Last-Event-ID`** (client reconnecting): Queries `ListSrvlogsSince(sinceID, limit=100)` to fetch events after the last one the client received. Results are already in chronological order (ASC).

**Without `Last-Event-ID`** (fresh connection): Queries `ListSrvlogs(filter, nil, limit=100)` to get the most recent events, then sends them oldest-first by iterating in reverse.

The backfill returns the highest event ID sent, which is used for duplicate suppression.

### 4. Event loop

```go
heartbeat := time.NewTicker(15 * time.Second)
defer heartbeat.Stop()

for {
    select {
    case msg, ok := <-sub.Chan():
        if !ok { return }                          // channel closed (broker shutdown)
        if msg.ID <= lastBackfilledID { continue }  // duplicate suppression
        writeSSEEvent(w, msg.ID, "srvlog", msg.Data)
        flusher.Flush()
    case <-heartbeat.C:
        fmt.Fprint(w, "event: heartbeat\ndata: \n\n")
        flusher.Flush()
    case <-r.Context().Done():
        return                                      // client disconnected
    }
}
```

### 5. SSE frame format

```
id: 12345
event: srvlog
data: {"id":12345,"hostname":"router-1",...}

```

Each frame includes the event ID (for `Last-Event-ID` reconnection), the event type (`srvlog` or `applog`), and the JSON payload.

### 6. Heartbeat

A `:keepalive` comment (actually `event: heartbeat`) is sent every 15 seconds. This serves two purposes: it keeps the connection alive through proxies and load balancers, and it detects dead connections early (the write will fail if the client has disconnected).

### 7. Duplicate suppression

Events that arrive on the broker channel with `ID <= lastBackfilledID` are skipped. This handles the overlap window between the backfill query and the subscription becoming active.

---

## Cursor-Based Pagination

Taillight uses keyset (cursor) pagination instead of OFFSET-based pagination. The implementation lives in `internal/model/srvlog.go` and `internal/postgres/srvlog_store.go`.

### Cursor encoding

A cursor encodes two values: the `received_at` timestamp (as Unix nanoseconds) and the row `id`:

```go
// Encode: "{unix_nanos},{id}" → base64url
func (c Cursor) Encode() string {
    raw := fmt.Sprintf("%d,%d", c.ReceivedAt.UnixNano(), c.ID)
    return base64.URLEncoding.EncodeToString([]byte(raw))
}

// Decode: base64url → parse nanos + id
func DecodeCursor(s string) (Cursor, error) {
    raw, err := base64.URLEncoding.DecodeString(s)
    parts := strings.SplitN(string(raw), ",", 2)
    nanos, _ := strconv.ParseInt(parts[0], 10, 64)
    id, _ := strconv.ParseInt(parts[1], 10, 64)
    return Cursor{ReceivedAt: time.Unix(0, nanos), ID: id}, nil
}
```

### Keyset pagination query

The store uses tuple comparison for stable, index-friendly pagination:

```go
if cursor != nil {
    qb = qb.Where("(received_at, id) < (?, ?)", cursor.ReceivedAt, cursor.ID)
}
qb = qb.OrderBy("received_at DESC", "id DESC").Limit(uint64(limit + 1))
```

PostgreSQL evaluates `(received_at, id) < (cursor_time, cursor_id)` as a composite comparison. Combined with `ORDER BY received_at DESC, id DESC`, this gives stable ordering without the problems of OFFSET.

### has_more detection

The query requests `limit + 1` rows. If more rows are returned than the limit, a next cursor is constructed from the last included row:

```go
if len(events) > limit {
    last := events[limit-1]
    nextCursor = &model.Cursor{ReceivedAt: last.ReceivedAt, ID: last.ID}
    events = events[:limit]
}
```

### Why keyset over OFFSET

- **Stable results**: OFFSET skips rows by position, so inserts/deletes between pages cause items to be skipped or duplicated. Keyset pagination anchors to a specific row.
- **O(1) seek**: The database uses the index to jump directly to the cursor position. OFFSET must scan and discard rows, making deep pages increasingly expensive.
- **No count needed**: The `limit + 1` trick determines `has_more` without a separate `COUNT(*)` query.

---

## Authentication System

The auth system (`internal/auth/`) supports two authentication methods: session cookies and API keys. Both store only hashed tokens in the database.

### Password hashing

Passwords are hashed with bcrypt at cost 12:

```go
const bcryptCost = 12

func HashPassword(password string) (string, error) {
    hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
    return string(hash), nil
}
```

To prevent timing-based username enumeration, a `DummyCheckPassword` function compares against a pre-computed dummy hash when the user is not found. This ensures the response time is the same whether the user exists or not.

### Session tokens

1. Generate 32 random bytes.
2. Encode as base64url -- this is the raw token sent to the client as a cookie (`tl_session`).
3. Compute SHA-256 hex digest of the raw token -- this hash is stored in the `sessions` table.

```go
func GenerateSessionToken() (raw, hash string, err error) {
    b := make([]byte, 32)
    rand.Read(b)
    raw = base64.URLEncoding.EncodeToString(b)
    hash = HashToken(raw)   // SHA-256 hex
    return raw, hash, nil
}
```

On authentication, the middleware extracts the cookie, hashes it, and looks up the session by hash. Sessions have an `expires_at` and a `last_seen_at` that is touched asynchronously (fire-and-forget goroutine) on each request.

Session management includes pruning (keep N most recent per user) and cleanup of expired sessions.

### API keys

API keys use a `tl_` prefix followed by 43 base62 characters (0-9, A-Z, a-z), generated using `crypto/rand`:

```go
const apiKeyPrefix = "tl_"
const apiKeyLen    = 43

func GenerateAPIKey() (fullKey, hash, prefix string, err error) {
    chars := make([]byte, apiKeyLen)
    for i := range chars {
        n, _ := rand.Int(rand.Reader, big.NewInt(62))
        chars[i] = base62Chars[n.Int64()]
    }
    fullKey = apiKeyPrefix + string(chars)
    hash = HashToken(fullKey)        // SHA-256 hex
    prefix = fullKey[:10]            // display prefix
    return fullKey, hash, prefix, nil
}
```

The full key is shown once at creation time. The database stores only the SHA-256 hash and a 10-character display prefix for identification.

API keys have scopes (`read`, `ingest`, `admin`), optional expiration, and can be revoked. `last_used_at` is touched asynchronously on each use.

### Middleware chain

The `SessionOrAPIKey` middleware tries authentication in order:

1. **Session cookie**: Extract `tl_session` cookie -> SHA-256 hash -> look up in `sessions` table (joined with `users`). If valid and the user is active, store user in context.

2. **Bearer API key**: Extract `Authorization: Bearer tl_...` header -> SHA-256 hash -> look up in `api_keys` table (joined with `users`). If valid, not revoked, not expired, and user is active, store user and scopes in context.

3. If neither succeeds, return 401 Unauthorized.

### Scope enforcement

`RequireScope(scope)` middleware checks API key scopes. Session-based auth (nil scopes) gets full access. The `admin` scope grants access to everything:

```go
func hasScope(scopes []string, target string) bool {
    for _, s := range scopes {
        if s == target || s == "admin" {
            return true
        }
    }
    return false
}
```

Routes are grouped by scope in `serve.go`: `read` for GET endpoints, `ingest` for the POST ingest endpoint, and `admin` for write operations.

---

## Data Access Layer

The store (`internal/postgres/srvlog_store.go`, `applog_store.go`) uses pgx with the squirrel query builder.

### Query building

A global `psq` builder configured for PostgreSQL dollar-sign placeholders:

```go
var psq = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)
```

Filters are applied dynamically. Each non-zero filter field adds a WHERE clause:

```go
func applySrvlogFilter(qb sq.SelectBuilder, f model.SrvlogFilter) sq.SelectBuilder {
    if f.Hostname != "" {
        if strings.Contains(f.Hostname, "*") {
            pattern := strings.ReplaceAll(escapeLike(f.Hostname), "*", "%")
            qb = qb.Where("hostname ILIKE ?", pattern)
        } else {
            qb = qb.Where(sq.Eq{"hostname": f.Hostname})
        }
    }
    // severity, facility, search, time range, etc.
    return qb
}
```

Wildcard hostnames (`*`) are converted to ILIKE patterns with `%`. All other string fields use exact equality.

### LIKE escaping

The `escapeLike` helper escapes `%`, `_`, and `\` so they are treated as literal characters in LIKE/ILIKE clauses:

```go
func escapeLike(s string) string {
    s = strings.ReplaceAll(s, `\`, `\\`)
    s = strings.ReplaceAll(s, `%`, `\%`)
    s = strings.ReplaceAll(s, `_`, `\_`)
    return s
}
```

This prevents SQL injection through filter parameters that reach LIKE clauses.

### Full-text search

Srvlog search uses case-insensitive `ILIKE` with `%` wrapping:

```go
qb = qb.Where("message ILIKE ?", "%"+escaped+"%")
```

Applog search uses PostgreSQL's `tsvector`/`tsquery` for indexed full-text search:

```go
qb = qb.Where("search_vector @@ plainto_tsquery('simple', ?)", f.Search)
```

The `search_vector` column is maintained by a database trigger. The `'simple'` dictionary avoids stemming, which is appropriate for log messages.

### Batch insert

Applog ingest uses pgx's `Batch` API to insert multiple rows in a single round-trip:

```go
func (s *Store) InsertLogBatch(ctx context.Context, events []model.AppLogEvent) ([]model.AppLogEvent, error) {
    batch := &pgx.Batch{}
    for _, e := range events {
        batch.Queue(insertSQL, e.Timestamp, e.Level, e.Service, ...)
    }
    results := s.pool.SendBatch(ctx, batch)
    // scan RETURNING rows...
}
```

Each INSERT uses `RETURNING *` so the caller gets back the populated `id` and `received_at` without additional queries.

### Meta caching

Distinct values for filter dropdowns (hostnames, programs, services, etc.) come from materialized cache tables (`srvlog_meta_cache`, `applog_meta_cache`) rather than live `SELECT DISTINCT` queries. This avoids expensive sequential scans on large hypertables. Only whitelisted columns are allowed:

```go
var allowedMetaColumns = map[string]struct{}{
    "hostname": {}, "programname": {}, "syslogtag": {},
}
```

### Batch queries for device summaries

Device summary endpoints use pgx's `SendBatch` to run 4 queries in a single database round-trip: last-seen time, severity/level breakdown, top normalized messages, and recent critical/error logs. This reduces latency compared to sequential queries.

---

## Metrics Collection

Taillight exposes Prometheus metrics (`internal/metrics/`) covering HTTP, SSE, database, and notification subsystems.

### HTTP metrics

The `HTTPMetrics` middleware (`internal/metrics/middleware.go`) wraps each request and records:

- `taillight_http_requests_total{method, path, status}` -- counter
- `taillight_http_request_duration_seconds{method, path}` -- histogram

The `path` label uses the chi route pattern (e.g., `/api/v1/srvlog/{id}`) rather than the actual URL, keeping metric cardinality bounded.

### SSE metrics

| Metric | Type | Description |
|--------|------|-------------|
| `sse_clients_active` | gauge | Current srvlog SSE connections |
| `applog_sse_clients_active` | gauge | Current applog SSE connections |
| `events_broadcast_total` | counter | Srvlog events broadcast |
| `events_dropped_total` | counter | Srvlog events dropped (slow clients) |
| `applog_events_broadcast_total` | counter | Applog events broadcast |
| `applog_events_dropped_total` | counter | Applog events dropped |

### Database pool metrics

Scraped from the pgxpool stats:

- `db_pool_active_conns` -- active connections
- `db_pool_idle_conns` -- idle connections
- `db_pool_total_conns` -- total connections

### Ingest metrics

- `applog_ingest_total` -- total log entries ingested
- `applog_ingest_batches_total` -- ingest POST requests
- `applog_ingest_errors_total` -- failed ingest requests

### Notification metrics

- `notification_rules_evaluated_total` -- events x rules evaluated
- `notification_rules_matched_total` -- rule matches
- `notification_dispatched_total` -- notifications sent to dispatch queue
- `notification_sent_total{channel_type, status}` -- delivery outcomes
- `notification_suppressed_total{reason}` -- suppressed by cooldown/rate limit
- `notification_send_duration_seconds` -- send latency histogram
- `notification_dispatch_queue_length` -- current queue depth

### Snapshot API

The `metrics.Snapshot()` function (`internal/metrics/collector.go`) reads current values from Prometheus gauges and counters and returns a `model.MetricsSnapshot` struct. This is used by the internal metrics API endpoint to serve a JSON summary without requiring Prometheus.

---

## Log Shipper

The log shipper (`pkg/logshipper/`) is a `slog.Handler` that sends taillight's own application logs to its applog ingest endpoint. This allows taillight to monitor itself.

### How it works

```go
shipper := logshipper.New(logshipper.Config{
    Endpoint:  "http://localhost:8080/api/v1/applog/ingest",
    APIKey:    "tl_...",
    Service:   "taillight",
    MinLevel:  slog.LevelInfo,
    BatchSize: 100,
})
```

The handler implements the `slog.Handler` interface. When `Handle` is called, it converts the `slog.Record` into a `logEntry` struct and pushes it to a buffered channel (default capacity 1024).

### Batching

A background goroutine consumes from the channel and batches entries:

- **Size-triggered flush**: When the batch reaches `BatchSize` (default 100), it is sent immediately.
- **Time-triggered flush**: A ticker fires every `FlushPeriod` (default 1 second) and flushes whatever has accumulated.
- **Shutdown flush**: On `Shutdown()`, the channel is drained and a final flush is performed using a fresh `context.Background()`.

### Backpressure

The channel send is non-blocking:

```go
select {
case h.ch <- entry:
default:
    h.dropped.Add(1)   // ring buffer behavior: drop on full
}
```

If the ingest endpoint is down or slow, the buffer fills and new entries are dropped. The `Dropped()` counter tracks how many entries were lost. Failed sends also increment `SendFailed()`, and the batch is capped at `BatchSize * 10` to prevent unbounded memory growth.

### Level mapping

The shipper maps `slog.Level` values to taillight's five canonical levels:

| slog.Level | Taillight Level |
|------------|-----------------|
| < LevelInfo | DEBUG |
| LevelInfo | INFO |
| LevelWarn | WARN |
| LevelError | ERROR |
| >= LevelFatal (12) | FATAL |

### MultiHandler

The `logshipper.MultiHandler` fans out records to multiple `slog.Handler` implementations. This is used to send logs to both the console (text handler) and the shipper simultaneously:

```go
logger := slog.New(logshipper.MultiHandler(
    slog.NewTextHandler(os.Stdout, nil),
    shipper,
))
```

### slog.Handler contract

The shipper correctly implements `WithAttrs` and `WithGroup` by creating new `Handler` instances that share the same channel and done signal but carry their own pre-resolved attributes and group prefixes. Attributes are resolved through nested groups, with special handling for `time.Duration` (string representation), `error` (`.Error()`), and `fmt.Stringer` types.

---

## Key Design Decisions

### Why SSE over WebSockets

Server-Sent Events were chosen over WebSockets for the real-time streaming:

- **Simpler protocol**: SSE is plain HTTP with a `text/event-stream` Content-Type. No upgrade handshake, no frame masking, no ping/pong management.
- **Automatic reconnection**: The `EventSource` browser API handles reconnection natively, including sending `Last-Event-ID` to resume from where the client left off.
- **HTTP/2 compatible**: SSE connections multiplex over a single TCP connection with HTTP/2, eliminating the "6 connections per domain" limit.
- **One-directional by design**: Log viewing is inherently read-only. The client never needs to send data over the streaming connection; filter changes open a new connection.

### Why cursor pagination over OFFSET

- OFFSET pagination becomes unstable with real-time data: new events push existing ones to different positions, causing duplicates or gaps when paginating.
- OFFSET performance degrades linearly with page depth (must scan and discard N rows). Keyset pagination uses an index seek, which is O(1) regardless of depth.
- The `limit + 1` trick for `has_more` avoids a second COUNT query.

### Why a dedicated LISTEN connection

`WaitForNotification` blocks until a notification arrives. If this ran on a pooled connection, that connection would be held indefinitely, reducing the pool's effective size. With a dedicated connection, the pool remains fully available for query workload. The tradeoff is managing reconnection separately, but this is handled by the `Listener`'s reconnect loop with exponential backoff and gap fill.

### Why per-client filtering in the broker

Instead of creating separate database subscriptions per filter, all events are broadcast through a single broker and filters are applied in memory. This means:

- Only one LISTEN connection per event type regardless of how many clients are connected.
- Only one database fetch per event (by ID) regardless of client count.
- Filter evaluation is cheap (field comparisons) compared to the cost of additional database queries.
- Adding or removing clients doesn't change the database load.

### Why TimescaleDB

The `srvlog_events` and `applog_events` tables are TimescaleDB hypertables, which provide:

- **Time-based partitioning**: Data is automatically chunked by time interval. Old chunks can be dropped efficiently for retention.
- **Compression**: Older chunks are compressed, reducing storage by 90%+ for log data.
- **Retention policies**: `add_retention_policy` automatically drops data older than the configured threshold.
- **time_bucket**: The volume/heatmap queries use `time_bucket()` for efficient time-series aggregation.
- **Standard PostgreSQL**: All existing pgx queries, indexes, and extensions work unchanged.
