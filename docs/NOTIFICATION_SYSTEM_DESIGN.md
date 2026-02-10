# Notification System Design — Taillight

**Date:** 2026-02-09
**Status:** Research complete, ready for implementation
**Contributors:** Go Systems Architect, Notification Systems Expert, SRE/Alerting Expert

---

## 1. Overview

Taillight needs a pluggable notification system that alerts operators via Slack, webhooks, and other channels when log events match configurable rules. The system must be **rock-solid** — no silent failures, no spam during log storms, and no missed critical alerts.

### Design Principles

1. **Zero new dependencies for backends** — All notification backends use stdlib `net/http` and `encoding/json`
2. **Reuse existing filter logic** — Rules convert to `model.SyslogFilter` / `model.AppLogFilter` and call the battle-tested `Matches()` methods
3. **Async delivery** — Rule evaluation is synchronous (microseconds), delivery is async via dispatch queue
4. **Fail-open for observability** — Every notification attempt (sent, suppressed, failed) is logged to a queryable audit trail
5. **Anti-spam by default** — Cooldowns, rate limiting, and circuit breakers are mandatory, not optional

### Event Flow

```
                                    ┌─────────────────┐
PostgreSQL ──NOTIFY──► Listener ──► │ Background       │──► SyslogBroker ──► SSE clients
                                    │ Worker           │
                                    │                  │──► NotifEngine.HandleSyslogEvent()
                                    └─────────────────┘           │
                                                                  ▼
HTTP POST ──► AppLogIngestHandler ──► ApplogBroker ──► SSE       Rule evaluation (sync, µs)
                                  │                               │
                                  └──► NotifEngine.HandleAppLogEvent()
                                                                  │
                                                           ┌──────▼──────┐
                                                           │ Burst       │ ← collect events
                                                           │ window      │   for 30s
                                                           └──────┬──────┘
                                                                  │ (window closes)
                                                           ┌──────▼──────┐
                                                           │ Cooldown    │ ← suppress if
                                                           │ check       │   recently sent
                                                           └──────┬──────┘
                                                                  │ (pass)
                                                           ┌──────▼──────┐
                                                           │ Dispatch    │ ← buffered channel
                                                           │ queue       │
                                                           └──────┬──────┘
                                                                  │
                                                        ┌─────────▼─────────┐
                                                        │ Worker goroutines │
                                                        │ (N=4 default)     │
                                                        └────┬────┬────┬────┘
                                                             │    │    │
                                                   ┌─────┐  │  ┌─▼──┐ │  ┌───────┐
                                                   │Rate │◄─┘  │Circ│ └─►│Retry  │
                                                   │Limit│     │Brk │    │(3 max)│
                                                   └──┬──┘     └─┬──┘    └───┬───┘
                                                      │          │           │
                                                      ▼          ▼           ▼
                                                   Slack    Webhook      Email
                                                      │          │           │
                                                      └──────────┼───────────┘
                                                                 ▼
                                                        notification_log
```

---

## 2. Package Structure

```
api/internal/notification/
    notifier.go        — Notifier interface, Payload, SendResult types
    engine.go          — Engine: rule eval, dispatch queue, lifecycle
    rule.go            — Rule, Channel domain types, filter converters
    cooldown.go        — In-memory per-rule cooldown tracker
    burstwatcher.go    — Per-rule burst window collector
    ratelimit.go       — Per-key token bucket rate limiter
    backend/
        slack.go       — Slack Incoming Webhook (Block Kit)
        discord.go     — Discord Webhook (embeds)
        teams.go       — Microsoft Teams Workflow Webhook (Adaptive Cards)
        pagerduty.go   — PagerDuty Events API v2 (trigger/resolve)
        webhook.go     — Generic configurable webhook (Go templates)
        email.go       — SMTP email (stdlib net/smtp)

api/internal/postgres/
    notification_store.go  — CRUD for channels, rules, notification_log

api/internal/handler/
    notification.go        — REST API for channel/rule management

api/migrations/
    000003_notifications.up.sql
    000003_notifications.down.sql
```

