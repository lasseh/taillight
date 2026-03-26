# Taillight Overview

## Introduction

Taillight is a real-time srvlog and application log viewer built for network operations teams. It streams filtered log events from a TimescaleDB database to browser clients via Server-Sent Events (SSE), providing instant visibility into what is happening across your infrastructure.

The primary use case is monitoring syslog output from network devices (Juniper, Cisco, Arista, and others) as srvlog events, but taillight also supports application logs ingested over HTTP. Events flow from rsyslog into PostgreSQL, through a Go backend that fans them out to connected browsers, and into a Vue 3 frontend with live filtering, dashboards, and search.

## Features

### Real-time streaming

- Srvlog and application log events pushed to the browser via SSE
- Per-client server-side filtering -- only matching events are sent to each connection
- Automatic reconnection with `Last-Event-ID` resume

### Filtering and search

- Filter by host, facility, severity, program, and free-text search
- Cursor-based pagination for browsing historical events
- Metadata endpoints for populating filter dropdowns (hosts, programs, facilities, tags, services, components)

### Dual log sources

- **Srvlog** -- ingested by rsyslog via ompgsql into PostgreSQL; a trigger fires `pg_notify` to push events to the Go backend in real time
- **Application logs** -- ingested via `POST /api/v1/applog/ingest` as JSON batches; stored in a separate hypertable with the same SSE fan-out

### Dashboard and analytics

- Aggregated volume charts with selectable time ranges
- Per-host breakdown and top-host rankings
- Severity distribution summaries for both srvlog and applog
- Continuous aggregates in TimescaleDB for fast dashboard queries
- Internal metrics history (DB pool, SSE clients, event rates) with volume charts

### Authentication and authorization

- Session-based login with cookie authentication
- API key support with scoped access: `read`, `ingest`, `admin`
- Configurable: run fully open during development or lock down for production
- User management: create users, revoke sessions, toggle active status

### Notification system

- Alert rules that match srvlog events by host, severity, facility, or program patterns
- Notification channels: Slack, webhooks, and email (SMTP)
- Anti-spam: burst windows collect matching events before firing, cooldown suppresses repeat alerts
- Rate limiting and circuit breakers per channel
- Managed via the API or the ALERTS tab in the UI

### Juniper netlog reference

- Import Juniper netlog message definitions from official XLSX files
- Lookup endpoint for enriching events with description, cause, and recommended action
- Supports both Junos and Junos Evolved

### Monitoring

- Prometheus `/metrics` endpoint with HTTP request metrics, SSE client counts, DB pool gauges, and notification counters
- Optional separate metrics listener to isolate metrics from the public API
- Internal metrics snapshots persisted to TimescaleDB for historical charts

### Log shipper

- Standalone `taillight-shipper` binary that tails log files or reads stdin and ships lines to the ingest API
- JSON line parsing with automatic field extraction
- File rotation support and graceful shutdown

### AI analysis (experimental)

- Daily srvlog analysis using a local Ollama LLM instance
- Scheduled morning briefings covering incidents, anomalies, and correlations
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
| TypeScript | 5.9 | Frontend type safety |
| Pinia | 3.x | Vue state management |
| Tailwind CSS | v4 | Utility-first styling |
| Vite | 7.x | Frontend build tool and dev server |
| Unovis | 1.6+ | Chart library for dashboards |
| TimescaleDB | PG 18 | Time-series hypertables with compression and retention |
| rsyslog | -- | Syslog collector with ompgsql output module |
| Prometheus | -- | Metrics collection and scraping |
| Docker Compose | -- | One-command deployment of the full stack |

## Project Layout

