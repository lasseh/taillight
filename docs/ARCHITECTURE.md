# Architecture

This document describes the system architecture, component breakdown, data flow, and deployment topology of Taillight.

## System Overview

Taillight is a real-time log aggregation and viewing platform with two independent ingestion pipelines: syslog (network devices via rsyslog) and applog (HTTP applications via REST API). Both pipelines converge on PostgreSQL/TimescaleDB and are pushed to browser clients through Server-Sent Events (SSE).

```text
                                 SYSLOG PIPELINE
                                 ===============

  +-----------+     UDP/TCP 514     +----------+     ompgsql INSERT     +-------------------+
  |  Network  | ------------------> |          | ----------------------> |                   |
  |  Devices  |                     | rsyslog  |                        |  PostgreSQL +      |
  |  (Junos,  |                     |          | ---+                   |  TimescaleDB      |
  |   IOS,    |                     +----------+    |                   |                   |
  |   etc.)   |                          |          |  impstats         |  syslog_events    |
  +-----------+                     filter chain    |  telemetry        |  rsyslog_stats    |
                                    msgid ->        +-----------------> |  applog_events    |
                                    programname ->                      |                   |
                                    facility ->                         +--------+----------+
                                    severity ->                                  |
                                                                    pg_notify    |
                                                                  'syslog_ingest'|
                                 APPLOG PIPELINE                                 |
                                 ===============                                 v

  +-----------+    POST /api/v1     +----------+                        +--------+----------+
  |   HTTP    |    /applog/ingest   |          |  LISTEN syslog_ingest  |                   |
  |   Apps    | ------------------> |  Go API  | <--------------------- |  PostgreSQL       |
  |  (slog,   |                     |  (chi)   |                        |  LISTEN/NOTIFY    |
  |  logrus,  |  batch INSERT       |          |                        +-------------------+
  |  etc.)    |  RETURNING *        +----+-----+
  +-----------+                          |
                                         | SSE (text/event-stream)
                                         |
                              +----------+-----------+
                              |                      |
                              v                      v
                      +---------------+      +---------------+
                      | SyslogBroker  |      | ApplogBroker  |
                      | (fan-out +    |      | (fan-out +    |
                      |  per-client   |      |  per-client   |
                      |  filtering)   |      |  filtering)   |
                      +-------+-------+      +-------+-------+
                              |                      |
                              v                      v
                      +--------------------------------------+
                      |           SSE Endpoints              |
                      |  /api/v1/syslog/stream               |
                      |  /api/v1/applog/stream                |
                      +------------------+-------------------+
                                         |
                                         v
                      +--------------------------------------+
                      |          Vue 3 Frontend              |
                      |   EventSource -> Pinia Store         |
                      |   Unovis Charts / Tailwind v4        |
                      +--------------------------------------+
```

## Component Breakdown

### rsyslog

**Role:** Receives syslog messages from network devices, applies a layered filter chain to reduce noise, and writes surviving events to PostgreSQL via `ompgsql`.

**Configuration structure** (`rsyslog/`):

| File | Purpose |
|------|---------|
| `rsyslog.conf` | Global settings, main queue (LinkedList, 50K deep, 5GB disk-assisted), include ordering |
| `conf.d/00-modules.conf` | Module loading: imudp (8 threads), imtcp, imuxsock, omprog, ompgsql, mmpstrucdata, mmjsonparse, impstats (60s, resetCounters=on) |
| `conf.d/01-templates.conf` | Output templates: NetworkDeviceFormat, JSONFormat, PgSQLInsert, PgSQLStatsInsert, LibreNMSFormat, DebugFields |
| `conf.d/02-outputs.conf` | Output rulesets: output_critical, output_local (per-host files), output_pgsql (ompgsql), output_librenms (omprog), output_remote |
| `conf.d/03-operational-logging.conf` | impstats processing: mmjsonparse, idle filtering, stdout logging, optional ompgsql for rsyslog_stats |
| `conf.d/10-inputs.conf` | Listeners: UDP 514 (rate-limited 20K/5s), TCP 514 (max 200 sessions), UDP 1514 (no rate limit) |
| `conf.d/20-ruleset.conf` | Main `network_devices` ruleset: parse structured data, filter chain, route to outputs |

**Filter chain** (evaluated in order, cheapest first):

