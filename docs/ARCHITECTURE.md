# Architecture

This document describes the system design, data flow, component interactions, database schema, and deployment topology of Taillight.

## System Overview

Taillight is a real-time syslog and application log viewer built on TimescaleDB, Server-Sent Events (SSE), and a Vue 3 frontend. Network devices send syslog to rsyslog, which inserts into PostgreSQL. Applications send structured logs via an HTTP ingest API. Both streams are broadcast to browser clients in real time through SSE fan-out brokers.

```
                                    +-------------------+
                                    |  Network Devices  |
                                    |  (Juniper, Cisco) |
                                    +--------+----------+
                                             |
                                         UDP/TCP 514
                                             |
                                    +--------v----------+
                                    |      rsyslog      |
                                    |  (ompgsql output) |
                                    +--------+----------+
                                             |
                                       SQL INSERT
                                             |
+--------------+                  +----------v-----------+                +-----------+
| Applications +--HTTP POST------>|     TimescaleDB      |<---LISTEN-----+ Go Server |
| (log ingest) |                  |  syslog_events       |               |           |
+--------------+                  |  applog_events       +--pg_notify--->+ Listener  |
                                  +-----------+----------+               +-----+-----+
                                              |                                |
                                              |                     +----------v----------+
                                              |                     |    SSE Brokers       |
                                              |                     | SyslogBroker         |
                                              |                     | AppLogBroker         |
                                              |                     +----------+----------+
                                              |                                |
                                              |                        SSE (text/event-stream)
                                              |                                |
                                              |                     +----------v----------+
                                              |                     |   Browser Clients   |
                                              |                     |   (Vue 3 SPA)       |
                                              |                     +---------------------+
                                              |
                                   +----------v----------+
                                   | Notification Engine |
                                   | Slack / Webhook /   |
                                   | Email backends      |
                                   +---------------------+
```

## Data Flow

### Syslog Pipeline

The syslog pipeline moves events from network devices to browser clients in under a second:

1. **rsyslog receives** -- Network devices send syslog messages over UDP or TCP to port 514. The `imudp` module runs 8 receiver threads with a batch size of 128. The `imtcp` module caps concurrent sessions at 200.

2. **Message processing** -- The `network_devices` ruleset applies `mmutf8fix` to sanitize non-UTF-8 bytes, then `mmpstrucdata` to parse RFC 5424 structured data. Messages pass through configurable filters (by msgid, programname, facility, severity, hostname).

3. **ompgsql INSERT** -- Surviving messages are inserted into `syslog_events` via the `PgSQLInsert` template. The output uses a disk-assisted LinkedList queue (50,000 entries, 2 GB disk, 4 worker threads) with batched transactions (128 INSERTs per transaction).

4. **Trigger fires pg_notify** -- The `trg_syslog_notify` trigger executes on every INSERT, calling `pg_notify('syslog_ingest', NEW.id::text)` to broadcast the new row's ID.

5. **Go Listener receives** -- `postgres.Listener` (`internal/postgres/listener.go`) holds a dedicated `pgx.Conn` (not from the pool) running `LISTEN syslog_ingest`. When a notification arrives, it parses the payload as an int64 row ID and sends a `Notification{Channel, ID}` struct into a buffered channel (default 1024).

6. **Fetch full event** -- The background worker in `startBackgroundWorkers` (`cmd/taillight/serve.go:242-257`) reads from the notification channel, calls `store.GetSyslog(ctx, id)` to fetch the complete event row, then broadcasts it to the SSE broker.

7. **SyslogBroker fan-out** -- `broker.SyslogBroker` (`internal/broker/syslog_broker.go`) holds a map of active subscriptions. On `Broadcast(event)`, it JSON-marshals the event once, then iterates all subscribers. Each subscriber has a per-client filter (`model.SyslogFilter`); only matching events are sent. Events are written to per-client buffered channels (64 slots). If a client's channel is full, the event is dropped and a metric is incremented.

8. **SSE to browser** -- `handler.SyslogSSEHandler.Stream` (`internal/handler/syslog_sse.go`) writes events as `text/event-stream` frames with `id:`, `event: syslog`, and `data:` fields. A 15-second heartbeat keeps the connection alive. On initial connect, the handler backfills up to 100 recent events (or resumes from `Last-Event-ID`).

### Application Log Pipeline

The applog pipeline follows a similar pattern but uses HTTP ingest instead of rsyslog:

