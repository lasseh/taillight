<!--
AI_CONTEXT: This is the complete, self-contained reference for integrating
Go applications with Taillight log ingestion via the logshipper package.

It contains everything needed to generate correct integration code:
package API, Config struct, slog.Handler implementation, level mapping,
attribute serialization rules, behavioral contracts, wire format,
error handling, framework patterns, and troubleshooting.

No external documents are required. All field names, defaults, and limits
are verified against the package source and server-side validation.

Import path: github.com/lasseh/taillight/api/pkg/logshipper
-->

# Go Log Shipper

Ship logs from any Go application to [Taillight](https://github.com/lasseh/taillight)'s
applog ingest endpoint using `log/slog` — no SDK-specific API to learn.

`logshipper` provides an `slog.Handler` that batches log entries in a
background goroutine and ships them via HTTP POST. It is non-blocking,
goroutine-safe, and drops entries on overflow rather than blocking your
application.

Source: [`api/pkg/logshipper/`](https://github.com/lasseh/taillight/tree/main/api/pkg/logshipper)

## Install

```sh
go get github.com/lasseh/taillight/api/pkg/logshipper
```

- **Go 1.21+** (requires `log/slog`)
- **No external dependencies** (stdlib only)

## Quick start

```go
package main

import (
    "context"
    "log/slog"
    "os"
    "os/signal"
    "time"

    "github.com/lasseh/taillight/api/pkg/logshipper"
)

func main() {
    handler := logshipper.New(logshipper.Config{
        Endpoint: "https://taillight.example.com/api/v1/applog/ingest",
        APIKey:   os.Getenv("TAILLIGHT_API_KEY"), // API key with "ingest" scope
        Service:  "my-api",                       // appears as the service name in Taillight UI
    })

    logger := slog.New(handler)
    slog.SetDefault(logger)

    slog.Info("server starting", "port", 8080)

    // Wait for interrupt.
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()
    <-ctx.Done()

    // Flush remaining logs on shutdown.
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := handler.Shutdown(shutdownCtx); err != nil {
        slog.Error("logshipper shutdown error", "error", err)
    }
}
```

## Exported API

The package exports these symbols:

| Symbol                  | Type       | Purpose                                       |
|-------------------------|------------|-----------------------------------------------|
| `Config`                | struct     | Configuration for the handler                 |
| `New(cfg Config)`       | func       | Create and start a handler                    |
| `Handler`               | struct     | `slog.Handler` implementation                 |
| `Handler.Shutdown(ctx)` | method     | Flush remaining entries and stop              |
| `Handler.Dropped()`     | method     | Count of entries dropped (buffer full)        |
| `Handler.SendFailed()`  | method     | Count of entries that failed to send          |
| `Handler.Enabled()`     | method     | `slog.Handler` interface                      |
| `Handler.Handle()`      | method     | `slog.Handler` interface                      |
| `Handler.WithAttrs()`   | method     | `slog.Handler` interface                      |
| `Handler.WithGroup()`   | method     | `slog.Handler` interface                      |
| `MultiHandler(...)`     | func       | Fan out to multiple `slog.Handler`s           |
| `LevelFatal`            | const      | Custom slog level for fatal entries (value 12)|

## Config reference

```go
logshipper.Config{
    Endpoint:    string          // Required
    APIKey:      string          // Optional (required when auth is enabled)
    Service:     string          // Required (server rejects entries without it)
    Component:   string          // Optional
    Host:        string          // Optional (defaults to os.Hostname())
    AddSource:   bool            // Optional (default: false)
    MinLevel:    slog.Level      // Optional (default: slog.LevelDebug)
    BatchSize:   int             // Optional (default: 100)
    FlushPeriod: time.Duration   // Optional (default: 1s)
    BufferSize:  int             // Optional (default: 1024)
    Client:      *http.Client    // Optional (default: http.DefaultClient)
}
```

| Field         | Type            | Default              | Required | Description                                                                  |
|---------------|-----------------|----------------------|----------|------------------------------------------------------------------------------|
| `Endpoint`    | `string`        | —                    | **yes**  | Full ingest URL, must end with `/api/v1/applog/ingest`                       |
| `APIKey`      | `string`        | `""`                 | no       | Bearer token sent as `Authorization: Bearer <key>`. Must have `ingest` scope when auth is enabled |
| `Service`     | `string`        | `""`                 | no*      | Service name attached to every entry. *The server requires this field — set it here or entries fail validation |
| `Component`   | `string`        | `""`                 | no       | Optional component label. Omitted from JSON when empty (`omitempty`)         |
| `Host`        | `string`        | `os.Hostname()`      | no       | Host/instance identifier. Auto-detected via `os.Hostname()` if not set       |
| `AddSource`   | `bool`          | `false`              | no       | Include `source` field with `file.go:line` from `runtime.CallersFrames`      |
| `MinLevel`    | `slog.Level`    | `slog.LevelDebug`    | no       | Minimum level to ship. Entries below this level are discarded by `Enabled()` before `Handle()` is called |
| `BatchSize`   | `int`           | `100`                | no       | Flush when the batch reaches this many entries                               |
| `FlushPeriod` | `time.Duration` | `1s`                 | no       | Flush at least this often. Whichever of `BatchSize` or `FlushPeriod` is reached first triggers a flush |
| `BufferSize`  | `int`           | `1024`               | no       | Buffered channel capacity. Entries are silently dropped when the channel is full |
| `Client`      | `*http.Client`  | `http.DefaultClient` | no       | HTTP client used for POST requests. Set timeouts here if needed              |

## Level mapping

The handler maps `slog.Level` values to Taillight's five canonical levels
using range-based matching:

| slog Level          | Numeric Value | Taillight Level |
|---------------------|---------------|-----------------|
| `slog.LevelDebug`   | -4 (and below)| `DEBUG`         |
| `slog.LevelInfo`    | 0             | `INFO`          |
| `slog.LevelWarn`    | 4             | `WARN`          |
| `slog.LevelError`   | 8             | `ERROR`         |
| `logshipper.LevelFatal` | 12        | `FATAL`         |

Intermediate values map to the highest level they meet or exceed. For example,
`slog.LevelInfo + 2` (value 2) maps to `INFO` because it's below `LevelWarn` (4).

### Using LevelFatal

```go
// Log a fatal-severity entry (the handler maps level 12+ to "FATAL").
slog.Log(ctx, logshipper.LevelFatal, "database connection lost",
    "host", "db-primary",
    "error", err,
)
```

### Level filtering with MinLevel

```go
// Only ship WARN and above — DEBUG and INFO are discarded.
handler := logshipper.New(logshipper.Config{
    Endpoint: os.Getenv("TAILLIGHT_URL"),
    APIKey:   os.Getenv("TAILLIGHT_API_KEY"),
    Service:  "my-api",
    MinLevel: slog.LevelWarn,
})
```

The server also accepts these level aliases (case-insensitive): `TRACE` → `DEBUG`,
`WARNING` → `WARN`, `CRITICAL` → `FATAL`, `PANIC` → `FATAL`.

## Structured attributes

Standard slog key-value pairs are serialized into the `attrs` JSON field:

```go
slog.Info("request handled",
    "method", "GET",
    "path", "/api/users",
    "status", 200,
    "duration", 42*time.Millisecond,
)
```

### WithAttrs — pre-resolved attributes

Use `slog.Logger.With()` to attach attributes to every entry from that logger:

```go
logger := slog.New(handler).With(
    "request_id", requestID,
    "user_id", userID,
)
logger.Info("order created", "order_id", order.ID)
// attrs: {"request_id": "abc-123", "user_id": 42, "order_id": 99}
```

Pre-resolved attributes are cloned — modifying the parent logger does not
affect child loggers.

### WithGroup — nested attribute objects

Use groups to namespace attributes into nested JSON objects:

```go
logger := slog.New(handler).WithGroup("http")
logger.Info("request", "method", "GET", "path", "/api/users")
// attrs: {"http": {"method": "GET", "path": "/api/users"}}
```

Groups can be stacked:

```go
logger := slog.New(handler).WithGroup("http").WithGroup("request")
logger.Info("received", "method", "GET")
// attrs: {"http": {"request": {"method": "GET"}}}
```

### Special type serialization

The handler applies these rules in order when serializing attribute values:

| Type                    | Serialization                              | Example                                           |
|-------------------------|--------------------------------------------|----------------------------------------------------|
| `time.Duration`         | `.String()` → human-readable string        | `42*time.Millisecond` → `"42ms"`                  |
| `error`                 | `.Error()` → string                        | `fmt.Errorf("timeout")` → `"timeout"`             |
| `fmt.Stringer` (only)   | `.String()` → string                       | `*url.URL` → `"https://example.com/path"`         |
| `json.Marshaler`        | Preserved as-is for `json.Marshal`         | `time.Time` → `"2026-01-15T10:30:00Z"` (RFC 3339)|
| Everything else         | Default `json.Marshal` behavior            | `int`, `string`, `struct`, `map`, `slice`, etc.   |

**Priority:** If a type implements both `fmt.Stringer` and `json.Marshaler`
(e.g., `time.Time`), the `json.Marshaler` is preferred. `fmt.Stringer` is
only used for types that do **not** implement `json.Marshaler`.

### Logging errors

```go
if err := db.Ping(); err != nil {
    slog.Error("database health check failed",
        "error", err,           // serialized as err.Error() string
        "host", "db-primary",
    )
}
```

Error values are always serialized as their `.Error()` string, not as empty
objects or via `json.Marshal`.

## Entry wire format

Each log entry is serialized into this JSON structure:

```jsonc
{
    "timestamp": "2026-01-15T10:30:00Z",          // time.Time from slog.Record
    "level": "INFO",                                // mapped from slog.Level (see table above)
    "msg": "request handled",                       // slog.Record.Message
    "service": "my-api",                            // from Config.Service
    "host": "prod-1",                               // from Config.Host or os.Hostname()
    "component": "server",                          // from Config.Component (omitted if empty)
    "source": "handler.go:42",                      // from runtime.CallersFrames (only if AddSource=true)
    "attrs": {                                      // from slog key-value pairs (omitted if none)
        "method": "GET",
        "status": 200,
        "duration": "42ms"
    }
}
```

Entries are batched and sent as:

```json
{
    "logs": [
        { "timestamp": "...", "level": "...", "msg": "...", ... },
        { "timestamp": "...", "level": "...", "msg": "...", ... }
    ]
}
```

The `component`, `host`, `source`, and `attrs` fields use `omitempty` — they
are omitted from the JSON when empty/nil.

## Behavioral specification

### Goroutine model

`logshipper.New()` starts a single background goroutine that drains a
buffered channel. The goroutine runs until `Shutdown()` is called.

### Non-blocking Handle

`Handle()` sends entries to a buffered channel using a `select` with a
`default` case. It **never blocks** the calling goroutine. If the channel
is full, the entry is silently dropped and the `Dropped()` counter is
incremented.

### Flush triggers

The background goroutine flushes a batch when **either** condition is met:

1. The batch accumulates `BatchSize` entries, **or**
2. `FlushPeriod` has elapsed since the last flush attempt (ticker)

Whichever happens first triggers the HTTP POST.

### Error handling on send failure

When a batch POST fails (HTTP >= 400, connection error, timeout):

1. A warning is logged via `slog.Default()`: `"logshipper send failed"`
2. `SendFailed()` counter is incremented by the batch size
3. The batch is **retained** for retry on the next flush cycle
4. If the retained batch grows to `BatchSize * 10` (default 1000), it is
   cleared to prevent unbounded memory growth

There is **no exponential backoff** — failed batches are retried on the next
ticker tick or when new entries fill a batch. The retry interval is
effectively `FlushPeriod` (default 1s).

### Batch retention on failure

```
Send succeeds  → batch cleared, counter reset
Send fails     → batch kept, retried next flush
Batch >= 10x   → batch cleared to prevent OOM
```

This means transient failures (brief network blip) are automatically retried,
but persistent failures eventually shed entries to protect memory.

## Shutdown and lifecycle

### Shutdown sequence

```go
shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
if err := handler.Shutdown(shutdownCtx); err != nil {
    slog.Error("logshipper shutdown", "error", err)
}
```

**What `Shutdown(ctx)` does:**
1. Closes the internal `done` channel (once, via `sync.Once`)
2. The goroutine drains all remaining entries from the channel
3. Flushes the final batch using `context.Background()` (not the shutdown
   context) so the final POST is not cancelled prematurely
4. Goroutine exits, `wg.Done()` is called
5. If the goroutine finishes before `ctx` deadline → returns `nil`
6. If `ctx` expires first → cancels internal context and returns `ctx.Err()`

`Shutdown` is **idempotent** — safe to call multiple times (the channel is
closed via `sync.Once`).

### Important: no atexit

Unlike the Python SDK, the Go handler does **not** register an automatic
cleanup hook. You must call `Shutdown()` explicitly in your application's
shutdown path. If you don't, buffered entries may be lost when the process
exits.

## MultiHandler — dual output

`MultiHandler` fans out log records to multiple `slog.Handler` implementations.
Use it to print logs locally while shipping to Taillight:

```go
shipper := logshipper.New(logshipper.Config{
    Endpoint: os.Getenv("TAILLIGHT_URL"),
    APIKey:   os.Getenv("TAILLIGHT_API_KEY"),
    Service:  "my-api",
})

logger := slog.New(logshipper.MultiHandler(
    shipper,
    slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}),
))
slog.SetDefault(logger)
```

**How MultiHandler works:**
- `Enabled()` returns `true` if **any** child handler is enabled for the level
- `Handle()` clones the record (`r.Clone()`) and sends to each enabled child
- `WithAttrs()` / `WithGroup()` propagate to all children
- Errors from children are combined via `errors.Join()`

## Monitoring

The handler exposes two methods for health checks:

```go
handler.Dropped()    // int64 — entries dropped because the channel was full
handler.SendFailed() // int64 — entries that failed to send (HTTP errors, timeouts, etc.)
```

Both use `atomic.Int64` and are safe to call from any goroutine.

Example health check endpoint:

```go
func healthHandler(shipper *logshipper.Handler) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        json.NewEncoder(w).Encode(map[string]any{
            "dropped":     shipper.Dropped(),
            "send_failed": shipper.SendFailed(),
            "healthy":     shipper.Dropped() == 0 && shipper.SendFailed() == 0,
        })
    }
}
```

If `Dropped()` is increasing, raise `BufferSize` or reduce log volume.
If `SendFailed()` is increasing, check the endpoint URL and API key.

## Integration patterns

### HTTP server with graceful shutdown

```go
package main

import (
    "context"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "time"

    "github.com/lasseh/taillight/api/pkg/logshipper"
)

func main() {
    shipper := logshipper.New(logshipper.Config{
        Endpoint:  os.Getenv("TAILLIGHT_URL"),
        APIKey:    os.Getenv("TAILLIGHT_API_KEY"),
        Service:   "my-api",
        Component: "server",
        AddSource: true,
    })

    logger := slog.New(logshipper.MultiHandler(
        shipper,
        slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}),
    ))
    slog.SetDefault(logger)

    srv := &http.Server{Addr: ":8080", Handler: mux}

    go func() {
        slog.Info("server starting", "addr", srv.Addr)
        if err := srv.ListenAndServe(); err != http.ErrServerClosed {
            slog.Error("server error", "error", err)
        }
    }()

    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()
    <-ctx.Done()

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    srv.Shutdown(shutdownCtx)

    // Flush logs AFTER the server has drained connections.
    if err := shipper.Shutdown(shutdownCtx); err != nil {
        slog.Error("logshipper shutdown", "error", err)
    }
}
```

### Request-scoped logger (chi / net/http middleware)

```go
func LoggingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        logger := slog.Default().With(
            "request_id", r.Header.Get("X-Request-ID"),
            "method", r.Method,
            "path", r.URL.Path,
        )
        ctx := context.WithValue(r.Context(), loggerKey, logger)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

// In handlers:
func handleGetUser(w http.ResponseWriter, r *http.Request) {
    logger := r.Context().Value(loggerKey).(*slog.Logger)
    logger.Info("fetching user", "user_id", chi.URLParam(r, "id"))
    // attrs: {"request_id": "abc", "method": "GET", "path": "/users/42", "user_id": "42"}
}
```

### gRPC server

```go
func main() {
    shipper := logshipper.New(logshipper.Config{
        Endpoint:  os.Getenv("TAILLIGHT_URL"),
        APIKey:    os.Getenv("TAILLIGHT_API_KEY"),
        Service:   "my-grpc-service",
        Component: "grpc",
    })
    slog.SetDefault(slog.New(shipper))

    lis, _ := net.Listen("tcp", ":50051")
    srv := grpc.NewServer()
    // ... register services ...

    go srv.Serve(lis)

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt)
    <-sigCh
    srv.GracefulStop()

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    shipper.Shutdown(ctx)
}
```

### Worker / background job

```go
func runWorker(ctx context.Context) {
    shipper := logshipper.New(logshipper.Config{
        Endpoint:  os.Getenv("TAILLIGHT_URL"),
        APIKey:    os.Getenv("TAILLIGHT_API_KEY"),
        Service:   "my-worker",
        Component: "jobs",
    })
    defer func() {
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        shipper.Shutdown(shutdownCtx)
    }()

    logger := slog.New(shipper)

    for {
        select {
        case <-ctx.Done():
            return
        case job := <-jobCh:
            logger.Info("processing job", "job_id", job.ID, "type", job.Type)
            if err := process(job); err != nil {
                logger.Error("job failed", "job_id", job.ID, "error", err)
            }
        }
    }
}
```

### Custom HTTP client with timeout

```go
handler := logshipper.New(logshipper.Config{
    Endpoint: os.Getenv("TAILLIGHT_URL"),
    APIKey:   os.Getenv("TAILLIGHT_API_KEY"),
    Service:  "my-api",
    Client: &http.Client{
        Timeout: 3 * time.Second,
        Transport: &http.Transport{
            MaxIdleConnsPerHost: 2,
        },
    },
})
```

## Environment variable configuration

A reusable factory pattern:

```go
package logging

import (
    "log/slog"
    "os"

    "github.com/lasseh/taillight/api/pkg/logshipper"
)

// NewShipper creates a logshipper.Handler configured from environment variables.
//
// Environment variables:
//
//   TAILLIGHT_URL       — Ingest endpoint URL (required)
//   TAILLIGHT_API_KEY   — API key with ingest scope (required when auth is enabled)
//   TAILLIGHT_SERVICE   — Service name (required)
//   TAILLIGHT_COMPONENT — Component label (optional)
func NewShipper() *logshipper.Handler {
    return logshipper.New(logshipper.Config{
        Endpoint:  os.Getenv("TAILLIGHT_URL"),
        APIKey:    os.Getenv("TAILLIGHT_API_KEY"),
        Service:   os.Getenv("TAILLIGHT_SERVICE"),
        Component: os.Getenv("TAILLIGHT_COMPONENT"),
        AddSource: true,
    })
}
```

## Ingest API reference

Full specification of the HTTP endpoint that the handler calls.

**Endpoint:** `POST /api/v1/applog/ingest`

**Headers:**
```
Content-Type: application/json
Authorization: Bearer <API_KEY>
```

The API key must have the **`ingest`** scope. Keys are created via the
`taillight apikey` CLI command. When authentication is disabled in the
server config, the `Authorization` header is optional.

### Request body

```json
{
    "logs": [
        {
            "timestamp": "2026-01-15T10:30:00Z",
            "level": "INFO",
            "msg": "hello world",
            "service": "my-app",
            "host": "prod-1",
            "component": "worker",
            "source": "main.go:42",
            "attrs": {"key": "value"}
        }
    ]
}
```

### Field constraints

| Field       | Required | Type   | Max Size   | Notes                                                      |
|-------------|----------|--------|------------|------------------------------------------------------------|
| `timestamp` | **yes**  | string | —          | RFC 3339 (e.g., `2026-01-15T10:30:00Z`). The handler uses `time.Time` from `slog.Record` |
| `level`     | **yes**  | string | —          | `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`. Case-insensitive. Aliases: `TRACE`→`DEBUG`, `WARNING`→`WARN`, `CRITICAL`/`PANIC`→`FATAL` |
| `msg`       | **yes**  | string | 64 KB      | The log message                                             |
| `service`   | **yes**  | string | 128 chars  | Service name                                                |
| `host`      | **yes**  | string | 256 chars  | Hostname or instance identifier                             |
| `component` | no       | string | 128 chars  | Component or subsystem label                                |
| `source`    | no       | string | 256 chars  | Source file and line (e.g., `main.go:42`)                   |
| `attrs`     | no       | object | 64 KB      | Arbitrary JSON key-value pairs                              |

### Batch limits

- Maximum **1,000 entries** per request
- Maximum **5 MB** request body

### Success response

**`202 Accepted`**

```json
{"accepted": 5}
```

The `accepted` field contains the number of entries stored.

### Error responses

All errors use this envelope:

```json
{
    "error": {
        "code": "validation_failed",
        "message": "logs[0]: service is required; logs[0]: host is required"
    }
}
```

Validation errors include the array index of each failing entry.

| HTTP Status | Error Code          | Cause                                                    |
|-------------|---------------------|----------------------------------------------------------|
| 400         | `invalid_json`      | Request body is not valid JSON                           |
| 400         | `empty_batch`       | `logs` array is empty                                    |
| 400         | `batch_too_large`   | More than 1,000 entries in the batch                     |
| 400         | `validation_failed` | One or more entries failed field validation (details in `message`) |
| 401         | `unauthorized`      | Missing or invalid API key (when auth is enabled)        |
| 403         | `forbidden`         | API key does not have the `ingest` scope                 |
| 413         | `body_too_large`    | Request body exceeds 5 MB                                |
| 500         | `insert_failed`     | Server-side database error (retrying may help)           |

## Troubleshooting

### No logs appearing in Taillight

1. Verify the endpoint URL ends with `/api/v1/applog/ingest`
2. Check that the API key has the `ingest` scope
3. Check `handler.SendFailed()` — if > 0, the handler cannot reach the server
4. Look for `"logshipper send failed"` warnings in stderr (logged via `slog.Default()`)
5. Ensure `Service` is set in Config (required by the server)
6. Check that `MinLevel` isn't filtering out the entries you expect

### Logs appear with a delay

The default `FlushPeriod` is 1 second. To reduce latency:
- Lower `FlushPeriod` (e.g., `250 * time.Millisecond`)
- Lower `BatchSize` if entries arrive slowly (e.g., `10`)

### `Dropped()` counter is increasing

The buffered channel is full — the app is producing logs faster than they
can be shipped. Options:
- Increase `BufferSize` (e.g., `4096` or `8192`)
- Reduce log volume (raise `MinLevel` to `slog.LevelWarn`)
- Check if `SendFailed()` is also increasing (network issues cause a backlog)

### `SendFailed()` counter is increasing

The HTTP POST to the ingest endpoint is failing. Check:
- Endpoint URL is correct and reachable
- API key is valid and has `ingest` scope
- Server is running and healthy
- The `Client` timeout is sufficient (default is `http.DefaultClient` with no timeout)

### 401 Unauthorized or 403 Forbidden

- **401**: API key is missing or invalid. Check the `APIKey` config value.
- **403**: API key exists but lacks the `ingest` scope. Create a new key
  with `taillight apikey create --scope ingest`.

### Lost entries on shutdown

The handler does **not** register any automatic cleanup. If the process exits
without calling `Shutdown()`, buffered entries are lost. Always call
`Shutdown(ctx)` in your teardown path with a reasonable timeout.

### Memory growing under persistent failures

When send failures persist, the handler retains failed batches for retry.
Batches are capped at `BatchSize * 10` (default 1000 entries) to prevent
unbounded growth. If memory pressure is a concern, lower `BatchSize` or
`BufferSize`.