```
taillight/
├── api/                           # Go backend
│   ├── cmd/
│   │   ├── taillight/             # Main CLI binary (serve, migrate, loadgen, etc.)
│   │   └── taillight-shipper/     # Standalone log file shipper
│   ├── internal/
│   │   ├── analyzer/              # AI-powered srvlog analysis
│   │   ├── auth/                  # Session + API key middleware, password hashing
│   │   ├── broker/                # SSE fan-out brokers (SrvlogBroker, AppLogBroker)
│   │   ├── config/                # Viper-based configuration loader
│   │   ├── handler/               # HTTP handlers (one file per domain)
│   │   ├── httputil/              # Shared HTTP utilities
│   │   ├── metrics/               # Prometheus collectors + HTTP middleware
│   │   ├── model/                 # Domain types, filter parsing, cursor pagination
│   │   ├── notification/          # Rule engine, burst detection, cooldown, backends
│   │   ├── ollama/                # Ollama LLM client
│   │   ├── postgres/              # Store (pgx queries), Listener (LISTEN/NOTIFY)
│   │   └── scheduler/             # Cron-like scheduler for analysis runs
│   ├── migrations/                # SQL migration files (golang-migrate)
│   ├── pkg/
│   │   └── logshipper/            # slog handler that ships logs to the ingest API
│   ├── docs/                      # Embedded OpenAPI spec
│   ├── config.yml.example         # Application tuning reference
│   ├── Dockerfile                 # Multi-stage Go build
│   ├── Makefile                   # Build, test, lint targets
│   └── go.mod
├── frontend/                      # Vue 3 SPA
│   ├── src/                       # TypeScript source, components, stores, views
│   ├── Dockerfile                 # Multi-stage frontend build
│   ├── Makefile                   # Install, dev, lint, build targets
│   ├── nginx.conf                 # Production static file server config
│   └── package.json
├── rsyslog/                       # Modular rsyslog config for network devices
├── docs/                          # Project documentation
├── .env.example                   # Per-deployment settings template
├── docker-compose.yml             # Full stack: PostgreSQL, API, rsyslog, frontend
├── Makefile                       # Root Makefile (delegates to api/ and frontend/)
└── README.md
```

## Getting Started

### Prerequisites

- Docker and Docker Compose (for the full stack)
- Go 1.26+ (for local backend development)
- Node.js 18+ and npm (for local frontend development)

### Docker Compose quickstart

Clone the repository and start the stack:

```sh
git clone https://github.com/lasseh/taillight.git
cd taillight
cp .env.example .env   # review and adjust for your environment
docker compose up -d
```

This starts four services:

| Service | URL | Description |
|---------|-----|-------------|
| Frontend | `http://localhost:3000` | Web UI |
| API | `http://localhost:8080` | REST + SSE backend |
| PostgreSQL | `localhost:5432` | TimescaleDB database |
| rsyslog | `localhost:1514` (UDP/TCP) | Syslog collector |

### Create a user

Authentication is disabled by default. To enable it, set `AUTH_ENABLED=true` in `.env`, then create a user:

```sh
docker compose exec api /app useradd --username admin --password adminadmin
```

### Generate test data

Populate the database with realistic srvlog events:

```sh
# Direct SQL insert into srvlog_events (fast, bypasses rsyslog)
docker compose exec api /app loadgen -n 500 --delay 50ms --jitter 100ms

# Via rsyslog (full pipeline: UDP -> rsyslog -> ompgsql -> srvlog_events -> NOTIFY)
docker compose exec api /app loadgen -n 500 --syslog rsyslog:514 --delay 50ms

# Application log events (via HTTP ingest API)
docker compose exec api /app applog-loadgen -n 500 --batch 50 \
  --endpoint http://localhost:8080/api/v1/applog/ingest
```

### Send syslog messages from the host

The rsyslog container listens on port 1514 (mapped from container port 514) and routes messages to the srvlog pipeline. Send RFC 5424 messages:

```sh
# Single test message
echo '<14>1 2025-02-07T12:00:00Z router01 rpd 1234 RPD_BGP_NEIGHBOR_STATE_CHANGED - BGP peer 10.0.0.1 state changed to Established' \
  | nc -u -w1 localhost 1514

# Using logger
logger -n localhost -P 1514 -d --rfc5424 -p local7.warning -t rpd \
  "BGP peer 10.0.0.1 state changed to Established"
```

### Default ports

| Service | Host Port | Container Port | Environment Variable |
|---------|-----------|----------------|----------------------|
| PostgreSQL | 5432 | 5432 | `POSTGRES_BIND` |
| API | 8080 | 8080 | `API_HOST_PORT` |
| rsyslog | 1514 | 514 | `RSYSLOG_HOST_PORT` |
| Frontend | 3000 | 80 | `FRONTEND_HOST_PORT` |