1. **HTTP POST ingest** -- Applications send batches of log entries to `POST /api/v1/applog/ingest` with a JSON body (`{"logs": [...]}`). The endpoint requires an API key with `ingest` scope.

2. **Validation** -- `handler.AppLogIngestHandler.Ingest` (`internal/handler/applog_ingest.go`) enforces limits: max 1000 entries per batch, 5 MB body, 64 KB per message, 128 chars per service/component. Log levels are normalized (e.g., WARNING -> WARN, CRITICAL -> ERROR).

3. **Batch INSERT** -- Validated entries are inserted via `store.InsertLogBatch`, which populates `id` and `received_at` from the database.

4. **Trigger fires pg_notify** -- The `trg_applog_meta_cache` trigger fires on INSERT, updating the `applog_meta_cache` table. Note: applog events are broadcast directly from the ingest handler (step 5), not via LISTEN/NOTIFY.

5. **AppLogBroker fan-out** -- The ingest handler calls `broker.Broadcast(event)` for each inserted event. `broker.AppLogBroker` (`internal/broker/applog_broker.go`) fans out to subscribers using the same pattern as `SyslogBroker`: per-client filters, 64-slot buffered channels, drop-on-full.

6. **Notification engine** -- If enabled, `notifEngine.HandleAppLogEvent(event)` evaluates all applog notification rules against the event.

7. **SSE to browser** -- `handler.AppLogSSEHandler.Stream` (`internal/handler/applog_sse.go`) streams events as `event: applog` SSE frames with the same backfill and heartbeat behavior as syslog.

## Component Architecture

### HTTP Server

The server uses `go-chi/chi/v5` as its router, configured in `setupRouter` (`cmd/taillight/serve.go:322-586`).

**Middleware stack** (applied in order):

| Middleware | Purpose |
|---|---|
| `middleware.RequestID` | Generates unique request ID |
| `middleware.RealIP` | Extracts client IP from X-Forwarded-For |
| `handler.RequestLogger` | Injects request-scoped logger into context |
| `middleware.Logger` | Logs request/response (skipped for /health and /api/v1/applog/ingest) |
| `middleware.Recoverer` | Catches panics, returns 500 |
| `metrics.HTTPMetrics` | Prometheus request count and latency histograms |
| `handler.SecurityHeaders` | CSP, X-Frame-Options, X-Content-Type-Options |
| `cors.Handler` | CORS with configurable allowed origins |

**Route groups by auth scope:**

- **Unauthenticated** -- `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`
- **Read** -- All GET endpoints: syslog list/detail/stream, applog list/detail/stream, stats, meta, device summaries, notifications list, analysis reports. Optionally behind auth via `auth_read_endpoints` config.
- **Ingest** -- `POST /api/v1/applog/ingest`. Requires API key with `ingest` scope.
- **Admin** -- Write operations: notification channel/rule CRUD, analysis trigger. Requires `admin` scope.

**Server timeouts:**

- `ReadHeaderTimeout`: 10s
- `IdleTimeout`: 120s
- Request timeout (middleware): 30s for REST endpoints, none for SSE streams, 15 min for analysis trigger

### SSE Brokers

Both `SyslogBroker` and `AppLogBroker` (`internal/broker/`) implement the same fan-out pattern:

```
Subscribe(filter) -> *Subscription    // Registers client, returns channel
Unsubscribe(sub)                      // Removes client, closes channel
Broadcast(event)                      // Fans out to matching clients
Shutdown()                            // Closes all client channels
```

Key design decisions:

- **Per-client filtering** -- Each subscription carries a filter struct. `Broadcast` calls `filter.Matches(event)` before sending, so clients only receive events they're interested in. This avoids wasting bandwidth on events the client would discard.
- **Buffered channels** -- Each client gets a 64-slot buffered channel (`subscriptionBufferSize = 64`). This absorbs brief bursts without blocking the broadcast loop.
- **Drop-on-full** -- If a client's channel is full (slow consumer), the event is dropped rather than blocking. A Prometheus counter tracks dropped events.
- **Max subscribers** -- Hard limit of 1000 concurrent SSE clients per broker (`maxSubscribers = 1000`). Returns `ErrTooManySubscribers` if exceeded.
- **Thread safety** -- `sync.RWMutex` protects the subscriber map. `Broadcast` takes a read lock; `Subscribe`/`Unsubscribe` take a write lock.

**SSE handler lifecycle** (`internal/handler/syslog_sse.go`, `applog_sse.go`):

