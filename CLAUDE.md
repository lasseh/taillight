# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Auto-commit
After completing a task or feature, always run /commit before stopping.

## Build & Test Commands

All Go commands run from `api/` via Make. The root Makefile delegates to component Makefiles.

```sh
# API (from api/)
make build          # build binary
make test           # run tests with -race
make lint           # golangci-lint run ./...
make fix            # apply Go version modernizations (go fix)
make coverage       # tests + coverage HTML report
make tidy           # go mod tidy + verify clean

# Frontend (from frontend/)
make install        # npm install
make dev            # start dev server
make lint           # type-check
make build          # production build

# Full stack (from repo root)
make up / make down # docker compose up/down
make test           # delegates to api/ tests
make lint           # delegates to api/ + frontend/ lint
```

Run a single test: `cd api && go test -race -run TestName ./internal/handler/...`

## Architecture

Real-time syslog/applog viewer. Data flows:

```
rsyslog → ompgsql → syslog_events (TimescaleDB) → pg_notify('syslog_ingest', id)
                                                          ↓
Go Listener (pgx LISTEN) → SyslogBroker (fan-out) → SSE → browser EventSource
                                ↓
                         NotificationEngine → Slack/Webhook backends
```

App logs arrive via HTTP ingest (`POST /api/v1/applog/ingest`) → `applog_events` table → `AppLogBroker` → SSE.

### Key packages

- `cmd/taillight/` — cobra CLI: `serve`, `migrate`, `loadgen`, `applog-loadgen`, `useradd`, `apikey`, `import`
- `cmd/taillight-shipper/` — standalone log file tailer that ships to the ingest API
- `internal/handler/` — HTTP handlers (one file per domain: `syslog.go`, `applog.go`, `stats.go`, `notification.go`, etc.)
- `internal/broker/` — SSE fan-out brokers (`SyslogBroker`, `AppLogBroker`) with per-client filtering
- `internal/postgres/` — `Store` (main pgx queries) + domain-specific `*_store.go` files + `Listener` (LISTEN/NOTIFY)
- `internal/model/` — domain types, filter parsing from HTTP query params, cursor pagination
- `internal/auth/` — session + API key middleware; scope-based access control (`read`, `ingest`, `admin`)
- `internal/notification/` — rule engine with burst detection, cooldown, rate limiting, circuit breakers
- `internal/config/` — viper config from `config.yml` + env vars
- `internal/metrics/` — Prometheus collectors + HTTP middleware
- `pkg/logshipper/` — slog handler that ships taillight's own logs to its applog ingest endpoint

### Handler patterns

- Each handler struct takes a store interface (defined in `store.go`, `stats.go`)
- Use `writeJSON`, `writeError`, `writeJSONStatus` from `response.go` for all responses
- Use `emptySlice[T]()` to ensure nil slices serialize as `[]` not `null`
- Use `LoggerFromContext(r.Context())` for request-scoped logging (never `slog.Default()` in handlers)
- Filters are parsed in `model/` via `ParseSyslogFilter(r)` / `ParseAppLogFilter(r)`
- Cursor pagination: `model.ParseCursor(r)` + `model.ParseLimit(r, default, max)`

### Auth scopes

Routes are grouped by scope in `serve.go:setupRouter`:
- **read** — all GET endpoints (optionally behind auth via `auth_read_endpoints` config)
- **ingest** — `POST /api/v1/applog/ingest` (API key with ingest scope)
- **admin** — write operations (notification CRUD, analysis trigger)

### SSE brokers

`SyslogBroker` and `AppLogBroker` use subscribe/unsubscribe with per-client filter matching. SSE handlers (`syslog_sse.go`, `applog_sse.go`) manage the lifecycle. SSE write functions return errors — callers must check them.

## Go Error Handling
Never silence unchecked errors with `defer func() { _ = err }()` wrappers.
If the error matters, handle it (log or return). If it truly doesn't matter (e.g., `defer resp.Body.Close()`), use `//nolint:errcheck` with a reason.

## Gotchas

- Go module is `github.com/lasseh/taillight` but lives in `api/` — imports use `github.com/lasseh/taillight/internal/...`
- `ByHost` field in `model.VolumeBucket` uses `by_host` JSON tag — frontend TypeScript depends on this name
- CORS credentials + wildcard origin = browser rejection; must check all origins for `*`
- `WriteHeader` must come AFTER setting Content-Type header (locked after WriteHeader)
- LIKE metacharacters (`%`, `_`) need escaping via `escapeLike()` in store queries
- TimescaleDB hypertables: `syslog_events` and `applog_events` are time-partitioned
- The `Listener` uses a dedicated pgx connection (not the pool) for LISTEN/NOTIFY
