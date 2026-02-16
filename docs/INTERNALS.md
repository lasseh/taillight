# Taillight Internals

Deep-dive reference for developers who want to understand, debug, or extend
Taillight's backend and frontend internals.

---

## Table of Contents

1. [LISTEN/NOTIFY Listener](#1-listennotify-listener)
2. [SSE Broker Pattern](#2-sse-broker-pattern)
3. [SSE Handler Lifecycle](#3-sse-handler-lifecycle)
4. [Applog Ingest Pipeline](#4-applog-ingest-pipeline)
5. [TimescaleDB Schema Design](#5-timescaledb-schema-design)
6. [Meta Cache System](#6-meta-cache-system)
7. [Cursor-Based Pagination](#7-cursor-based-pagination)
8. [Authentication System](#8-authentication-system)
9. [Frontend Store Architecture](#9-frontend-store-architecture)
10. [rsyslog Filter Pipeline](#10-rsyslog-filter-pipeline)
11. [LLM Analysis Subsystem](#11-llm-analysis-subsystem)
12. [Log Shipper](#12-log-shipper)
13. [Prometheus Metrics](#13-prometheus-metrics)

---

## 1. LISTEN/NOTIFY Listener

**Source:** `api/internal/postgres/listener.go`

The `Listener` is the bridge between PostgreSQL and the SSE brokers. It holds a
**dedicated `pgx.Conn`** (separate from the connection pool) that runs
`LISTEN syslog_ingest` and `LISTEN applog_ingest`. A separate connection is
required because `WaitForNotification` blocks the connection for the entire
lifetime of the listener -- sharing a pool connection would starve normal
queries.

### Connection Lifecycle

```
connect() -> LISTEN syslog_ingest -> LISTEN applog_ingest -> recv loop
```

The `recv` goroutine calls `conn.WaitForNotification(ctx)` in a tight loop.
Each notification payload is the row ID as text, which is parsed into an `int64`
and sent to a buffered channel:

```go
type Notification struct {
    Channel string  // "syslog_ingest" or "applog_ingest"
    ID      int64
}
```

### Auto-Reconnect

On connection loss, the listener enters an exponential backoff loop:

| Parameter | Value |
|-----------|-------|
| Initial backoff | 1 second |
| Maximum backoff | 30 seconds |
| Backoff multiplier | 2x |
| Jitter | random 0 to backoff/2 |

The jitter prevents thundering herd when multiple instances reconnect
simultaneously. Each reconnect attempt increments the
`taillight_listener_reconnects_total` Prometheus counter.

### Channel Utilization Monitoring

A separate goroutine polls the notification channel every 30 seconds. If the
buffer is more than 80% full, a warning is logged with the current length and
capacity. This provides an early signal that the SSE brokers or downstream
consumers are not keeping up.

```go
usage := float64(len(ch)) / float64(cap(ch))
if usage > 0.8 {
    l.logger.Warn("notification channel near capacity", ...)
}
```

The buffer size defaults to 1024 and is configurable via the
`notification_buffer_size` config key.

### Graceful Shutdown

`Shutdown(ctx)` cancels the listener context, which unblocks
`WaitForNotification` and causes the recv goroutine to close the channel and
return. The dedicated connection is then closed explicitly.

---

## 2. SSE Broker Pattern

**Source:** `api/internal/broker/syslog_broker.go`, `api/internal/broker/applog_broker.go`

`SyslogBroker` and `ApplogBroker` are structurally identical fan-out
dispatchers. Each holds a map of subscriptions protected by an `RWMutex`:

```go
type SyslogBroker struct {
    mu          sync.RWMutex
    subscribers map[*SyslogSubscription]struct{}
    logger      *slog.Logger
}
```

Using the subscription pointer as the map key avoids needing a separate ID
allocator -- each subscription is inherently unique by its memory address.

### Subscription Structure

Each subscription pairs a buffered channel (capacity 64) with a filter:

```go
type SyslogSubscription struct {
    ch     chan SyslogMessage  // buffered, 64
    filter model.SyslogFilter
}
```

The buffer size (64) is a tradeoff: large enough to absorb short bursts without
blocking the broadcast goroutine, small enough that a stalled client doesn't
consume unbounded memory.

### Broadcast Path

```
Broadcast(event) -> marshal JSON once -> RLock -> iterate subscribers
    -> filter.Matches() -> non-blocking send -> RUnlock
```

Key details:

- **Marshal once**: the event is JSON-serialized a single time, then the
  `[]byte` is sent to all matching subscribers. This avoids O(N) marshaling.
- **Filter first**: `filter.Matches()` runs before the channel send, so
  non-matching events never touch the channel.
- **Non-blocking send**: uses `select { case ch <- msg: default: drop }`.
  If the channel is full, the event is dropped and both a metric
  (`taillight_events_dropped_total`) and a warning log are emitted.
- **Early exit**: if `b.Len() == 0`, the broadcast returns immediately without
  marshaling.

### Slow Client Handling

Slow clients (those whose channel fills up) simply lose events. There is no
disconnect, no backpressure to PostgreSQL, and no retry -- the broker logs a
warning and increments a counter. The SSE handler on the client side will
reconnect with `Last-Event-ID` if it detects a gap.

### Shutdown

`Shutdown()` takes a write lock, closes all subscription channels, and clears
the map. Each SSE handler detects the channel closure and returns, cleanly
ending the HTTP response.

---

## 3. SSE Handler Lifecycle

**Source:** `api/internal/handler/syslog_sse.go`, `api/internal/handler/applog_sse.go`

The SSE handlers follow a careful protocol to avoid race conditions between
historical backfill and live events.

### Subscribe-Before-Backfill

```go
sub := h.broker.Subscribe(filter)
defer h.broker.Unsubscribe(sub)
lastBackfilledID := h.backfill(w, r, filter, flusher)
```

The subscription is created **before** the backfill query runs. This ensures
that events inserted between the end of the backfill query and the start of
live streaming are captured in the subscription channel rather than lost.

The `lastBackfilledID` is used to deduplicate: any live event with
`msg.ID <= lastBackfilledID` is skipped.

### Backfill Strategy

Two modes, selected by the presence of `Last-Event-ID`:

| Condition | Query | Order |
|-----------|-------|-------|
| `Last-Event-ID` present | `ListSyslogsSince(sinceID, limit)` | ASC (chronological) |
| No `Last-Event-ID` | `ListSyslogs(filter, nil, 100)` | Reversed to ASC |

Both paths cap backfill at 100 events. The `Last-Event-ID` header is the
primary mechanism; a `lastEventId` query parameter is also accepted as a
fallback for clients that cannot set custom headers (e.g., `EventSource`
without polyfill).

### Main Loop

The handler enters a three-way select:

```go
select {
case msg, ok := <-sub.Chan():
    // Live event from broker
case <-heartbeat.C:
    // 15-second keepalive
case <-r.Context().Done():
    // Client disconnected
}
```

### SSE Wire Format

```
id: 12345
event: syslog
data: {"id":12345,"hostname":"router1",...}

```

For applog events, `event: applog` is used instead. Heartbeats use:

```
event: heartbeat
data:

```

### HTTP Headers

| Header | Value | Purpose |
|--------|-------|---------|
| `Content-Type` | `text/event-stream` | SSE standard |
| `Cache-Control` | `no-cache` | Prevent proxy caching |
| `Connection` | `keep-alive` | Keep TCP alive |
| `X-Accel-Buffering` | `no` | Disable nginx response buffering |

---

## 4. Applog Ingest Pipeline

**Source:** `api/internal/handler/applog_ingest.go`

Unlike syslog events (which arrive via rsyslog's ompgsql and trigger
LISTEN/NOTIFY), applog events are pushed directly via HTTP.

### Request Flow

```
POST /api/v1/applog/ingest
  -> MaxBytesReader (5 MB cap)
  -> JSON decode
  -> Validate (max 1000 entries, field lengths, level normalization)
  -> Batch INSERT with RETURNING *
  -> Direct broadcast to ApplogBroker
  -> 202 Accepted
```

### Validation Limits

| Field | Max Length |
|-------|-----------|
| Batch size | 1000 entries |
| Body size | 5 MB |
| Service | 128 chars |
| Component | 128 chars |
| Host | 256 chars |
| Source | 256 chars |
| Message | 64 KB |
| Attrs (JSON) | 64 KB |

### Level Normalization

Incoming levels are mapped to a canonical set:

| Input | Canonical |
|-------|-----------|
| `TRACE` | `DEBUG` |
| `DEBUG` | `DEBUG` |
| `INFO` | `INFO` |
| `WARN`, `WARNING` | `WARN` |
| `ERROR` | `ERROR` |
| `FATAL`, `CRITICAL`, `PANIC` | `FATAL` |

### Why No LISTEN/NOTIFY for Applog

The ingest handler broadcasts directly to the `ApplogBroker` after the batch
INSERT returns. This avoids the round-trip through PostgreSQL's notification
system, which would cause duplicate broadcasts (once from the handler, once
from the trigger). The syslog path uses LISTEN/NOTIFY because events arrive
from rsyslog -- an external process that writes directly to the database --
where no in-process broker is available.

### Batch INSERT

Uses the pgx `Batch` API to queue individual `INSERT ... RETURNING *`
statements, sent in a single round-trip. The RETURNING clause populates the
auto-generated `id` and `received_at` fields, which are needed for SSE
broadcast and client deduplication.

---

## 5. TimescaleDB Schema Design

**Source:** `api/migrations/000001_init_schema.up.sql`

### Hypertable Configuration

| Table | Partition Column | Chunk Interval | Segment By | Order By |
|-------|-----------------|----------------|------------|----------|
| `syslog_events` | `received_at` | 1 day | `hostname` | `received_at DESC` |
| `applog_events` | `received_at` | 1 day | `service` | `received_at DESC, id DESC` |
| `rsyslog_stats` | `collected_at` | 1 day | `origin` | `collected_at DESC` |

The `segment_by` column determines columnstore grouping within each chunk.
Queries filtered by `hostname` (syslog) or `service` (applog) can skip
irrelevant segments entirely.

### No PRIMARY KEY

TimescaleDB requires the partition column to be included in any unique
constraint. Since we use `received_at` partitioning but `id` as the logical
identifier, a traditional `PRIMARY KEY (id)` is not possible. Instead, we rely
on `BIGINT GENERATED ALWAYS AS IDENTITY` for uniqueness and a B-tree index on
`(id)` for single-row lookups.

### Compression and Retention

| Policy | syslog_events | applog_events | rsyslog_stats |
|--------|---------------|---------------|---------------|
| Columnstore compression | After 1 day | After 1 day | After 1 day |
| Retention (chunk drop) | 90 days | 90 days | 30 days |

Compression converts row-oriented chunks to columnar storage, typically
achieving 10-20x compression. The default TimescaleDB 7-day policy is replaced
with 1-day to compress data sooner.

### Index Strategy

**syslog_events:**

| Index | Columns | Purpose |
|-------|---------|---------|
| `idx_syslog_received_id` | `(received_at DESC, id DESC)` | Cursor pagination |
| `idx_syslog_id` | `(id)` | Single event lookup |
| `idx_syslog_host_received` | `(hostname, received_at DESC)` | Host filter |
| `idx_syslog_severity_received` | `(severity, received_at DESC, id DESC) WHERE severity <= 3` | Partial index for errors |
| `idx_syslog_programname` | `(programname)` | Program filter |
| `idx_syslog_facility` | `(facility)` | Facility filter |
| `idx_syslog_fromhost_ip` | `(fromhost_ip)` | IP filter |
| `idx_syslog_syslogtag` | `(syslogtag)` | Tag filter |
| `idx_syslog_message_trgm` | `GIN (message gin_trgm_ops)` | ILIKE substring search |

**applog_events:**

| Index | Columns | Purpose |
|-------|---------|---------|
| `idx_applog_received_id` | `(received_at DESC, id DESC)` | Cursor pagination |
| `idx_applog_service_received` | `(service, received_at DESC)` | Service filter |
| `idx_applog_level_received` | `(level, received_at DESC)` | Level filter |
| `idx_applog_attrs` | `GIN (attrs jsonb_path_ops)` | JSONB attribute queries |
| `idx_applog_search` | `GIN (search_vector)` | Full-text search |

The partial index on severity (`WHERE severity <= 3`) covers only error-level
and above events. This keeps the index small while accelerating the most common
alerting queries.

### Full-Text Search

Applog uses a generated `tsvector` column:

```sql
search_vector tsvector GENERATED ALWAYS AS (
    to_tsvector('simple', coalesce(service,'') || ' ' ||
                          coalesce(component,'') || ' ' ||
                          coalesce(host,'') || ' ' ||
                          coalesce(msg,''))
) STORED
```

Queries use `plainto_tsquery('simple', ?)` for natural word matching. Syslog
uses trigram-based `ILIKE` search instead, because syslog messages often
contain structured tokens (e.g., `CHASSISD_BLOWERS_SPEED`) that benefit from
substring matching rather than word-boundary tokenization.

### Autovacuum Tuning

```sql
ALTER TABLE syslog_events SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_analyze_scale_factor = 0.02
);
```

The defaults (20% / 10%) are far too lazy for a high-insert append-only table.
Lowering to 5% / 2% ensures statistics stay current and dead tuples are cleaned
promptly.

---

## 6. Meta Cache System

**Source:** `api/migrations/000001_init_schema.up.sql` (triggers), `api/internal/postgres/store.go` (queries)

### Problem

Filter dropdowns need the set of distinct hostnames, program names, services,
etc. Running `SELECT DISTINCT hostname FROM syslog_events` on a multi-million
row hypertable is prohibitively slow, even with indexes.

### Solution

Three cache tables maintain the set of known values:

| Table | Key | Populated By |
|-------|-----|-------------|
| `syslog_meta_cache` | `(column_name, value)` composite PK | `cache_syslog_meta()` trigger |
| `syslog_facility_cache` | `facility` PK | `cache_syslog_facility()` trigger |
| `applog_meta_cache` | `(column_name, value)` composite PK | `cache_applog_meta()` trigger |

Each trigger fires on `AFTER INSERT` and does `INSERT ... ON CONFLICT DO NOTHING`.
This is effectively free for existing values (the conflict is a no-op) and
costs only one small write for genuinely new values.

### Queried Columns

- **syslog**: `hostname`, `programname`, `syslogtag` (plus `facility` in its
  own table)
- **applog**: `service`, `component`, `host`

The Go store validates the requested column against a hardcoded allowlist
before interpolating it into a query:

```go
var allowedMetaColumns = map[string]struct{}{
    "hostname":    {},
    "programname": {},
    "syslogtag":   {},
}
```

### Tradeoff

Meta cache values are never deleted. If a hostname stops sending logs, its
entry persists in the cache even after the underlying data ages out past the
90-day retention window. This is an accepted tradeoff -- stale filter options
are preferable to slow DISTINCT queries.

---

## 7. Cursor-Based Pagination

**Source:** `api/internal/model/syslog.go` (Cursor type), `api/internal/postgres/store.go` (query)

### Why Keyset Pagination

Traditional `OFFSET`-based pagination degrades as offset grows (the database
must scan and discard rows). Keyset pagination uses an indexed WHERE clause that
seeks directly to the correct position, making page N as fast as page 1.

### Cursor Encoding

A cursor encodes a `(received_at, id)` pair as base64:

```go
func (c Cursor) Encode() string {
    raw := fmt.Sprintf("%d,%d", c.ReceivedAt.UnixNano(), c.ID)
    return base64.URLEncoding.EncodeToString([]byte(raw))
}
```

The timestamp is stored as nanoseconds since epoch to preserve full precision.
The base64 encoding makes the cursor opaque to clients -- they should not parse
or construct cursors manually.

### Query Pattern

```sql
SELECT ... FROM syslog_events
WHERE (received_at, id) < ($cursor_ts, $cursor_id)
  AND <filter conditions>
ORDER BY received_at DESC, id DESC
LIMIT $limit + 1
```

The `LIMIT + 1` trick: request one extra row. If we get it, there are more
pages (`has_more = true`), and we trim the result to `$limit`. The last
included row's `(received_at, id)` becomes the next cursor.

### Response Envelope

```json
{
  "data": [...],
  "cursor": "base64-encoded-cursor",
  "has_more": true
}
```

---

## 8. Authentication System

**Source:** `api/internal/auth/` (middleware, crypto), `api/internal/handler/auth.go` (endpoints), `api/internal/postgres/auth_store.go` (storage)

### Auth Method Priority

The `SessionOrAPIKey` middleware tries three methods in order:

1. **Session cookie** (`tl_session`) -- SHA-256 hash the cookie value, look up
   in `sessions` table joined with `users`
2. **DB API key** (`Authorization: Bearer tl_...`) -- SHA-256 hash the token,
   look up in `api_keys` table joined with `users`
3. **Config static key** -- constant-time compare against keys defined in
   `config.yml`

On success, the authenticated `*model.User` is stored in the request context
via `auth.WithUser()`. Config keys do not have a user context (`UserFromContext`
returns nil).

### Session Management

| Parameter | Value |
|-----------|-------|
| Cookie name | `tl_session` |
| Token size | 32 random bytes, base64-encoded |
| Storage | SHA-256 hash of token |
| Expiry | 30 days |
| Max per user | 10 (oldest pruned on login) |
| Flags | HttpOnly, SameSite=Lax, Secure (auto-detected) |

Session `last_seen_at` is updated asynchronously via a fire-and-forget
background goroutine to avoid adding latency to every authenticated request:

```go
go func() {
    _, _ = s.pool.Exec(context.Background(),
        `UPDATE sessions SET last_seen_at = now() WHERE token_hash = $1`, tokenHash)
}()
```

### API Keys

- **Format**: `tl_` prefix + 43 base62 characters (total 46 characters)
- **Storage**: SHA-256 hash (never store the raw key)
- **Display prefix**: first 10 characters stored for UI display (`tl_Ab3xY...`)
- **Expiry**: optional, checked at lookup time
- **Revocation**: `revoked_at` timestamp; revoked keys are excluded by a
  partial unique index: `WHERE revoked_at IS NULL`

`last_used_at` is updated asynchronously on each use, same pattern as sessions.

### Timing Safety

Several measures prevent timing-based enumeration:

1. **Unknown username**: `DummyCheckPassword()` burns the same bcrypt CPU time
   as a real check, then returns "invalid credentials"
2. **Inactive account**: bcrypt runs **before** checking `is_active`, so the
   response time is identical for active and inactive accounts
3. **Config keys**: `constantTimeMatch()` uses `crypto/subtle.ConstantTimeCompare`
   and always iterates all keys (never short-circuits on match)

### Admin Role

The `is_admin` boolean on the `users` table gates two endpoints:

- `PATCH /api/v1/auth/users/{id}/active` -- enable/disable accounts
- `GET /api/v1/auth/users` -- list all users

API key revocation requires either ownership or admin status.

---

## 9. Frontend Store Architecture

**Source:** `frontend/src/stores/event-store-factory.ts`, `frontend/src/stores/filter-store-factory.ts`, `frontend/src/composables/useEventStream.ts`

### Event Store Factory

`createEventStore<TEvent>()` produces a generic Pinia store that manages the
full lifecycle for both syslog and applog event lists:

```
Enter -> Reset state -> Load initial page (100 events) -> Mark initial load complete -> Start accepting SSE events
```

Key implementation details:

- **shallowRef for events**: The events array uses `shallowRef` instead of
  `ref` to skip Vue's deep reactivity tracking. Events are immutable once
  received, so deep observation is wasted work. Mutations happen by replacing
  the entire array (`events.value = [...events.value, event]`).

- **Deduplication via `_knownIds` Set**: Every event ID (from both history loads
  and SSE) is tracked in a `Set<number>`. When the set exceeds 10,000 entries,
  the oldest 5,000 are trimmed to prevent unbounded growth:

  ```typescript
  if (_knownIds.size > 10000) {
      const iter = _knownIds.values()
      for (let i = 0; i < 5000; i++) {
          _knownIds.delete(iter.next().value!)
      }
  }
  ```

- **AbortController for history requests**: When filters change, any in-flight
  `loadHistory` request is aborted via `AbortController` before starting a new
  one. This prevents stale responses from overwriting fresher data.

- **Filter watch triggers re-enter**: A `watch` on `activeFilters` calls
  `enter()` (which resets and reloads) whenever the user changes any filter.

### Filter Store Factory

`createFilterStore<K>(id, filterKeys, routeName)` produces a Pinia store that
keeps filter state bidirectionally synced with URL query parameters:

- **Read from URL**: `initFromURL()` reads query params into the reactive
  `filters` object on mount
- **Write to URL**: a `watch` on `filters` calls `router.replace()` to update
  the URL without navigation
- **activeFilters computed**: omits empty values, so the URL stays clean

### SSE Composables

`createEventStream<T>(path, eventName)` is a module-level singleton that
manages a single `EventSource` connection:

| Parameter | Value |
|-----------|-------|
| Initial backoff | 1 second |
| Maximum backoff | 30 seconds |
| Heartbeat timeout | 35 seconds (2x server's 15s heartbeat) |

Reconnect uses the `lastEventId` query parameter to resume from the last
received event. A watchdog timer checks every 5 seconds whether a heartbeat
or event has been received within the timeout window; if not, the connection
is torn down and rescheduled.

`useSyslogStream()` and `useAppLogStream()` are thin wrappers that instantiate
the singleton for each event type:

```typescript
const stream = createEventStream<SyslogEvent>('/api/v1/syslog/stream', 'syslog')
```

### Theme System

19 themes defined in `frontend/src/lib/themes.ts` (14 dark, 5 light), each
providing an `id`, `name`, and `chartColors` array. Themes are applied via a
`data-theme` attribute on the root element, which CSS custom properties key off
of. Severity colors are overridden for light themes to maintain contrast.

---

## 10. rsyslog Filter Pipeline

**Source:** `rsyslog/conf.d/20-ruleset.conf`, `rsyslog/filters/`

### Processing Order

The `network_devices` ruleset processes messages through a layered filter chain,
ordered cheapest-first to reject noise as early as possible:

```
Phase 0: mmpstrucdata (parse RFC 5424 structured data)
Phase 1: Filters
   1. Critical severity capture (sev <= 2 always logged)
   2. $msgid event name filter (fastest, most precise)
   3. UI_COMMIT trigger routing
   4. $programname filter (daemon-level drops)
   5. Facility filter
   6. Severity threshold filter (drops debug globally)
   7. Hostname/IP filter
Phase 2: Output routing
   -> LibreNMS (omprog)
   -> Local per-host files (omfile DynaFile)
   -> PostgreSQL (ompgsql, optional)
   -> Remote forwarding (omfwd, optional)
```

### Exception Keywords

Every filter that drops messages checks for exception keywords first. This
prevents accidentally silencing genuine problems:

```
if ($msgid == "CHASSISD_BLOWERS_SPEED" or ...) then {
    if (not re_match(tolower($msg), "major|critical|alarm|failed")) then { stop }
}
```

The `re_match(tolower($msg), ...)` pattern is used because rsyslog's POSIX ERE
does not support `(?i)` for case-insensitive matching.

Common exception keywords: `error`, `fail`, `critical`, `down`, `denied`,
`alarm`, `panic`, `trap`.

### Critical Severity Capture

Before any filters run, severity <= 2 (crit, alert, emerg) is routed to
`/var/log/network/critical_logs.log`. This ensures critical events are never
silently dropped by downstream filters.

### Queue Configuration

The main ruleset uses a disk-assisted LinkedList queue:

```
queue.type="LinkedList"
queue.size="10000"
queue.maxDiskSpace="1g"
queue.saveOnShutdown="on"
queue.workerThreads="2"
```

### impstats Telemetry

The `impstats` module reports throughput counters every 60 seconds with
`resetCounters=on`, meaning each snapshot contains **deltas** (not cumulative
totals). The `operational_stats` ruleset parses the JSON stats, filters idle
components (submitted=0, size=0, no discards), and logs active stats to stdout.
Stats are also optionally written to the `rsyslog_stats` hypertable for the
backend dashboard.

---

## 11. LLM Analysis Subsystem

**Source:** `api/internal/analyzer/`

### Architecture

The analysis subsystem is optional -- it requires a running Ollama instance.
When enabled, it follows this pipeline:

```
Scheduler (configurable time, default 03:00)
  -> Analyzer.Run()
    -> Ping Ollama (availability check)
    -> Gather data (top msgids, severity comparison, error hosts, new msgids, event clusters)
    -> Build prompt (system + user messages)
    -> Call Ollama chat API
    -> Store report in analysis_reports table
```

### Data Gathering

The analyzer queries multiple facets of recent log activity to build context
for the LLM:

| Query | Purpose |
|-------|---------|
| `GetTopMsgIDs` | Most frequent event types with severity breakdown |
| `GetSeverityComparison` | Current vs baseline severity distribution |
| `GetTopErrorHosts` | Hosts producing the most errors |
| `GetNewMsgIDs` | Event types seen for the first time |
| `GetEventClusters` | Time windows with correlated events across hosts |
| `LookupJuniperRefs` | Juniper syslog reference entries for context |

### Report Storage

Reports are stored in the `analysis_reports` table with metadata:

| Field | Description |
|-------|-------------|
| `model` | Ollama model name |
| `period_start`, `period_end` | Analysis time window |
| `report` | Generated markdown text |
| `prompt_tokens` | Input token count |
| `completion_tokens` | Output token count |
| `duration_ms` | Total run time |
| `status` | `completed` or `failed` |

### Configuration

| Parameter | Default | Config Key |
|-----------|---------|------------|
| Ollama URL | `http://localhost:11434` | `analysis.ollama_url` |
| Model | `llama3` | `analysis.model` |
| Temperature | 0.3 | `analysis.temperature` |
| Context window | 8192 | `analysis.num_ctx` |
| Schedule | `03:00` | `analysis.schedule_at` |

---

## 12. Log Shipper

**Source:** `api/pkg/logshipper/handler.go`, `api/pkg/logshipper/multi.go`

> **Using logshipper in your own Go app?** See
> [`api/pkg/logshipper/README.md`](../api/pkg/logshipper/README.md) for install,
> quick-start, `MultiHandler`, and config reference.

The log shipper is a standard `slog.Handler` that batches and ships log entries
to Taillight's applog ingest endpoint. It can be embedded in any Go application
that uses `log/slog`. Taillight itself uses it in "eat your own dog food" mode
-- its own logs appear in its own UI.

### Architecture

```
slog.Logger
  -> MultiHandler
    -> slog.TextHandler (console output)
    -> logshipper.Handler
      -> buffered channel (1024)
      -> batch loop (100 entries or 1s flush)
      -> POST /api/v1/applog/ingest
```

### Handler Implementation

The `Handler` implements `slog.Handler` and converts each `slog.Record` into a
`logEntry` struct, then pushes it to a buffered channel:

```go
select {
case h.ch <- entry:
default:
    h.dropped.Add(1)  // atomic counter, no blocking
}
```

If the channel is full, the entry is silently dropped and counted. This is
critical to avoid backpressure from log shipping affecting the application.

### Batch Loop

A background goroutine consumes from the channel and flushes in two cases:

1. **Batch full**: when the buffer reaches `BatchSize` (default 100)
2. **Timer fires**: every `FlushPeriod` (default 1 second)

On shutdown, the channel is drained with a fresh `context.Background()` to
ensure remaining entries are flushed even if the parent context is cancelled.

### MultiHandler

`MultiHandler` fans out each log record to multiple `slog.Handler`
implementations. It checks `Enabled()` on each handler individually and clones
the record before passing to each handler to prevent mutation conflicts.

### Configuration

| Parameter | Default | Config Key |
|-----------|---------|------------|
| Enabled | false | `logshipper.enabled` |
| Service | `taillight` | `logshipper.service` |
| Component | `server` | `logshipper.component` |
| Min level | INFO | `logshipper.min_level` |
| Batch size | 100 | `logshipper.batch_size` |
| Flush period | 1 second | `logshipper.flush_period` |
| Buffer size | 1024 | `logshipper.buffer_size` |

### Infinite Recursion Prevention

The log shipper must not log its own ingest requests, as that would create an
infinite loop (log -> ship -> ingest -> log -> ship -> ...). This is handled by
a `SkipPath` middleware that suppresses logging for the ingest endpoint.

---

## 13. Prometheus Metrics

**Source:** `api/internal/metrics/metrics.go`

All metrics use the `taillight_` namespace and are registered via `promauto`
(auto-registering with the default Prometheus registry).

### HTTP Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `http_requests_total` | Counter | method, path, status | Request count |
| `http_request_duration_seconds` | Histogram | method, path | Latency (default buckets) |

### SSE Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `sse_clients_active` | Gauge | -- | Connected syslog SSE clients |
| `events_broadcast_total` | Counter | -- | Syslog events broadcast |
| `events_dropped_total` | Counter | -- | Syslog events dropped (slow clients) |
| `applog_sse_clients_active` | Gauge | -- | Connected applog SSE clients |
| `applog_events_broadcast_total` | Counter | -- | Applog events broadcast |
| `applog_events_dropped_total` | Counter | -- | Applog events dropped |

### Ingest Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `applog_ingest_total` | Counter | -- | Total log entries ingested |
| `applog_ingest_batches_total` | Counter | -- | Ingest POST requests |
| `applog_ingest_errors_total` | Counter | -- | Failed ingest requests |

### Infrastructure Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `listener_reconnects_total` | Counter | -- | LISTEN/NOTIFY reconnection attempts |
| `notifications_received_total` | Counter | channel | Notifications by channel |
| `db_pool_active_conns` | Gauge | -- | Active pool connections |
| `db_pool_idle_conns` | Gauge | -- | Idle pool connections |
| `db_pool_total_conns` | Gauge | -- | Total pool connections |

### Analysis Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `analysis_runs_total` | Counter | status | Analysis runs (completed/failed) |
| `analysis_duration_seconds` | Histogram | -- | Run duration (30s-600s buckets) |

### Metrics Server

An optional separate metrics server can be configured via `metrics_addr`. When
set, Prometheus metrics are served on a dedicated port, keeping the metrics
endpoint separate from the main API (useful for firewall rules and service
mesh configurations).