---

## 3. Core Interfaces

### Notifier

```go
// Notifier is the interface every notification backend must implement.
type Notifier interface {
    // Type returns the backend type identifier (e.g., "slack", "webhook").
    Type() string

    // Send delivers a notification to the given channel.
    // Must be safe for concurrent use.
    Send(ctx context.Context, channel Channel, payload Payload) SendResult

    // Validate checks that a channel's config is valid for this backend.
    // Called at channel creation/update time (fail fast).
    Validate(channel Channel) error
}
```

### Payload

```go
type EventKind string

const (
    EventKindSyslog EventKind = "syslog"
    EventKindAppLog EventKind = "applog"
)

type Payload struct {
    Kind        EventKind
    RuleName    string
    Timestamp   time.Time
    EventCount  int                 // number of events in this burst (≥1)
    SyslogEvent *model.SyslogEvent  // the triggering event (first in burst)
    AppLogEvent *model.AppLogEvent  // the triggering event (first in burst)
    BaseURL     string              // taillight UI base URL for deep links
}
```

### SendResult

```go
type SendResult struct {
    Success    bool
    StatusCode int
    Error      error
    Duration   time.Duration
    RetryAfter time.Duration // from HTTP 429 Retry-After header
}
```

### Channel

```go
type ChannelType string

const (
    ChannelTypeSlack     ChannelType = "slack"
    ChannelTypeDiscord   ChannelType = "discord"
    ChannelTypeTeams     ChannelType = "teams"
    ChannelTypePagerDuty ChannelType = "pagerduty"
    ChannelTypeWebhook   ChannelType = "webhook"
    ChannelTypeEmail     ChannelType = "email"
)

type Channel struct {
    ID        int64           `json:"id"`
    Name      string          `json:"name"`
    Type      ChannelType     `json:"type"`
    Config    json.RawMessage `json:"config"`   // backend-specific JSON blob
    Enabled   bool            `json:"enabled"`
    CreatedAt time.Time       `json:"created_at"`
    UpdatedAt time.Time       `json:"updated_at"`
}
```

Backend-specific config examples:

```json
// Slack
{"webhook_url": "https://hooks.slack.com/services/T.../B.../xxx"}

// Discord
{"webhook_url": "https://discord.com/api/webhooks/123/abc"}

// Teams
{"webhook_url": "https://...workflow.microsoft.com/..."}

// PagerDuty
{"routing_key": "YOUR_INTEGRATION_KEY", "severity_map": {"0":"critical","3":"error","4":"warning"}}

// Generic Webhook
{"url": "https://example.com/alert", "method": "POST", "headers": {"X-API-Key": "secret"}, "template": "..."}

// Email
{"smtp_host": "smtp.example.com", "smtp_port": 587, "username": "...", "password": "...", "from": "alerts@example.com", "to": ["ops@example.com"]}
```

### Rule

```go
type Rule struct {
    ID        int64     `json:"id"`
    Name      string    `json:"name"`
    Enabled   bool      `json:"enabled"`
    EventKind EventKind `json:"event_kind"`

    // Syslog filter fields (same semantics as model.SyslogFilter).
    Hostname    string `json:"hostname,omitempty"`
    Programname string `json:"programname,omitempty"`
    Severity    *int   `json:"severity,omitempty"`      // exact match or ≤ threshold
    SeverityMax *int   `json:"severity_max,omitempty"`   // upper bound for range
    Facility    *int   `json:"facility,omitempty"`
    SyslogTag   string `json:"syslogtag,omitempty"`
    MsgID       string `json:"msgid,omitempty"`

    // AppLog filter fields (same semantics as model.AppLogFilter).
    Service   string `json:"service,omitempty"`
    Component string `json:"component,omitempty"`
    Host      string `json:"host,omitempty"`
    Level     string `json:"level,omitempty"`

    // Shared.
    Search string `json:"search,omitempty"`

    // Notification behavior.
    ChannelIDs      []int64 `json:"channel_ids"`
    BurstWindow     int     `json:"burst_window"`     // seconds to collect events before sending (default 30)
    CooldownSeconds int     `json:"cooldown_seconds"` // seconds to suppress after sending (default 300)
}

// SyslogFilter converts the rule's filter fields to a model.SyslogFilter
// for reuse of the existing Matches() logic.
func (r Rule) SyslogFilter() model.SyslogFilter { ... }

// AppLogFilter converts the rule's filter fields to a model.AppLogFilter.
func (r Rule) AppLogFilter() model.AppLogFilter { ... }
```