1. Parse filter from query parameters
2. Set SSE headers (`Content-Type: text/event-stream`, `X-Accel-Buffering: no`)
3. Subscribe to broker (before backfill to avoid race)
4. Backfill recent events or resume from `Last-Event-ID`
5. Enter event loop: read from subscription channel, write SSE frames, send heartbeats every 15s
6. Exit on client disconnect or broker shutdown

### PostgreSQL Listener

`postgres.Listener` (`internal/postgres/listener.go`) manages a dedicated PostgreSQL connection for `LISTEN/NOTIFY`:

- **Dedicated connection** -- Uses a raw `pgx.Conn` (not from the pool) because `LISTEN` requires a persistent connection. The pool connection is separate for queries.
- **Channels** -- Currently listens on `syslog_ingest`. Notifications carry the row ID as the payload.
- **Reconnection** -- On connection loss, the listener reconnects with exponential backoff (1s initial, 30s max) plus jitter to avoid thundering herd.
- **Gap fill** -- After reconnecting, queries `SELECT id FROM syslog_events WHERE id > $lastSeenID ORDER BY id ASC LIMIT 10000` to catch events missed during disconnection.
- **Buffer monitoring** -- A goroutine checks channel utilization every 30s. Warns at 80% capacity.
- **Graceful shutdown** -- Cancels the context, closes the connection, and lets the goroutine drain.

### Notification Engine

`notification.Engine` (`internal/notification/engine.go`) provides rule-based alerting with burst aggregation and delivery resilience:

**Rule evaluation:**

1. Events are passed to `HandleSyslogEvent` or `HandleAppLogEvent`
2. Each enabled rule is evaluated: filter fields (hostname, severity, search, etc.) are matched
3. Matching events are added to the `GroupTracker` with a group key (e.g., hostname)

**Burst aggregation:**

- `GroupTracker` collects events within a configurable burst window (default 30s)
- After the window closes, a single notification is dispatched with the event count
- Cooldown with exponential backoff prevents notification storms (default 60s, max 1h)

**Dispatch pipeline:**

1. `onGroupFlush` resolves rule -> channels mapping and enqueues a `dispatchJob`
2. Worker goroutines (default 4) process jobs from a buffered channel (default 1024)
3. Each channel delivery goes through rate limiting and a per-channel circuit breaker

**Circuit breakers** (per channel, via `sony/gobreaker`):

- Opens after 5 consecutive failures
- Half-open allows 2 probe requests
- Resets after 60s timeout

**Backends:**

| Type | Package | Delivery |
|---|---|---|
| Slack | `internal/notification/backend` | Webhook POST |
| Webhook | `internal/notification/backend` | HTTP POST with JSON payload |
| Email | `internal/notification/backend` | SMTP with STARTTLS |

### Auth Layer

Authentication is handled by `internal/auth/middleware.go`:

**Two auth mechanisms:**

1. **Session cookies** -- `tl_session` cookie contains a random token. The SHA-256 hash is stored in the `sessions` table. Sessions expire and are cleaned every 15 minutes.
2. **API keys** -- `Authorization: Bearer tl_...` header. Keys are 43 base62 characters with a `tl_` prefix. The SHA-256 hash is stored in `api_keys`. Keys carry scopes (`read`, `ingest`, `admin`).

**Middleware chain:**

- `SessionOrAPIKey` -- Tries session cookie first, then Bearer token. Stores authenticated user and scopes in context.
- `RequireScope(scope)` -- Checks context scopes. Session auth (nil scopes) gets full access. The `admin` scope implies all other scopes.
- `AllowAnonymous` -- Used when auth is disabled; stores a synthetic anonymous user.

**Security measures:**

- Passwords hashed with bcrypt (cost 12)
- Timing-safe dummy check prevents username enumeration
- Tokens stored as SHA-256 hashes (never plaintext)
- API key prefix stored for display (`tl_0123456...`)

## Database Schema

All tables live in a single `taillight` database on TimescaleDB (PostgreSQL with the timescaledb extension).

### syslog_events (hypertable)

