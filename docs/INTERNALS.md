# Internals

This document covers the internal implementation details of the taillight backend. It is aimed at contributors and developers who want to understand how the code works, why specific design decisions were made, and how the major subsystems fit together.

All file paths are relative to `api/` unless otherwise noted. Standing decisions live as ADRs in `docs/adr/` at the repo root.

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

A separate `StatsStore` interface in `internal/handler/stats.go` covers volume and summary queries. The concrete `postgres.Store` satisfies all of these interfaces — ~25 narrow consumer-side interfaces over one Store type is the deliberate module boundary (see the Data Access Layer section and `docs/adr/0003-store-stays-one-type.md`).

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

- `model.ParseSrvlogFilter(r)` / `model.ParseNetlogFilter(r)` -- hostname, fromhost_ip, programname, severity, facility, syslogtag, msgid, search, time range
- `model.ParseAppLogFilter(r)` -- service, component, host, level (rank or exact), search, time range
- `model.ParseCursor(r)` -- decodes the `cursor` query param
- `model.ParseLimit(r, default, max)` -- extracts and clamps the `limit` param

Validation is centralized in a `queryParams` helper (`internal/model/filterparse.go`) so length bounds, integer ranges, and IP/RFC3339 parsing live in one place instead of three copies. All string filter parameters are capped at 500 characters (`maxFilterStringLen`); typed parameters return per-field errors for invalid values.

### Wire-contract golden fixtures

The highest-churn API surfaces — the three event shapes, the `{data, cursor, has_more}` envelopes, and the applog ingest request — are pinned by golden JSON fixtures under `internal/handler/testdata/golden/`. `TestGolden` (`internal/handler/golden_test.go`) marshals canonical fixtures from the real Go types; the frontend asserts the exact same files against its TypeScript types (`frontend/src/types/__tests__/contract-goldens.test.ts`). An intentional contract change is regenerated with `go test ./internal/handler -run TestGolden -update` and breaks whichever side wasn't updated. A route-inventory test (`cmd/taillight/route_inventory_test.go`) walks the full chi router and fails if any registered route is missing from the OpenAPI spec (`docs/openapi.yml`, embedded and served at `/api/v1/openapi.yml` + `/api/docs`). Rationale: `docs/adr/0004-contract-goldens-not-codegen.md`.

---

## SSE Broker System

The broker system fans out events to connected SSE clients with per-client filtering. There is **one** generic implementation, `Broker[E, F]` (`internal/broker/broker.go`); `SrvlogBroker`, `NetlogBroker`, and `AppLogBroker` are thin constructor wrappers that bind the event type, the SSE label, the ID extractor, and the Prometheus counters.

### Data structures

```
Broker[E any, F interface{ Matches(E) bool }]
  mu          sync.RWMutex
  subscribers map[*Subscription[F]]struct{}
  byClient    map[string]int        // per-client-key connection counts
  getID       func(E) int64
  label       string                // "srvlog" | "netlog" | "applog"
  metrics     BrokerMetrics         // OnSubscribe/OnUnsubscribe/OnBroadcast/OnDrop callbacks

Subscription[F]
  ch        chan Message            // buffered, cap=512
  filter    F
  clientKey string                  // "user:<uuid>" or "ip:<addr>"
```

Subscribers are tracked in a map keyed by pointer. This gives O(1) subscribe/unsubscribe and avoids index management. The `BrokerMetrics` callbacks let the generic broker update feed-specific Prometheus counters without importing the metrics package.

### Subscribe / Unsubscribe lifecycle

1. **Subscribe**: Creates a `Subscription` with a buffered channel (capacity 512) and the client's filter. Under the write lock it enforces two limits — 1000 subscribers per broker (`ErrTooManySubscribers`) and 20 per client key (`ErrTooManyClientSubscribers`; the key is the authenticated user ID, falling back to the client IP, so one user or one host can't exhaust the pool).

2. **Unsubscribe**: Acquires write lock, checks if the subscription exists (idempotent), removes it, closes the channel, and decrements the per-client count. The close signals the SSE handler's event loop to exit.

### Per-client filter matching

Each filter type implements a `Matches(event)` method that checks all non-zero fields against the event. The syslog filters check hostname (with wildcard support), fromhost_ip, programname, severity, severity_max, facility, syslogtag, msgid, and search (case-insensitive substring). The applog filter checks service, component, host (with wildcard), level (rank-based: "WARN" matches WARN, ERROR, FATAL — or exact via `level_exact`), and search (against both msg and attrs).

