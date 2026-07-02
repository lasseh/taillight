# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/).

This project is deployed continuously and does not cut tagged, versioned
releases. The **[Unreleased]** section below is therefore a permanent running
log of everything shipped since `0.1.0` — it is intentional, not a backlog of
work waiting for a release.

## [Unreleased]

<!-- Continuous deploy: add entries here under Added/Changed/Fixed/Security.
     Do not rename this section to a version or create release tags. -->

> The terminal UI (`taillight-tui`) and the SSH server that hosts it
> (`taillight-wish`) were extracted into a separate repository —
> https://github.com/lasseh/taillight-tui. Their changelog now lives there.

### Added

#### Analysis
- Email completed analysis reports by selecting email notification channels on a schedule (`notify_channel_ids`); channel ids are validated as existing email-type channels and snapshotted onto each report at enqueue time

#### Netlog feed
- Add netlog feed backend: model, store, broker, handlers, LISTEN/NOTIFY
- Add netlog frontend feed with rsyslog dual-port config
- Add loadgen-netlog CLI command, analysis multi-feed support, OpenAPI spec
- Parameterize analysis engine by feed (srvlog, netlog, all)
- Add netlog notification rules, unify syslog summary types
- Unify syslog sections with fuchsia netlog color
- Add "view all logs" link on netlog device detail page

#### Hosts page
- Add hosts overview endpoint with per-host stats
- Add hosts page with status cards, expandable rows, and filters
- Add status dots, sparklines, and error ratio to hosts table
- Show host IP address on device detail pages

#### Device pages
- Redesign device page with fullscreen layout and SyslogRow-based logs
- Redesign applog device page to match syslog device layout
- Show emerg/alert/crit/error severity cards matching dashboard layout
- Update device stats in real-time from SSE instead of polling
- Persist active log tab in URL query across refreshes
- Add responsive mobile layout for top messages
- Pre-compute msg_pattern on INSERT for fast top-messages queries
- Show 7d timespan label on device top messages heading

#### Notifications
- Add ntfy notification backend for mobile push alerts
- Add ntfy channel type to notification UI
- Add email channel type to notification channels UI
- Add scheduled summary digests for log activity
- Add per-user browser notification preferences

#### Authentication
- Add LDAP authentication for FreeIPA integration
- Add user management admin page

#### Home dashboard
- Add severity timeline stacked bar charts
- Add edit dashboard to hide/show home page widgets
- Make severity/level counts clickable links to filtered views
- Auto-reconnect home dashboard when API recovers
- Add global connection loss banner to all pages

#### Mobile
- Make syslog and applog pages mobile-friendly
- Add apply button to mobile filter panels
- Use two-line log rows on mobile for better readability
- Redesign mobile log rows with severity color bar
- Use color bar layout for home page log rows

#### Filtering
- Add wildcard glob support to host dropdown search
- Support exact severity filter via `?severity=` query param
- Add exact level filter (`level_exact`) for applog events
- Make detail fields clickable to apply as filters
- Add focus mode to hide header and filters on log views

#### Analysis reports
- Scope analysis reports to specific hosts: `GET /api/v1/analysis/hosts` endpoint, host picker in the create-report panel, host scope surfaced in the report list and detail
- Make analysis prompts scope-aware so host-scoped runs focus on the selected hosts
- Email delivery of completed analysis reports: the email backend renders the report markdown inline in the message body (goldmark), fired via a worker hook with CAS idempotency, recipients set by `analysis.notify_emails`; dormant `attach_pdf` plumbing committed but off by default
- Print-friendly analysis report export: "Export PDF" prints a server-rendered standalone document (`GET /api/v1/analysis/reports/{slug}/print`) via a hidden iframe, so the full report paginates across multiple A4 pages instead of clipping to one. Mail and print share one renderer (`internal/report`) — a paper-light masthead for print, the dark-bar card for email — so the two formats never drift
- Short-circuit empty-window analysis runs without calling the LLM
- Show report created date/time as an inline pill, human-readable titles in the report list, and an ops-briefing header on report detail
- Gate the "Analysis Reports" nav link on an `analysis` feature flag (served from `GET /api/v1/config/features`, sourced from `analysis.enabled`) so the link is hidden on deployments without analysis