```text
Incoming message
    |
    v
[Phase 0] Parse RFC 5424 structured data (mmpstrucdata)
    |
    v
[Always] Route severity <= 2 to critical log file
    |
    v
[05-by-msgid.conf]      MSGID exact match (fastest)
  - Chassis noise: CHASSISD_BLOWERS_SPEED, TEMP_ZONE, FAN, PSU, SENSORS
  - RPD scheduler: RPD_SCHED_CALLBACK, RPD_SCHED_MODULE_INFO
  - RT_FLOW sessions: SESSION_CREATE, SESSION_CLOSE (keep denies)
  - SNMP traps: TRAP_COLD_START, TRAP_WARM_START, AUTH_FAILURE
  - Config audit: UI_CFG_AUDIT_SET, UI_CMDLINE_READ_LINE
  - BGP routine: BGP_CONNECT, BGP_READ, BGP_WRITE (keep state changes)
  - Each drop rule has exception keywords (major|critical|alarm|error|fail)
    |
    v
[10-by-programname.conf] Daemon-level drops
  - cron (unconditional), ntpd, mib2d, dcd, lacpd, cosd, alarmd, sshd, pfed
  - Exception keywords per daemon (error, fail, critical, etc.)
    |
    v
[30-by-facility.conf]   Facility-based filtering
  - Drop local7 info (Juniper daemon default)
    |
    v
[40-by-severity.conf]   Global severity threshold
  - Drop all debug (severity 7)
    |
    v
[50-by-hostname.conf]   Hostname/IP-based filtering (empty by default)
    |
    v
[Phase 2] Route to outputs: output_pgsql, output_local, output_librenms
```

**Docker integration:** The config files are Docker-ready by default (`server="postgres"`, `pwd="taillight"`). The Dockerfile validates the config with `rsyslogd -N1`. For company-specific deployments, mount custom `conf.d/` and `filters/` via `docker-compose.override.yml`.

### PostgreSQL + TimescaleDB

**Role:** Primary data store with time-series partitioning, columnstore compression, real-time push via LISTEN/NOTIFY, and authentication tables.

**Hypertables:**

| Table | Partition Column | Segment By | Chunk Interval | Compression | Retention |
|-------|-----------------|-----------|----------------|-------------|-----------|
| `syslog_events` | `received_at` | `hostname` | 1 day | Columnstore after 1 day | 90 days |
| `applog_events` | `received_at` | `service` | 1 day | Columnstore after 1 day | 90 days |
| `rsyslog_stats` | `collected_at` | `origin` | 1 day | Columnstore after 1 day | 30 days |

**Key indexes:**

| Index | Purpose |
|-------|---------|
| `(received_at DESC, id DESC)` | Cursor-based keyset pagination (primary access pattern) |
| `(hostname, received_at DESC)` | Filter by host with time ordering |
| `(severity, received_at DESC, id DESC) WHERE severity <= 3` | Partial index for critical events |
| `(programname, received_at DESC)` | Compound filter+sort index |
| `message gin_trgm_ops` | Trigram GIN index for ILIKE substring search |
| `search_vector GIN` | Full-text search on applog (tsvector across service, component, host, msg) |

**LISTEN/NOTIFY triggers:**

- `trg_syslog_notify`: fires `pg_notify('syslog_ingest', NEW.id::text)` on every syslog INSERT
- Applog: trigger was removed (migration 000003) because the ingest handler broadcasts directly, which would cause duplicate events

**Meta cache tables:**

- `syslog_meta_cache`: populated by trigger, caches distinct (hostname, programname, syslogtag) values
- `syslog_facility_cache`: caches distinct facility codes
- `applog_meta_cache`: caches distinct (service, component, host) values
- Avoids expensive DISTINCT queries on large hypertables

**Authentication tables:**

| Table | Key Details |
|-------|-------------|
| `users` | UUID PK, bcrypt password_hash, is_admin, is_active, case-insensitive unique username |
| `sessions` | SHA-256 token_hash PK, 30-day expiry, user_id FK, ip_address, user_agent |
| `api_keys` | SHA-256 key_hash (unique where not revoked), `tl_` prefix, optional expiry, revocable |

**Other tables:**

- `juniper_syslog_ref`: Juniper syslog message reference (name, description, cause, action, OS)
- `analysis_reports`: LLM analysis output (model, period, report text, token counts, duration)

### Go API

**Role:** HTTP/SSE server that bridges the database to browser clients. Provides REST endpoints for querying, SSE streams for real-time push, HTTP ingest for application logs, and optional LLM analysis.

**Entry point:** `api/cmd/taillight/serve.go` -- cobra subcommand `serve`.

**Middleware stack** (applied in order):

