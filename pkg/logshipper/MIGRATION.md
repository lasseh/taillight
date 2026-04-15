# logshipper Migration Guide

This guide is written for an AI assistant (or careful human) upgrading a Go
project that imports `github.com/lasseh/taillight/pkg/logshipper` to the
version that includes the reliability and security fixes.

Work through the steps in order. Do not skip verification.

---

## 1. Bump the dependency

From the downstream project root:

```sh
go get github.com/lasseh/taillight/pkg/logshipper@main
go mod tidy
```

If a tag exists (e.g. `pkg/logshipper/v0.1.0`), prefer the tag:

```sh
go get github.com/lasseh/taillight/pkg/logshipper@v0.1.0
```

The build **will fail** after this step. That's expected — the next steps fix it.

---

## 2. Find every call site

Two patterns to search for:

```sh
# Constructor calls — every one of these needs updating.
rg -n 'logshipper\.New\(' --type go

# APIKey assignments — may need a cast (see step 4).
rg -n 'APIKey:\s' --type go | rg -i 'logshipper|Config'
```

Work through every match. Do not assume there is only one.

---

## 3. Breaking change: `New` now returns `(*Handler, error)`

### Before
```go
shipper := logshipper.New(logshipper.Config{
    Endpoint: "...",
    APIKey:   apiKey,
    Service:  "myapp",
})
```

### After
```go
shipper, err := logshipper.New(logshipper.Config{
    Endpoint: "...",
    APIKey:   logshipper.Secret(apiKey),
    Service:  "myapp",
})
if err != nil {
    // handle — see guidance below
}
```

### How to handle the error

The error fires only for invalid config (bad `Endpoint` scheme, empty host).
Pick the pattern that fits the surrounding code:

- **In `main` / startup code:** log and `os.Exit(1)`. A broken logger config
  at startup is a fatal misconfiguration.
- **In a library constructor that returns an error:** propagate with
  `fmt.Errorf("init logshipper: %w", err)`.
- **In a fallback-capable logger setup** (e.g. the caller already has a
  stderr `slog.Handler`): log the error via the fallback and continue without
  shipping. Example:
  ```go
  shipper, err := logshipper.New(cfg)
  if err != nil {
      slog.New(consoleHandler).Error("logshipper init failed", "error", err)
      return slog.New(consoleHandler), nil
  }
  ```

Do **not** use `_` to discard the error. Do **not** `panic` unless the
surrounding code already panics on startup errors.

---

## 4. Breaking change: `Config.APIKey` is now `logshipper.Secret`

`Secret` is a named string type with redacting `String()`, `GoString()`, and
`MarshalJSON()` methods. Accidental `%+v` / JSON dumps now print
`[REDACTED]` instead of the token.

### String literal — no change needed
```go
APIKey: "hardcoded-token",  // still compiles (untyped string constant)
```

### String variable — needs a cast
```go
// Before
APIKey: apiKey,

// After
APIKey: logshipper.Secret(apiKey),
```

### Struct field of type string — needs a cast
```go
// Before
APIKey: cfg.LogShipper.APIKey,

// After
APIKey: logshipper.Secret(cfg.LogShipper.APIKey),
```

Do **not** change the upstream variable's type to `logshipper.Secret` — that
leaks the library type into the caller's config structs. Cast only at the
`logshipper.Config{}` literal.

---

## 5. Runtime behavior change: endpoint validation

`New` now rejects:
- Empty `Endpoint`
- `Endpoint` with a scheme other than `http` or `https`
- `Endpoint` with no host (e.g. `http://`)

If the project ever set `Endpoint` from user input, env, or config file,
verify the error handling in step 3 actually surfaces the problem to the
operator — don't let it silently disable log shipping.

---

## 6. Optional: adopt the new Config fields

These are **all optional**. Existing code keeps working without touching
them. Add them only if the project has a relevant need.

| Field | Type | Purpose |
|---|---|---|
| `InsecureSkipVerify` | `bool` | Disable TLS verification for the ingest endpoint. Only for self-signed certs in dev/internal. Ignored if `Client` is set. |
| `SendTimeout` | `time.Duration` | Per-request HTTP timeout. Default 30s. Enforced whether or not `Client` is set. |
| `Redact` | `func(key string, value any) any` | Called for every attr value before JSON marshalling. Return `nil` to drop the attr. Use to scrub PII, tokens, session IDs. |

### Example: TLS skip for a self-signed internal endpoint
```go
logshipper.Config{
    Endpoint:           "https://taillight.internal/api/v1/applog/ingest",
    APIKey:             logshipper.Secret(apiKey),
    Service:            "myapp",
    InsecureSkipVerify: true,
}
```

### Example: redact known-sensitive attr keys
```go
logshipper.Config{
    // ...
    Redact: func(key string, v any) any {
        switch key {
        case "password", "token", "authorization", "cookie":
            return "[REDACTED]"
        }
        return v
    },
}
```

---

## 7. Do NOT change these

- Existing `Config.Client` injection patterns. If the caller builds its own
  `*http.Client` (e.g. to set custom CAs, proxies, auth transports), leave
  it alone. `InsecureSkipVerify` on `Config` is ignored when `Client` is set
  — that's intentional, the caller already owns TLS.
- Existing `Dropped()` / `SendFailed()` counter reads. Semantics are
  unchanged; internally they're now shared across derived loggers, which
  is strictly an improvement.
- Existing `Shutdown(ctx)` call sites. The signature is the same. Shutdown
  now additionally marks the handler as closing so concurrent `Handle`
  calls drop fast instead of racing the channel — no caller change required.

---

## 8. Verify

Run in order, fix any failure before proceeding:

```sh
go build ./...
go vet ./...
go test -race ./...
```

If the project has a `Makefile`, prefer:

```sh
make build
make lint
make test
```

Manual smoke test worth doing: run the binary locally, confirm log entries
actually reach the ingest endpoint (check for a successful HTTP 2xx in the
server logs, or verify `shipper.SendFailed() == 0` after a known number of
shipped records).

---

## 9. What the caller gets for free

No code changes required for any of these — they're automatic:

- **No more hung drain loop** on a stalled ingest endpoint (30s default timeout).
- **No more lost entries** on shutdown race (new `closing` flag).
- **Accurate `Dropped()` / `SendFailed()` counters** across `logger.With(...)` chains.
- **No more Bearer token leak** on HTTP redirects (`CheckRedirect` disabled).
- **URL scheme validation** rejects `file://`, `gopher://`, etc.
- **Failed batches dropped immediately** instead of growing to 10×`BatchSize`
  and then getting dumped all at once.

---

## Troubleshooting

**`New` returns "endpoint %q: scheme must be http or https"**
The `Endpoint` config value is malformed. Check for typos, missing
`http://` prefix, or accidentally passing just a hostname.

**`cannot use X (variable of type string) as logshipper.Secret`**
You missed a `logshipper.Secret(...)` cast in step 4. Re-run the grep.

**`assignment mismatch: 1 variable but logshipper.New returns 2 values`**
You missed a call site in step 3. Re-run the grep.

**Tests hang or take 30s to shut down**
Check whether a test is submitting logs to an unreachable endpoint without
calling `Shutdown` with a short-deadline context. `defer shipper.Shutdown(
ctx)` with a 1–2 second timeout is the fix.