Time filters (`From`/`To`) are intentionally excluded from `Matches()` -- live SSE clients receive future events, so filtering by time range would be wrong.

Wildcard matching (`matchWildcard` in `internal/model/srvlog.go`) supports `*` as a glob character with case-insensitive comparison. The first segment anchors at the start, the last segment anchors at the end, and `*` matches any sequence in between.

### Broadcasting

```go
func (b *Broker[E, F]) Broadcast(event E) {
    data, err := json.Marshal(event)    // marshal once for all clients
    msg := Message{ID: b.getID(event), Data: data}
    b.metrics.OnBroadcast()

    b.mu.RLock()                        // read lock: concurrent reads OK
    defer b.mu.RUnlock()
    for sub := range b.subscribers {
        if !sub.filter.Matches(event) {
            continue                    // skip non-matching clients
        }
        select {
        case sub.ch <- msg:             // non-blocking send
        default:
            b.metrics.OnDrop()          // slow client: drop, don't block
        }
    }
}
```

Key details:
- The event is JSON-marshaled once, then the same `[]byte` is shared across all subscriptions — SSE payloads therefore cannot drift from the REST payloads, which marshal the same model structs.
- A read lock (`RLock`) is held during broadcast so subscribes/unsubscribes block but multiple broadcasts can proceed concurrently.
- The send is non-blocking: if a client's 512-message buffer is full, the event is dropped and a metric is incremented. This prevents one slow client from blocking all others.

### Limits and metrics

| Constant | Value | Purpose |
|----------|-------|---------|
| `subscriptionBufferSize` | 512 | Per-client channel buffer |
| `maxSubscribers` | 1000 | Maximum concurrent SSE clients per broker |
| `maxSubscribersPerClient` | 20 | Maximum connections per user/IP key |

Metrics tracked per feed: `taillight_sse_clients_active` (gauge), `taillight_events_broadcast_total`, `taillight_events_dropped_total`, with `netlog_*` / `applog_*` variants.

---

## LISTEN/NOTIFY Pipeline

Events flow from PostgreSQL to Go through the `Listener` (`internal/postgres/listener.go`), which uses PostgreSQL's LISTEN/NOTIFY mechanism to receive real-time notifications when new rows are inserted into `srvlog_events` or `netlog_events`.

### Why a dedicated connection

The Listener uses a bare `pgx.Conn` -- not a connection from the pool. This is because `WaitForNotification` is a blocking call that holds the connection indefinitely. Using a pool connection would tie up a slot and eventually starve other queries. The dedicated connection is created via `pgx.Connect()` and managed separately from the `pgxpool.Pool`. The listen goroutine is the connection's *single owner* — it alone calls `Close`, so shutdown can never race the connection (pgx.Conn is not safe for concurrent use).

### Architecture

```
PostgreSQL                        Go Listener                     ingestbridge workers
  INSERT → trigger →            ┌──────────────┐
  pg_notify('srvlog_ingest'     │  pgx.Conn    │
   or 'netlog_ingest', id) ───► │  LISTEN ×2   │──► Notification{channel, id}
                                └──────────────┘         │  (buffered chan, default 1024)
                                                         ▼
                                            Dispatch: GetSrvlog/GetNetlog(id)
                                                         │
                                                         ▼
                                        broker.Broadcast(event) + engine.Handle*Event
```

### Notification flow

1. **Receive**: `conn.WaitForNotification(ctx)` blocks until a notification arrives on either channel. The payload is the row ID as a string.

2. **Parse**: The payload is parsed to `int64`. Invalid payloads increment `taillight_listener_payload_parse_errors_total` and are skipped.

3. **Track**: A per-channel `lastSeenID` (`srvlog_ingest` and `netlog_ingest` each have their own atomic) is updated for gap-fill on reconnection.

4. **Dispatch**: A `Notification{Channel, ID}` is sent on the buffered channel. On the consumer side, `notification_workers` goroutines (default 4, started in `serve.go:startBackgroundWorkers`) call `ingestbridge.Dispatch` — the routing step extracted behind a small `EventFetcher` port so it is unit-testable without a database (`docs/adr/0001-listener-stays-postgres-bound.md`). Dispatch fetches the full row by ID (30s timeout), broadcasts it to the feed's broker, and hands it to the notification engine.

