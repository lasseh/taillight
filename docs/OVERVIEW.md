# Taillight

Taillight is a real-time syslog and application log viewer. It streams filtered log events from TimescaleDB to browser clients via Server-Sent Events (SSE). Built for network operations teams running Juniper and other network devices, Taillight provides a fast, filterable live log tail with history search.

## Features

**Real-time streaming** -- SSE pushes new log events to all connected browsers the moment they arrive. PostgreSQL LISTEN/NOTIFY triggers the broadcast, so there is no polling delay.

**Dual log sources** -- Taillight ingests both traditional syslog (via rsyslog with ompgsql) and structured application logs (via an HTTP JSON ingest API). Each source has its own SSE stream, query endpoints, and dashboard.

**Advanced filtering** -- Filter by host, facility, severity, program, syslog tag, message ID, and source IP. Full-text substring search uses PostgreSQL trigram indexes for fast ILIKE queries. Filters apply to both the live stream and historical queries.

**TimescaleDB backend** -- Syslog events and application logs are stored in TimescaleDB hypertables with automatic chunking, compression, and retention policies. Cursor-based pagination provides efficient access to large datasets.

**Dashboards** -- The frontend includes volume charts (events over time), severity breakdowns, per-host statistics, and an rsyslog health dashboard showing input/output/queue metrics from impstats.

**User authentication** -- Session-based authentication with 30-day cookies, database-backed API keys (Bearer `tl_*` prefix), and optional static API keys in config. Authentication can be disabled entirely for trusted-network deployments.

**Juniper reference enrichment** -- Import Juniper syslog reference data from official XLSX files. The UI displays descriptions, causes, and recommended actions for known syslog message IDs.

**Prometheus metrics** -- Exposes HTTP request metrics, SSE client counts, notification throughput, and database pool statistics. Metrics are served on a configurable separate port.

**19 color themes** -- Tokyo Night, Dracula, Catppuccin (Mocha/Macchiato/Frappe/Latte), One Dark, Solarized Dark, Monokai, Nord, Gruvbox Dark, Rose Pine, SynthWave 84, GitHub Light, Atom One Light, Winter, and more.

**Load generators** -- Built-in `loadgen` and `applog-loadgen` commands generate realistic test data for syslog and application logs respectively, useful for development and demo environments.

**LLM analysis (experimental)** -- Optional daily log analysis using a local Ollama instance. Produces a morning operations briefing covering incidents, anomalies, and correlations.

## Technology Stack

| Layer     | Technology                                                    |
|-----------|---------------------------------------------------------------|
| Backend   | Go 1.24+, chi router, pgx/pgxpool, cobra CLI, viper config   |
| Frontend  | Vue 3, TypeScript, Tailwind CSS v4, Pinia, Unovis charts     |
| Database  | TimescaleDB on PostgreSQL 18                                  |
| Ingestion | rsyslog with ompgsql (syslog), HTTP JSON API (application logs) |
| Streaming | Server-Sent Events via PostgreSQL LISTEN/NOTIFY               |
| License   | GPL v3                                                        |

## Quickstart

### Prerequisites

- Docker and Docker Compose

### Start the full stack

```sh
docker compose up -d
```

This starts four services:

| Service    | Port  | Description                       |
|------------|-------|-----------------------------------|
| `frontend` | 3000  | Nginx serving the Vue SPA         |
| `api`      | 8080  | Go backend (REST + SSE)           |
| `rsyslog`  | 1514  | Syslog receiver (UDP + TCP)       |
| `postgres` | 35432 | TimescaleDB (PostgreSQL 18)       |

Open `http://localhost:3000` in a browser.

### Create a user (when auth is enabled)

```sh
docker compose exec api /app useradd --username admin --password changeme123 --admin
```

### Generate test data

Insert syslog events directly into the database:

```sh
docker compose exec api /app loadgen -n 500
```

Send syslog messages through rsyslog (full pipeline):

```sh
docker compose exec api /app loadgen -n 100 --syslog rsyslog:514 --protocol udp
```

Generate application log events via the HTTP ingest API:

```sh
docker compose exec api /app applog-loadgen -n 200 --endpoint http://api:8080/api/v1/applog/ingest
```

### Send real syslog messages

Point your devices or syslog forwarders at `<docker-host>:1514` using UDP or TCP:

```sh
logger -n localhost -P 1514 -d "Test message from $(hostname)"
```

## Configuration

Taillight reads configuration from `config.yaml` (searched in `.`, `/etc/taillight`, `/`). Every field can be overridden with an environment variable of the same name (uppercase), resolved via viper's `AutomaticEnv()`.

### config.yaml reference

```yaml
# PostgreSQL connection string (required).
database_url: "postgres://user:password@localhost:5432/taillight"

# HTTP server listen address.
listen_addr: ":8080"

# Log level: debug, info, warn, error.
log_level: "info"

# Static API keys for applog ingest authentication.
# Leave empty to allow unauthenticated ingest.
api_keys:
  # - "changeme"

# Enable session-based authentication.
# Set to false for trusted-network deployments.
auth_enabled: false

# Require authentication on read endpoints (syslog, applog, meta, stats, SSE).
# Only takes effect when auth_enabled is true.
auth_read_endpoints: false

# CORS allowed origins. Defaults to localhost dev origins when empty.
cors_allowed_origins:
  # - "https://taillight.example.com"

# Serve /metrics on a separate listener (e.g. ":9090").
# Empty disables the metrics endpoint.
metrics_addr: ""

# Database connection pool.
db_max_conns: 10
db_min_conns: 2

# LISTEN/NOTIFY channel buffer size.
notification_buffer_size: 8192

# Ship the API's own logs to the applog ingest endpoint.
logshipper:
  enabled: false
  service: "taillight"
  component: "api"
  # host: ""             # Override hostname (defaults to os.Hostname()).
  min_level: "warn"      # Only ship warn and above to avoid feedback loops.
  # api_key: "changeme"  # Must match an api_keys entry if auth is enabled.

# AI-powered daily syslog analysis using Ollama (experimental).
# analysis:
#   enabled: false
#   ollama_url: "http://localhost:11434"
#   model: "llama3.1:8b"
#   schedule_at: "06:00"       # UTC (HH:MM).
#   temperature: 0.3
#   num_ctx: 8192
```

