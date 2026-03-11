# Taillight Feature Exploration — Opportunity Map

## Current State Summary

Taillight is a real-time syslog/applog viewer with:
- **Ingestion**: rsyslog→ompgsql→TimescaleDB (syslog), HTTP ingest API (applog)
- **Real-time**: pg_notify→Go listener→SSE brokers with per-client filtering
- **Notifications**: Rule engine with burst detection, cooldown, rate limiting, circuit breakers (Slack, Webhook, Email backends)
- **Analytics**: Volume charts, device summaries, severity/level breakdowns, LLM-powered analysis reports
- **Auth**: Session + API key auth with scope-based access (read, ingest, admin)
- **Frontend**: Vue 3 + TypeScript + Tailwind + Unovis charts, full SSE streaming UI
- **Monitoring**: Prometheus metrics, rsyslog stats ingestion, self-metrics tracking

---

## A. Alerting & Notifications

### Current Architecture
- `Notifier` interface: `Send(ctx, channel, payload) SendResult` + `Validate(channel) error`
- 3 backends: Slack (Block Kit), Webhook (Go text/template), Email (MIME HTML)
- All use SSRF-safe HTTP client (blocks RFC1918, loopback, link-local)
- Backends registered in `serve.go` via `engine.RegisterBackend(type, impl)`
- Rule matching delegates to `model.SyslogFilter.Matches()` / `AppLogFilter.Matches()`
- GroupTracker: accumulate→burst flush→cooldown→digest cycle with exponential backoff
- Payload carries full event pointer + metadata (rule name, count, window, digest flag)
- Notification log: hypertable with 30-day retention, stores full payload JSON

### New Backends (Discord, Teams, Telegram, PagerDuty)

**Effort: Low per backend (~2-3 hours each)**

Each backend is self-contained. To add Discord:
1. New file `api/internal/notification/backend/discord.go`
2. Implement `Notifier` interface (Validate parses config JSON, Send posts to webhook)
3. Add `ChannelTypeDiscord` constant to `notification.go`
4. Register in `serve.go` line ~112: `engine.RegisterBackend(ChannelTypeDiscord, backend.NewDiscord(logger))`
5. Migration: add `'discord'` to CHECK constraint on `notification_channels.type`
6. Frontend: add Discord option to channel type dropdown + webhook_url config field

Discord webhooks accept embeds (similar to Slack blocks). Teams uses Adaptive Cards. PagerDuty has Events API v2 (severity routing, dedup keys).

**Key files:**
- `api/internal/notification/backend/slack.go` — template to follow
- `api/internal/notification/notifier.go` — interface definition
- `api/cmd/taillight/serve.go:109-121` — registration

### Notification Templates

**Effort: Medium (~4-5 hours)**

Webhook backend already has full Go `text/template` support with a `marshal` helper. Slack and Email have fixed formats.

To extend:
1. Add `body_template` field to Slack and Email config structs
2. In `Send()`, if template is set, execute it against `Payload` instead of hardcoded format
3. Validate template parsing in `Validate()`
4. Frontend: add template editor field to channel config (textarea with variable reference)

Payload fields available: `.Kind`, `.RuleName`, `.Timestamp`, `.EventCount`, `.IsDigest`, `.GroupKey`, `.Window`, `.SyslogEvent.Hostname`, `.SyslogEvent.Message`, `.SyslogEvent.SeverityLabel`, etc.

### Rule Dry-Run / Preview

**Effort: Low-Medium (~2-3 hours)**

No engine changes needed — purely a handler-level feature:
1. New endpoint: `POST /api/v1/notifications/rules/{id}/dry-run` (admin scope)
2. Accept optional `{from, to}` time range (default: last 1 hour)
3. Fetch rule from DB, convert to `model.SyslogFilter` or `model.AppLogFilter`
4. Query `ListSyslogs` / `ListAppLogs` with that filter (limit 1000)
5. Return `{matched_count, total_scanned, sample_events: [first 10]}`

Alternative: dry-run a draft rule (not yet saved) by accepting rule JSON in request body.

### Alert Acknowledgment

**Effort: Medium-High (~5-6 hours)**

Currently `notification_log` is append-only with no ack tracking.

