# logshipper

`logshipper` is an `slog.Handler` that batches log entries and ships them to a
Taillight applog ingest endpoint over HTTP.

Drop it into any Go application that uses `log/slog` — your existing log calls
become the integration point, no SDK-specific API to learn.

## Install

```
go get github.com/lasseh/taillight/pkg/logshipper
```

## Quick start

```go
package main

import (
    "context"
    "log/slog"
    "os"
    "os/signal"
    "time"

    "github.com/lasseh/taillight/pkg/logshipper"
)

func main() {
    handler := logshipper.New(logshipper.Config{
        Endpoint:  "https://taillight.example.com/api/v1/applog/ingest",
        APIKey:    os.Getenv("TAILLIGHT_API_KEY"),
        Service:   "my-api",
        Component: "server",
        Host:      "prod-1",
    })

    logger := slog.New(handler)
    slog.SetDefault(logger)

    slog.Info("server starting", "port", 8080)

    // Flush on shutdown.
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()
    <-ctx.Done()

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := handler.Shutdown(shutdownCtx); err != nil {
        slog.Error("shutdown error", "err", err)
    }
}
```

## Log locally and ship remotely

Use `MultiHandler` to fan out to multiple handlers — e.g. pretty-print to
stderr in development while shipping to Taillight:

```go
shipper := logshipper.New(logshipper.Config{
    Endpoint: "https://taillight.example.com/api/v1/applog/ingest",
    APIKey:   os.Getenv("TAILLIGHT_API_KEY"),
    Service:  "my-api",
    Host:     hostname,
})

logger := slog.New(logshipper.MultiHandler(
    shipper,
    slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}),
))
```

## Config reference

| Field         | Type            | Default          | Description                                              |
|---------------|-----------------|------------------|----------------------------------------------------------|
| `Endpoint`    | `string`        | —                | Ingest URL (required)                                    |
| `APIKey`      | `string`        | —                | Bearer token for authentication                          |
| `Service`     | `string`        | —                | Service name attached to every entry                     |
| `Component`   | `string`        | `""`             | Optional component label                                 |
| `Host`        | `string`        | `""`             | Host/instance identifier (required by the ingest API)    |
| `MinLevel`    | `slog.Level`    | `slog.LevelDebug`| Minimum level to ship                                    |
| `BatchSize`   | `int`           | `100`            | Flush when batch reaches this size                       |
| `FlushPeriod` | `time.Duration` | `1s`             | Flush at least this often                                |
| `BufferSize`  | `int`           | `1024`           | Buffered channel capacity                                |
| `Client`      | `*http.Client`  | `http.DefaultClient` | HTTP client for requests                            |

## How it works

1. `logshipper.New()` starts a background goroutine that drains a buffered channel.
2. Each `slog` call pushes a log entry onto the channel (non-blocking; drops if full).
3. Entries are flushed as a JSON batch POST when the batch is full or the flush timer fires.
4. `Shutdown()` drains remaining entries and stops the goroutine.

Call `handler.Dropped()` to check how many entries were dropped due to a full buffer.

## Structured attributes

Standard slog attributes are serialized into the `attrs` JSON field:

```go
logger.Info("request handled",
    "method", "GET",
    "path", "/api/users",
    "status", 200,
    "duration_ms", 42,
)
```

This ships as:

```json
{
    "timestamp": "2025-01-15T10:30:00Z",
    "level": "INFO",
    "msg": "request handled",
    "service": "my-api",
    "host": "prod-1",
    "attrs": {
        "method": "GET",
        "path": "/api/users",
        "status": 200,
        "duration_ms": 42
    }
}
```

`WithAttrs` and `WithGroup` work as expected — pre-resolved attributes are
included in every entry from that logger, and groups nest into the attrs object.