### Environment variable overrides

Any config field can be set via environment variable. Viper maps field names directly:

```sh
DATABASE_URL=postgres://...  LISTEN_ADDR=:9090  AUTH_ENABLED=true  ./taillight serve
```

## CLI Reference

The `taillight` binary provides the following commands:

### serve

Start the HTTP/SSE server.

```sh
taillight serve
```

### migrate

Run database migrations (golang-migrate).

```sh
taillight migrate up                  # Apply all pending migrations
taillight migrate down                # Roll back all migrations
taillight migrate down --steps 1      # Roll back one migration
taillight migrate version             # Show current migration version
taillight migrate force <version>     # Force set version (recovery)
```

Flags:
- `--path` -- path to migrations directory (default: `migrations`)
- `--steps` -- number of migrations to roll back (down only, default: 0 = all)

### useradd

Create a new user account.

```sh
taillight useradd --username admin --password changeme123 --admin
```

Flags:
- `--username` (required) -- username for the new account
- `--password` (required) -- password (minimum 8 characters)
- `--admin` -- grant admin privileges

### apikey

Generate a database-backed API key for a user. The full key is printed once to stdout.

```sh
taillight apikey --username admin --name "ci-pipeline"
```

Flags:
- `--username` (required) -- username to create the key for
- `--name` (required) -- descriptive name for the key

### loadgen

Generate random syslog events for testing. By default, events are inserted directly into PostgreSQL. Use `--syslog` to send RFC 5424 messages over the network.

```sh
taillight loadgen -n 500
taillight loadgen -n 100 --syslog localhost:1514 --protocol udp
taillight loadgen -n 50 --delay 100ms --jitter 200ms
```

Flags:
- `-n` -- number of events (default: 100)
- `--delay` -- fixed delay between inserts (e.g. `100ms`)
- `--jitter` -- random jitter added to delay (e.g. `200ms`)
- `--syslog` -- send via syslog instead of SQL (`host:port`)
- `--protocol` -- syslog transport: `udp` or `tcp` (default: `udp`)

### applog-loadgen

Generate random application log events via the HTTP ingest API.

```sh
taillight applog-loadgen -n 500
taillight applog-loadgen -n 200 --endpoint https://taillight.example.com/api/v1/applog/ingest --api-key tl_abc123
```

Flags:
- `-n` -- number of events (default: 100)
- `--delay` -- fixed delay between batches
- `--jitter` -- random jitter added to delay
- `--endpoint` -- ingest API URL (default: `http://localhost:8080/api/v1/applog/ingest`)
- `--api-key` -- Bearer token for API authentication
- `--batch` -- events per API request (default: 50, max: 1000)
- `-k, --insecure` -- skip TLS certificate verification

### import

Import Juniper syslog reference data from an official XLSX file.

```sh
taillight import --file junos-syslog-messages.xlsx --os junos
taillight import --file junos-evo-syslog-messages.xlsx --os junos-evolved
```

Flags:
- `-f, --file` (required) -- path to Juniper syslog XLSX file
- `-o, --os` (required) -- target OS: `junos` or `junos-evolved`

### version

Print the build version.

```sh
taillight version
```

## Deployment

### Docker Compose (default)

The default `docker-compose.yml` runs the full stack with an nginx reverse proxy in front of the Vue SPA. The frontend container proxies `/api` requests to the API container. This is the simplest setup and works for single-host deployments.

```sh
docker compose up -d
```

### Separate subdomains

For production deployments where the frontend and API run on separate subdomains (e.g. `taillight.example.com` and `api.taillight.example.com`), set the `API_URL` environment variable on the frontend container and configure CORS on the API:

```yaml
# docker-compose.override.yml
services:
  frontend:
    environment:
      - API_URL=https://api.taillight.example.com
  api:
    environment:
      - CORS_ALLOWED_ORIGINS=https://taillight.example.com
```

### Production nginx with TLS

Place nginx in front of the Docker stack with TLS termination. Key considerations:

- Proxy `/api` to the API container on port 8080
- SSE streams require `proxy_buffering off` and long timeouts
- Set `X-Forwarded-For` and `X-Real-IP` headers for accurate client IPs

### Standalone API (no Docker)

Build and run the API binary directly:

```sh
cd api
make build
./taillight migrate up
./taillight serve
```

The frontend can be built separately with `npm run build` and served by any static file server.

## Backup and Restore

Taillight uses TimescaleDB, which requires TimescaleDB-aware backup tools.

### Backup

```sh
pg_dump -Fc -h localhost -p 35432 -U taillight taillight > taillight.dump
```

### Restore

```sh
pg_restore -Fc -h localhost -p 35432 -U taillight -d taillight taillight.dump
```

Both the source and target databases must have the TimescaleDB extension installed. Use `timescaledb-parallel-copy` for large dataset imports.