```text
RequestID -> RealIP -> RequestLogger -> Logger -> Recoverer
  -> SecurityHeaders -> HTTPMetrics (Prometheus) -> CORS -> Auth
```

Security headers include X-Content-Type-Options, X-Frame-Options, Referrer-Policy, Permissions-Policy, and Content-Security-Policy.

**Route structure:**

```text
/health                          GET   Health check (DB ping)
/api/v1/
  auth/
    login                        POST  Session login (unauthenticated)
    logout                       POST  Session logout (unauthenticated)
    me                           GET   Current user info
    me/email                     PATCH Update email
    keys                         GET   List API keys
    keys                         POST  Create API key
    keys/{id}                    DEL   Revoke API key
    users                        GET   List users (admin)
    users/{id}/active            PATCH Toggle user active (admin)
    users/{id}/password          PATCH Update user password (admin)
  syslog/
    stream                       GET   SSE stream (no timeout)
    /                            GET   List events (paginated)
    /{id}                        GET   Single event
  meta/
    hosts                        GET   Cached hostnames
    programs                     GET   Cached program names
    facilities                   GET   Cached facility codes
    tags                         GET   Cached syslog tags
  stats/
    volume                       GET   Event volume over time
    summary                      GET   Syslog summary stats
  juniper/
    lookup                       GET   Juniper message reference
  rsyslog/
    stats/summary                GET   rsyslog impstats summary
    stats/volume                 GET   rsyslog throughput over time
  analysis/
    reports                      GET   List analysis reports
    reports/latest               GET   Latest report
    reports/{id}                 GET   Single report
    reports/trigger              POST  Trigger analysis (15min timeout)
  applog/
    stream                       GET   SSE stream (no timeout)
    /                            GET   List events (paginated)
    /{id}                        GET   Single event
    meta/services                GET   Cached service names
    meta/components              GET   Cached component names
    meta/hosts                   GET   Cached host names
    stats/volume                 GET   Applog volume over time
    stats/summary                GET   Applog summary stats
    ingest                       POST  Batch ingest (authenticated)
```

**Key subsystems:**

**LISTEN/NOTIFY Listener** (`internal/postgres/listener.go`):
- Dedicated `pgx.Conn` (separate from the connection pool)
- Runs `LISTEN syslog_ingest` and `LISTEN applog_ingest`
- `WaitForNotification` loop in a goroutine
- Auto-reconnect with exponential backoff (1s initial, 30s max) plus jitter
- Channel utilization monitor (warns at 80% capacity, default buffer 1024)

**SSE Broker** (`internal/broker/`):
- `SyslogBroker` and `ApplogBroker` -- identical pattern
- `sync.RWMutex` protects `map[*Subscription]struct{}`
- Per-client filter: `filter.Matches(event)` evaluated for every subscriber
- Buffered channel per client (64 slots)
- Non-blocking send: drops events for slow clients (counted by Prometheus)
- `Shutdown()` closes all channels for graceful termination

**SSE Handler** (`internal/handler/syslog_sse.go`):
1. Parse filter from query parameters
2. Subscribe to broker BEFORE backfill (prevents race condition)
3. Backfill: resume from `Last-Event-ID` header, or send 100 most recent events
4. Skip events with `ID <= lastBackfilledID` (deduplication)
5. Live stream with 15-second heartbeat ticker
6. Sets `X-Accel-Buffering: no` for nginx

**Applog Ingest** (`internal/handler/applog_ingest.go`):
1. `MaxBytesReader` (5 MB limit)
2. Validate batch (max 1000 entries, field length limits, level normalization)
3. `store.InsertLogBatch` -- batch INSERT RETURNING * (populates ID and ReceivedAt)
4. Loop: `broker.Broadcast(event)` for each inserted row
5. No LISTEN/NOTIFY -- direct broadcast avoids duplicate events

**Background workers** (started in `serve.go`):
- Notification bridge: reads from Listener channel, fetches syslog event by ID, broadcasts to SyslogBroker
- DB pool metrics: updates Prometheus gauges every 15 seconds
- Session cleanup: purges expired sessions every 15 minutes

**Connection pool:** pgxpool with configurable max/min conns (default 10/2).

**Configuration** (`internal/config/config.go`): Viper-based YAML config with environment variable overrides. Supports `config.yaml` from current directory, `/etc/taillight`, or `/`.

### LLM Analysis Subsystem

**Role:** Optional scheduled analysis of syslog data using a local Ollama instance.

