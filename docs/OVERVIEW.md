# Taillight Overview

## Introduction

Taillight is a real-time log viewer for network operations teams. It streams three log feeds — netlog (network device syslog), srvlog (server syslog), and applog (application logs over HTTP ingest) — from a TimescaleDB database to its clients: a Vue 3 browser SPA in this repo, plus a terminal client and an SSH server in the companion [taillight-tui](https://github.com/lasseh/taillight-tui) repo — all over Server-Sent Events (SSE).

The primary use case is monitoring syslog output from network devices (Juniper, Cisco, Arista, and others) as netlog events and server syslog (Linux, Docker, PostgreSQL) as srvlog events. Application logs arrive over an authenticated HTTP ingest API. Events flow from rsyslog into PostgreSQL, through a Go backend that fans them out to connected clients, and into either a browser with live filtering, dashboards, and search, or a terminal UI with vim-style navigation.

## Features

### Real-time streaming

- Netlog, srvlog, and application log events pushed to the browser via SSE
- Per-client server-side filtering -- only matching events are sent to each connection
- Automatic reconnection with `Last-Event-ID` resume

### Filtering and search

- Filter by host, facility, severity, program, and free-text search
- Cursor-based pagination for browsing historical events
- Metadata endpoints for populating filter dropdowns (hosts, programs, facilities, tags, services, components)

### Three log feeds

- **Netlog** -- network device syslog, ingested by rsyslog (port 514) via ompgsql into `netlog_events`; a trigger fires `pg_notify` to push events to the Go backend in real time
- **Srvlog** -- server syslog, same pipeline on rsyslog port 515 into `srvlog_events`
- **Application logs** -- ingested via `POST /api/v1/applog/ingest` as JSON batches; stored in a separate hypertable and broadcast directly from the ingest handler (no LISTEN/NOTIFY hop)

All three feeds are always enabled — a feed you don't point logs at simply stays empty. The real toggles are the optional subsystems: notifications, AI analysis, LDAP, and Netbox.

### Dashboard and analytics

- Aggregated volume charts with selectable time ranges
- Hosts overview page with per-host status, sparklines, and error ratios (srvlog + netlog)
- Severity/level distribution summaries for all three feeds
- Per-device detail pages for netlog, srvlog, and applog
- Continuous aggregates in TimescaleDB for fast dashboard queries
- Internal metrics history (DB pool, SSE clients, event rates) with volume charts

### Authentication and authorization

- Session-based login with cookie authentication
- API key support with scoped access: `read`, `ingest`, `admin`
- Optional LDAP authentication (Active Directory, FreeIPA) with group-to-role mapping
- Configurable: run fully open on a private network or lock down for production
- User management: create users, revoke sessions, toggle active status

### Notification system

- Alert rules that match netlog/srvlog events by host, severity, facility, program, or msgid, and applog events by service, component, host, or level — plus free-text search on all feeds
- Notification channels: Slack, webhooks, email (SMTP), and ntfy
- Fire-first anti-spam: the first matching event fires immediately; repeats within a silence window collapse into a single digest, and the window grows linearly (capped) while a rule keeps firing
- Per-channel rate limiting, circuit breakers, and bounded delivery retry
- Managed via the API or the ALERTS tab in the UI — see [NOTIFICATIONS.md](NOTIFICATIONS.md)

### Scheduled summary digests

- Recurring (daily/weekly/monthly) log activity digests sent through notification channels
- Managed in the UI, stored in the database (`summary_schedules`)

### Juniper netlog reference

- Import Juniper syslog message definitions from official XLSX files
- Lookup endpoint for enriching events with description, cause, and recommended action
- Supports both Junos and Junos Evolved
- Auto-import on startup from a configurable directory (`juniper_ref_path`)

### Netbox enrichment

- The netlog detail page looks up IPs, prefixes, AS numbers, interfaces, and the source device against a [Netbox](https://netbox.dev/) instance
- Lazy, cached, and fully optional — configured under `netbox:` in `api/config.yml`

### Monitoring

- Prometheus `/metrics` endpoint with HTTP request metrics, SSE client counts, DB pool gauges, and notification counters
- Optional separate metrics listener to isolate metrics from the public API
- Internal metrics snapshots persisted to TimescaleDB for historical charts

### Log shipper

- Standalone `taillight-shipper` binary that tails log files or reads stdin and ships lines to the ingest API
- JSON line parsing with automatic field extraction
- File rotation support and graceful shutdown
- Also: `pkg/logshipper` (Go `slog.Handler`) and `taillight-sdk` (Python `logging.Handler` on PyPI)

### Terminal UI & SSH server

- A terminal UI client (`taillight-tui`) and an SSH server that hosts it (`taillight-wish`) live in the companion repo [taillight-tui](https://github.com/lasseh/taillight-tui) — both consume this API over HTTP/SSE

### AI analysis (optional, default-off)

- Netlog and srvlog analysis using a local Ollama LLM instance — no log data leaves the box
- Async report generation with permalink URLs, optional host scoping, and print/PDF export
- Recurring schedules managed in the UI (stored in the database); completed reports can be emailed via notification channels
- On-demand analysis trigger via the API

## Technology Stack

| Technology | Version | Purpose |
|------------|---------|---------|
| Go | 1.26+ | Backend API server, SSE broker, CLI tools |
| chi | v5 | HTTP router with middleware (request ID, CORS, recovery, timeouts) |
| pgx | v5 | PostgreSQL driver with connection pooling |
| golang-migrate | v4 | Database schema migrations |
| Cobra | -- | CLI framework for all subcommands |
| Viper | -- | Configuration management (YAML + env vars) |
| Vue 3 | 3.5+ | Frontend SPA framework |
| TypeScript | 6.0 | Frontend type safety |
| Pinia | 3.x | Vue state management |
| Tailwind CSS | v4 | Utility-first styling |
| Vite | 8.x | Frontend build tool and dev server |
| Unovis | 1.6+ | Chart library for dashboards |
| TimescaleDB | PG 18 | Time-series hypertables with compression and retention |
| rsyslog | -- | Syslog collector with ompgsql output module |
| Ollama | -- | Local LLM runtime for the optional analysis feature |
| Prometheus | -- | Metrics collection and scraping |
| Docker Compose | -- | One-command deployment of the full stack |

## Project Layout

```
taillight/
├── api/                           # Go backend (module github.com/lasseh/taillight)
│   ├── cmd/
│   │   ├── taillight/             # Main CLI binary (serve, migrate, loadgens, etc.)
│   │   └── taillight-shipper/     # Standalone log file shipper
│   ├── internal/
│   │   ├── analyzer/              # AI analysis pipeline (gather → prompt → Ollama)
│   │   ├── auth/                  # Session + API key middleware, password hashing
│   │   ├── broker/                # Generic SSE fan-out broker + per-feed wrappers
│   │   ├── config/                # Viper-based configuration loader
│   │   ├── handler/               # HTTP handlers (one file per domain)
│   │   ├── httputil/              # Shared HTTP utilities
│   │   ├── ingestbridge/          # LISTEN/NOTIFY → broker/engine dispatch
│   │   ├── juniperref/            # Juniper reference XLSX parsing + auto-import
│   │   ├── ldap/                  # Optional LDAP authentication client
│   │   ├── metrics/               # Prometheus collectors + HTTP middleware
│   │   ├── model/                 # Domain types, filter parsing, cursor pagination
│   │   ├── netbox/                # Optional Netbox enrichment client
│   │   ├── notification/          # Rule engine, suppressor, backends
│   │   ├── ollama/                # Ollama LLM client
│   │   ├── postgres/              # Store (pgx queries), Listener (LISTEN/NOTIFY)
│   │   ├── report/                # Shared analysis-report renderer (email + print)
│   │   ├── scheduler/             # Summary + analysis schedulers (60s tick)
│   │   └── worker/                # Queued analysis worker
│   ├── migrations/                # SQL migration files (golang-migrate)
│   ├── docs/                      # Embedded OpenAPI spec + Scalar docs handler
│   ├── reference/                 # Juniper XLSX drop-in dir (gitignored contents)
│   ├── config.yml.example         # Application tuning reference
│   ├── Dockerfile                 # Multi-stage Go build
│   ├── Makefile                   # Build, test, lint targets
│   └── go.mod
├── pkg/
│   └── logshipper/                # Go slog.Handler that ships logs to the ingest API
│                                  # (own module: github.com/lasseh/taillight/pkg/logshipper)
├── sdk/
│   └── python/                    # Python logging.Handler SDK (taillight-sdk on PyPI)
├── frontend/                      # Vue 3 SPA
│   ├── src/                       # TypeScript source, components, stores, views
│   ├── Dockerfile                 # Multi-stage frontend build
│   ├── Makefile                   # Install, dev, lint, build targets
│   ├── nginx.conf                 # Production static file server config
│   └── package.json
├── rsyslog/                       # Modular rsyslog config (netlog + srvlog rulesets)
├── docs/                          # Project documentation + ADRs (docs/adr/)
├── .env.example                   # Per-deployment settings template
├── docker-compose.yml             # Full stack: PostgreSQL, API, rsyslog, frontend
├── Makefile                       # Root Makefile (delegates to api/ and frontend/)
└── README.md
```

## Getting Started

### Prerequisites

- Docker and Docker Compose (for the full stack)
- Go 1.26+ (for local backend development)
- Node.js 20.19+ / 22.12+ and npm (for local frontend development; required by Vite 8)

### Docker Compose quickstart

Clone the repository and start the stack:

```sh
git clone https://github.com/lasseh/taillight.git
cd taillight
cp .env.example .env   # review and adjust for your environment
make up                # copies api/config.yml from the example on first run
```

(`make up` wraps `docker compose up -d`.) This starts four services:

| Service | URL | Description |
|---------|-----|-------------|
| Frontend | `http://localhost:3000` | Web UI |
| API | `http://localhost:8080` | REST + SSE backend |
| PostgreSQL | `localhost:5432` | TimescaleDB database |
| rsyslog | `localhost:1514` (netlog), `localhost:1515` (srvlog) | Syslog collector (UDP + TCP) |

### Create a user

Authentication is enabled by default (`AUTH_ENABLED=true` in `.env`). Create a user:

```sh
docker compose exec api /app useradd --username admin --password adminadmin
```

To run without authentication (private networks only), set `AUTH_ENABLED=false` in `.env`.

### Generate test data

Populate the database with realistic events:

```sh
# Netlog events — network device logs (direct SQL insert, fast)
docker compose exec api /app loadgen-netlog -n 500 --delay 50ms --jitter 100ms

# Netlog via rsyslog (full pipeline: UDP -> rsyslog -> ompgsql -> netlog_events -> NOTIFY)
docker compose exec api /app loadgen-netlog -n 500 --syslog rsyslog:514 --delay 50ms

# Srvlog events — server logs (direct SQL insert)
docker compose exec api /app loadgen-srvlog -n 500 --delay 50ms --jitter 100ms

# Srvlog via rsyslog (srvlog ruleset listens on port 515)
docker compose exec api /app loadgen-srvlog -n 500 --syslog rsyslog:515 --delay 50ms

# Application log events (via HTTP ingest API)
docker compose exec api /app loadgen-applog -n 500 --batch 50 \
  --endpoint http://localhost:8080/api/v1/applog/ingest
```

### Send syslog messages from the host

The rsyslog container listens on host port 1514 for netlog (container port 514) and host port 1515 for srvlog (container port 515). Send RFC 5424 messages:

```sh
# Single test message to the netlog feed
echo '<14>1 2025-02-07T12:00:00Z router01 rpd 1234 RPD_BGP_NEIGHBOR_STATE_CHANGED - BGP peer 10.0.0.1 state changed to Established' \
  | nc -u -w1 localhost 1514

# Using logger (netlog)
logger -n localhost -P 1514 -d --rfc5424 -p local7.warning -t rpd \
  "BGP peer 10.0.0.1 state changed to Established"

# Server syslog goes to the srvlog feed on 1515
logger -n localhost -P 1515 -d --rfc5424 -t sshd "Accepted publickey for admin"
```

### Default ports

| Service | Host Port | Container Port | Environment Variable |
|---------|-----------|----------------|----------------------|
| PostgreSQL | 5432 | 5432 | `POSTGRES_BIND` |
| API | 8080 | 8080 | `API_HOST_PORT` |
| rsyslog (netlog) | 1514 | 514 | `RSYSLOG_NETLOG_PORT` |
| rsyslog (srvlog) | 1515 | 515 | `RSYSLOG_SRVLOG_PORT` |
| Frontend | 3000 | 8080 | `FRONTEND_HOST_PORT` |

Host ports only affect access from the host machine. Container-to-container communication always uses internal ports. In production, set the rsyslog ports to `514`/`515`.

### Local development

**Backend:**

```sh
cd api
cp config.yml.example config.yml   # fill in database credentials
make build
make test
make lint
```

**Frontend:**

```sh
cd frontend
npm install
npm run dev    # starts Vite dev server on http://localhost:5173
```

## Configuration

Configuration is split across two files:

### `.env` -- per-deployment settings

Passwords, ports, and feature toggles that change between environments. Docker Compose reads this file automatically and passes values as environment variables.

| Variable | Default | Description |
|----------|---------|-------------|
| `POSTGRES_PASSWORD` | `taillight` | Database password (shared by all containers) |
| `POSTGRES_BIND` | `127.0.0.1:5432` | Bind address and port for PostgreSQL (`host:port`). Set to `0.0.0.0:5432` for remote access or change the port to avoid conflicts. |
| `API_HOST_PORT` | `8080` | Host port for the API |
| `RSYSLOG_NETLOG_PORT` | `1514` | Host port for netlog syslog input (set to `514` in production) |
| `RSYSLOG_SRVLOG_PORT` | `1515` | Host port for srvlog syslog input (set to `515` in production) |
| `FRONTEND_HOST_PORT` | `3000` | Host port for the web UI |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `AUTH_ENABLED` | `true` | Enable authentication (sessions + API keys) |
| `DEMO_MODE` | `false` | Read-only demo mode: all write endpoints return 403 |
| `REAL_IP_HEADER` | *(empty)* | Trusted client-IP header set by your reverse proxy (e.g. `X-Real-IP`). Empty = trust only the TCP peer. |
| `API_URL` | *(empty)* | Frontend API URL (empty = same-origin proxy via nginx) |

### `api/config.yml` -- application tuning

Settings that rarely change between environments. Viper priority: defaults -> `config.yml` -> environment variables. Environment variables always override file values. Defaults below are the built-in defaults that apply when a key is absent; see [`config.yml.example`](../api/config.yml.example) for an annotated reference.

#### Authentication

| Key | Default | Description |
|-----|---------|-------------|
| `auth_read_endpoints` | `true` | Require authentication on read endpoints (log lists, meta, stats, SSE). Only takes effect when `AUTH_ENABLED` is true. |
| `demo_mode` | `false` | All write endpoints return 403 Forbidden (used by the public demo) |
| `cookie_secure` | `false` | Force the `Secure` flag on session cookies regardless of `X-Forwarded-Proto` |

#### Client IP resolution

| Key | Default | Description |
|-----|---------|-------------|
| `real_ip_header` | `""` | Trusted single-IP header the reverse proxy overwrites (e.g. `X-Real-IP`). Empty = trust only the TCP peer. |
| `trusted_proxies` | `[]` | CIDRs (or bare IPs) of proxies allowed to set `real_ip_header`. When set, the header is honored only for requests whose TCP peer is inside the list. Empty = header trusted from any peer. |

#### CORS

| Key | Default | Description |
|-----|---------|-------------|
| `cors_allowed_origins` | localhost dev origins | List of allowed origins. For production, specify origins explicitly. Empty defaults to localhost:5173 and localhost:3000. |

#### Metrics

| Key | Default | Description |
|-----|---------|-------------|
| `metrics_addr` | `""` (disabled) | Serve `/metrics` on a separate listener (e.g. `":9090"`). Empty disables the dedicated metrics endpoint. |

#### Database pool

| Key | Default | Description |
|-----|---------|-------------|
| `db_max_conns` | `30` | Maximum connections in the pgx pool |
| `db_min_conns` | `2` | Minimum idle connections in the pgx pool |

#### LISTEN/NOTIFY

| Key | Default | Description |
|-----|---------|-------------|
| `notification_buffer_size` | `1024` | Channel buffer for PostgreSQL LISTEN/NOTIFY notifications. Increase if you see "notification channel near capacity" warnings. |
| `notification_workers` | `4` | Goroutines that fetch notified rows and broadcast them to SSE clients |

#### Juniper reference auto-import

| Key | Default | Description |
|-----|---------|-------------|
| `juniper_ref_path` | `"reference/juniper"` | Directory scanned on startup for Juniper syslog reference XLSX files; auto-imported when the reference table is empty for that OS. Empty disables. |

#### Log shipper

The API can ship its own logs to its applog ingest endpoint (logs remain on stdout regardless).

| Key | Default | Description |
|-----|---------|-------------|
| `logshipper.enabled` | `false` | Enable self log shipping |
| `logshipper.service` | `"taillight"` | Service name in shipped logs |
| `logshipper.component` | `"server"` | Component name in shipped logs |
| `logshipper.host` | `os.Hostname()` | Override hostname |
| `logshipper.min_level` | `"info"` | Minimum level to ship (`debug`, `info`, `warn`, `error`) |
| `logshipper.api_key` | `""` | Bearer token (must match an `api_keys` entry when auth is enabled) |
| `logshipper.batch_size` | `100` | Entries per HTTP request |
| `logshipper.flush_period` | `1s` | Flush interval |
| `logshipper.buffer_size` | `1024` | Buffered channel capacity |

#### Data retention

Applied on startup as TimescaleDB retention policies. Minimum 1 day.

| Key | Default | Description |
|-----|---------|-------------|
| `retention.srvlog_days` | `90` | Srvlog events retention |
| `retention.netlog_days` | `90` | Netlog events retention |
| `retention.applog_days` | `90` | Application log events retention |
| `retention.notification_log_days` | `30` | Notification log retention |
| `retention.rsyslog_stats_days` | `30` | rsyslog statistics retention |
| `retention.metrics_days` | `30` | Internal metrics retention |

#### SMTP (email notifications)

The email backend delivers only when `smtp.host` is set; channels can be created beforehand.

| Key | Default | Description |
|-----|---------|-------------|
| `smtp.host` | `""` | SMTP server hostname |
| `smtp.port` | `587` | SMTP server port |
| `smtp.username` | `""` | SMTP username |
| `smtp.password` | `""` | SMTP password |
| `smtp.from` | `"taillight@localhost"` | Sender address |
| `smtp.tls` | `true` | Use STARTTLS |
| `smtp.auth_type` | `"plain"` | Auth method: `"plain"`, `"crammd5"`, or `""` (no auth) |

#### LDAP authentication

Optional. When enabled, logins are first verified against the directory (Active Directory or FreeIPA); LDAP users are synced to the local database for session/API-key support, and local bcrypt users keep working.

| Key | Default | Description |
|-----|---------|-------------|
| `ldap.enabled` | `false` | Enable LDAP authentication |
| `ldap.url` | `"ldaps://ipa.example.com:636"` | LDAP server URL |
| `ldap.starttls` | `false` | Use STARTTLS on port 389 instead of LDAPS |
| `ldap.tls_skip_verify` | `false` | Skip TLS certificate verification (dev only) |
| `ldap.ca_bundle` | `""` | PEM file of extra trusted CAs added to the system roots |
| `ldap.bind_dn` | `""` | Service account DN for user lookups (AD: use a UPN) |
| `ldap.bind_password` | `""` | Service account password |
| `ldap.user_search_base` | `"cn=users,cn=accounts,dc=example,dc=com"` | Base DN for user searches |
| `ldap.user_filter` | `"(&(objectClass=person)(uid=%s))"` | Filter with `%s` username placeholder (AD: `(sAMAccountName=%s)`) |
| `ldap.group_role_map` | `{}` | Map of group (full DN or bare CN) to role. `admin` grants is_admin; any other value authorizes a regular user. A user in no mapped group is denied login. |

#### Notification engine

Channels and rules are managed via the API or the ALERTS tab in the UI. See [NOTIFICATIONS.md](NOTIFICATIONS.md) for the delivery model.

| Key | Default | Description |
|-----|---------|-------------|
| `notification.enabled` | `false` | Enable the notification engine |
| `notification.rule_refresh_interval` | `30s` | How often rules/channels are reloaded from the database |
| `notification.dispatch_workers` | `4` | Concurrent notification sender goroutines |
| `notification.dispatch_buffer` | `1024` | Internal dispatch queue size |
| `notification.default_silence` | `5m` | After the first alert on a fingerprint, suppress duplicates for this long |
| `notification.default_silence_max` | `15m` | Cap on silence growth while a rule keeps firing digests |
| `notification.default_coalesce` | `0s` | Optional batching window for the first alert (0 = fire immediately) |
| `notification.send_timeout` | `10s` | HTTP timeout for each backend send attempt |

#### Netbox enrichment

| Key | Default | Description |
|-----|---------|-------------|
| `netbox.enabled` | `false` | Enable Netbox enrichment on the netlog detail page |
| `netbox.url` | `""` | Base URL of the Netbox instance |
| `netbox.token` | `""` | API token (prefer the `NETBOX_TOKEN` env var) |
| `netbox.auth_scheme` | `"token"` | `"token"` (legacy `Authorization: Token`) or `"bearer"` (OAuth-style) |
| `netbox.timeout` | `3s` | Per-call HTTP timeout |
| `netbox.cache_ttl` | `10m` | In-memory cache TTL for lookups (including negative results) |
| `netbox.tls_skip_verify` | `false` | Skip TLS verification (self-signed test instances) |

#### AI analysis

Uses a local Ollama instance to produce ops briefings from netlog/srvlog data. Config holds the model/runtime settings only — report schedules, feed selection, and email recipients are managed in the UI and stored in the database (`analysis_schedules`).

| Key | Default | Description |
|-----|---------|-------------|
| `analysis.enabled` | `false` | Enable AI analysis |
| `analysis.ollama_url` | `"http://localhost:11434"` | Ollama API URL |
| `analysis.model` | `"llama3"` | LLM model name |
| `analysis.temperature` | `0.3` | LLM temperature (lower = more deterministic) |
| `analysis.num_ctx` | `8192` | Context window size in tokens |
| `analysis.prompts_dir` | `""` | Override the embedded prompt templates (dir path) |
| `analysis.ollama_timeout` | `"2h"` | HTTP timeout for a single Ollama call |
| `analysis.run_timeout` | `"4h"` | Overall timeout for one analysis run |

### Environment variable overrides

Any config key can be overridden by setting an environment variable. Viper maps nested keys using underscores: `retention.srvlog_days` becomes `RETENTION_SRVLOG_DAYS`, `netbox.token` becomes `NETBOX_TOKEN`. Secret keys (`LOGSHIPPER_API_KEY`, `LDAP_BIND_PASSWORD`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `NETBOX_TOKEN`) are explicitly env-bound so they work even without a config-file entry. The following are typically set in `.env` and should not be duplicated in `config.yml`:

- `DATABASE_URL`
- `LISTEN_ADDR`
- `LOG_LEVEL`
- `AUTH_ENABLED`

### Company-specific deployments

For site-specific rsyslog filters, custom config, and production ports, use `docker-compose.override.yml`. See `docker-compose.override.example.yml` for a reference layout.

## CLI Reference

The `taillight` binary provides the following subcommands. The `--config` flag is global (default: search for `config.yml` in `.` and `/etc/taillight`).

### `serve`

Start the HTTP/SSE server.

```sh
taillight serve
taillight serve --config /etc/taillight/config.yml
```

### `migrate`

Run database migrations using golang-migrate.

```sh
taillight migrate up                # apply all pending migrations
taillight migrate down              # roll back all migrations
taillight migrate down --steps 1    # roll back one migration
taillight migrate version           # show current migration version
taillight migrate force 42          # force set version (use with caution)
```

| Subcommand | Description |
|------------|-------------|
| `up` | Apply all pending migrations |
| `down` | Roll back migrations (`--steps N` for partial rollback) |
| `version` | Show current migration version and dirty status |
| `force <version>` | Force set migration version without running migrations |

| Flag | Default | Description |
|------|---------|-------------|
| `--path` | `migrations` | Path to migrations directory |
| `--steps` | `0` (all) | Number of migrations to roll back (down only) |

### `loadgen-netlog` / `loadgen-srvlog`

Generate random test events. `loadgen-netlog` produces network device logs with realistic hostnames, programs, and messages from Juniper, Cisco, and Arista device profiles; `loadgen-srvlog` produces server logs (Linux, nginx, PostgreSQL, Docker).

```sh
# Direct SQL insert (bypasses rsyslog; still fires trigger -> NOTIFY -> SSE)
taillight loadgen-srvlog -n 1000 --delay 100ms --jitter 200ms

# Via rsyslog (full pipeline over RFC 5424 UDP; netlog=514, srvlog=515)
taillight loadgen-netlog -n 1000 --syslog localhost:1514 --delay 100ms

# TCP transport
taillight loadgen-netlog -n 1000 --syslog localhost:1514 --protocol tcp
```

| Flag | Default | Description |
|------|---------|-------------|
| `-n` | `100` | Number of events to generate |
| `--delay` | `0` | Fixed delay between inserts |
| `--jitter` | `0` | Random jitter added to delay |
| `--syslog` | `""` | Send via syslog instead of SQL (`host:port`) |
| `--protocol` | `udp` | Syslog transport: `udp` or `tcp` |

### `loadgen-applog`

Generate random application log events via the HTTP ingest API.

```sh
taillight loadgen-applog -n 1000 --batch 50 \
  --endpoint http://localhost:8080/api/v1/applog/ingest

# With authentication
taillight loadgen-applog -n 1000 --api-key tl_abc123...
```

| Flag | Default | Description |
|------|---------|-------------|
| `-n` | `100` | Number of events to send |
| `--batch` | `50` | Events per API request (max 1000) |
| `--delay` | `0` | Fixed delay between batches |
| `--jitter` | `0` | Random jitter added to delay |
| `--endpoint` | `http://localhost:8080/api/v1/applog/ingest` | Ingest API endpoint URL |
| `--api-key` | `""` | Bearer token for API authentication |
| `-k`, `--insecure` | `false` | Skip TLS certificate verification |

### `useradd`

Create a new user account.

```sh
taillight useradd --username operator --password changeme123
taillight useradd --username admin --password changeme123 --admin
```

| Flag | Default | Description |
|------|---------|-------------|
| `--username` | *(required)* | Username for the new account |
| `--password` | *(required)* | Password (minimum 8 characters) |
| `--admin` | `false` | Grant admin privileges |

### `apikey`

Generate a database-backed API key for a user. The full key is printed to stdout once.

```sh
taillight apikey --username operator --name "ingest-key" --scopes ingest
taillight apikey --username admin --name "full-access" --scopes read,admin,ingest
```

| Flag | Default | Description |
|------|---------|-------------|
| `--username` | *(required)* | Username to create the key for |
| `--name` | *(required)* | Descriptive name for the key |
| `--scopes` | `ingest` | Comma-separated scopes: `ingest`, `read`, `admin` |

### `import`

Import Juniper syslog reference data from XLSX files. Files are available from Juniper's documentation site. (The same import is available over HTTP: `POST /api/v1/juniper/ref/upload`, admin scope.)

```sh
taillight import --file System_Log_Messages_Junos_OS.xlsx --os junos
taillight import --file System_Log_Messages_Junos_OS_Evolved.xlsx --os junos-evolved
```

| Flag | Default | Description |
|------|---------|-------------|
| `-f`, `--file` | *(required)* | Path to Juniper syslog XLSX file |
| `-o`, `--os` | *(required)* | Target OS: `junos` or `junos-evolved` |

### `version`

Print the build version.

```sh
taillight version
```

## The Log Shipper

`taillight-shipper` is a standalone binary that reads log lines from stdin or tails log files and ships them to the taillight ingest API. It runs independently of the main taillight server. Cross-compiled binaries ship with each [release](https://github.com/lasseh/taillight/releases).

### Modes

| Mode | Command | Description |
|------|---------|-------------|
| Stdin pipe | `./app \| taillight-shipper -c config.yml` | Read lines from a piped process |
| File follow | `taillight-shipper -c config.yml` | Tail one or more log files |
| Both | `./app \| taillight-shipper -c config.yml -t` | Pipe stdin (teed back to stdout) + tail files |

### Build

```sh
cd api && make build-shipper
```

### Configuration

Create a YAML config file (see `api/cmd/taillight-shipper/config.example.yml`):

```yaml
endpoint: http://localhost:8080/api/v1/applog/ingest
api_key: ""
service: my-app
component: ""
batch_size: 100
flush_period: 1s
buffer_size: 1024

files:
  - path: /var/log/myapp/api.log
    service: myapp-api
    component: http
  - path: /var/log/myapp/worker.log
    service: myapp-worker
    component: jobs
```

| Field | Default | Description |
|-------|---------|-------------|
| `endpoint` | *(required)* | Taillight ingest URL |
| `api_key` | `""` | Bearer token for authentication |
| `service` | *(required)* | Default service name (used for stdin, fallback for files) |
| `component` | `""` | Default component name |
| `host` | `os.Hostname()` | Host identifier |
| `batch_size` | `100` | Flush when batch reaches this size |
| `flush_period` | `1s` | Flush at least this often |
| `buffer_size` | `1024` | Buffered channel capacity per handler |
| `tls_skip_verify` | `false` | Skip TLS certificate verification |
| `files` | `[]` | List of files to tail (each can override service, component, host) |

### Line parsing

Each line is parsed as JSON first. If that fails, it is treated as plain text with `INFO` level and the current timestamp.

For JSON lines, the following fields are extracted:

| JSON field | Maps to |
|------------|---------|
| `time` or `timestamp` | Record timestamp (RFC 3339) |
| `level` | Log level (`DEBUG`, `INFO`, `WARN`/`WARNING`, `ERROR`) |
| `msg` or `message` | Log message |
| All other fields | Stored as structured attributes |

### File tailing

- Continuously reads new lines as they are appended
- Re-opens files after logrotate or similar rotation
- Waits for files to appear if they do not exist at startup
- Starts reading from the end of the file (only ships new lines)

### Examples

```sh
# Ship a process's stdout
./my-api | taillight-shipper -c config.yml

# Ship stdout while keeping terminal output
./my-api | taillight-shipper -c config.yml -t

# Tail multiple log files (no stdin)
taillight-shipper -c config.yml
```

## Terminal UI & SSH access

The terminal UI client (`taillight-tui`) and the SSH server that hosts it
(`taillight-wish`) live in a separate repository:
**https://github.com/lasseh/taillight-tui**. Both are HTTP/SSE clients of
this API — see that repo for build, configuration, keybindings, and usage.

## Backup and Restore

TimescaleDB hypertables require special handling for backups:

```sh
# Backup (includes TimescaleDB catalog)
pg_dump -Fc -U taillight -d taillight > taillight.dump

# Restore (TimescaleDB extension must already exist in the target database)
pg_restore -U taillight -d taillight taillight.dump
```

For production, consider `timescaledb-backup` or WAL archiving for point-in-time recovery.

## What's Next

- [Architecture](ARCHITECTURE.md) -- system design, data flow diagrams, deployment topology
- [Internals](INTERNALS.md) -- deep dive into SSE brokers, LISTEN/NOTIFY, schema, auth, analysis
- [Notifications](NOTIFICATIONS.md) -- notification system setup, channels, rules, anti-spam
- [Decision records](adr/) -- ADRs for the standing architecture decisions
- [Interactive API Docs](http://localhost:8080/api/docs) -- Scalar/OpenAPI UI (when running locally)
- [rsyslog Configuration](../rsyslog/README.md) -- rsyslog setup and deployment