### Engine

```go
type Engine struct {
    store      Store
    backends   map[ChannelType]Notifier
    channels   []Channel            // in-memory cache, refreshed periodically
    rules      []Rule               // in-memory cache, refreshed periodically
    bursts     *BurstWatcher        // per-rule burst window collector
    cooldowns  *CooldownTracker     // per-rule post-send cooldown
    rateLimits *PerKeyLimiter
    breakers   *BreakerRegistry
    dispatchCh chan dispatchJob
    logger     *slog.Logger
    cfg        NotificationConfig
}

func NewEngine(store Store, cfg NotificationConfig, logger *slog.Logger) *Engine
func (e *Engine) RegisterBackend(t ChannelType, n Notifier)
func (e *Engine) HandleSyslogEvent(event model.SyslogEvent)
func (e *Engine) HandleAppLogEvent(event model.AppLogEvent)
func (e *Engine) Start(ctx context.Context)
func (e *Engine) Shutdown(ctx context.Context) error
```

---

## 4. Database Schema

```sql
-- Notification channels (configured backends)
CREATE TABLE notification_channels (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT UNIQUE NOT NULL,
    type       TEXT NOT NULL CHECK (type IN ('slack','discord','teams','pagerduty','webhook','email')),
    config     JSONB NOT NULL DEFAULT '{}',
    enabled    BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Notification rules (alert conditions)
CREATE TABLE notification_rules (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name             TEXT UNIQUE NOT NULL,
    enabled          BOOLEAN NOT NULL DEFAULT true,
    event_kind       TEXT NOT NULL CHECK (event_kind IN ('syslog', 'applog')),
    -- Syslog filter fields (nullable = don't filter on this field)
    hostname         TEXT,
    programname      TEXT,
    severity         SMALLINT CHECK (severity BETWEEN 0 AND 7),
    severity_max     SMALLINT CHECK (severity_max BETWEEN 0 AND 7),
    facility         SMALLINT CHECK (facility BETWEEN 0 AND 23),
    syslogtag        TEXT,
    msgid            TEXT,
    -- AppLog filter fields
    service          TEXT,
    component        TEXT,
    host             TEXT,
    level            TEXT CHECK (level IN ('DEBUG','INFO','WARN','ERROR','FATAL')),
    -- Shared
    search           TEXT,
    -- Behavior
    burst_window     INTEGER NOT NULL DEFAULT 30,   -- seconds to collect events before sending
    cooldown_seconds INTEGER NOT NULL DEFAULT 300,  -- seconds to suppress after sending
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Many-to-many: rules → channels
CREATE TABLE notification_rule_channels (
    rule_id    BIGINT REFERENCES notification_rules(id) ON DELETE CASCADE,
    channel_id BIGINT REFERENCES notification_channels(id) ON DELETE CASCADE,
    PRIMARY KEY (rule_id, channel_id)
);

-- Audit trail (TimescaleDB hypertable)
CREATE TABLE notification_log (
    id          BIGINT GENERATED ALWAYS AS IDENTITY,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    rule_id     BIGINT NOT NULL,
    channel_id  BIGINT NOT NULL,
    event_kind  TEXT NOT NULL,
    event_id    BIGINT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('sent','suppressed','failed')),
    reason      TEXT,                -- suppression reason or error message
    event_count INT NOT NULL DEFAULT 1,  -- number of events in burst
    status_code INTEGER,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    payload     JSONB                -- the notification payload sent (null for suppressed)
);

SELECT create_hypertable('notification_log', 'created_at', chunk_time_interval => INTERVAL '7 days');
SELECT add_retention_policy('notification_log', INTERVAL '30 days');

CREATE INDEX idx_notification_log_rule ON notification_log (rule_id, created_at DESC);
CREATE INDEX idx_notification_log_status ON notification_log (status, created_at DESC);
```