Host ports only affect access from the host machine. Container-to-container communication always uses internal ports.

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
| `RSYSLOG_HOST_PORT` | `1514` | Host port for srvlog syslog input (set to `514` in production) |
| `FRONTEND_HOST_PORT` | `3000` | Host port for the web UI |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `AUTH_ENABLED` | `false` | Enable authentication (sessions + API keys) |
| `API_URL` | *(empty)* | Frontend API URL (empty = same-origin proxy via nginx) |

### `api/config.yml` -- application tuning

Settings that rarely change between environments. Viper priority: defaults -> `config.yml` -> environment variables. Environment variables always override file values.

#### Authentication

| Key | Default | Description |
|-----|---------|-------------|
| `auth_read_endpoints` | `true` | Require authentication on read endpoints (srvlog, applog, meta, stats, SSE). Only takes effect when `AUTH_ENABLED` is true. |

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
| `db_max_conns` | `10` | Maximum connections in the pgx pool |
| `db_min_conns` | `2` | Minimum idle connections in the pgx pool |

#### LISTEN/NOTIFY buffer

| Key | Default | Description |
|-----|---------|-------------|
| `notification_buffer_size` | `8192` | Channel buffer for PostgreSQL LISTEN/NOTIFY notifications. Increase if you see "notification channel near capacity" warnings. |

#### Log shipper

The API can ship its own logs to its applog ingest endpoint (logs remain on stdout regardless).

| Key | Default | Description |
|-----|---------|-------------|
| `logshipper.enabled` | `true` | Enable log shipping |
| `logshipper.service` | `"taillight"` | Service name in shipped logs |
| `logshipper.component` | `"api"` | Component name in shipped logs |
| `logshipper.host` | `os.Hostname()` | Override hostname |
| `logshipper.min_level` | `"warn"` | Minimum level to ship (`debug`, `info`, `warn`, `error`) |
| `logshipper.api_key` | `""` | Bearer token (must match an `api_keys` entry when auth is enabled) |

#### Data retention

Applied on startup as TimescaleDB retention policies. Minimum 1 day.

| Key | Default | Description |
|-----|---------|-------------|
| `retention.srvlog_days` | `90` | Srvlog events retention |
| `retention.applog_days` | `90` | Application log events retention |
| `retention.notification_log_days` | `30` | Notification log retention |
| `retention.rsyslog_stats_days` | `30` | rsyslog statistics retention |
| `retention.metrics_days` | `30` | Internal metrics retention |

#### SMTP (email notifications)

The email notification backend is only enabled when `smtp.host` is set.

| Key | Default | Description |
|-----|---------|-------------|
| `smtp.host` | `""` | SMTP server hostname |
| `smtp.port` | `587` | SMTP server port |
| `smtp.username` | `""` | SMTP username |
| `smtp.password` | `""` | SMTP password |
| `smtp.from` | `"taillight@localhost"` | Sender address |
| `smtp.tls` | `true` | Use STARTTLS |
| `smtp.auth_type` | `"plain"` | Auth method: `"plain"`, `"crammd5"`, or `""` (no auth) |

#### Notification engine

Channels and rules are managed via the API or the ALERTS tab in the UI.

| Key | Default | Description |
|-----|---------|-------------|
| `notification.enabled` | `false` | Enable the notification engine |
| `notification.rule_refresh_interval` | `30s` | How often rules/channels are reloaded from the database |
| `notification.dispatch_workers` | `4` | Concurrent notification sender goroutines |
| `notification.dispatch_buffer` | `1024` | Internal dispatch queue size |
| `notification.default_burst_window` | `30s` | Collect matching events into one notification for this duration |
| `notification.default_cooldown` | `5m` | Suppress repeat notifications per rule after firing |
| `notification.send_timeout` | `10s` | HTTP timeout for each backend send |
| `notification.global_rate_limit` | `100` | Reserved for future use |

#### AI analysis

Uses a local Ollama instance to produce daily ops briefings from srvlog data.