```text
+----------+     daily schedule      +----------+     HTTP API      +--------+
| Scheduler| --------------------->  | Analyzer | ----------------> | Ollama |
| (HH:MM   |                        | (gather  |                   | (local |
|  UTC)     |                        |  + run)  |                   |  LLM)  |
+----------+                         +----+-----+                   +--------+
                                          |
                                          v
                                 +------------------+
                                 | analysis_reports |
                                 | (PostgreSQL)     |
                                 +------------------+
```

- **Scheduler** (`internal/scheduler/`): Runs analysis at a configured UTC time daily
- **Analyzer** (`internal/analyzer/`): Gathers syslog data for a period, builds a prompt, calls Ollama, stores the report
- **Ollama client** (`internal/ollama/`): HTTP client for the Ollama generate API
- Configurable model, temperature, context window size

### Vue Frontend

**Role:** Single-page application for real-time log viewing, filtering, and dashboard visualization.

**Technology:** Vue 3 + TypeScript + Tailwind CSS v4 + Pinia + Unovis charts.

**Key patterns:**

**Event Store Factory** (`createEventStore`):
- Factory function that creates Pinia stores for both syslog and applog
- SSE subscription with `EventSource`
- `_knownIds` Set for deduplication (capped at 10,000 entries)
- Cursor-based pagination with `AbortController` for superseded requests
- Automatic reconnection on SSE disconnect

**Filter Store Factory** (`createFilterStore`):
- Factory function for filter state management
- Bidirectional sync with URL query parameters
- Filter changes trigger SSE reconnection with updated parameters

**SSE Composables:**
- `useSyslogStream` / `useAppLogStream` wrap `EventSource`
- Auto-reconnect with backoff on connection loss
- `Last-Event-ID` support for seamless resume

**Theme System:** 16 color themes with CSS custom properties, persisted in localStorage.

**Build and deploy:**
- Development: `npm run dev` (Vite dev server, port 5173)
- Production: multi-stage Docker build (Node 22 -> nginx:alpine)
- nginx serves static files only (no API proxy)
- A separate reverse proxy handles routing to both frontend and API (see `docs/nginx-reverse-proxy.conf.example`)

### nginx

**Role:** Static file server in the frontend Docker container. Serves the Vue SPA with SPA-style routing fallback.

**Configuration** (`frontend/nginx.conf`):

```text
location /                              -> try_files (SPA fallback)
location ~* \.(js|css|...)              -> 1 year cache, immutable
location /health                        -> 200 OK
location /healthz                       -> 200 ok
```

API proxying is handled by a separate reverse proxy in production (see `docs/nginx-reverse-proxy.conf.example`).

Security headers: X-Frame-Options DENY, X-Content-Type-Options nosniff, Referrer-Policy strict-origin-when-cross-origin.

## Data Flow Diagrams

### Syslog Pipeline (detail)

```text
Network Device
    |
    | UDP/TCP port 514
    v
rsyslog (imudp/imtcp)
    |
    | network_devices ruleset
    v
mmpstrucdata (parse RFC 5424 SD)
    |
    v
Filter chain: msgid -> programname -> facility -> severity -> hostname
    |
    | (survivors only)
    v
ompgsql: INSERT INTO syslog_events (...) VALUES (...)
    |
    | PostgreSQL trigger: trg_syslog_notify
    v
pg_notify('syslog_ingest', NEW.id::text)
    |
    | PostgreSQL LISTEN connection (dedicated pgx.Conn)
    v
Listener.recv() -> Notification{Channel: "syslog_ingest", ID: <id>}
    |
    | notification bridge goroutine
    v
store.GetSyslog(ctx, id) -> SyslogEvent
    |
    v
SyslogBroker.Broadcast(event)
    |
    | for each subscriber:
    |   if filter.Matches(event) -> send to buffered channel
    v
SSE handler: fmt.Fprintf(w, "id: %d\nevent: syslog\ndata: %s\n\n", id, json)
    |
    | text/event-stream
    v
Browser EventSource -> Pinia store -> reactive UI
```

### Applog Pipeline (detail)

Applications can send logs to the ingest endpoint directly, or use the provided
integration tools:

- **Go apps** -- use the [`logshipper`](../api/pkg/logshipper/README.md)
  package, a drop-in `slog.Handler` that batches and ships logs in the
  background.
- **Non-Go apps / log files** -- use the `taillight-shipper` CLI
  (`api/cmd/taillight-shipper/`) to tail files or pipe stdin.
- **Direct HTTP** -- POST JSON batches to the ingest endpoint from any language.