1. Migration: add `ack_status TEXT DEFAULT 'pending'`, `ack_by UUID REFERENCES users(id)`, `ack_at TIMESTAMPTZ`
2. New endpoint: `PATCH /api/v1/notifications/log/{id}` with `{ack_status: "acked"|"resolved"}`
3. Store method: `AckNotificationLog(ctx, id, status, userID)`
4. Frontend: ack/resolve buttons in notification log view
5. Optional: metrics for MTTA (mean time to ack) and MTTR (mean time to resolve)

### Event Correlation

**Effort: High**

Current GroupTracker only groups within a single rule. Cross-rule correlation needs:
- New `Correlator` stage between engine and GroupTracker
- Rules tagged with correlation group ID
- Correlator accumulates matches from multiple rules within a time window
- Fires single notification when threshold met
- Defer unless concrete use case exists

### Escalation Policies

**Effort: High**

Notify team A, then on-call B after N minutes if unacknowledged:
- Requires alert acknowledgment first (dependency)
- New `escalation_policies` table with ordered steps (channel, delay, condition)
- Background goroutine checking unacked alerts against escalation timers
- Integration with on-call tools (PagerDuty, OpsGenie) for schedule awareness

### Notification Digest

**Effort: Medium (~4 hours)**

Scheduled summary of all fired rules:
- New cron job (reuse existing analysis schedule pattern)
- Query notification_log for period, aggregate by rule
- Format as email digest (reuse Email backend)
- Config: `notification.digest_schedule`, `notification.digest_recipients`

---

## B. Search & Analytics

### Current Architecture
- **Syslog search**: `message ILIKE '%term%'` with trigram GIN index
- **AppLog search**: Full-text via `search_vector @@ plainto_tsquery('simple', ?)`
- **Filter SQL**: Dynamic via squirrel query builder in `applySyslogFilter()` / `applyAppLogFilter()`
- **Pagination**: Keyset cursor on `(received_at, id)` tuple, base64-encoded
- **Volume stats**: `time_bucket()` at query time, no continuous aggregates
- **Meta cache**: Trigger-populated for autocomplete (10k limit per column)
- **No export capabilities**

### Log Export (CSV/JSON/NDJSON)

**Effort: Low (~2-3 hours)**

1. New handler methods: `ExportSyslog` / `ExportAppLog`
2. Route: `GET /api/v1/syslog/export?format=csv&...filters...` (read scope)
3. Stream chunked response: iterate with cursor pagination, write rows incrementally
4. Set `Content-Disposition: attachment; filename=syslog-export-{timestamp}.csv`
5. Formats: CSV (`encoding/csv`), NDJSON (one JSON object per line)
6. Safety: max 100k rows or 24h time range per export
7. Frontend: download button in filter bar

**Key files:**
- `api/internal/handler/syslog.go` — add ExportSyslog method
- `api/internal/postgres/store.go:113-152` — reuse ListSyslogs with cursor iteration

### Syslog Full-Text Search

**Effort: Low (~2 hours)**

AppLog has FTS, syslog uses ILIKE only:
1. Migration: add `search_vector tsvector GENERATED ALWAYS AS (to_tsvector('simple', ...)) STORED`
2. GIN index on search_vector
3. Update `applySyslogFilter()` to use `@@ plainto_tsquery()` instead of ILIKE
4. Keep trigram index as fallback for substring/regex

**Caveat**: Adding GENERATED column to existing hypertable may require chunk rebuild.

### Wildcard on More Filter Fields

**Effort: Low (~1 hour)**

Only hostname/host support `*` wildcards. The `matchWildcard()` function already exists:
1. In `applySyslogFilter()`: apply wildcard logic to `programname`, `syslogtag`
2. In `applyAppLogFilter()`: apply to `service`, `component`
3. In `Matches()` methods: use `matchWildcard()` instead of `==`

### Saved Filters / Views

**Effort: Medium (~4-5 hours)**

Filters are URL-serializable. Need persistent storage:
1. Migration: `saved_views(id, user_id UUID FK, name TEXT, event_kind TEXT, filter_json JSONB, created_at, updated_at)`
2. Store: CRUD on saved_views
3. Handler: `GET/POST/DELETE /api/v1/views` (read/admin scope)
4. Frontend: save button in filter bar, dropdown to load saved views
5. Filter JSON stores query param set (e.g., `{"hostname":"prod-*","severity_max":"3"}`)

### Continuous Aggregates

**Effort: Medium (~3-4 hours)**

No continuous aggregates exist. All volume queries scan raw data.

