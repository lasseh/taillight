# Architecture

This document describes the system design, data flow, component interactions, database schema, and deployment topology of Taillight.

## System Overview

Taillight is a real-time log viewer built on TimescaleDB, Server-Sent Events (SSE), and multiple clients. It serves three feeds: **netlog** (network device syslog), **srvlog** (server syslog), and **applog** (application logs). Network devices and servers send syslog to rsyslog, which inserts into PostgreSQL as netlog or srvlog events. Applications send structured logs via an HTTP ingest API. All three streams are broadcast in real time through SSE fan-out brokers to clients: a Vue 3 browser SPA in this repo, plus a terminal binary (`taillight-tui`) and an SSH server (`taillight-wish`) that live in the companion [taillight-tui](https://github.com/lasseh/taillight-tui) repository. All consume the same HTTP API and SSE endpoints.

All three feeds are always on (see `docs/adr/0002-feed-flags-removed.md`); the optional subsystems — notifications, AI analysis, LDAP, Netbox — are constructed conditionally at startup and cost nothing when disabled.

```
  Syslog paths (netlog + srvlog share the same pipeline shape):

  +-------------------+                       +-------------------+
  |  Network Devices  |                       |     Servers       |
  |  (Juniper, Cisco) |                       | (Linux, Docker)   |
  +--------+----------+                       +---------+---------+
           |                                            |
       UDP/TCP 514                                  UDP/TCP 515
           |                                            |
  +--------v--------------------------------------------v--------+
  |                          rsyslog                             |
  |     network_devices ruleset       server_logs ruleset        |
  +--------+--------------------------------------------+--------+
           | ompgsql INSERT                              | ompgsql INSERT
  +--------v----------+                       +----------v--------+
  |   netlog_events   |                       |   srvlog_events   |
  |   (TimescaleDB)   |                       |   (TimescaleDB)   |
  +--------+----------+                       +----------+--------+
           | pg_notify('netlog_ingest', id)              | pg_notify('srvlog_ingest', id)
           +--------------------+------------------------+
                                |
                     +----------v----------+
                     |     Go Server       |
                     |  Listener (LISTEN)  |
                     +----------+----------+
                                |  fetch row by id (ingestbridge workers)
              +-----------------+-----------------+
              |                                   |
     +--------v--------+                 +--------v--------+
     |  NetlogBroker   |                 |  SrvlogBroker   +---> NotificationEngine
     |  (per-client    |                 |  (per-client    |     (Slack/Webhook/
     |   filtering)    |                 |   filtering)    |      Email/ntfy)
     +--------+--------+                 +--------+--------+
              |                                   |
              +----------------+------------------+
                               |
                    SSE (text/event-stream)
                               |
        +----------------------+----------------------+
        |                      |                       |
+-------v--------+    +--------v---------+    +--------v---------+
| Browser Client |    |  taillight-tui   |    |  taillight-wish  |
| (Vue 3 SPA)    |    |  (terminal)      |    |  (SSH -> TUI)    |
+----------------+    +------------------+    +------------------+

  Application log path:

  +--------------+       +----------------+       +------------------+
  | Applications |       |   Go Server    |       |   TimescaleDB    |
  | (log sources)+------>| Ingest Handler +------>|  applog_events   |
  +--------------+ HTTP  +-------+--------+  SQL  +------------------+
                  POST           |         INSERT
                                 |  (broadcast directly, no LISTEN/NOTIFY)
                         +-------v--------+
                         | AppLogBroker   +---> NotificationEngine
                         | (per-client    |
                         |  filtering)    |
                         +-------+--------+
                                 |
                          SSE (text/event-stream)
```

## Data Flow

### Syslog Pipelines (netlog and srvlog)

The two syslog pipelines are structurally identical and move events from source to browser in under a second. Netlog arrives on port 514 (ruleset `network_devices` → `netlog_events`); srvlog on port 515 (ruleset `server_logs` → `srvlog_events`).

1. **rsyslog receives** -- Devices send syslog over UDP or TCP. The `imudp` module runs 8 receiver threads with a batch size of 128. The `imtcp` inputs cap concurrent sessions (200 for netlog, 500 for srvlog).

2. **Message processing** -- Each ruleset applies `mmutf8fix` to sanitize non-UTF-8 bytes, then `mmpstrucdata` to parse RFC 5424 structured data. Critical-severity messages (emerg/alert/crit) are captured before any filtering; the rest pass through configurable filters (by msgid, programname, facility, severity, hostname).

3. **ompgsql INSERT** -- Surviving messages are inserted via the `PgSQLNetlogInsert` / `PgSQLSrvlogInsert` templates. Each output uses a disk-assisted LinkedList queue (50,000 entries, 2 GB disk, 4 worker threads) with batched transactions (128 INSERTs per transaction) and infinite retry at 30s intervals.

4. **Trigger fires pg_notify** -- The `trg_netlog_notify` / `trg_srvlog_notify` trigger executes on every INSERT, calling `pg_notify('<feed>_ingest', NEW.id::text)` to broadcast the new row's ID. A second BEFORE-INSERT trigger computes `msg_pattern` (numbers and IPs normalized) for top-message aggregation.

5. **Go Listener receives** -- `postgres.Listener` (`internal/postgres/listener.go`) holds a dedicated `pgx.Conn` (not from the pool) running `LISTEN srvlog_ingest` and `LISTEN netlog_ingest`. When a notification arrives, it parses the payload as an int64 row ID and sends a `Notification{Channel, ID}` struct into a buffered channel (`notification_buffer_size`, default 1024).

6. **Fetch full event** -- A pool of bridge workers (`notification_workers`, default 4) started in `startBackgroundWorkers` (`cmd/taillight/serve.go`) reads from the notification channel and calls `ingestbridge.Dispatch`, which fetches the complete row by ID (`GetSrvlog`/`GetNetlog`, 30s timeout), broadcasts it to the matching SSE broker, and hands it to the notification engine.

7. **Broker fan-out** -- `broker.SrvlogBroker` and `broker.NetlogBroker` are thin wrappers over a generic `Broker[E, F]` (`internal/broker/broker.go`). On `Broadcast(event)`, the broker JSON-marshals the event once, then iterates all subscribers. Each subscriber has a per-client filter; only matching events are sent. Events are written to per-client buffered channels (512 slots). If a client's channel is full, the event is dropped and a metric is incremented.

8. **SSE to browser** -- The SSE handlers (`internal/handler/srvlog_sse.go`, `netlog_sse.go`) stream events as `text/event-stream` frames with `id:`, `event: srvlog`/`event: netlog`, and `data:` fields. A 15-second heartbeat keeps the connection alive. On initial connect, the handler backfills up to 100 recent events (or resumes from `Last-Event-ID`).

### Application Log Pipeline

The applog pipeline uses HTTP ingest instead of rsyslog, and broadcasts in-process instead of via LISTEN/NOTIFY:

1. **HTTP POST ingest** -- Applications send batches of log entries to `POST /api/v1/applog/ingest` with a JSON body (`{"logs": [...]}`). The endpoint requires an API key with `ingest` scope.

2. **Validation** -- `handler.AppLogIngestHandler.Ingest` (`internal/handler/applog_ingest.go`) enforces limits: max 1000 entries per batch, 5 MB body, 64 KB per message and per attrs blob, 128 chars per service/component, 256 chars per host/source. Log levels are normalized to the five canonical levels (aliases: TRACE -> DEBUG, WARNING -> WARN, CRITICAL -> FATAL, PANIC -> FATAL).

3. **Server-captured metadata** -- The handler stamps `source_ip` (the resolved client IP) and `api_key_id` (the authenticating key) onto each row. Both come from the request context, never the body, so shippers cannot spoof them.

4. **Batch INSERT** -- Validated entries are inserted via `store.InsertLogBatch` (pgx batch, `RETURNING` populates `id` and `received_at`). A trigger maintains the `applog_meta_cache` table; another computes `msg_pattern`.

5. **AppLogBroker fan-out** -- The ingest handler calls `broker.Broadcast(event)` for each inserted event — same generic broker, per-client filters, 512-slot buffered channels, drop-on-full.

6. **Notification engine** -- If enabled, `notifEngine.HandleAppLogEvent(event)` evaluates all applog notification rules against the event.

7. **SSE to browser** -- `handler.AppLogSSEHandler.Stream` streams events as `event: applog` SSE frames with the same backfill and heartbeat behavior as the syslog feeds.

## Component Architecture

### HTTP Server

The server uses `go-chi/chi/v5` as its router, configured in `setupRouter` (`cmd/taillight/serve.go`).

**Middleware stack** (applied in order):

| Middleware | Purpose |
|---|---|
| `middleware.RequestID` | Generates unique request ID |
| `clientIPMiddleware` | Resolves client IP into context (`GetClientIP`): the trusted `real_ip_header` if set — honored only from `trusted_proxies` peers when that list is configured — else the TCP peer. Replaces the deprecated, spoofable `middleware.RealIP` |
| `handler.RequestLogger` | Injects request-scoped logger into context |
| `middleware.Logger` | Logs request/response (skipped for `/health`, and for `/api/v1/applog/ingest` when self log-shipping is on) |
| `metrics.HTTPMetrics` | Prometheus request count and latency histograms (outer to Recoverer so panic-500s are recorded) |
| `middleware.Recoverer` | Catches panics, returns 500 |
| `handler.SecurityHeaders` | CSP, X-Frame-Options, X-Content-Type-Options, Referrer-Policy, Permissions-Policy, HSTS |
| `cors.Handler` | CORS with configurable allowed origins |
| `auth.DenyWrites` | Demo mode only: non-GET requests return 403 (ingest exempt from private IPs) |

**Route groups by auth scope:**

- **Unauthenticated** -- `GET /api/v1/config/features` (frontend feature flags), `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`
- **Authenticated (session or key)** -- `/auth/me`, email/preference updates, API key CRUD, session revocation; user management additionally requires the `admin` scope
- **Read** -- All GET endpoints: netlog/srvlog/applog list/detail/stream/export/meta/stats/device, hosts overview, juniper lookup, rsyslog + taillight metrics, notification config, summary schedules, analysis reports/schedules. Optionally behind auth via `auth_read_endpoints` config.
- **Ingest** -- `POST /api/v1/applog/ingest`. Requires API key with `ingest` scope.
- **Admin** -- Write operations: notification channel/rule CRUD, summary schedule CRUD + trigger, analysis report create/delete, analysis schedule CRUD + run-now, Juniper reference XLSX upload. Requires the `admin` grant.

**Server timeouts:**

- `ReadHeaderTimeout`: 10s, `ReadTimeout`: 60s, `WriteTimeout`: 60s, `IdleTimeout`: 120s
- Request timeout (middleware): 30s for REST endpoints, 5 min for CSV export, 2 min for reference upload, none for SSE streams (which set a 30s per-write deadline instead)

### SSE Brokers

`SrvlogBroker`, `NetlogBroker`, and `AppLogBroker` (`internal/broker/`) are 4-line wrappers around one generic implementation:

```
Broker[E, F interface{ Matches(E) bool }]
  Subscribe(filter, clientKey) -> (*Subscription[F], error)  // registers client
  Unsubscribe(sub)                                           // removes client, closes channel
  Broadcast(event)                                           // fans out to matching clients
  Shutdown()                                                 // closes all client channels
```

Key design decisions:

- **Per-client filtering** -- Each subscription carries a filter struct. `Broadcast` calls `filter.Matches(event)` before sending, so clients only receive events they're interested in.
- **Buffered channels** -- Each client gets a 512-slot buffered channel (`subscriptionBufferSize`). This absorbs bursts without blocking the broadcast loop.
- **Drop-on-full** -- If a client's channel is full (slow consumer), the event is dropped rather than blocking. A Prometheus counter tracks dropped events.
- **Limits** -- Hard cap of 1000 concurrent SSE clients per broker (`maxSubscribers`) and 20 per client key (`maxSubscribersPerClient`; the key is the authenticated user ID, falling back to the client IP). `Subscribe` returns `ErrTooManySubscribers` / `ErrTooManyClientSubscribers`.
- **Marshal once** -- The event is JSON-marshaled a single time and the same bytes are shared across all subscriber channels, so REST and SSE payloads can never drift (both marshal the same model structs).
- **Thread safety** -- `sync.RWMutex` protects the subscriber map. `Broadcast` takes a read lock; `Subscribe`/`Unsubscribe` take a write lock.

**SSE handler lifecycle** (shared `sseStreamer` in `internal/handler/sse_stream.go`):

1. Parse filter from query parameters
2. Set SSE headers (`Content-Type: text/event-stream`, `X-Accel-Buffering: no`)
3. Subscribe to broker (before backfill to avoid losing events in the gap)
4. Backfill recent events or resume from `Last-Event-ID`
5. Enter event loop: read from subscription channel, write SSE frames (30s write deadline each), send heartbeats every 15s
6. Exit on client disconnect or broker shutdown

### PostgreSQL Listener

`postgres.Listener` (`internal/postgres/listener.go`) manages a dedicated PostgreSQL connection for `LISTEN/NOTIFY`:

- **Dedicated connection** -- Uses a raw `pgx.Conn` (not from the pool) because `WaitForNotification` blocks and would starve the pool. The listen goroutine is the connection's single owner; shutdown never touches the conn directly.
- **Channels** -- Listens on `srvlog_ingest` and `netlog_ingest`. Notifications carry the row ID as the payload; per-channel `lastSeenID` is tracked atomically.
- **Reconnection** -- On connection loss, the listener reconnects with exponential backoff (1s initial, 30s max) plus jitter to avoid thundering herd.
- **Gap fill** -- After reconnecting, queries each feed table for `id > lastSeenID` (up to 10,000 rows per channel) to recover events missed during the disconnect. Hitting the cap logs a warning and increments a truncation metric — the gap was only partially recovered.
- **Buffer monitoring** -- A goroutine checks channel utilization every 30s and warns at 80% capacity.
- **Graceful shutdown** -- Cancels the context; the listen goroutine closes its own connection, and `Shutdown` waits (bounded by ctx) for it to exit.

The Listener deliberately has no storage-agnostic port — it is Postgres-bound by decision (`docs/adr/0001-listener-stays-postgres-bound.md`). Only the shallow dispatch step (channel switch → fetch by id → broadcast → engine handoff) is extracted into `internal/ingestbridge` for unit testing.

### Notification Engine

`notification.Engine` (`internal/notification/engine.go`) provides rule-based alerting with a syslog-style **fire-first** delivery model — the first event alerts immediately; suppression only applies to repeats. See [NOTIFICATIONS.md](NOTIFICATIONS.md) for the operator-facing guide.

**Rule evaluation:**

1. Events are passed to `HandleSrvlogEvent`, `HandleNetlogEvent`, or `HandleAppLogEvent`
2. Each enabled rule of the matching `event_kind` is evaluated: filter fields (hostname, severity, service, level, search, ...) are matched in memory
3. Matching events are recorded in the `Suppressor` under a fingerprint (`ruleID:groupKey`, group key from the rule's `group_by` field)

**Silence-window model (suppressor.go):**

- A clean fingerprint fires **immediately** (or after an optional sub-second `coalesce` window that batches a first-alert flood into one message with a real count)
- Further matches during the **silence window** (default 5m) only increment a counter; when the window closes, one **digest** ("N more events in the last window") is emitted and the silence window grows linearly (default cap 15m)
- A fully quiet window closes the fingerprint — the next match fires immediately again
- Memory is bounded: 10,000 fingerprints per rule, 50,000 globally, and per-fingerprint retained payloads are trimmed (attrs dropped, message capped at 4 KB)

**Dispatch pipeline:**

1. A flush resolves the rule -> channels mapping and enqueues a `dispatchJob`
2. Worker goroutines (default 4) process jobs from a buffered channel (default 1024)
3. Each channel delivery passes a per-channel-type token-bucket rate limiter and a per-channel circuit breaker, then sends with a 10s timeout and bounded retry (`5s → 30s → 2m → 10m`)
4. Every outcome (sent/suppressed/failed) is written to the `notification_log` audit table

**Circuit breakers** (per channel, via `sony/gobreaker/v2`): open after 5 consecutive failures, allow 2 half-open probes, reset after 60s.

**Backends** (`internal/notification/backend`):

| Type | Delivery |
|---|---|
| Slack | Webhook POST |
| Webhook | HTTP POST with JSON payload |
| Email | SMTP with STARTTLS (also renders analysis reports and summary digests) |
| ntfy | HTTP POST to an ntfy topic (mobile push) |

### AI Analysis Subsystem

Optional (`analysis.enabled`, default off). When enabled, `setupAnalysis` (`cmd/taillight/serve.go`) wires four pieces:

- **Analyzer** (`internal/analyzer`) — the pipeline: Postgres aggregates the window first (top msgids, severity vs 7-day baseline, new signatures, clusters, sparkline timeline, samples, Juniper references), then a local Ollama LLM narrates the compact summary. Prompt modes: `daily`, `weekly`, `incident`. Empty windows short-circuit to a deterministic stub without calling the LLM. See `api/internal/analyzer/README.md`.
- **Worker** (`internal/worker`) — a single-goroutine queue (depth 5; a full queue returns 429 to the API caller). Each run is bounded by `analysis.run_timeout` (default 4h). Lifecycle rows move `pending → running → completed | failed`; orphaned rows from a crash are reconciled at boot.
- **Scheduler** (`internal/scheduler`) — ticks every 60s and fires due `analysis_schedules` rows (daily/weekly/monthly at a time-of-day in a timezone, 5-minute firing window with retry, `last_run_at` double-fire guard).
- **Completion email** — a report carries `notify_channel_ids` snapshotted from its schedule; on completion the worker wins a `notified_at` CAS (exactly-once) and mails the rendered report through those email channels via the notification engine.

Reports are addressed by slug (`GET /api/v1/analysis/reports/{slug}`), can be scoped to an explicit host set, and have a server-rendered print view (`.../{slug}/print`) that the frontend's "Export PDF" prints via a hidden iframe. Email and print share one renderer (`internal/report`).

Analysis feeds are `netlog`, `srvlog`, or `all` — where `all` means *all syslog* (srvlog + netlog union). Applog is excluded by design (architecture review D3). The `analysis` flag is the one real feature flag surfaced by `GET /api/v1/config/features`.

### Summary Scheduler

When the notification engine is enabled, `scheduler.SummaryScheduler` runs on the same 60s tick and fires `summary_schedules` rows: periodic (daily/weekly/monthly) digests of log activity — top issues per feed, severity breakdowns — delivered through notification channels. Managed via `/api/v1/notifications/summaries` and the UI.

### Clients

Multiple clients consume the same HTTP/SSE API. The backend has no per-client code paths — a client is anything that can authenticate and hold an `EventSource` (or equivalent SSE reader).

**Browser SPA** (`frontend/`): Vue 3 app served by nginx. Uses native `EventSource` for SSE, Pinia stores for state, and Unovis for charts. Cookie or (less commonly) Bearer auth.

**Terminal UI & SSH server** — `taillight-tui` (a terminal client) and `taillight-wish` (an SSH server that hosts it) live in the companion repository [taillight-tui](https://github.com/lasseh/taillight-tui). Both are HTTP/SSE clients of this API: each lazy-initializes per-feed SSE streams and uses the same endpoints the browser does. `taillight-wish` additionally ships its own session logs to the applog ingest endpoint via `pkg/logshipper` (service `taillight-wish`, component `ssh-server`).

**Shipping SDKs** — `pkg/logshipper` (Go `slog.Handler`, its own module) and `sdk/python` (`taillight-sdk` on PyPI) are contract-bearing consumers of the ingest API. The wire contract for events, envelopes, and the ingest request is pinned by golden-fixture tests shared between Go and TypeScript (`docs/adr/0004-contract-goldens-not-codegen.md`).

### Auth Layer

Authentication is handled by `internal/auth/middleware.go`:

**Two auth mechanisms:**

1. **Session cookies** -- `tl_session` cookie contains a random token. The SHA-256 hash is stored in the `sessions` table. Sessions live 30 days, are pruned to the 10 most recent per user, and expired rows are cleaned every 15 minutes.
2. **API keys** -- `Authorization: Bearer tl_...` header. Keys are 43 base62 characters with a `tl_` prefix. The SHA-256 hash is stored in `api_keys`. Keys carry scopes (`read`, `ingest`, `admin`).

**Middleware chain:**

- `SessionOrAPIKey` -- Tries session cookie first, then Bearer token. Stores authenticated user (and scopes, for API keys) in context.
- `RequireScope(scope)` -- Checks the grant. API keys hold a scope if it is listed or they hold `admin`. Session auth holds every non-admin scope, but the `admin` scope additionally requires the user's admin flag — a session is *not* a blanket admin grant.
- `AllowAnonymous` -- Used when auth is disabled; stores a synthetic anonymous user.

**LDAP (optional):** when `ldap.enabled` is set, login attempts first bind a service account, search for the user, bind as the user, and map the user's groups to a role via `ldap.group_role_map` (`admin` grants is_admin; a user in no mapped group is denied). LDAP users are synced into `users` with `auth_source='ldap'`; local bcrypt users keep working. An internal CA can be trusted via `ldap.ca_bundle` without disabling verification.

**Security measures:**

- Passwords hashed with bcrypt (cost 12)
- Timing-safe dummy check prevents username enumeration
- Application-level login rate limit: 5 attempts/minute per client IP (burst 10)
- Tokens stored as SHA-256 hashes (never plaintext)
- API key prefix stored for display (`tl_0123456...`)
- Session/key last-seen updates run through an async touch worker so the hot path never blocks on them

## Database Schema

All tables live in a single `taillight` database on TimescaleDB (PostgreSQL with the timescaledb extension). Schema is managed by golang-migrate (`api/migrations/`).

### srvlog_events and netlog_events (hypertables)

The two syslog event tables are column-identical; only names and triggers differ.

```sql
CREATE TABLE srvlog_events (            -- netlog_events is identical
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
    raw_message     TEXT,
    msg_pattern     TEXT        NOT NULL DEFAULT ''   -- computed on INSERT
);
```

**Hypertable settings (both):**

| Setting | Value |
|---|---|
| Partition column | `received_at` |
| Chunk interval | 1 day |
| Segment by | `hostname` |
| Order by | `received_at DESC` |
| Columnstore | After 1 day |
| Retention | 90 days (configurable) |

**Indexes (per table, `srvlog`/`netlog` prefix):**

| Name | Columns | Purpose |
|---|---|---|
| `idx_*_received_id` | `(received_at DESC, id DESC)` | Cursor pagination |
| `idx_*_id` | `(id)` | Single event lookup / SSE backfill |
| `idx_*_host_received` | `(hostname, received_at DESC)` | Host filter |
| `idx_*_severity_received` | `(severity, received_at DESC, id DESC) WHERE severity <= 3` | Critical event filter |
| `idx_*_programname` | `(programname, received_at DESC)` | Program filter |
| `idx_*_facility` | `(facility, received_at DESC)` | Facility filter |
| `idx_*_fromhost_ip` | `(fromhost_ip, received_at DESC)` | Source IP filter |
| `idx_*_syslogtag` | `(syslogtag, received_at DESC)` | Tag filter |
| `idx_*_message_trgm` | `message gin_trgm_ops (GIN)` | Trigram substring search |

**Triggers (per table):**

- `trg_*_notify` -- Fires `pg_notify('<feed>_ingest', id)` on INSERT
- `trg_*_msg_pattern` -- BEFORE INSERT: normalizes numbers/IPs in the first 200 chars into `msg_pattern`
- `trg_*_meta_cache` -- Upserts hostname/programname/syslogtag into the feed's meta cache
- `trg_*_facility_cache` -- Upserts facility code into the feed's facility cache

**Continuous aggregates:** `srvlog_summary_hourly` and `netlog_summary_hourly` bucket counts by hour × hostname × severity (real-time aggregation enabled, columnstore after 3 days) and back the dashboard summary queries.

### applog_events (hypertable)

Primary event table for application logs ingested via HTTP.

```sql
CREATE TABLE applog_events (
    id            BIGINT GENERATED ALWAYS AS IDENTITY,
    received_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    timestamp     TIMESTAMPTZ NOT NULL,
    level         TEXT        NOT NULL,
    service       TEXT        NOT NULL,
    component     TEXT        NOT NULL DEFAULT '',
    host          TEXT        NOT NULL DEFAULT '',
    msg           TEXT        NOT NULL,
    source        TEXT        NOT NULL DEFAULT '',
    attrs         JSONB,
    msg_pattern   TEXT        NOT NULL DEFAULT '',
    source_ip     INET,           -- captured by the ingest handler, not the client
    api_key_id    UUID,           -- ID of the API key that authenticated the batch
    search_vector tsvector GENERATED ALWAYS AS (
        to_tsvector('simple', coalesce(service,'') || ' ' ||
                              coalesce(component,'') || ' ' ||
                              coalesce(host,'') || ' ' ||
                              coalesce(msg,'') || ' ' ||
                              coalesce(attrs::text,''))
    ) STORED
);
```

**Hypertable settings:**

| Setting | Value |
|---|---|
| Partition column | `received_at` |
| Chunk interval | 1 day |
| Segment by | `service` |
| Order by | `received_at DESC, id DESC` |
| Columnstore | After 1 day |
| Retention | 90 days (configurable) |

**Indexes:**

| Name | Columns | Purpose |
|---|---|---|
| `idx_applog_received_id` | `(received_at DESC, id DESC)` | Cursor pagination |
| `idx_applog_id` | `(id)` | Single event lookup / SSE backfill |
| `idx_applog_service_received` | `(service, received_at DESC)` | Service filter |
| `idx_applog_level_received` | `(level, received_at DESC)` | Level filter |
| `idx_applog_host_received` | `(host, received_at DESC)` | Host filter |
| `idx_applog_search` | `search_vector (GIN)` | Full-text search |

**Triggers:** `trg_applog_meta_cache` (service/component/host upsert) and `trg_applog_msg_pattern`. The continuous aggregate `applog_summary_hourly` buckets counts by hour × service × level.

### Auth Tables

```sql
-- users: bcrypt password hashes, admin flag, active status, auth source, UI prefs
users (id UUID PK, username TEXT UNIQUE (case-insensitive), password_hash, email,
    is_active, is_admin, preferences JSONB, auth_source ['local','ldap'], ...)

-- sessions: SHA-256 hashed tokens, expiry, last-seen tracking
sessions (token_hash TEXT PK, user_id FK, expires_at, last_seen_at, ip_address, user_agent)

-- api_keys: SHA-256 hashed keys, scopes array, optional expiry
api_keys (id UUID PK, user_id FK, name, key_hash, key_prefix, scopes TEXT[], expires_at, revoked_at)
```

### Notification Tables

```sql
-- notification_channels: configured backends
notification_channels (id BIGINT PK, name UNIQUE, type ['slack','webhook','email','ntfy'],
    config JSONB, enabled)

-- notification_rules: alert conditions with filter fields and silence-model behavior
notification_rules (id BIGINT PK, name UNIQUE, enabled, event_kind ['srvlog','netlog','applog'],
    hostname, programname, severity, severity_max, facility, syslogtag, msgid,   -- syslog kinds
    service, component, host, level,                                             -- applog kind
    search,                                                                      -- all kinds
    silence_ms, silence_max_ms, coalesce_ms, group_by)

-- notification_rule_channels: many-to-many mapping
notification_rule_channels (rule_id FK, channel_id FK, PK(rule_id, channel_id))

-- notification_log: audit trail (hypertable, 7-day chunks, 30-day retention)
notification_log (id, created_at, rule_id, channel_id, event_kind, event_id,
    status ['sent','suppressed','failed'], reason, event_count, status_code,
    duration_ms, payload JSONB)

-- summary_schedules: recurring digest schedules + channel mapping
summary_schedules (id, name UNIQUE, enabled, frequency ['daily','weekly','monthly'],
    day_of_week, day_of_month, time_of_day, timezone, event_kinds TEXT[],
    severity_max, hostname, top_n, last_run_at)
summary_schedule_channels (schedule_id FK, channel_id FK)
```

### Analysis Tables

```sql
-- analysis_reports: async LLM report lifecycle
analysis_reports (id PK, slug UNIQUE, feed ['netlog','srvlog','all'], prompt_mode,
    hosts TEXT[],                       -- empty = all hosts
    model, period_start, period_end, report, error,
    prompt_tokens, completion_tokens,
    status ['pending','running','completed','failed'],
    created_at, started_at, completed_at,
    notified_at,                        -- CAS column: completion email fires exactly once
    notify_channel_ids BIGINT[])        -- email channels snapshotted from the schedule
-- partial unique index on (feed, period_end, prompt_mode, hosts)
-- WHERE status IN ('pending','running') prevents duplicate active runs

-- analysis_schedules: recurring analysis runs
analysis_schedules (id PK, name UNIQUE, enabled, feed, frequency,
    day_of_week, day_of_month, time_of_day, timezone,
    notify_channel_ids BIGINT[], last_run_at)
```

### Supporting Tables

```sql
-- per-feed meta caches: distinct filter-dropdown values with last_seen_at
srvlog_meta_cache / netlog_meta_cache / applog_meta_cache
    (column_name TEXT, value TEXT, last_seen_at, PK(column_name, value))
srvlog_facility_cache / netlog_facility_cache (facility SMALLINT PK)

-- juniper_netlog_ref: Juniper syslog reference documentation (unique on name+os)
juniper_netlog_ref (id, name, message, description, type, severity, cause, action, os)

-- rsyslog_stats: impstats telemetry (hypertable, 1-day chunks, 30-day retention)
rsyslog_stats (collected_at, origin, name, stats JSONB)

-- taillight_metrics: application metrics snapshots (hypertable, 1-day chunks, 30-day retention)
taillight_metrics (collected_at,
    sse_clients_srvlog, sse_clients_netlog, sse_clients_applog,
    db_pool_active, db_pool_idle, db_pool_total,
    events_broadcast, events_dropped,
    netlog_events_broadcast, netlog_events_dropped,
    applog_events_broadcast, applog_events_dropped,
    applog_ingest_total, applog_ingest_errors, listener_reconnects)
```

## Deployment Topology

### Docker Compose (Development / Small Production)

The standard deployment runs 4 containers on a bridge network:

```
+------------------------------------------------------------------+
|                       Docker Compose                             |
|                                                                  |
|  +-------------+    +----------+    +-----------+   +----------+ |
|  |  postgres   |    |   api    |    |  rsyslog  |   | frontend | |
|  | TimescaleDB |    | Go HTTP  |    |  ompgsql  |   | Vue SPA  | |
|  | :5432       |    | :8080    |    | :514 :515 |   |nginx:8080| |
|  +------+------+    +----+-----+    +-----+-----+   +----+-----+ |
|         |                |                |               |      |
|         +--------+-------+--------+-------+               |      |
|                  |   taillight network    |               |      |
|                  +------------------------+               |      |
+------------------------------------------------------------------+
          |               |                |                |
      :5432           :8080       :1514/udp+tcp (netlog)  :3000
     (host)          (host)       :1515/udp+tcp (srvlog)  (host)
```

**Services:**

| Service | Image | Port (host) | Purpose |
|---|---|---|---|
| `postgres` | `timescale/timescaledb:latest-pg18` | 5432 | TimescaleDB with pg_trgm, pg_stat_statements |
| `api` | Built from `api/Dockerfile` (repo-root context) | 8080 | Go HTTP/SSE server |
| `rsyslog` | Built from `./rsyslog` | 1514 (netlog), 1515 (srvlog), UDP+TCP | Syslog receiver with ompgsql |
| `frontend` | Built from `./frontend` | 3000 (container port 8080) | Vue 3 SPA served by nginx |

**Health checks:**

- `postgres` -- `psql -U taillight -d taillight -c 'SELECT 1'` (5s interval)
- `api` -- `wget --spider http://localhost:8080/health` (10s interval)
- `rsyslog` -- `logger -t healthcheck test` (30s interval)
- `frontend` -- `wget --spider http://127.0.0.1:8080/healthz` (10s interval)

**Volumes:** `pgdata` for PostgreSQL data persistence. Config mounted read-only at `/config.yml`. All containers use the `json-file` logging driver so container logs never leak into host syslog (the API ships its own logs via the built-in logshipper instead).

### Production (nginx Reverse Proxy)

In production, an nginx reverse proxy sits in front of all services on a single domain:

```
                          Internet
                             |
                         443 (TLS)
                             |
                     +-------v--------+
                     |     nginx      |
                     | TLS termination|
                     | rate limiting  |
                     | gzip           |
                     +--+-----+----+--+
                        |     |    |
           /api/*       |     |    |      /*
           /health      |     |    +-------> frontend:8080
                        |     |
         /api/v1/*/stream     |
         (SSE, no buffering)  |
                        |     +-----------> api:8080
                        +-----------------> api:8080
```

**nginx configuration** (`docs/nginx-reverse-proxy.conf.example`):

- **TLS** -- TLSv1.2 and TLSv1.3, HTTP/2 enabled
- **Rate limiting** -- Login: 10 req/min (burst 5). API: 30 req/s (burst 50). Returns 429.
- **SSE support** -- `proxy_buffering off`, `proxy_read_timeout 24h` for stream endpoints
- **Real client IP** -- the proxy sets `X-Real-IP`; pair it with `real_ip_header` + `trusted_proxies` in the API config
- **Gzip** -- Enabled for JSON, CSS, JavaScript
- **HTTP -> HTTPS redirect** -- Port 80 returns 301 to HTTPS

**Frontend nginx** (`frontend/nginx.conf`) sets security headers on every response, including a `Content-Security-Policy-Report-Only` header (promoted to enforcing once violation reports are clean).

**Metrics server** -- When `metrics_addr` is set (e.g., `:9090`), a separate HTTP server exposes Prometheus metrics at `/metrics`, isolated from the main API.

### Releases

Taillight cuts semver `vX.Y.Z` tags (`docs/adr/0005-tagged-releases-adopted.md`): `make release` runs tests, tags, and pushes; CI builds cross-platform `taillight` + `taillight-shipper` binaries, the GitHub release, and a multi-arch Docker image. The Python SDK releases independently via `py-v*` tags. Continuous deploy to production is unchanged — tags serve self-hosters and shipper distribution.

## Frontend Architecture

The frontend is a Vue 3 Single Page Application built with TypeScript, Vite, and Tailwind CSS.

### Router (`src/router.ts`)

| Route | View | Description |
|---|---|---|
| `/` | `HomeView` | Live recent events from all three streams + severity timelines |
| `/netlog` | `NetlogListView` | Filterable netlog event list with SSE |
| `/netlog/device/:hostname` | `NetlogDeviceView` | Per-device netlog summary |
| `/netlog/:id` | `NetlogView` | Single netlog detail (+ optional Netbox panel) |
| `/srvlog` | `SrvlogListView` | Filterable srvlog event list with SSE |
| `/srvlog/device/:hostname` | `DeviceView` | Per-device srvlog summary |
| `/srvlog/:id` | `SrvlogView` | Single srvlog detail |
| `/hosts` | `HostsView` | Hosts overview (srvlog + netlog) with status and sparklines |
| `/volume` | `VolumeView` | Volume charts per feed + rsyslog/taillight self-metrics tabs |
| `/applog` | `AppLogListView` | Filterable applog event list with SSE |
| `/applog/device/:hostname` | `AppLogDeviceView` | Per-host applog summary |
| `/applog/:id` | `AppLogView` | Single applog detail |
| `/notifications` | `NotificationsView` | Channel/rule/summary management, delivery log |
| `/analysis` | `AnalysisView` | AI report list + create panel (only when analysis is enabled) |
| `/analysis/reports/:slug` | `AnalysisReportView` | Report detail with print/PDF export |
| `/analysis/*` (disabled) | `FeatureDisabledView` | Shown when the analysis flag is off |
| `/settings` | `SettingsView` | User settings, themes |
| `/settings/api-keys` | `ApiKeysView` | API key management |
| `/admin/users` | `UsersView` | User administration (`meta: { admin: true }`) |
| `/login` | `LoginView` | Login form (`meta: { public: true }`) |

The router's `beforeEach` hook redirects unauthenticated users to `/login` unless the route has `meta: { public: true }`; routes with `meta: { admin: true }` additionally require an admin user. Before the router is created, the app fetches `GET /api/v1/config/features` — the three feed keys are constant `true` (feeds are always on); the `analysis` flag decides whether the analysis routes and nav render.

### Pinia Stores

- **`srvlog-events`** / **`netlog-events`** / **`applog-events`** -- Event lists with cursor pagination and sliding-window scrollback (factory pattern via `event-store-factory.ts`)
- **`srvlog-filters`** / **`netlog-filters`** / **`applog-filters`** -- Active filter state (factory via `filter-store-factory.ts`)
- **`meta`** / **`netlog-meta`** / **`applog-meta`** -- Cached hostnames, programs, services for filter dropdowns
- **`srvlog-volume`** / **`netlog-volume`** / **`applog-volume`** -- Chart data (factory via `volume-store.ts`)
- **`rsyslog-stats`** / **`taillight-metrics`** -- Pipeline self-observability tabs
- **`hosts`** -- Hosts overview data
- **`auth`** -- User session state, login/logout
- **`home`** -- Home page recent events + severity timelines from SSE streams
- **`scroll`** -- Scroll position tracking for auto-scroll behavior

### SSE Composables

- **`useEventStream`** (`src/composables/useEventStream.ts`) -- Generic SSE client factory with exponential backoff reconnection (up to 30s), heartbeat watchdog (35s timeout, ~2× the server's 15s heartbeat), and `Last-Event-ID` resume support.
- **`useSrvlogStream`** / **`useNetlogStream`** / **`useAppLogStream`** -- Singleton instances for `GET /api/v1/{feed}/stream` with the matching event name.
- **`useDeviceLogStream`** -- Feed-parameterized device-page live stream.

## Performance Characteristics

### Database

- **Cursor pagination** -- Keyset pagination using `(received_at, id) < (?, ?)` tuple comparison avoids OFFSET performance degradation on large tables.
- **Compound indexes** -- Every filter column has a compound index with `received_at DESC` to enable sort elimination and index-only scans.
- **Trigram search** -- `pg_trgm` GIN indexes on `message` enable efficient `ILIKE '%term%'` substring search without sequential scans.
- **Full-text search** -- Applog events use a stored `tsvector` column with a GIN index for full-text search across service, component, host, message, and attrs.
- **Columnstore** -- Chunks older than 1 day are converted to columnar storage, reducing storage footprint.
- **Meta caches** -- Instead of `SELECT DISTINCT` (which scans entire hypertables), trigger-maintained cache tables provide O(1) lookups for filter dropdown values.
- **Pre-computed msg_pattern** -- Top-message aggregation groups by a trigger-computed normalized pattern instead of regexing at query time.
- **Batch queries** -- Device summaries send 4 queries and the hosts overview 5 queries in a single round-trip via `pgx.Batch`.
- **Continuous aggregates** -- Hourly rollups back the summary dashboards.
- **Connection pool** -- `pgxpool` with configurable max/min connections (default 30/2) and a query tracer feeding per-operation latency/error metrics.

### SSE

- **Per-client buffer** -- 512-message buffered channel per subscriber absorbs burst traffic.
- **Max subscribers** -- 1000 concurrent SSE clients per broker (3000 total across the three feeds), 20 per user/IP.
- **Heartbeat** -- 15-second interval keeps connections alive through proxies; every write carries a 30s deadline so a dead client can't wedge a handler.
- **Backfill** -- Up to 100 recent events sent on connect, avoiding empty initial state.
- **Drop-on-full** -- Slow clients get events dropped rather than blocking the broadcast loop.

### rsyslog

- **Main queue** -- LinkedList queue, 50,000 entries, 5 GB disk overflow, 4 worker threads.
- **Discard policy** -- At 45,000 entries (90%), info/debug messages are discarded first.
- **PostgreSQL queues** -- One per feed: 50,000 entries, 2 GB disk, 4 workers, 128 INSERTs per batch transaction, infinite retry.
- **UDP threads** -- 8 receiver threads with batch size 128 for high-throughput UDP ingestion.
- **TCP sessions** -- Capped (200 netlog / 500 srvlog) to prevent fd exhaustion.

## Security Model

### Authentication Modes

Taillight supports three authentication configurations:

1. **No auth** (`auth_enabled: false`) -- All endpoints are public. An anonymous user is injected into the context. Suitable for private networks only.
2. **Auth without read protection** (`auth_enabled: true`, `auth_read_endpoints: false`) -- Write and ingest endpoints require authentication. Read endpoints (GET) are public.
3. **Full auth** (`auth_enabled: true`, `auth_read_endpoints: true`) -- All endpoints require authentication. This is the default and the recommendation for production.

### Scope System

| Scope | Access |
|---|---|
| `read` | All GET endpoints (netlog, srvlog, applog, stats, meta, notifications, analysis) |
| `ingest` | `POST /api/v1/applog/ingest` |
| `admin` | All write operations (notification CRUD, analysis writes, reference upload, user management) |

API keys carry explicit scopes (`admin` implies the others). Session-based auth grants the non-admin scopes; admin routes additionally require the user's `is_admin` flag.

### Client IP & Trusted Proxies

Client IPs feed the login rate limiter, applog `source_ip` attribution, the demo-mode write gate, and the per-client SSE cap. Resolution is spoof-resistant: with no proxy, only the TCP peer is trusted; behind a proxy, set `real_ip_header` (e.g. `X-Real-IP`) and list the proxy addresses in `trusted_proxies` so the header is honored only from those peers.

### CORS

- Configurable allowed origins via `cors_allowed_origins` config
- Credentials (`AllowCredentials: true`) only when origins are explicitly listed (not `*`)
- Defaults to localhost dev origins if unconfigured (with a startup warning)

### Security Headers

Applied by `handler.SecurityHeaders` middleware on the API:

- `Content-Security-Policy` -- `default-src 'self'`; `connect-src` extended with the configured CORS origins
- `X-Frame-Options: DENY`
- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Permissions-Policy` -- camera/microphone/geolocation disabled
- `Strict-Transport-Security` -- 2-year max-age

The frontend's nginx serves its own header set plus a report-only CSP (see Deployment above).

### Credential Security

- **Passwords** -- bcrypt with cost factor 12
- **Timing-safe login** -- Dummy bcrypt check on invalid usernames prevents enumeration
- **Login rate limiting** -- 5/min per IP in the application (plus nginx limits in front)
- **Session tokens** -- 32 bytes of `crypto/rand`, base64-encoded, stored as SHA-256 hash
- **API keys** -- `tl_` prefix + 43 base62 chars (crypto/rand), stored as SHA-256 hash
- **Key display** -- Only the first 10 characters stored as `key_prefix` for identification
- **Secrets via env** -- `SMTP_PASSWORD`, `NETBOX_TOKEN`, `LDAP_BIND_PASSWORD`, `LOGSHIPPER_API_KEY` are explicitly env-bindable so they never need to live in the config file

### Demo Mode

`demo_mode: true` makes the API read-only: every non-GET request returns 403, except the ingest endpoint from private/loopback addresses (so the demo's loadgen containers keep working while the internet cannot write).

### Rate Limiting (nginx)

- Login endpoint: 10 requests/minute per IP (burst 5)
- API endpoints: 30 requests/second per IP (burst 50)
- SSE streams: no rate limit (long-lived connections)