Primary event table for syslog messages from network devices.

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
```

**Hypertable settings:**

| Setting | Value |
|---|---|
| Partition column | `received_at` |
| Chunk interval | 1 day |
| Segment by | `hostname` |
| Order by | `received_at DESC` |
| Columnstore | After 1 day |
| Retention | 90 days (configurable) |

**Indexes:**

| Name | Columns | Purpose |
|---|---|---|
| `idx_syslog_received_id` | `(received_at DESC, id DESC)` | Cursor pagination |
| `idx_syslog_id` | `(id)` | Single event lookup |
| `idx_syslog_host_received` | `(hostname, received_at DESC)` | Host filter |
| `idx_syslog_severity_received` | `(severity, received_at DESC, id DESC) WHERE severity <= 3` | Critical event filter |
| `idx_syslog_programname` | `(programname, received_at DESC)` | Program filter |
| `idx_syslog_facility` | `(facility, received_at DESC)` | Facility filter |
| `idx_syslog_fromhost_ip` | `(fromhost_ip, received_at DESC)` | Source IP filter |
| `idx_syslog_syslogtag` | `(syslogtag, received_at DESC)` | Tag filter |
| `idx_syslog_message_trgm` | `message gin_trgm_ops (GIN)` | Trigram substring search |

**Triggers:**

- `trg_syslog_notify` -- Fires `pg_notify('syslog_ingest', id)` on INSERT
- `trg_syslog_meta_cache` -- Upserts hostname/programname/syslogtag into `syslog_meta_cache`
- `trg_syslog_facility_cache` -- Upserts facility code into `syslog_facility_cache`

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
| `idx_applog_service_received` | `(service, received_at DESC)` | Service filter |
| `idx_applog_level_received` | `(level, received_at DESC)` | Level filter |
| `idx_applog_host_received` | `(host, received_at DESC)` | Host filter |
| `idx_applog_search` | `search_vector (GIN)` | Full-text search |

**Triggers:**

- `trg_applog_meta_cache` -- Upserts service/component/host into `applog_meta_cache`

### Auth Tables

```sql
-- users: bcrypt password hashes, admin flag, active status
users (id UUID PK, username TEXT UNIQUE, password_hash, email, is_active, is_admin, ...)

-- sessions: SHA-256 hashed tokens, expiry, last-seen tracking
sessions (token_hash TEXT PK, user_id FK, expires_at, last_seen_at, ip_address, user_agent)

-- api_keys: SHA-256 hashed keys, scopes array, optional expiry
api_keys (id UUID PK, user_id FK, name, key_hash, key_prefix, scopes TEXT[], expires_at, revoked_at)
```

### Notification Tables

```sql
-- notification_channels: configured backends (slack, webhook, email)
notification_channels (id BIGINT PK, name UNIQUE, type, config JSONB, enabled)

-- notification_rules: alert conditions with filter fields and behavior config
notification_rules (id BIGINT PK, name UNIQUE, enabled, event_kind,
    hostname, programname, severity, severity_max, facility, syslogtag, msgid,
    service, component, host, level, search,
    burst_window, cooldown_seconds, max_cooldown_seconds, group_by)

-- notification_rule_channels: many-to-many mapping
notification_rule_channels (rule_id FK, channel_id FK, PK(rule_id, channel_id))

-- notification_log: audit trail (hypertable, 7-day chunks, 30-day retention)
notification_log (id, created_at, rule_id, channel_id, event_kind, event_id,
    status ['sent','suppressed','failed'], reason, event_count, status_code,
    duration_ms, payload JSONB)
```

### Supporting Tables

```sql
-- syslog_meta_cache: distinct hostnames, programs, tags with last_seen_at
syslog_meta_cache (column_name TEXT, value TEXT, last_seen_at, PK(column_name, value))

-- syslog_facility_cache: distinct facility codes
syslog_facility_cache (facility SMALLINT PK)

-- applog_meta_cache: distinct services, components, hosts with last_seen_at
applog_meta_cache (column_name TEXT, value TEXT, last_seen_at, PK(column_name, value))

-- juniper_syslog_ref: Juniper syslog reference documentation
juniper_syslog_ref (id, name, message, description, type, severity, cause, action, os)

-- analysis_reports: LLM-generated log analysis reports
analysis_reports (id, generated_at, model, period_start, period_end, report, ...)

-- rsyslog_stats: impstats telemetry (hypertable, 1-day chunks, 30-day retention)
rsyslog_stats (collected_at, origin, name, stats JSONB)

-- taillight_metrics: application metrics snapshots (hypertable, 1-day chunks, 30-day retention)
taillight_metrics (collected_at, sse_clients_syslog, sse_clients_applog,
    db_pool_active, db_pool_idle, db_pool_total,
    events_broadcast, events_dropped, applog_events_broadcast, ...)