#### Log feeds
- Sliding-window scrollback keeps web log feeds bounded in memory while paging back through history

#### Export
- Add CSV export for filtered log results

#### Rsyslog
- Extract Arista/Cisco event tags into msgid and strip from message
- Add state, Arista/Cisco interface, and MAC highlighting
- Add severity rewrite filter to downgrade ESWD_DAI_FAILED to notice
- Add output_dropped ruleset for logging filtered messages

#### Infrastructure
- Add demo_mode config to block write endpoints
- Self-host JetBrains Mono for offline/air-gapped deployments
- Add gzip, static asset caching, and SPA headers to nginx configs
- Add continuous aggregates for syslog and applog summaries
- Add database hardening migration
- Harden server configuration
- Add frontend unit tests and global error boundary
- Add taillight-handler Python package for PyPI
- Configure Dependabot for all package ecosystems

#### UX polish
- Add arrow indicator to hostname links in log detail
- Add severity level label to applog detail header
- Default API key scope to ingest, improve selection visibility
- Add subtle pulse animation to paused banner
- Add severity level details to empty state messages

#### Observability & operations
- Per-operation database query metrics (`taillight_db_query_duration_seconds`, `taillight_db_query_errors_total`) via a pgx `QueryTracer`
- Per-channel circuit-breaker state gauge (`taillight_notification_breaker_state`) and transition counter, so operators can alert on a channel's breaker opening
- Truncation signal (warn log + `taillight_listener_gap_fill_truncated_total`) when a listener gap-fill pass hits its row cap and recovery is incomplete
- Server-wide HTTP `ReadTimeout` to bound slow-drip request bodies; panic-induced 500s are now recorded in request metrics
- Standalone `idx_applog_id` index backing SSE Last-Event-ID backfill and single-event lookups, built per-chunk to avoid blocking ingest
- Gated database integration test harness (`make test-integration`, ephemeral TimescaleDB) plus a CI job, covering keyset pagination, batch-insert ordering, and the API-key/user join

### Changed

- LDAP auth: replace the single `admin_group` DN with a `group_role_map` (group full-DN or bare CN → `admin`/regular role; matched case-insensitively, highest role wins, membership in no mapped group denies login), add an optional `ca_bundle` to trust an internal CA without `tls_skip_verify`, and drop the FreeIPA-only `nsAccountLock` account-lock check now that AD is supported
- Overhaul the analyzer data block sent to Ollama: srvlog rows now group by `COALESCE(NULLIF(msgid,''), msg_pattern)` so RFC 3164 events (sshd/systemd/kernel/cron) reach the prompt instead of being dropped; each top signature ships with 1-2 verbatim sample messages, a 12-48 cell volume sparkline with peak timestamps, top programs/facilities for srvlog, and per-signature host distribution; system prompts updated across daily/weekly/incident with anti-hallucination rules around quoting samples
- Rework `/analysis`: async per-feed reports with `pending`/`running`/`completed`/`failed` lifecycle, slug-based URLs (`/analysis/reports/<feed>-YYYY-MM-DD-HHMM`), single-worker queue (depth 5) with partial-unique-index guard against duplicate active runs, in-DB recurring schedules (`analysis_schedules`) replacing the single `analysis.schedule_at`/`analysis.feed` config keys
- Rename syslog to srvlog across entire codebase
- Rename dashboard to volume, move alerts to settings dropdown
- Flatten dropdown menu into page links and account sections
- Consolidate 14 migrations into 8 clean files
- Extract generic broker with type-safe fan-out
- Move logshipper to standalone module at `pkg/logshipper/`
- Replace `nxadm/tail` with custom tailer to ensure follow-only mode
- Simplify log detail views: remove redundant tags, event IDs, raw message
- Merge attributes into message box on applog detail page
- Change "paused" label to "auto-scroll off" in status bar
- Change license from GPL-3.0 to MIT
- Improve light themes with white-card-on-gray-canvas pattern
- Bump color contrast ratios to pass WCAG AA across all themes
- Improve Lighthouse scores across all categories
- Unify srvlog/netlog/applog SSE handlers into one generic streamer behind a testable sink seam
- Extract shared filter query-param parsing/validation across srvlog/netlog/applog
- Unify the notification engine's per-plane rule fan-in into one generic handler
- Enforce the notification Rule kind↔fields invariant via `Rule.Validate`, closing an UpdateRule validation gap
- Share the syslog filter→SQL builder and meta-string queries between srvlog and netlog stores
- Make rate-limiter and circuit-breaker eviction testable via an internal seam
- Extract the LISTEN/NOTIFY dispatch into a testable `ingestbridge` module
- Calibrate the analyzer's hardware/CPU urgency: ban threshold/catastrophe vocab, make the hardware-escalation rule three-clause-mandatory, auto-escalate hardware faults, group new-signature families, and render CPU/correlation data as badges and tables
- Sharpen the daily prompt for senior Juniper/network engineers; require a status line in the TL;DR; pin report sections and retry on structural drift; cap clusters, truncate signatures, and force host backticks
- Make analyzer timeouts configurable and raise defaults (`ollama_timeout` 2h, `run_timeout` 4h)
- Strip oversized applog attrs from list and SSE responses to keep payloads bounded