1. Migration:
   ```sql
   CREATE MATERIALIZED VIEW syslog_volume_1h
   WITH (timescaledb.continuous) AS
   SELECT time_bucket('1 hour', received_at) AS bucket,
          hostname, count(*) AS cnt
   FROM syslog_events
   GROUP BY bucket, hostname
   WITH DATA;
   ```
2. Same for applog (group by service)
3. Update `GetVolume()` to query view for ranges > 6h
4. Refresh policy: hourly with 1-hour offset
5. Retention: 1 year on cagg vs 90 days on raw

**Benefits**: 7d/30d dashboard queries go from millions of rows to thousands.

### Advanced Query Language

**Effort: High**

Support queries like `severity <= 3 AND hostname:prod-* AND NOT message:"scheduled task"`:
- Query parser → AST → SQL translator with parameter binding
- Libraries: `github.com/antonmedv/expr` or hand-rolled
- **Pragmatic alternative**: UI-driven filter composition with AND/OR toggle (medium effort, covers 80%)

### Cross-Log Search

**Effort: High**

Unified search across syslog + applog with time correlation:
- Unified search endpoint combining both queries
- Results interleaved by timestamp
- Requires common result type or polymorphic response
- Time-aligned correlation view in frontend

### Search Suggestions / History

**Effort: Medium (~3-4 hours)**

1. New table: `search_history(id, user_id, event_kind, query_params JSONB, created_at)`
2. Record searches in handler middleware (debounced, deduplicated)
3. Endpoint: `GET /api/v1/search/suggestions?q=prod` — prefix match on recent searches
4. Frontend: autocomplete dropdown in search field

---

## C. Auth & Operations

### Current Architecture
- **Auth**: Session cookie (30-day) + API key (Bearer `tl_...`)
- **Scopes**: 3 fixed values (read, ingest, admin). Sessions bypass all scope checks.
- **Users**: `is_admin` boolean, bcrypt (cost 12), UUID PKs
- **Rate limiting**: Login only (5/min per IP). No global API limits.
- **Health check**: DB connectivity only
- **No audit logging**

### RBAC (Role-Based Access Control)

**Effort: Medium (~5-6 hours)**

Migration path (backward-compatible):
1. New tables:
   ```sql
   roles(id SERIAL PK, name TEXT UNIQUE, description TEXT)
   role_permissions(role_id FK, permission TEXT)
   user_roles(user_id UUID FK, role_id FK)
   ```
2. Default roles: `viewer` (read:*), `operator` (read:* + write:notification), `admin` (all)
3. Migrate: `is_admin=true` → admin role, others → viewer
4. New middleware: `RequirePermission("write", "notification")` replaces `RequireScope("admin")`
5. Load roles→permissions on auth, cache in context
6. Session auth loads permissions from roles (no longer god mode)

**Key files:**
- `api/internal/auth/middleware.go:97-119` — RequireScope to replace
- `api/internal/handler/auth.go:412-416` — validScopes to expand
- `api/cmd/taillight/serve.go:402-578` — route grouping

### Audit Logging

**Effort: Medium (~4-5 hours)**

1. Migration:
   ```sql
   CREATE TABLE audit_log (
     id BIGINT GENERATED ALWAYS AS IDENTITY,
     timestamp TIMESTAMPTZ NOT NULL DEFAULT now(),
     user_id UUID REFERENCES users(id),
     action TEXT NOT NULL,
     resource_type TEXT NOT NULL,
     resource_id TEXT,
     details JSONB,
     ip_address INET,
     PRIMARY KEY (timestamp, id)
   );
   -- TimescaleDB hypertable, 1-year retention
   ```
2. `AuditLogger` with `Log(ctx, action, resourceType, resourceID, details)` method
3. Fire-and-forget (async insert) to avoid request latency
4. Capture: login/logout, user CRUD, API key create/revoke, rule/channel CRUD, analysis trigger
5. Endpoint: `GET /api/v1/audit` (admin scope) with filters

### API Rate Limiting

**Effort: Medium (~3-4 hours)**

1. Middleware using `golang.org/x/time/rate` (already a dependency)
2. Per-API-key: `map[keyHash]*rate.Limiter` with TTL eviction
3. Per-IP for anonymous/session requests
4. Config: `rate_limit.requests_per_second`, `rate_limit.burst`
5. Return 429 with `Retry-After` header
6. Prometheus counter: `taillight_rate_limited_total{scope}`