```

## Deployment Topology

### Docker Compose (Development / Small Production)

The standard deployment runs 4 containers on a bridge network:

```
+----------------------------------------------------------------+
|                     Docker Compose                             |
|                                                                |
|  +------------+    +----------+    +---------+    +----------+ |
|  |  postgres   |    |   api    |    | rsyslog |    | frontend | |
|  | TimescaleDB |    | Go HTTP  |    | ompgsql |    | Vue SPA  | |
|  | :5432       |    | :8080    |    | :514    |    | nginx:80 | |
|  +------+------+    +----+-----+    +----+----+    +----+-----+ |
|         |               |               |               |      |
|         +-------+-------+-------+-------+               |      |
|                 |     taillight network  |               |      |
|                 +-----------------------+               |      |
+----------------------------------------------------------------+
          |               |               |               |
      :5432           :8080         :1514/udp          :3000
     (host)          (host)        :1514/tcp          (host)
```

**Services:**

| Service | Image | Port (host) | Purpose |
|---|---|---|---|
| `postgres` | `timescale/timescaledb:latest-pg18` | 5432 | TimescaleDB with pg_trgm, pg_stat_statements |
| `api` | Built from `./api` | 8080 | Go HTTP/SSE server |
| `rsyslog` | Built from `./rsyslog` | 1514 (UDP+TCP) | Syslog receiver with ompgsql |
| `frontend` | Built from `./frontend` | 3000 | Vue 3 SPA served by nginx |

**Health checks:**

- `postgres` -- `psql -U taillight -d taillight -c 'SELECT 1'` (5s interval)
- `api` -- `wget --spider http://localhost:8080/health` (10s interval)
- `rsyslog` -- `logger -t healthcheck test` (30s interval)
- `frontend` -- `wget --spider http://127.0.0.1:80/healthz` (10s interval)