### Removed

- **Feed feature flags** (`features.srvlog`, `features.netlog`, `features.applog`) — all three log feeds are now always enabled. The flags had three different meanings (only netlog was fully gated) and no known deployment disabled a feed. `GET /api/v1/config/features` keeps its response shape: the feed keys now always report `true`, and `analysis` remains the one real flag. Deployed configs with a `features:` block are unaffected — unknown keys are ignored
- Dormant `attach_pdf` email plumbing (channel config flag, `PDFRenderer` interface, multipart/mixed attachment path) — removed before ever being wired to a renderer; an `attach_pdf` key in existing channel configs is now silently ignored. Design preserved in `.scratch/email-analysis-reports/PRD.md` and git history

### Fixed

- **Keyset pagination dropped one event per page boundary** in the srvlog/netlog/applog list endpoints — the next cursor was set to the look-ahead "peek" row, which the strict `<` next-page query then excluded; now uses the last returned row (caught by a new DB integration test)
- Digest notifications rendered "in the last 0 seconds" — the digest window is now set on flush
- Notification rate limiter consumed a token on every retry attempt, abandoning alerts on low-burst channels (e.g. email) after two failures and preventing the circuit breaker from tripping; now gated once per notification with retries bypassing it
- A failing notification channel's multi-minute retry backoff no longer blocks the dispatch worker or delays sibling channels in the same job (delivery runs off the worker)
- Send-on-closed-channel panic in the auth touch worker during graceful shutdown (shutdown reordered + touch path made structurally panic-safe)
- Notification rule level/severity/facility are validated up front (clear 400) instead of surfacing an opaque 500 from the DB constraint; the applog level filter now fails closed on an unrecognised level
- Docker entrypoint now fails fast when all migration attempts fail, instead of booting against a half-migrated schema
- Nested config secrets (`smtp.password`, `netbox.token`, `ldap.bind_password`, `logshipper.api_key`) are now overridable via environment variables as documented
- Final notification audit-log row now survives shutdown (recorded on a detached context)
- SSE handlers flush headers on connect, so `EventSource` `onopen` fires immediately on a quiet stream instead of after the first heartbeat
- Reduce disconnect banner flicker on wake from sleep (visibility-aware reconnect, 5s grace period)
- Register export routes before `/{id}` to prevent param capture
- Cursor pagination off-by-one, missing `rows.Err()`, health timeout
- Increase SSE subscription buffer to reduce dropped events
- Gate device SSE behind initial fetch to fix event ordering
- Suppress context canceled errors from client disconnects
- Restore meta cache triggers removed in hardening migration
- Move continuous aggregate refresh to app startup (CALL cannot run in migration tx)
- Optimize slow top-messages query on device pages
- Use indexed `received_at` for applog device last-seen query
- Remove `omitzero` from AppLogEvent to fix frontend crash
- Query `last_seen` from events table instead of stale meta cache
- Skip browser notifications for backfilled events older than 30s
- Clear conflicting severity/level filter params on change
- Set PGDATA to match volume mount, preventing data loss
- Use dvh for mobile viewport to pin header and status bar
- Set Prism global before loading language components (vite 8)
- Make text selection visible in login input fields
- Treat 502-504 gateway errors as connection failures on home page
- Show friendly connection error on home page when API is down
- Update nginx SSE paths to srvlog/netlog/applog
- Query analysis meta caches by `(column_name, value)` instead of bare hostname
- Treat msgid `-` as RFC 5424 NIL so events fall through to `msg_pattern`
- Stop annotating hostnames with "(inferred from hostname)" in analysis reports