```text
HTTP Application
    |
    | POST /api/v1/applog/ingest
    | { "logs": [ { timestamp, level, service, host, msg, ... } ] }
    v
AppLogIngestHandler.Ingest()
    |
    | 1. MaxBytesReader (5 MB)
    | 2. JSON decode
    | 3. Validate (max 1000 entries, field lengths, level normalization)
    v
store.InsertLogBatch(ctx, events)
    |
    | batch INSERT INTO applog_events (...) VALUES (...), (...), ...
    | RETURNING id, received_at, ...
    v
[]AppLogEvent (with DB-assigned IDs and timestamps)
    |
    | for each inserted event:
    v
ApplogBroker.Broadcast(event)      <-- direct broadcast, no LISTEN/NOTIFY
    |
    | for each subscriber:
    |   if filter.Matches(event) -> send to buffered channel
    v
SSE handler: fmt.Fprintf(w, "id: %d\nevent: applog\ndata: %s\n\n", id, json)
    |
    | text/event-stream
    v
Browser EventSource -> Pinia store -> reactive UI
```

### SSE Connection Lifecycle

```text
Client connects: GET /api/v1/syslog/stream?hostname=rtr01&severity_max=3
    |
    v
1. Parse filter from query parameters
    |
    v
2. Subscribe to broker (BEFORE backfill)
   broker.Subscribe(filter) -> *SyslogSubscription{ch, filter}
    |
    v
3. Backfill:
   a. If Last-Event-ID header present:
      store.ListSyslogsSince(filter, lastID, 100) -> send events ASC
   b. Else:
      store.ListSyslogs(filter, nil, 100) -> send events oldest-first
   Record lastBackfilledID
    |
    v
4. Live stream loop:
   select {
     case msg <- sub.Chan():
       if msg.ID > lastBackfilledID -> write SSE frame, flush
     case <- heartbeat.C (15s):
       write "event: heartbeat\ndata: \n\n", flush
     case <- r.Context().Done():
       return (client disconnected)
   }
    |
    v
5. Disconnect: defer broker.Unsubscribe(sub) -> close channel, decrement gauge
```

## Technology Stack

| Component | Technology | Version | Purpose |
|-----------|-----------|---------|---------|
| **Runtime** | Go | 1.24 | API server, SSE broker, CLI tools |
| **HTTP Router** | chi | v5.2.5 | Lightweight, idiomatic Go HTTP routing |
| **Database Driver** | pgx | v5.8.0 | PostgreSQL driver with LISTEN/NOTIFY support |
| **Connection Pool** | pgxpool | v5.8.0 | Connection pooling with health checks |
| **Query Builder** | squirrel | v1.5.4 | SQL query construction |
| **Migrations** | golang-migrate | v4.19.1 | Database schema migrations |
| **CLI Framework** | cobra | v1.10.2 | Subcommand-based CLI (serve, migrate, loadgen, useradd, apikey) |
| **Configuration** | viper | v1.21.0 | YAML config with environment variable overrides |
| **Metrics** | prometheus/client_golang | v1.23.2 | Prometheus metrics exposition |
| **Passwords** | golang.org/x/crypto/bcrypt | - | Bcrypt password hashing |
| **Database** | PostgreSQL 18 + TimescaleDB | latest | Time-series storage, hypertables, columnstore compression |
| **Syslog Collector** | rsyslog | Debian Trixie | High-performance syslog reception and filtering |
| **Frontend Framework** | Vue | 3.5 | Reactive SPA framework |
| **Build Tool** | Vite | 6.2 | Fast HMR development server and production bundler |
| **CSS** | Tailwind CSS | v4.1 | Utility-first CSS framework |
| **State Management** | Pinia | 2.3 | Vue store with factory pattern |
| **Charts** | Unovis | 1.6 | Data visualization library |
| **Type Checking** | TypeScript | 5.7 | Static type analysis |
| **Reverse Proxy** | nginx | Alpine | SSE-aware reverse proxy, static file serving |
| **Container Runtime** | Docker Compose | - | Multi-service orchestration |
| **API Runtime Image** | distroless/static-debian12:nonroot | - | Minimal, no-shell production container |

## Deployment Topology

### Docker Compose (default)