### Reconnection with gap fill

When the connection drops:

1. The old connection is closed by the listen goroutine.
2. `reconnect()` re-establishes the connection with exponential backoff (1s initial, 30s max) plus random jitter to avoid thundering herd. `taillight_listener_reconnects_total` is incremented per attempt.
3. After reconnecting, `fillGap()` runs per channel: it queries the channel's table (`channelTable` maps `srvlog_ingest` → `srvlog_events`, `netlog_ingest` → `netlog_events`) for IDs greater than that channel's `lastSeenID` (up to 10,000) and pushes them into the notification channel.
4. A full page (exactly 10,000 rows) means the outage exceeded the cap — recovery was incomplete. This is surfaced distinctly via `taillight_listener_gap_fill_truncated_total` and a warning log rather than silently advancing.

### Channel monitoring

A background goroutine checks the notification channel utilization every 30 seconds. If the buffer exceeds 80% capacity, a warning is logged. This indicates event bursts are outpacing consumption and `notification_buffer_size` (or `notification_workers`) may need to be increased.

---

## SSE Handler Lifecycle

The per-feed SSE handlers (`internal/handler/srvlog_sse.go`, `netlog_sse.go`, `applog_sse.go`) are thin: each builds an `sseStreamer` (`internal/handler/sse_stream.go`) — the shared generic that owns the streaming invariants for all three feeds, so a fix lands once.

### 1. Setup

```go
flusher, ok := w.(http.Flusher)     // verify streaming support
filter, err := model.ParseSrvlogFilter(r)

w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("Connection", "keep-alive")
w.Header().Set("X-Accel-Buffering", "no")  // disable nginx buffering
```

The `http.ResponseWriter` is wrapped in an `sseSink` adapter (write event / write heartbeat / flush / set write deadline). Tests substitute an in-memory sink, which is how the subscribe-before-backfill ordering and dedup logic are unit-tested without an HTTP server.

### 2. Subscribe before backfill

```go
sub, err := s.broker.Subscribe(filter, clientKey)
defer s.broker.Unsubscribe(sub)
```

The subscription is created **before** the backfill query. This is critical: if we queried first and subscribed second, events arriving between the query and the subscribe would be lost. Subscribing first means those events land in the channel buffer and are delivered after the backfill. The `clientKey` (user ID, else client IP) feeds the per-client connection cap. Headers are flushed immediately after subscribing so the browser's `EventSource` fires `onopen` without waiting for the first event.

### 3. Backfill

The backfill logic handles two cases:

**With `Last-Event-ID`** (client reconnecting): Queries `List*Since(sinceID, limit=100)` to fetch events after the last one the client received. Results are already in chronological order (ASC).

**Without `Last-Event-ID`** (fresh connection): Queries the most recent 100 matching events, then sends them oldest-first by iterating in reverse.

The backfill returns the highest event ID sent, which is used for duplicate suppression.

### 4. Event loop

```go
heartbeat := time.NewTicker(15 * time.Second)
defer heartbeat.Stop()

for {
    select {
    case msg, ok := <-sub.Chan():
        if !ok { return }                           // channel closed (broker shutdown)
        if msg.ID <= lastBackfilledID { continue }  // duplicate suppression
        sink.setWriteDeadline(time.Now().Add(30 * time.Second))
        sink.writeEvent(msg.ID, label, msg.Data)
        sink.flush()
    case <-heartbeat.C:
        sink.setWriteDeadline(time.Now().Add(30 * time.Second))
        sink.writeHeartbeat()
        sink.flush()
    case <-ctx.Done():
        return                                      // client disconnected
    }
}
```

Every write (event or heartbeat) sets a 30-second per-write deadline via `http.ResponseController`, so a dead TCP peer cannot wedge the handler goroutine.

### 5. SSE frame format

```
id: 12345
event: srvlog
data: {"id":12345,"hostname":"router-1",...}

```

Each frame includes the event ID (for `Last-Event-ID` reconnection), the event type (`srvlog`, `netlog`, or `applog`), and the JSON payload.

### 6. Heartbeat

An `event: heartbeat` frame is sent every 15 seconds. This serves two purposes: it keeps the connection alive through proxies and load balancers, and it detects dead connections early (the write fails if the client has disconnected). The frontend's watchdog treats 35 seconds without a heartbeat as a dead stream and reconnects.