### Security

- Replace the deprecated, spoofable chi `middleware.RealIP` (GHSA-3fxj-6jh8-hvhx / GHSA-rjr7-jggh-pgcp / GHSA-9g5q-2w5x-hmxf) with a config-driven, safe-by-default client-IP resolver. The real client IP (used for login rate-limiting, `applog_events.source_ip` attribution, and the demo write gate) is now read only from the trusted `real_ip_header` when set, otherwise from the TCP peer — forwarded headers are no longer trusted unconditionally. **Behavior change:** deployments behind a reverse proxy must set `real_ip_header` (e.g. `X-Real-IP`) or every client is attributed to the proxy's IP, collapsing per-IP login rate-limiting into one bucket. Bumps `github.com/go-chi/chi/v5` to 5.3.0
- Require Go 1.26.4, resolving the `net/textproto` standard-library advisory GO-2026-5039 (`govulncheck` now reports no vulnerabilities in called code)
- Block the IPv4 "this host" range (`0.0.0.0/8`) and unspecified addresses (`0.0.0.0`, `::`) in the SSRF webhook guard, with a stdlib classification catch-all that also normalises IPv4-mapped IPv6 (e.g. `::ffff:127.0.0.1`)
- Redact secret webhook/Slack/ntfy URLs from transport errors before they are persisted to the notification audit log or shipped via application logs
- Block IPv6 ULA in SSRF check
- Fix email subject templating injection
- Fix rate limiter race condition
- Fix bcrypt init safety and CreateKey encoding
- Fix ntfy header injection
- Update npm deps to resolve security vulnerabilities
- Bump `golang.org/x/crypto` to v0.52.0 for CVE fixes

## [0.1.0] - 2026-03-11

Initial open-source release. Taillight was developed as an internal tool before
being open-sourced; the git history was reset prior to the public release.

### Added

- Real-time srvlog streaming via SSE with per-client server-side filtering
- Application log ingestion over HTTP (`POST /api/v1/applog/ingest`)
- Dashboard with aggregated volume charts and selectable time ranges
- Device-level log views with per-host statistics
- Cursor-based pagination for historical log browsing
- Notification engine with burst detection, cooldown, and rate limiting
- Slack, webhook, and email notification backends
- SSRF-safe webhook delivery with DNS rebinding protection
- Session-based authentication with bcrypt password hashing
- API key authentication with scoped access control (read, ingest, admin)
- Login rate limiting and session management
- Security headers middleware (CSP, HSTS, X-Frame-Options)
- Prometheus metrics endpoint with HTTP middleware instrumentation
- TimescaleDB hypertables for srvlog and applog events
- Docker Compose setup with rsyslog, TimescaleDB, Go API, and Vue frontend
- CLI commands: serve, migrate, loadgen, applog-loadgen, useradd, apikey, import
- taillight-shipper: standalone log file tailer for ingest API
- logshipper: slog handler for shipping app logs to the ingest endpoint
- Multiple color themes (dark, light, nord, dracula, solarized, monokai, gruvbox)
- Vue 3 frontend with Tailwind CSS and real-time EventSource integration
