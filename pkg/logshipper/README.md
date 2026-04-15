# logshipper

`logshipper` is an `slog.Handler` that batches log entries and ships them to a
Taillight applog ingest endpoint over HTTP.

Drop it into any Go application that uses `log/slog` — your existing log calls
become the integration point, no SDK-specific API to learn.

> **Upgrading from an earlier version?** See [MIGRATION.md](MIGRATION.md) for
> the two breaking changes (`New` now returns an error, `APIKey` is now a
> `Secret` type).

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
    handler, err := logshipper.New(logshipper.Config{
        Endpoint:  "https://taillight.example.com/api/v1/applog/ingest",
        APIKey:    logshipper.Secret(os.Getenv("TAILLIGHT_API_KEY")),
        Service:   "my-api",
        Component: "server",
        Host:      "prod-1",
    })
    if err != nil {
        slog.Error("logshipper init failed", "error", err)
        os.Exit(1)
    }

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

Use `MultiHandler` to fan out — e.g. pretty-print to stderr in development
while shipping to Taillight:

```go
shipper, err := logshipper.New(logshipper.Config{
    Endpoint: "https://taillight.example.com/api/v1/applog/ingest",
    APIKey:   logshipper.Secret(os.Getenv("TAILLIGHT_API_KEY")),
    Service:  "my-api",
    Host:     hostname,
})
if err != nil {
    // fall back to stderr-only if the shipper can't start
    return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

logger := slog.New(logshipper.MultiHandler(
    shipper,
    slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}),
))
```

## The `Secret` type

`Config.APIKey` is a `logshipper.Secret` (a named string type) rather than a
plain `string`. Its `String()`, `GoString()`, and `MarshalJSON()` methods all
return `"[REDACTED]"`, so accidental logging via `%v` / `%+v` / `%#v` or JSON
encoding can never leak the token.

String literals convert implicitly, so hardcoded values still work without a
cast:

```go
APIKey: "hardcoded-token",   // OK — untyped string constant
APIKey: logshipper.Secret(apiKeyVar),  // needed for string variables
```

Cast to `string` only at the point of use (the library does this internally
when building the `Authorization` header).

## TLS for self-signed / internal endpoints

Set `InsecureSkipVerify: true` to skip TLS verification. Intended for
development or internal endpoints with a self-signed cert — do not use
against public endpoints:

```go
handler, err := logshipper.New(logshipper.Config{
    Endpoint:           "https://taillight.internal/api/v1/applog/ingest",
    APIKey:             logshipper.Secret(apiKey),
    Service:            "my-api",
    InsecureSkipVerify: true,
})
```

For stricter setups (custom CA pool, client certs, proxy), build your own
`*http.Client` and pass it via `Config.Client`. When `Client` is set,
`InsecureSkipVerify` is ignored and you own the full TLS configuration.

## Scrubbing sensitive attributes

`Config.Redact` runs for every attr value before JSON marshalling. Return
`nil` to drop the attr entirely. Use it to scrub PII, tokens, session IDs:

```go
handler, err := logshipper.New(logshipper.Config{
    // ...
    Redact: func(key string, v any) any {
        switch key {
        case "password", "token", "authorization", "cookie":
            return "[REDACTED]"
        case "email":
            return maskEmail(v)
        }
        return v
    },
})
```

For sensitive types you control, implement `slog.LogValuer` — `logshipper`
resolves `LogValuer` before calling `Redact`, so you can redact at the type
level without a per-key switch.

## Config reference

| Field                | Type                            | Default          | Description                                                                                 |
|----------------------|---------------------------------|------------------|---------------------------------------------------------------------------------------------|
| `Endpoint`           | `string`                        | —                | Ingest URL. Must be `http://` or `https://` with a non-empty host. Validated in `New`.      |
| `APIKey`             | `Secret`                        | —                | Bearer token for authentication. Redacted in all string/JSON formatting.                    |
| `Service`            | `string`                        | —                | Service name attached to every entry.                                                       |
| `Component`          | `string`                        | `""`             | Optional component label.                                                                   |
| `Host`               | `string`                        | `os.Hostname()`  | Host/instance identifier.                                                                   |
| `AddSource`          | `bool`                          | `false`          | Include source `file:line` from the calling function.                                       |
| `MinLevel`           | `slog.Level`                    | `slog.LevelDebug`| Minimum level to ship.                                                                      |
| `BatchSize`          | `int`                           | `100`            | Flush when batch reaches this size.                                                         |
| `FlushPeriod`        | `time.Duration`                 | `1s`             | Flush at least this often.                                                                  |
| `BufferSize`         | `int`                           | `1024`           | Buffered channel capacity. Entries are dropped (and counted) when full.                     |
| `SendTimeout`        | `time.Duration`                 | `30s`            | Per-request HTTP timeout. Enforced whether or not `Client` is set.                          |
| `Client`             | `*http.Client`                  | built-in         | Optional custom HTTP client. If set, `InsecureSkipVerify` is ignored; caller owns TLS.      |
| `InsecureSkipVerify` | `bool`                          | `false`          | Disable TLS certificate verification. Only honored when `Client` is nil.                    |
| `Redact`             | `func(key string, v any) any`   | `nil`            | Called for every attr value before marshalling. Return `nil` to drop the attr.              |

## How it works

1. `logshipper.New()` validates the config, builds an HTTP client with sane
   defaults, and starts a background goroutine that drains a buffered channel.
2. Each `slog` call pushes a log entry onto the channel (non-blocking; drops
   if full, counted in `Dropped()`).
3. Entries are flushed as a JSON batch `POST` when the batch is full or the
   flush timer fires.
4. Failed batches are **dropped**, not retained — `SendFailed()` is
   incremented by the batch size. Callers that need durability should front
   the ingest with a sidecar agent (vector, fluent-bit, etc.).
5. `Shutdown(ctx)` marks the handler as closing (concurrent `Handle` calls
   drop fast instead of racing the channel), drains remaining entries, and
   waits for the final flush to finish or `ctx` to expire.

### Built-in guarantees

- **Bounded send time** — every HTTP request is capped by `SendTimeout`
  (30s default). A hung endpoint cannot stall the drain loop.
- **No Bearer token leak on redirect** — the built-in client sets
  `CheckRedirect` to `http.ErrUseLastResponse`.
- **No SSRF via malformed endpoints** — `New` rejects any URL whose scheme
  is not `http` or `https`, or whose host is empty.
- **Accurate counters across `With(...)` chains** — `Dropped()` and
  `SendFailed()` aggregate over all handlers derived from a single `New`
  call via `WithAttrs` / `WithGroup`.
- **Predictable memory** — failed batches are dropped immediately rather
  than retained and grown until a cap is hit.

## Observability

```go
handler.Dropped()     // entries dropped due to full buffer or post-shutdown submission
handler.SendFailed()  // entries that belonged to a batch that failed to POST
```

Both counters are cumulative and shared across every handler derived from
the same `New` call.

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
included in every entry from that logger, and groups nest into the attrs
object. Counters and the shutdown flag are shared across the derived
handlers, so `logger.With(...)` does not lose metrics.