---

## 5. Anti-Spam Guardrails

This is the most critical aspect of the system. Four layers of protection, each independent.

Syslog and applog events are **discrete, fire-and-forget** — they never get acknowledged or resolved. Unlike Prometheus alerts that have a lifecycle (firing → resolved), log events simply happen. The anti-spam model is designed around this reality: collect bursts, send one notification per burst, then cool down.

### Layer 1: Burst Window

When a rule matches its first event, a **burst window** opens (default 30 seconds). During this window, all subsequent matching events for the same rule are collected silently. When the window closes, a single notification is sent summarizing the burst:

- **1 event in burst:** "rpd on router1: BGP peer 10.0.0.1 connection lost" (show the full event)
- **N events in burst:** "12 events matched rule 'core-rpd-errors' in the last 30s" (show the first event + count)

| Parameter | Default | Purpose |
|-----------|---------|---------|
| `burst_window` | 30s | Time to collect events before sending one notification |

**Implementation:** `BurstWatcher` — `map[int64]*burst` keyed by rule ID. Each `burst` holds the first event (for display), a count, and a timer. On timer fire, flush to dispatch queue with `EventCount` set.

```
Event arrives → Rule matches → BurstWatcher.Add(ruleID, payload)
                                     │
                    ┌────────────────┼────────────────────┐
                    │ No active      │ Active burst?       │
                    │ burst?         │                     │
                    │ Start burst,   │ Increment count,    │
                    │ store first    │ keep first event    │
                    │ event, start   │                     │
                    │ timer          │                     │
                    └────────┬───────┴──────────┬──────────┘
                             │                   │
                         Timer fires ────────────┘
                             │
                    Send notification with EventCount
                             │
                    Enter cooldown for this rule
```

### Layer 2: Per-Rule Cooldown

After a burst notification is sent, the rule enters **cooldown** for `cooldown_seconds` (default 5 min). During cooldown:
- New matching events are counted but **not dispatched**
- Logged to `notification_log` with `status = 'suppressed'`
- When cooldown expires:
  - If events arrived during cooldown → send a summary: "147 more events suppressed during cooldown"
  - If no events arrived → reset, ready for the next burst

This means the worst-case notification rate per rule is: one at burst window close, then one every `cooldown_seconds` thereafter (summary only).

**Implementation:** `CooldownTracker` — `map[int64]cooldownState` keyed by rule ID. Each state tracks last-fire time and a suppressed event count. Background goroutine checks for expired cooldowns every 10 seconds and flushes summaries.

```
Burst fires → Notification sent → Cooldown starts (5 min)
                                        │
                              Events arrive during cooldown:
                              count++ (not dispatched)
                                        │
                              Cooldown expires ──┬── count > 0 → send summary
                                                 └── count = 0 → reset (idle)
```

### Layer 3: Per-Channel Rate Limiting

Token bucket rate limiter per destination URL, using `golang.org/x/time/rate`:

| Backend | Rate | Burst | Rationale |
|---------|------|-------|-----------|
| Slack | 1/sec | 3 | Slack enforces ~1 msg/sec per channel |
| Discord | 2/sec | 5 | Discord allows 5 req/2s per webhook |
| Teams | 1/sec | 3 | Conservative (poorly documented) |
| PagerDuty | 10/sec | 20 | PagerDuty allows 7500/min |
| Webhook | 5/sec | 10 | Configurable per channel |
| Email | 0.1/sec | 2 | 1 email per 10 seconds |

**Implementation:** `PerKeyLimiter` — `map[string]*rate.Limiter` keyed by channel ID. If `limiter.Allow()` returns false, the notification is queued for later (not dropped).

### Layer 4: Global Rate Limit (Safety Net)

A single `rate.Limiter` shared across all dispatch workers:
- Default: 100 notifications per hour (configurable)
- Protects against misconfigured rules that match too broadly
- When hit, logs a warning: `"global rate limit reached, notifications delayed"`

### Layer 5: Circuit Breaker per Destination

One circuit breaker per channel (webhook URL), using `sony/gobreaker/v2`:

| State | Behavior |
|-------|----------|
| **Closed** | Normal operation. Track success/failure counts. |
| **Open** | All sends fail immediately. Transitions to half-open after 60s. |
| **Half-Open** | Allow 2 probe requests. If they succeed → close. If they fail → reopen. |

**Trigger:** Opens after 5 consecutive failures.

**Fallback when open:** Notifications are logged with `status = 'failed'` and `reason = 'circuit breaker open'`. They are NOT retried automatically — the operator should fix the channel config. A Prometheus metric `notification_circuit_breaker_state` exposes the current state.

### Layer Summary

```
Event → Rule match → Burst window (collect 30s) → Cooldown check → Dispatch queue
                                                                         │
    Dispatch worker picks up job:                                        │
    ├── Per-channel rate limit check ◄───────────────────────────────────┘
    ├── Global rate limit check
    ├── Circuit breaker check
    ├── HTTP POST (with retry: 3 attempts, exponential backoff)
    └── Log result to notification_log
```

---

## 6. Notification Backend Details

### Slack (Incoming Webhook)

- **API:** `POST https://hooks.slack.com/services/T.../B.../xxx` with JSON body
- **Auth:** Webhook URL is the credential
- **Format:** Block Kit with severity-colored header, field grid (hostname, program, severity, facility), message in code block, deep link to Taillight UI
- **Burst format:** "12 events matched rule 'core-rpd-errors' in the last 30s" with first event shown
- **Rate limit:** 1 msg/sec per channel. Respect HTTP 429 + `Retry-After`.
- **Dependencies:** None (stdlib `net/http`)

### Discord (Webhook)

- **API:** `POST https://discord.com/api/webhooks/{id}/{token}` with JSON body
- **Format:** Embed objects with severity-mapped color (decimal int), field grid, code block for message, footer with timestamp
- **Color mapping:** emerg/alert/crit → red `15158332`, err → orange `15105570`, warning → yellow `16776960`, notice/info → green `3066993`, debug → blue `3447003`
- **Rate limit:** 5 req/2s per webhook. Parse `X-RateLimit-*` headers.

### Microsoft Teams (Workflow Webhook)

- **API:** POST to Power Automate Workflow URL with Adaptive Card payload
- **Format:** Adaptive Card v1.4 wrapped in `{"type":"message","attachments":[...]}` envelope. FactSet for field grid, monospace TextBlock for message, Action.OpenUrl for deep link.
- **Note:** Office 365 Connectors retiring April 2026 — Workflow webhooks are the replacement.
- **Payload limit:** ~28 KB per Adaptive Card.

### PagerDuty (Events API v2)

- **API:** `POST https://events.pagerduty.com/v2/enqueue`
- **Auth:** `routing_key` in request body (integration key from PagerDuty service)
- **Actions:** `trigger`, `acknowledge`, `resolve`
- **Dedup key:** `taillight:{hostname}:{programname}:{severity}` — subsequent events merge into same incident
- **Severity mapping:** syslog 0-2 → `critical`, 3 → `error`, 4-5 → `warning`, 6-7 → `info`
- **Auto-resolve:** Not applicable — syslog/applog events are fire-and-forget, no resolve signal exists. PagerDuty incidents must be resolved manually or via PagerDuty's own auto-resolve timer.
- **Rate limit:** 7500 events/min (generous)