### 7. Duplicate suppression

Events that arrive on the broker channel with `ID <= lastBackfilledID` are skipped. This handles the overlap window between the backfill query and the subscription becoming active.

---

## Cursor-Based Pagination

Taillight uses keyset (cursor) pagination instead of OFFSET-based pagination. The cursor types live in `internal/model/srvlog.go`; the queries in the per-feed store files (`internal/postgres/srvlog_store.go`, `netlog_store.go`, `applog_store.go`).

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

The query requests `limit + 1` rows. If more rows are returned than the limit, a next cursor is constructed from the last *included* row (the peek row is discarded):

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

The auth system (`internal/auth/`) supports two authentication methods: session cookies and API keys, with an optional LDAP verifier in front of local passwords. Both token types store only hashes in the database.

### Password hashing

Passwords are hashed with bcrypt at cost 12:

```go
const bcryptCost = 12

func HashPassword(password string) (string, error) {
    hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
    return string(hash), nil
}
```

To prevent timing-based username enumeration, a `DummyCheckPassword` function compares against a pre-computed dummy hash when the user is not found. This ensures the response time is the same whether the user exists or not. The login endpoint is additionally rate-limited in the application: 5 attempts/minute per client IP (burst 10), with idle per-IP limiters evicted after 15 minutes.

### Session tokens

1. Generate 32 random bytes.
2. Encode as base64url -- this is the raw token sent to the client as a cookie (`tl_session`).
3. Compute SHA-256 hex digest of the raw token -- this hash is stored in the `sessions` table.

Sessions live 30 days (`sessionDuration`). On login, the user's sessions are pruned to the 10 most recent (`maxSessionsPerUser`); a background job cleans expired sessions every 15 minutes.

On authentication, the middleware extracts the cookie, hashes it, and looks up the session by hash. `last_seen_at` is updated through the `AuthStore` touch worker — a buffered channel (256 ops) drained by a dedicated goroutine — so the request path never blocks on the bookkeeping UPDATE. The worker is drained on shutdown (`authStore.Stop()`), after the HTTP server has finished in-flight requests.

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

API keys have scopes (`read`, `ingest`, `admin`), optional expiration, and can be revoked. `last_used_at` is touched asynchronously via the same touch worker.

### LDAP authentication (optional)

When `ldap.enabled` is set, `internal/ldap` verifies logins against a directory (Active Directory or FreeIPA) before falling back to local bcrypt:

1. Bind with the service account (`bind_dn`/`bind_password`; over LDAPS or StartTLS, optionally trusting an internal CA via `ca_bundle`).
2. Search for the user under `user_search_base` with `user_filter` (the `%s` placeholder receives the escaped username).
3. Bind as the found user with the supplied password.
4. Map the user's groups through `group_role_map` (full DN or bare CN): `admin` grants is_admin, any other value authorizes a regular user, and a user in **no** mapped group is denied.
5. Upsert the user locally (`UpsertLDAPUser`, `auth_source='ldap'`) so sessions and API keys work identically for LDAP and local users.

### OIDC single sign-on (optional)

When `oidc.enabled` is set, `internal/oidc` runs an Authorization Code + PKCE flow against the configured provider (`GET /auth/oidc/login` → IdP → `GET /auth/oidc/callback`):

