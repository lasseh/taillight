# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

#### Terminal UI (taillight-tui)
- Add standalone TUI binary using Charmbracelet bubbletea v2, bubbles v2, lipgloss v2
- Real-time SSE streaming for srvlog, applog, and netlog with per-tab lazy connections
- Dashboard view with summary cards, recent high-severity events, and recent applog errors
- Hosts inventory view with status dots, feed badges, trend arrows, and vim navigation
- Notification rules and channels read-only view with sub-tabs
- Settings view with connection info and keyboard reference
- Detail sidebar with thin border, auto-updates on cursor navigation
- Filter bar with search input, hostname/program/service pickers
- Ring buffer (10k events) with virtual-scrolling table for high-throughput streams
- Tokyo Night theme matching web GUI colors exactly (severity, applog levels, accents)
- Syntax highlighting for log messages using jink lexer (IPs, numbers, states, protocols)
- Applog rows show component column and inline attrs as key=value pairs
- Multi-segment status bar pinned to bottom (LIVE/OFFLINE, filter pills, help)
- Toast notification overlays for critical events on any SSE stream using lipgloss Canvas/Layer compositor
- Tab bar with logo, primary tabs left, secondary tabs right, thin separator line
- Vim-style navigation (j/k, g/G, ctrl+d/u) with Enter for detail, Esc to close
- Config file support (~/.config/taillight/tui.yml) with CLI flag overrides

#### SSH server (taillight-wish)
- Add wish-based SSH server for hosting TUI over SSH
- Each SSH session gets independent app instance with own SSE streams and state
- TrueColor forced via WithColorProfile and COLORTERM=truecolor environment injection
- PTY slave fallback to session I/O for compatibility across SSH configurations
- activeterm middleware rejects non-PTY sessions (blocks SSH scanners)
- Graceful shutdown with 10s timeout on SIGINT/SIGTERM
- Health check on startup validates API connectivity before accepting connections

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

### Changed

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

### Fixed

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

### Security

- Block IPv6 ULA in SSRF check
- Fix email subject templating injection
- Fix rate limiter race condition
- Fix bcrypt init safety and CreateKey encoding
- Fix ntfy header injection
- Update npm deps to resolve security vulnerabilities

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