### Generic Webhook

- **Configurable:** URL, HTTP method (default POST), custom headers, body template
- **Template engine:** Go `text/template` with event fields as template variables
- **Auth:** Bearer token, Basic Auth, or custom header — configured in channel config JSON
- **Template validation:** Parsed at channel creation time (fail fast on syntax errors)
- **Default template:** JSON with `source`, `severity`, `hostname`, `program`, `message`, `timestamp`, `link`

### Email (SMTP)

- **Go approach:** stdlib `net/smtp` with STARTTLS on port 587
- **Format:** multipart/alternative (HTML + plain text). HTML with severity-colored header, table layout for fields, monospace message.
- **Burst format:** When `EventCount > 1`, show summary table of events in the burst (future enhancement)
- **Subject line:** `[CRIT] host1.example.com/sshd: Failed password for root`

### No External Notification Libraries

All backends are simple HTTP POST operations with stdlib `net/http`. This gives full control over message formatting (Block Kit, Adaptive Cards, embeds) and protocol features (PagerDuty dedup/resolve). Total backend code: ~500-700 lines across all 6 backends.

---

## 7. Configuration

### `config.yaml` section

```yaml
notification:
  enabled: true
  rule_refresh_interval: 30s   # how often to reload rules from DB
  dispatch_workers: 4           # concurrent delivery goroutines
  dispatch_buffer: 1024         # buffered channel size
  default_burst_window: 30s     # collect events before sending (overridable per rule)
  default_cooldown: 5m          # suppress after sending (overridable per rule)
  send_timeout: 10s             # HTTP timeout for backend calls
  global_rate_limit: 100        # max notifications per hour (safety net)
```

### Go config struct

```go
type NotificationConfig struct {
    Enabled             bool          // default: false
    RuleRefreshInterval time.Duration // default: 30s
    DispatchWorkers     int           // default: 4
    DispatchBuffer      int           // default: 1024
    DefaultBurstWindow  time.Duration // default: 30s
    DefaultCooldown     time.Duration // default: 5m
    SendTimeout         time.Duration // default: 10s
    GlobalRateLimit     int           // default: 100 per hour
}
```

Rules and channels are managed entirely via REST API (stored in PostgreSQL) — no restart needed to add/modify/disable rules.

---

## 8. REST API

```
# Channels (configured notification backends)
GET    /api/v1/notifications/channels           — list all channels
POST   /api/v1/notifications/channels           — create channel
GET    /api/v1/notifications/channels/{id}      — get channel
PUT    /api/v1/notifications/channels/{id}      — update channel
DELETE /api/v1/notifications/channels/{id}      — delete channel
POST   /api/v1/notifications/channels/{id}/test — send test notification

# Rules (alert conditions → channel routing)
GET    /api/v1/notifications/rules              — list all rules
POST   /api/v1/notifications/rules              — create rule
GET    /api/v1/notifications/rules/{id}         — get rule
PUT    /api/v1/notifications/rules/{id}         — update rule
DELETE /api/v1/notifications/rules/{id}         — delete rule

# Audit log
GET    /api/v1/notifications/log                — query notification history
GET    /api/v1/notifications/stats              — notification volume per rule/channel
```

All endpoints require authentication (same middleware as existing auth endpoints).

---

## 9. Integration Points

### Syslog path — `api/cmd/taillight/serve.go`

In the background worker that bridges LISTEN/NOTIFY to the broker:

```go
syslogBroker.Broadcast(event)
if notifEngine != nil {
    notifEngine.HandleSyslogEvent(event)
}
```