1. Discovery is lazy: provider metadata and JWKS are fetched from `issuer_url` on the first login attempt (a 10s-timeout HTTP client bounds every call), so an unreachable IdP at boot never keeps taillight down.
2. `BeginLogin` generates state, nonce, and a PKCE S256 verifier; the handler seals them (plus the post-login redirect path) into an HMAC-signed, HttpOnly, 10-minute cookie scoped to the OIDC endpoints, then redirects to the provider's authorization endpoint.
3. The callback verifies and clears the state cookie, checks the echoed `state`, and exchanges the code (with the PKCE verifier) at the token endpoint.
4. The ID token is validated — JWKS signature, issuer, audience, expiry, nonce — and its claims mapped to a normalized identity (`username_claim`/`email_claim`/`groups_claim`).
5. Gating runs in `internal/oidc`: verified email (unless `email_verified_required=false`), `allowed_domains`/`allowed_users` (OR'd), `allowed_groups` membership, and `admin_groups` → is_admin.
6. Upsert keyed on the `(issuer, subject)` claims (`UpsertOIDCUser`, `auth_source='oidc'`, empty password): first login provisions (username collisions get a numeric suffix — an IdP-controlled claim never links to an existing account), later logins refresh email and is_admin. `is_active` stays under local admin control.
7. The shared session tail (`establishSession`) issues an ordinary `tl_session` cookie; downstream middleware cannot tell OIDC sessions from password sessions. Failures redirect to `/login?error=<sso_failed|sso_denied|sso_forbidden|sso_expired>` with details only in the server log.

Password login and password change are refused for externally managed (LDAP/OIDC) users.

### Middleware chain

The `SessionOrAPIKey` middleware tries authentication in order:

1. **Session cookie**: Extract `tl_session` cookie -> SHA-256 hash -> look up in `sessions` table (joined with `users`). If valid and the user is active, store user in context.

2. **Bearer API key**: Extract `Authorization: Bearer tl_...` header -> SHA-256 hash -> look up in `api_keys` table (joined with `users`). If valid, not revoked, not expired, and user is active, store user, scopes, and key ID in context (the key ID is what the ingest handler stamps onto `applog_events.api_key_id`).

3. If neither succeeds, return 401 Unauthorized.

### Scope enforcement

`RequireScope(scope)` delegates to `HasGrant`:

```go
func HasGrant(scopes []string, user *model.User, scope string) bool {
    if scopes == nil {              // session auth
        if scope == ScopeAdmin {
            return user != nil && user.IsAdmin
        }
        return user != nil          // sessions hold every non-admin scope
    }
    return hasScope(scopes, scope)  // API keys: listed scope, or "admin" implies all
}
```

Session auth is deliberately *not* a blanket grant: admin routes require the user's `is_admin` flag even with a valid session. Routes are grouped by scope in `serve.go`: `read` for GET endpoints, `ingest` for the POST ingest endpoint, and `admin` for write operations.

---

## Data Access Layer

The store (`internal/postgres/`) uses pgx with the squirrel query builder. There is exactly **one** stateless `Store` type (plus `AuthStore`, which earns its own type by owning the touch-worker goroutine — the standing rule is that a store domain gets its own type when it acquires state or lifecycle, not before; see `docs/adr/0003-store-stays-one-type.md`).

### File layout — one file per consumer-interface cluster

The implementation files align with the consumer interfaces they back:

| File | Backs |
|---|---|
| `store.go` | `Store` type, `Ping`, retention policies, cagg refresh, shared helpers (`psq`, `escapeLike`, `getVolume`, `getSyslogSummary`) |
| `srvlog_store.go` / `netlog_store.go` | Feed queries: get/list/since, meta lists, volume, summary, device summary |
| `applog_store.go` | Ingest batch insert + applog queries |
| `host_store.go` | Hosts overview (`ListHosts`, 5 queries in one batch) |
| `analysis_store.go` | Analyzer aggregations (top msgids, baselines, clusters, samples, ...) |
| `analysis_report_store.go` | Report lifecycle (pending/running/completed/failed, slug lookup, notified CAS, orphan reconcile) |
| `analysis_schedule_store.go` | Analysis schedule CRUD |
| `auth_store.go` | `AuthStore`: users, sessions, API keys + async touch worker |
| `notification_store.go` | Channels, rules, notification log |
| `summary_store.go` | Summary schedules + `GetTopIssues` |
| `juniper_ref_store.go` | Juniper reference lookup/upsert |
| `rsyslog_stats_store.go` / `taillight_metrics_store.go` | Telemetry tables |
| `syslogfilter.go` | Shared srvlog/netlog WHERE-clause construction |
| `listener.go` | LISTEN/NOTIFY (see above) |
| `querytracer.go` | pgx tracer feeding `taillight_db_query_duration_seconds` / `_errors_total` |

### Query building

A global `psq` builder configured for PostgreSQL dollar-sign placeholders:

```go
var psq = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)
```

Srvlog and netlog have field-identical filters (distinct types), so the WHERE-clause construction — the bug-prone part: escaping, wildcard ILIKE, the `::inet` cast, severity ranges — lives once in `syslogFilterClause` / `applySyslogFilter` (`syslogfilter.go`); the two per-feed adapters are mechanical field maps:

```go
func applySyslogFilter(qb sq.SelectBuilder, f syslogFilterClause) sq.SelectBuilder {
    if f.Hostname != "" {
        if strings.Contains(f.Hostname, "*") {
            pattern := strings.ReplaceAll(escapeLike(f.Hostname), "*", "%")
            qb = qb.Where("hostname ILIKE ?", pattern)
        } else {
            qb = qb.Where(sq.Eq{"hostname": f.Hostname})
        }
    }
    // fromhost_ip, programname, severity, severity_max, facility, tag, msgid,
    // search, time range ...
    return qb
}
```

The same shape recurs for genuinely-identical per-feed SQL: `getVolume(ctx, table, groupCol, ...)` and `getSyslogSummary(ctx, aggTable, ...)` are private helpers with thin per-feed wrappers.

### LIKE escaping

The `escapeLike` helper (`store.go`) escapes `%`, `_`, and `\` so they are treated as literal characters in LIKE/ILIKE clauses:

```go
func escapeLike(s string) string {
    s = strings.ReplaceAll(s, `\`, `\\`)
    s = strings.ReplaceAll(s, `%`, `\%`)
    s = strings.ReplaceAll(s, `_`, `\_`)
    return s
}
```

This prevents filter parameters from smuggling LIKE metacharacters into pattern matches.

### Full-text search

Syslog search uses case-insensitive `ILIKE` with `%` wrapping (served by the trigram GIN index):

```go
qb = qb.Where("message ILIKE ?", "%"+escaped+"%")
```

Applog search uses PostgreSQL's `tsvector`/`tsquery` for indexed full-text search:

```go
qb = qb.Where("search_vector @@ plainto_tsquery('simple', ?)", f.Search)
```

The `search_vector` column is a stored generated column. The `'simple'` dictionary avoids stemming, which is appropriate for log messages.

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

Each INSERT uses `RETURNING` so the caller gets back the populated `id` and `received_at` without additional queries.

### Meta caching

Distinct values for filter dropdowns (hostnames, programs, services, etc.) come from trigger-maintained cache tables (`srvlog_meta_cache`, `netlog_meta_cache`, `applog_meta_cache`) rather than live `SELECT DISTINCT` queries over hypertables. Only whitelisted columns are allowed:

```go
var allowedMetaColumns = map[string]struct{}{
    "hostname": {}, "programname": {}, "syslogtag": {},
}
```

### Batch queries for summaries

Device summary endpoints send 4 queries in one `SendBatch` round-trip (last-seen, severity/level breakdown, top normalized messages via `msg_pattern`, recent critical logs). The hosts overview (`host_store.go`) sends 5 (severity counts, previous-period totals, hourly sparkline, top error patterns, last-seen).

---

## Metrics Collection

Taillight exposes Prometheus metrics (`internal/metrics/`), all under the `taillight_` namespace, covering HTTP, SSE, database, listener, analysis, and notification subsystems.

### HTTP metrics

The `HTTPMetrics` middleware (`internal/metrics/middleware.go`) wraps each request and records:

- `taillight_http_requests_total{method, path, status}` -- counter
- `taillight_http_request_duration_seconds{method, path}` -- histogram

The `path` label uses the chi route pattern (e.g., `/api/v1/srvlog/{id}`) rather than the actual URL, keeping metric cardinality bounded.

### SSE metrics (per feed)

| Metric | Type | Description |
|--------|------|-------------|
| `sse_clients_active` / `netlog_sse_clients_active` / `applog_sse_clients_active` | gauge | Current SSE connections |
| `events_broadcast_total` / `netlog_…` / `applog_…` | counter | Events broadcast |
| `events_dropped_total` / `netlog_…` / `applog_…` | counter | Events dropped (slow clients) |

### Listener metrics

- `listener_reconnects_total` -- reconnection attempts
- `listener_gap_fill_events_total{channel}` / `listener_gap_fill_duration_seconds{channel}` -- gap-fill recovery
- `listener_gap_fill_truncated_total{channel}` -- gap fill hit the 10,000-row cap (events may be missing)
- `listener_payload_parse_errors_total{channel}` -- malformed NOTIFY payloads
- `notifications_received_total{channel}` -- notifications consumed by the bridge workers

### Database metrics

- `db_pool_active_conns` / `db_pool_idle_conns` / `db_pool_total_conns` -- pgxpool gauges
- `db_query_duration_seconds{operation}` / `db_query_errors_total{operation}` -- from the pgx `QueryTracer`

### Ingest metrics

- `applog_ingest_total` -- log entries ingested
- `applog_ingest_batches_total` -- ingest POST requests
- `applog_ingest_errors_total` -- failed ingest requests

### Analysis metrics

- `analysis_runs_total{status}` -- completed/failed runs
- `analysis_duration_seconds` -- run latency
- `analysis_structure_retries_total` -- reports that needed the corrective retry

### Notification metrics

- `notification_rules_evaluated_total` / `notification_rules_matched_total`
- `notification_dispatched_total` / `notification_dispatch_queue_length`
- `notification_sent_total{channel_type, status}` / `notification_send_duration_seconds`
- `notification_send_attempts_total` -- individual attempts including retries
- `notification_suppressed_total{reason}` -- silence window / rate limit / breaker
- `notification_fingerprints_dropped_total` -- suppressor cap hit (per-rule 10k / global 50k)
- `notification_breaker_state{channel_id, channel_name}` / `notification_breaker_transitions_total{channel_id, channel_name, to}`

### Snapshot API

The `metrics.Snapshot()` function (`internal/metrics/collector.go`) reads current values from Prometheus gauges and counters and returns a `model.MetricsSnapshot` struct. A background job in `serve.go` persists a snapshot into the `taillight_metrics` hypertable every 30 seconds; the `/api/v1/metrics/*` endpoints chart that history without requiring Prometheus.

---

## Analysis Worker & Scheduler

The analysis subsystem (optional; wired in `serve.go:setupAnalysis` only when `analysis.enabled`) is split into analyzer, worker, and scheduler so each piece is independently testable.

### Analyzer (`internal/analyzer`)

One run = one report: gather Postgres aggregates for the window (never raw log lines), render the mode's prompt templates, call Ollama, validate the response structure (exact H2 section set per mode; one corrective retry, keep whichever reply validates), prepend a header. Empty windows short-circuit to a deterministic stub with 0/0 tokens — the LLM is never asked to narrate the absence of data. Prompts are hot-reloadable via `analysis.prompts_dir`. Full pipeline documentation: `internal/analyzer/README.md`.

### Worker (`internal/worker/analysis.go`)

A single goroutine drains a queue of report IDs (`QueueDepth = 5`):

- `Enqueue` inserts a `pending` row first, then queues the ID; if the queue is full the row is deleted (using `context.WithoutCancel` so a client disconnect can't orphan it) and `ErrQueueFull` bubbles up as HTTP 429.
- `process` marks the row `running`, runs the analyzer under `analysis.run_timeout`, and marks `completed` or `failed`. Failure messages are sanitized before persistence (backend URLs and upstream bodies collapse to coarse messages; the full error is only logged).
- A partial unique index on `(feed, period_end, prompt_mode, hosts) WHERE status IN ('pending','running')` means a concurrent duplicate surfaces as `ErrDuplicateActiveReport` → HTTP 409.
- On completion, the worker runs the `notified_at` CAS (`MarkReportNotified` returns won exactly once per row) and only the winner invokes the completion callback, which resolves the report's snapshotted `notify_channel_ids` live and mails the rendered report through the notification engine.
- At boot, `ReconcileOrphanedReports` fails any `pending`/`running` rows left behind by a crash before the worker accepts new work.

### Scheduler (`internal/scheduler`)

`AnalysisScheduler` and `SummaryScheduler` both tick every 60 seconds. The analysis scheduler fires a schedule when the wall clock is within `[scheduled, scheduled+5m]` in the schedule's timezone, the frequency's day check passes, and `last_run_at` is older than half the period (double-fire guard). A failed enqueue leaves `last_run_at` unstamped so the next tick retries while still inside the firing window; `scheduledPeriodEnd` truncates to the scheduled minute so retries land on the same period (and therefore the same slug and duplicate-active key). Both schedulers take an injectable `now func() time.Time` clock, which is what the table-driven scheduler tests use to walk fake time.

---

## Log Shipper

The log shipper (`pkg/logshipper/` at the repo root — its own Go module, `github.com/lasseh/taillight/pkg/logshipper`) is a `slog.Handler` that sends application logs to a taillight applog ingest endpoint. The server itself uses it for self-monitoring (`logshipper.enabled`), and `taillight-wish` uses it from the companion repo.

### How it works

```go
shipper, err := logshipper.New(logshipper.Config{
    Endpoint:  "http://localhost:8080/api/v1/applog/ingest",
    APIKey:    logshipper.Secret("tl_..."),
    Service:   "taillight",
    MinLevel:  slog.LevelInfo,
    BatchSize: 100,
})
```

The handler implements the `slog.Handler` interface. When `Handle` is called, it converts the `slog.Record` into a `logEntry` struct and pushes it to a buffered channel (default capacity 1024). `Config.APIKey` is a `Secret` — a string type whose `String`/`GoString`/`MarshalJSON` all return `[REDACTED]`, so the key can't leak through logging or dumps.

### Batching

A background goroutine consumes from the channel and batches entries:

- **Size-triggered flush**: When the batch reaches `BatchSize` (default 100), it is sent immediately.
- **Time-triggered flush**: A ticker fires every `FlushPeriod` (default 1 second) and flushes whatever has accumulated.
- **Shutdown flush**: On `Shutdown()`, the channel is drained and a final flush is performed. In `serve.go` this happens while the HTTP server is still accepting — the shipper POSTs to the local ingest route, so both the server and the auth touch worker must still be alive at that point in the shutdown order.

### Backpressure

The channel send is non-blocking:

```go
select {
case h.ch <- entry:
default:
    h.dropped.Add(1)   // drop on full
}
```

If the ingest endpoint is down or slow, the buffer fills and new entries are dropped. The `Dropped()` counter tracks how many entries were lost. A failed send is retained for exactly one retry on the next flush tick; a second consecutive failure drops the whole batch (counted by `SendFailed()`) so memory stays bounded.

### Level mapping

The shipper maps `slog.Level` values to taillight's five canonical levels:

| slog.Level | Taillight Level |
|------------|-----------------|
| < LevelInfo | DEBUG |
| LevelInfo | INFO |
| LevelWarn | WARN |
| LevelError | ERROR |
| >= logshipper.LevelFatal (12) | FATAL |

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

## Terminal UI & SSH server

The terminal UI (`taillight-tui`) and the SSH server that hosts it
(`taillight-wish`), formerly implemented here as `internal/tui` and
`cmd/taillight-wish`, now live in a separate repository:
https://github.com/lasseh/taillight-tui.

---

## Key Design Decisions

Standing decisions are recorded as ADRs under `docs/adr/` (repo root): 0001 listener stays Postgres-bound, 0002 feed flags removed, 0003 one Store type, 0004 contract goldens, 0005 tagged releases.

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

`WaitForNotification` blocks until a notification arrives. If this ran on a pooled connection, that connection would be held indefinitely, reducing the pool's effective size. With a dedicated connection, the pool remains fully available for query workload. The tradeoff is managing reconnection separately, but this is handled by the `Listener`'s reconnect loop with exponential backoff and per-channel gap fill.

### Why per-client filtering in the broker

Instead of creating separate database subscriptions per filter, all events are broadcast through a single broker per feed and filters are applied in memory. This means:

- Only one LISTEN connection for both syslog feeds regardless of how many clients are connected.
- Only one database fetch per event (by ID) regardless of client count.
- Filter evaluation is cheap (field comparisons) compared to the cost of additional database queries.
- Adding or removing clients doesn't change the database load.

### Why fire-first notifications

Taillight deliberately has no incident model — no resolved/cleared state, no pending-alert delay. The first matching event alerts immediately (a NOC tool must not sit on the first BGP flap); suppression exists only to stop repeat spam, which is what the fingerprint silence-window + digest model does. This is a binding product decision.

### Why aggregates before the LLM

The analysis pipeline never streams raw log lines into a prompt. Postgres — which is domain-aware — collapses the window into ranked aggregates with exact counts, and the model only narrates them. Prompts stay ~4–6K tokens, numbers stay exact, and hallucination surface shrinks. See `internal/analyzer/README.md`.

### Why TimescaleDB

The `srvlog_events`, `netlog_events`, and `applog_events` tables are TimescaleDB hypertables, which provide:

- **Time-based partitioning**: Data is automatically chunked by time interval. Old chunks can be dropped efficiently for retention.
- **Columnstore compression**: Older chunks are compressed, reducing storage by 90%+ for log data.
- **Retention policies**: `add_retention_policy` automatically drops data older than the configured threshold.
- **time_bucket + continuous aggregates**: The volume and summary queries use `time_bucket()` and hourly caggs for efficient time-series aggregation.
- **Standard PostgreSQL**: All existing pgx queries, indexes, and extensions work unchanged.