**Volumes:** `pgdata` for PostgreSQL data persistence. Config mounted read-only at `/config.yml`.

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
           /health      |     |    +-------> frontend:80
                        |     |
         /api/v1/*/stream     |
         (SSE, no buffering)  |
                        |     +-----------> api:8080
                        +-----------------> api:8080
```

**nginx configuration** (`docs/nginx-reverse-proxy.conf.example`):

- **TLS** -- TLSv1.2 and TLSv1.3, HTTP/2 enabled
- **Rate limiting** -- Login: 10 req/min (burst 5). API: 30 req/s (burst 50).
- **SSE support** -- `proxy_buffering off`, `proxy_read_timeout 86400s` for stream endpoints
- **Security headers** -- `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`
- **Gzip** -- Enabled for JSON, CSS, JavaScript (min 256 bytes)
- **HTTP -> HTTPS redirect** -- Port 80 returns 301 to HTTPS

**Metrics server** -- When `metrics_addr` is set (e.g., `:9090`), a separate HTTP server exposes Prometheus metrics at `/metrics`, isolated from the main API.

## Frontend Architecture

The frontend is a Vue 3 Single Page Application built with TypeScript, Vite, and Tailwind CSS.

### Router (`src/router.ts`)

| Route | View | Description |
|---|---|---|
| `/` | `HomeView` | Live recent events from both streams |
| `/syslog` | `SyslogListView` | Filterable syslog event list with SSE |
| `/syslog/:id` | `SyslogView` | Single event detail |
| `/dashboard` | `DashboardView` | Volume charts, severity distribution |
| `/device/:hostname` | `DeviceView` | Per-device summary |
| `/applog` | `AppLogListView` | Filterable applog event list with SSE |
| `/applog/:id` | `AppLogView` | Single applog detail |
| `/applog/device/:hostname` | `AppLogDeviceView` | Per-host applog summary |
| `/notifications` | `NotificationsView` | Channel/rule management, delivery log |
| `/settings` | `SettingsView` | User settings |
| `/settings/api-keys` | `ApiKeysView` | API key management |
| `/login` | `LoginView` | Login form |

Auth-guarded: the router's `beforeEach` hook redirects unauthenticated users to `/login` unless the route has `meta: { public: true }`.

### Pinia Stores

- **`syslog-events`** / **`applog-events`** -- Event lists with cursor pagination (factory pattern via `event-store-factory.ts`)
- **`syslog-filters`** / **`applog-filters`** -- Active filter state (factory via `filter-store-factory.ts`)
- **`meta`** / **`applog-meta`** -- Cached hostnames, programs, services for filter dropdowns
- **`dashboard`** / **`applog-dashboard`** / **`volume-dashboard`** -- Chart data for dashboards
- **`auth`** -- User session state, login/logout
- **`home`** -- Home page recent events from SSE streams
- **`scroll`** -- Scroll position tracking for auto-scroll behavior

### SSE Composables

- **`useEventStream`** (`src/composables/useEventStream.ts`) -- Generic SSE client factory with exponential backoff reconnection (1s-30s), heartbeat watchdog (35s timeout), and `Last-Event-ID` resume support.
- **`useSyslogStream`** -- Singleton instance for `GET /api/v1/syslog/stream` with event name `syslog`.
- **`useAppLogStream`** -- Singleton instance for `GET /api/v1/applog/stream` with event name `applog`.

## Performance Characteristics

### Database

- **Cursor pagination** -- Keyset pagination using `(received_at, id) < (?, ?)` tuple comparison avoids OFFSET performance degradation on large tables.
- **Compound indexes** -- Every filter column has a compound index with `received_at DESC` to enable sort elimination and index-only scans.
- **Trigram search** -- `pg_trgm` GIN index on `message` enables efficient `ILIKE '%term%'` substring search without sequential scans.
- **Full-text search** -- Applog events use a stored `tsvector` column with a GIN index for full-text search across service, component, host, message, and attrs.
- **Columnstore** -- Chunks older than 1 day are converted to columnar storage, reducing storage footprint.
- **Meta caches** -- Instead of `SELECT DISTINCT` (which scans entire hypertables), trigger-maintained cache tables provide O(1) lookups for filter dropdown values.
- **Batch queries** -- Device summary uses `pgx.Batch` to send 4 queries in a single round-trip.
- **Connection pool** -- `pgxpool` with configurable max/min connections (default 10/2).

### SSE

- **Per-client buffer** -- 64-message buffered channel per subscriber absorbs burst traffic.
- **Max subscribers** -- 1000 concurrent SSE clients per broker (2000 total across syslog + applog).
- **Heartbeat** -- 15-second interval keeps connections alive through proxies and load balancers.
- **Backfill** -- Up to 100 recent events sent on connect, avoiding empty initial state.
- **Drop-on-full** -- Slow clients get events dropped rather than blocking the broadcast loop.

### rsyslog

- **Main queue** -- LinkedList queue, 50,000 entries, 5 GB disk overflow, 4 worker threads.
- **Discard policy** -- At 45,000 entries (90%), info/debug messages are discarded first.
- **PostgreSQL queue** -- 50,000 entries, 2 GB disk, 4 workers, 128 INSERTs per batch transaction.
- **UDP threads** -- 8 receiver threads with batch size 128 for high-throughput UDP ingestion.
- **TCP sessions** -- Capped at 200 concurrent connections to prevent fd exhaustion.

## Security Model

### Authentication Modes

Taillight supports three authentication configurations:

1. **No auth** (`auth_enabled: false`) -- All endpoints are public. An anonymous user is injected into the context. Suitable for private networks.
2. **Auth without read protection** (`auth_enabled: true`, `auth_read_endpoints: false`) -- Write and ingest endpoints require authentication. Read endpoints (GET) are public.
3. **Full auth** (`auth_enabled: true`, `auth_read_endpoints: true`) -- All endpoints require authentication. Recommended for production.

### Scope System

API keys carry scopes that restrict access:

| Scope | Access |
|---|---|
| `read` | All GET endpoints (syslog, applog, stats, meta, notifications) |
| `ingest` | `POST /api/v1/applog/ingest` |
| `admin` | All write operations (notification CRUD, analysis trigger, user management) |

Session-based auth (login with username/password) grants full access (no scope restrictions).

### CORS

- Configurable allowed origins via `cors_allowed_origins` config
- Credentials (`AllowCredentials: true`) only when origins are explicitly listed (not `*`)
- Defaults to localhost dev origins if unconfigured

### Security Headers

Applied by `handler.SecurityHeaders` middleware:

- `Content-Security-Policy` -- Restricts `connect-src` to configured CORS origins
- `X-Frame-Options: DENY`
- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: strict-origin-when-cross-origin`

### Credential Security

- **Passwords** -- bcrypt with cost factor 12
- **Timing-safe login** -- Dummy bcrypt check on invalid usernames prevents enumeration
- **Session tokens** -- 32 bytes of `crypto/rand`, base64-encoded, stored as SHA-256 hash
- **API keys** -- `tl_` prefix + 43 base62 chars (crypto/rand), stored as SHA-256 hash
- **Key display** -- Only first 10 characters stored as `key_prefix` for identification

### Rate Limiting (nginx)

- Login endpoint: 10 requests/minute per IP (burst 5)
- API endpoints: 30 requests/second per IP (burst 50)
- SSE streams: no rate limit (long-lived connections)