### AppLog path — `api/internal/handler/applog_ingest.go`

In the ingest handler after broadcasting:

```go
for i := range inserted {
    h.broker.Broadcast(inserted[i])
    if h.notifEngine != nil {
        h.notifEngine.HandleAppLogEvent(inserted[i])
    }
}
```

### Wiring in `serve.go`

```go
var notifEngine *notification.Engine
if cfg.Notification.Enabled {
    notifStore := postgres.NewNotificationStore(pool)
    notifEngine = notification.NewEngine(notifStore, cfg.Notification, logger)
    notifEngine.RegisterBackend(notification.ChannelTypeSlack, backend.NewSlack(logger))
    notifEngine.RegisterBackend(notification.ChannelTypeDiscord, backend.NewDiscord(logger))
    notifEngine.RegisterBackend(notification.ChannelTypeTeams, backend.NewTeams(logger))
    notifEngine.RegisterBackend(notification.ChannelTypePagerDuty, backend.NewPagerDuty(logger))
    notifEngine.RegisterBackend(notification.ChannelTypeWebhook, backend.NewWebhook(logger))
    notifEngine.RegisterBackend(notification.ChannelTypeEmail, backend.NewEmail(logger))
    notifEngine.Start(ctx)
}
```

### Shutdown sequence

```
1. Close SSE brokers (clients disconnect)
2. Shutdown notification engine (drain dispatch queue, finish in-flight sends)
3. Shutdown listener
4. Shutdown log shipper
5. Shutdown HTTP servers
6. Close DB pool (deferred)
```

Engine shuts down after brokers but before DB pool close — in-flight dispatches can still write to `notification_log`.

---

## 10. Prometheus Metrics

```
notification_rules_evaluated_total         counter   — events × rules evaluated
notification_rules_matched_total           counter   — rule matches
notification_dispatched_total              counter   — notifications sent to dispatch queue
notification_sent_total{channel,status}    counter   — delivery outcomes (success/failed)
notification_suppressed_total{reason}      counter   — suppression reasons (cooldown/rate_limit/circuit_breaker)
notification_send_duration_seconds         histogram — delivery latency per backend
notification_dispatch_queue_length         gauge     — current dispatch queue depth
notification_circuit_breaker_state{channel} gauge    — 0=closed, 1=half-open, 2=open
```

---

## 11. Dependencies

### New dependencies required

| Library | Purpose | Size |
|---------|---------|------|
| `golang.org/x/time/rate` | Token bucket rate limiting | Tiny (part of x/time) |
| `github.com/sony/gobreaker/v2` | Circuit breaker | Small, zero deps |

### Optional (recommended for future)

| Library | Purpose | When |
|---------|---------|------|
| `github.com/riverqueue/river` | Persistent job queue | When at-least-once delivery is needed across restarts |
| `github.com/hashicorp/go-retryablehttp` | HTTP client with retries | If custom retry logic becomes complex |

### Not recommended

| Library | Reason |
|---------|--------|
| `nikoksr/notify` | Massive dep tree, no rich formatting, no dedup, library itself disclaims reliability |

---

## 12. MVP vs Future Enhancements

### MVP — Rock-Solid Foundation

| Feature | Complexity | Notes |
|---------|------------|-------|
| Notification channels (CRUD API) | Low | Database + REST handler |
| Notification rules with filter matching | Medium | Reuse existing `Matches()` |
| Burst watcher | Low | In-memory per-rule collector, ~150 lines |
| Cooldown tracker with summary | Low | In-memory, ~100 lines. Sends "N more suppressed" on expiry |
| Per-channel rate limiting | Low | `x/time/rate`, ~100 lines |
| Circuit breaker per channel | Low | `sony/gobreaker`, ~50 lines |
| Dispatch queue with worker pool | Medium | Buffered channel + goroutines |
| Slack backend | Low | ~80 lines |
| Generic webhook backend | Low | ~100 lines with templates |
| Notification audit log | Low | TimescaleDB hypertable |
| Test notification endpoint | Low | Bypass burst/cooldown |
| Prometheus metrics | Low | Counters + gauges |