```text
+----------------------------------------------------------+
|  Docker Compose Network                                  |
|                                                          |
|  +-----------+    :5432    +---------+    :8080          |
|  | postgres  | <---------- |   api   | <-------+        |
|  | (tsdb +   |   pgxpool   | (Go)    |         |        |
|  |  pg18)    |   + LISTEN   |         |         |        |
|  +-----------+              +---------+         |        |
|       ^                                         |        |
|       | ompgsql                          proxy_pass      |
|  +-----------+              +-----------+       |        |
|  | rsyslog   |              | frontend  | ------+        |
|  | (Debian)  |              | (nginx +  |                |
|  +-----------+              |  Vue SPA) |                |
|   :1514->514                +-----------+                |
|                              :3000->80                   |
+----------------------------------------------------------+

Host ports:
  35432 -> postgres:5432
  8080  -> api:8080
  1514  -> rsyslog:514 (UDP+TCP)
  3000  -> frontend:80
```

Service dependencies:
- `postgres` starts first (healthcheck: `pg_isready`)
- `api` waits for postgres healthy, then runs migrations via initdb.sh
- `rsyslog` waits for postgres healthy
- `frontend` waits for api to start

### Production (separate subdomains)

For production, the recommended topology uses separate subdomains with an external nginx or load balancer:

```text
  taillight.example.com     -> frontend (nginx, static SPA)
  api.taillight.example.com -> Go API (direct or behind reverse proxy)
```

The frontend container serves only static files. `API_URL` is injected at container startup so the SPA makes direct requests to the API. See `docs/nginx-reverse-proxy.conf.example` for the recommended production reverse proxy configuration.

## Security Model

### Authentication Flow

```text
Request arrives
    |
    v
1. Check cookie: "tl_session"
   |-- found -> SHA-256 hash -> sessions table lookup
   |   |-- valid, not expired -> store User in context -> proceed
   |   |-- invalid/expired -> fall through
   |
2. Check header: "Authorization: Bearer <token>"
   |-- starts with "tl_" -> SHA-256 hash -> api_keys table lookup
   |   |-- valid, not revoked, not expired -> store User in context -> proceed
   |   |-- invalid -> fall through
   |
3. Check config-based API keys (constant-time comparison)
   |-- match -> proceed (no User in context)
   |-- no match -> 401 Unauthorized
```

**Key security properties:**
- Passwords: bcrypt (cost 10) with timing-safe dummy check to prevent username enumeration
- Session tokens: 32 bytes cryptographic random, base64-encoded to client, SHA-256 hash in DB
- API keys: `tl_` prefix + 43 base62 chars, SHA-256 hash in DB, prefix stored for display
- Config keys: constant-time comparison (`crypto/subtle.ConstantTimeCompare`)
- Sessions: 30-day expiry, max 10 per user, fire-and-forget `last_seen_at` update, periodic cleanup (15 min)
- Admin role: `is_admin` boolean, required for `SetUserActive`, `ListUsers`; key revocation requires ownership or admin

**Authentication modes:**
- `auth_enabled: true, auth_read_endpoints: true` -- all endpoints require authentication
- `auth_enabled: true, auth_read_endpoints: false` -- only write/admin endpoints require auth, reads are public
- `auth_enabled: false` -- all endpoints public, anonymous user injected

### Prometheus Metrics

All metrics use the `taillight_` namespace:

| Metric | Type | Description |
|--------|------|-------------|
| `http_requests_total` | Counter | Requests by method, path, status |
| `http_request_duration_seconds` | Histogram | Request latency by method, path |
| `sse_clients_active` | Gauge | Connected syslog SSE clients |
| `applog_sse_clients_active` | Gauge | Connected applog SSE clients |
| `events_broadcast_total` | Counter | Syslog events broadcast |
| `events_dropped_total` | Counter | Syslog events dropped (slow clients) |
| `applog_events_broadcast_total` | Counter | Applog events broadcast |
| `applog_events_dropped_total` | Counter | Applog events dropped |
| `applog_ingest_total` | Counter | Log entries ingested |
| `applog_ingest_batches_total` | Counter | Ingest batch requests |
| `applog_ingest_errors_total` | Counter | Failed ingest requests |
| `notifications_received_total` | Counter | LISTEN/NOTIFY notifications by channel |
| `listener_reconnects_total` | Counter | Listener reconnection attempts |
| `db_pool_active_conns` | Gauge | Active DB connections |
| `db_pool_idle_conns` | Gauge | Idle DB connections |
| `db_pool_total_conns` | Gauge | Total DB connections |
| `analysis_runs_total` | Counter | Analysis runs by status |
| `analysis_duration_seconds` | Histogram | Analysis run duration |

Metrics are served on a separate HTTP server when `metrics_addr` is configured (e.g., `:9090`), keeping the metrics endpoint isolated from the main API.