### OAuth2/OIDC

**Effort: High (~8-10 hours)**

1. Dependencies: `github.com/coreos/go-oidc/v3` + `golang.org/x/oauth2`
2. Migration: `oauth_providers` + `oauth_links` tables
3. Routes: `/auth/oauth/{provider}/login` → redirect, `/auth/oauth/{provider}/callback` → session
4. Config: providers list with client_id, client_secret, discovery_url
5. User linking: match by email or auto-create
6. Creates same session cookie as local login

### Health Check Expansion

**Effort: Low (~1 hour)**

Add to health response:
- Listener status (connected/disconnected, last notification timestamp)
- Broker stats (active SSE clients, dropped events)
- Notification engine status (dispatch queue depth, circuit breaker states)
- DB pool utilization (active/idle/total)

### Circuit Breaker Dashboard

**Effort: Low (~1-2 hours)**

Expose notification circuit breaker state in UI:
1. New endpoint: `GET /api/v1/notifications/health` (admin scope)
2. Return per-channel breaker state (closed/half-open/open, failure count, last failure)
3. Optional: `POST /api/v1/notifications/channels/{id}/reset-breaker` to manually reset
4. Frontend: status badges on channel cards in notification settings

---

## D. Event Enrichment & Context

### Interface Descriptions
Map (hostname, ifname)→description, enrich syslog events in real-time. See separate design doc. **Effort: Medium**

### Device Inventory
Store device metadata (location, role, OS, vendor). Display in event views and device summaries. **Effort: Medium**

### Structured Data Parsing
RFC 5424 structured-data is stored but unused. Parse into key-value pairs, index, make searchable. **Effort: Low**

### Message Template Detection
Auto-detect recurring message patterns using regex normalization (already exists in `GetDeviceSummary`). Surface as "message types" in UI. **Effort: Medium**

---

## E. Integrations & Data Pipeline

### SNMP Poller
Periodic interface description/device info collection. **Effort: Medium**

### Kafka/AMQP Export
Publish events to message broker for downstream consumers. **Effort: High**

### S3/Object Storage Archive
Export aged-out logs before retention deletes them. **Effort: Medium**

### Webhook Inbound
Accept events from external systems (GitHub, Grafana, etc.). **Effort: Medium**

### Grafana Data Source
Plugin or API compatibility for Grafana dashboards. **Effort: Medium**

---

## F. UX & Frontend

### Event Annotations
Bookmark/tag/comment on events for team collaboration. **Effort: Medium**

### Theme Toggle
Dark/light mode selector (theme system exists, no UI toggle). **Effort: Low**

### Mobile Responsive
Optimize dense tables and charts for small screens. **Effort: Medium**

### Keyboard Shortcuts
Navigate events, toggle filters, jump to views. **Effort: Low**

### Event Suppression
Hide known-noisy patterns from default views. **Effort: Medium**

### Correlation Timeline
Multi-host event timeline for incident investigation. **Effort: High**

---

## Underutilized Existing Features

1. **Structured Data field** — stored but never indexed, searched, or displayed
2. **rsyslog stats** — full pipeline exists, rarely used; could power operational alerts
3. **Notification log** — filterable API exists, UI only shows summary
4. **Device summaries** — endpoints exist, no cross-device comparison
5. **Taillight self-metrics** — 30s snapshots, partially surfaced in dashboard
6. **Analysis report history** — persisted, no trend analysis or diffing
7. **LogShipper self-reporting** — meta-monitoring capability
8. **Webhook template engine** — full Go text/template, only used for webhook backend

---

## Recommended Implementation Order

### Quick wins (1-3 hours each)
1. Log export (CSV/NDJSON)
2. New notification backends (Discord, Teams)
3. Wildcard on more filter fields
4. Health check expansion
5. Rule dry-run
6. Circuit breaker dashboard
7. Theme toggle

### Medium projects (4-6 hours each)
1. Saved filters/views
2. Notification templates
3. Continuous aggregates
4. Audit logging
5. Alert acknowledgment
6. RBAC
7. API rate limiting

### Ambitious projects (8+ hours)
1. OAuth2/OIDC SSO
2. Advanced query language
3. Cross-log search
4. Event correlation
5. Anomaly detection
6. Custom dashboard builder