| Key | Default | Description |
|-----|---------|-------------|
| `analysis.enabled` | `false` | Enable AI analysis |
| `analysis.ollama_url` | `"http://localhost:11434"` | Ollama API URL |
| `analysis.model` | `"llama3.1:8b"` | LLM model name |
| `analysis.schedule_at` | `"06:00"` | Daily run time in UTC (HH:MM) |
| `analysis.temperature` | `0.3` | LLM temperature (lower = more deterministic) |
| `analysis.num_ctx` | `8192` | Context window size in tokens |

### Environment variable overrides

Any config key can be overridden by setting an environment variable. Viper maps nested keys using underscores: `retention.srvlog_days` becomes `RETENTION_SRVLOG_DAYS`. The following are typically set in `.env` and should not be duplicated in `config.yml`:

- `DATABASE_URL`
- `LISTEN_ADDR`
- `LOG_LEVEL`
- `AUTH_ENABLED`

### Company-specific deployments

For site-specific rsyslog filters, custom config, and production ports, use `docker-compose.override.yml`. See `docker-compose.override.example.yml` for a reference layout.

## CLI Reference

The `taillight` binary provides the following subcommands:

### `serve`

Start the HTTP/SSE server.

```sh
taillight serve
taillight serve --config /etc/taillight/config.yml
```

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `config.yml` | Path to configuration file |

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

### `loadgen`

Generate random srvlog events for testing. Events use realistic hostnames, programs, and messages from Juniper, Cisco, and Arista device profiles.

```sh
# Direct SQL insert (bypasses rsyslog)
taillight loadgen -n 1000 --delay 100ms --jitter 200ms

# Via rsyslog (full pipeline over RFC 5424 UDP)
taillight loadgen -n 1000 --syslog localhost:1514 --delay 100ms

# TCP transport
taillight loadgen -n 1000 --syslog localhost:1514 --protocol tcp
```

| Flag | Default | Description |
|------|---------|-------------|
| `-n` | `100` | Number of events to generate |
| `--delay` | `0` | Fixed delay between inserts |
| `--jitter` | `0` | Random jitter added to delay |
| `--syslog` | `""` | Send via syslog instead of SQL (`host:port`) |
| `--protocol` | `udp` | Syslog transport: `udp` or `tcp` |

### `applog-loadgen`

Generate random application log events via the HTTP ingest API.

```sh
taillight applog-loadgen -n 1000 --batch 50 \
  --endpoint http://localhost:8080/api/v1/applog/ingest

# With authentication
taillight applog-loadgen -n 1000 --api-key tl_abc123...
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

Import Juniper netlog reference data from XLSX files. Files are available from Juniper's documentation site.

```sh
taillight import --file juniper-netlog.xlsx --os junos
taillight import --file juniper-evolved.xlsx --os junos-evolved
```

| Flag | Default | Description |
|------|---------|-------------|
| `-f`, `--file` | *(required)* | Path to Juniper netlog XLSX file |
| `-o`, `--os` | *(required)* | Target OS: `junos` or `junos-evolved` |

### `version`

Print the build version.

```sh
taillight version
```

## The Log Shipper

`taillight-shipper` is a standalone binary that reads log lines from stdin or tails log files and ships them to the taillight ingest API. It runs independently of the main taillight server.

### Modes

| Mode | Command | Description |
|------|---------|-------------|
| Stdin pipe | `./app \| taillight-shipper -c config.yml` | Read lines from a piped process |
| File follow | `taillight-shipper -c config.yml` | Tail one or more log files |
| Both | `./app \| taillight-shipper -c config.yml -t` | Pipe stdin + tail files simultaneously |

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
- [Internals](INTERNALS.md) -- deep dive into SSE brokers, LISTEN/NOTIFY, schema, auth, frontend stores
- [Notifications](NOTIFICATIONS.md) -- notification system setup, channels, rules, anti-spam
- [Interactive API Docs](http://localhost:8080/api/docs) -- Scalar/OpenAPI UI (when running locally)
- [rsyslog Configuration](../rsyslog/README.md) -- rsyslog setup and deployment