### Phase 2 — More Backends

| Feature | Complexity | Notes |
|---------|------------|-------|
| Discord backend | Low | ~70 lines |
| Teams backend | Low | ~80 lines |
| PagerDuty backend (trigger only) | Low | Dedup key for incident merging |
| Email backend | Medium | SMTP + HTML templates |
| Global rate limit | Low | Single `rate.Limiter` |

### Phase 3 — Operational Maturity

| Feature | Complexity | Notes |
|---------|------------|-------|
| Silences / maintenance windows | Medium | DB-backed, API + UI |
| Notification statistics API | Low | Aggregate from audit log |
| Persistent delivery queue (River) | Medium | Survives restarts |
| Severity-based routing (multi-channel) | Low | Already supported by rule→channel many-to-many |

---

## 13. Implementation Order

```
Phase 1 (MVP):
 1. Migration — schema (notification_channels, notification_rules, notification_log)
 2. Types — notifier.go, rule.go (interfaces, domain types)
 3. Burst watcher — burstwatcher.go (per-rule burst window collector)
 4. Cooldown — cooldown.go (in-memory tracker with summary on expiry)
 5. Rate limiter — ratelimit.go (per-key token bucket)
 6. Store — notification_store.go (CRUD)
 7. Engine — engine.go (rule eval, dispatch queue, workers)
 8. Backends — slack.go, webhook.go
 9. Config — config.go (add NotificationConfig)
10. Wiring — serve.go, applog_ingest.go
11. Handler — notification.go (REST API)
12. Metrics — metrics.go (Prometheus counters)
13. Tests — unit tests for rule matching, burst watcher, cooldown, engine, backends

Phase 2 (more backends):
14. Backends — discord.go, teams.go, pagerduty.go, email.go

Phase 3 (operational):
15. Silences — silence model, store, API
16. River integration — persistent queue
17. Statistics API
```

---

## Appendix A: Example Configuration

```yaml
# config.yaml
notification:
  enabled: true
  dispatch_workers: 4
  default_burst_window: 30s
  default_cooldown: 5m
  send_timeout: 10s
```

Then via API:

```bash
# Create a Slack channel
curl -X POST /api/v1/notifications/channels \
  -d '{"name":"ops-alerts","type":"slack","config":{"webhook_url":"https://hooks.slack.com/services/T.../B.../xxx"}}'

# Test it
curl -X POST /api/v1/notifications/channels/1/test

# Create a rule: alert on severity ≤ 3 (error and above) from any host
curl -X POST /api/v1/notifications/rules \
  -d '{"name":"high-severity","event_kind":"syslog","severity":3,"channel_ids":[1],"burst_window":30,"cooldown_seconds":300}'

# Create a rule: alert on rpd errors from core routers (longer cooldown for noisy rule)
curl -X POST /api/v1/notifications/rules \
  -d '{"name":"core-rpd-errors","event_kind":"syslog","hostname":"core-*","programname":"rpd","severity":3,"channel_ids":[1],"burst_window":60,"cooldown_seconds":600}'
```

## Appendix B: Severity Reference

| Syslog | Label | PagerDuty | Slack Color | Discord Color |
|--------|-------|-----------|-------------|---------------|
| 0 | emerg | critical | `#E74C3C` | `15158332` |
| 1 | alert | critical | `#E74C3C` | `15158332` |
| 2 | crit | critical | `#E74C3C` | `15158332` |
| 3 | err | error | `#E67E22` | `15105570` |
| 4 | warning | warning | `#F1C40F` | `16776960` |
| 5 | notice | warning | `#3498DB` | `3447003` |
| 6 | info | info | `#2ECC71` | `3066993` |
| 7 | debug | info | `#95A5A6` | `9807270` |
