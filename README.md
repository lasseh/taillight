# taillight

[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)
[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://go.dev)

Real-time syslog and application log viewer. Streams filtered log events from TimescaleDB to browser clients via Server-Sent Events.

## Features

- **Real-time streaming** — syslog and app log events pushed to the browser via SSE
- **Advanced filtering** — filter by host, facility, severity, program, and full-text search
- **Dual log sources** — syslog (via rsyslog/ompgsql) and application logs (via HTTP ingest API)
- **TimescaleDB backend** — hypertables with compression and retention policies for efficient storage
- **Dashboard** — aggregated views with continuous aggregates for fast analytics
- **User authentication** — session-based auth with API key support for ingest
- **Juniper reference data** — import syslog message definitions from Juniper XLSX files
- **Load generators** — built-in tools for generating test syslog and app log events
- **Notification system** — pluggable alert rules with Slack and webhook backends, burst windows, and cooldown anti-spam
- **Prometheus metrics** — `/metrics` endpoint for monitoring
- **Docker Compose** — one-command deployment of the full stack

## Architecture

```
rsyslog (ompgsql) -> PostgreSQL -> LISTEN/NOTIFY -> Go SSE backend -> browser EventSource
```

1. rsyslog filters and inserts syslog events into `syslog_events` (TimescaleDB hypertable)
2. A PostgreSQL trigger fires `pg_notify('syslog_ingest', id)` on each INSERT
3. The Go backend holds a persistent `LISTEN` connection and fans out events to SSE clients
4. The Vue frontend connects via `EventSource` for live updates and queries the API for history

## Screenshots

**Home** — at-a-glance severity stats, top hosts, and recent high-severity events for both syslog and applog

![Home](docs/screenshots/home.png)

**Syslog stream** — real-time filtered event stream with severity, host, program, and full-text search

![Syslog stream](docs/screenshots/syslog-stream.png)

**Dashboard** — per-host volume charts with selectable time ranges

![Dashboard](docs/screenshots/dashboard.png)

## Components

| Directory | Description |
|-----------|-------------|
| `api/` | Go SSE backend (chi router, pgx, LISTEN/NOTIFY) |
| `frontend/` | Vue 3 SPA (TypeScript, Tailwind CSS) |
| `rsyslog/` | Modular rsyslog filtering config for Juniper network devices |
| `docs/` | Design documents and improvement notes |

## Quickstart

### Docker Compose (full stack)

```sh
cp .env.example .env   # review and adjust for your environment
docker compose up -d
```

This starts PostgreSQL (TimescaleDB), the API, rsyslog (with ompgsql), and the frontend. The frontend is available at `http://localhost:3000`, the API at `http://localhost:8080`.

### Default ports

| Service    | Host Port | Container Port | Variable             |
|------------|-----------|----------------|----------------------|
| PostgreSQL | 5432      | 5432           | `POSTGRES_HOST_PORT` |
| API        | 8080      | 8080           | `API_HOST_PORT`      |
| rsyslog    | 1514      | 514            | `RSYSLOG_HOST_PORT`  |
| Frontend   | 3000      | 80             | `FRONTEND_HOST_PORT` |

Host ports only affect access from the host machine. Container-to-container communication always uses internal ports.

### Create a user

```sh
docker compose exec api /app useradd --username admin --password admin
```

### Generate test data

The built-in load generators are the easiest way to populate the database:

```sh
# Generate syslog events (direct SQL insert, bypasses rsyslog)
docker compose exec api /app loadgen -n 100 --delay 100ms --jitter 200ms

# Generate syslog events via rsyslog (full pipeline: UDP → rsyslog → ompgsql → PostgreSQL)
docker compose exec api /app loadgen -n 100 --syslog rsyslog:514 --delay 100ms

# Or from the host (outside Docker):
./taillight loadgen -n 100 --syslog localhost:1514 --delay 100ms

# Generate app log events (via HTTP ingest API)
docker compose exec api /app applog-loadgen -n 100 --batch 50 --endpoint http://localhost:8080/api/v1/applog/ingest
```

### Send syslog messages

The rsyslog container listens on UDP/TCP 1514 (mapped from container port 514). Send RFC 5424 messages from your host:

```sh
# Single RFC 5424 test message
echo '<14>1 2025-02-07T12:00:00Z router01 rpd 1234 RPD_BGP_NEIGHBOR_STATE_CHANGED - BGP peer 10.0.0.1 state changed to Established' | nc -u -w1 localhost 1514

# Using logger with RFC 5424 format
logger -n localhost -P 1514 -d --rfc5424 -p local7.warning -t rpd "BGP peer 10.0.0.1 state changed to Established"
```

### Local development

**API:**

```sh
cd api
cp config.yml.example config.yml  # fill in real database credentials
make build
make test
make lint
```

**CLI Commands:**

```sh
# Start the HTTP/SSE server
./taillight serve

# Database migrations
./taillight migrate up              # Apply all pending migrations
./taillight migrate down --steps 1  # Roll back one migration
./taillight migrate version         # Show current version

# Generate syslog events (direct SQL insert)
./taillight loadgen -n 1000 --delay 100ms --jitter 200ms

# Generate syslog events via rsyslog (RFC 5424 over UDP)
./taillight loadgen -n 1000 --syslog localhost:1514 --delay 100ms

# Generate random app log events (via HTTP ingest API)
./taillight applog-loadgen -n 1000 --batch 50 --endpoint http://localhost:8080/api/v1/applog/ingest

# Import Juniper syslog reference data from XLSX
./taillight import --file juniper-syslog.xlsx --os junos
```

**Frontend:**

```sh
cd frontend
npm install
npm run dev
```

## Configuration

Configuration is split across two files to keep deployment simple:

### `.env` — per-deployment settings

Passwords, ports, and feature toggles that change between environments. Docker Compose reads this automatically and passes values as environment variables.

| Variable | Default | Description |
|----------|---------|-------------|
| `POSTGRES_PASSWORD` | `taillight` | Database password (shared by all containers) |
| `POSTGRES_HOST_PORT` | `5432` | Host port for PostgreSQL |
| `API_HOST_PORT` | `8080` | Host port for the API |
| `RSYSLOG_HOST_PORT` | `1514` | Host port for syslog (set to `514` in production) |
| `FRONTEND_HOST_PORT` | `3000` | Host port for the web UI |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `AUTH_ENABLED` | `false` | Enable authentication (sessions + API keys) |
| `API_URL` | *(empty)* | Frontend API URL (empty = same-origin proxy via nginx) |

### `api/config.yml` — application tuning

Settings that rarely change between environments: CORS origins, connection pool sizes, retention policies, notification engine, SMTP, and AI analysis.

```yaml
# CORS allowed origins (empty = allow all, for dev)
cors_allowed_origins:
  - "https://taillight.example.com"

# Database pool
db_max_conns: 10
db_min_conns: 2

# Data retention (days)
retention:
  syslog_days: 90
  applog_days: 90
```

Environment variables always override config file values (Viper priority: defaults → config.yml → env vars). Settings like `DATABASE_URL`, `LISTEN_ADDR`, `LOG_LEVEL`, and `AUTH_ENABLED` are set in `.env` and should not be added to `config.yml`.

### Company-specific deployments

For site-specific rsyslog filters, custom config, and production ports, use `docker-compose.override.yml`. See `docker-compose.override.example.yml` for a reference layout.

## Production Deployment (nginx)

For production, deploy behind nginx for TLS termination, rate limiting, and request size limits.

```nginx
http {
    # Rate limiting zones
    limit_req_zone $binary_remote_addr zone=ingest:10m rate=100r/s;
    limit_req_zone $binary_remote_addr zone=api:10m rate=50r/s;

    # Request body size limit (protects against memory exhaustion)
    client_max_body_size 1m;

    upstream taillight {
        server 127.0.0.1:8080;
        keepalive 32;
    }

    server {
        listen 443 ssl;
        http2 on;
        server_name taillight.example.com;

        ssl_certificate     /etc/ssl/certs/taillight.crt;
        ssl_certificate_key /etc/ssl/private/taillight.key;

        # Ingest endpoint: rate limited, authenticated via API key
        location /api/v1/applog/ingest {
            limit_req zone=ingest burst=200 nodelay;
            client_max_body_size 5m;  # Allow larger batches for ingest

            proxy_pass http://taillight;
            proxy_http_version 1.1;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Request-ID $request_id;
        }

        # SSE streams: long-lived connections, no buffering
        location ~ ^/api/v1/(syslog|applog)/stream$ {
            proxy_pass http://taillight;
            proxy_http_version 1.1;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header Connection '';

            # Disable buffering for SSE
            proxy_buffering off;
            proxy_cache off;

            # Long timeout for SSE connections
            proxy_read_timeout 24h;
            proxy_send_timeout 24h;
        }

        # REST API: standard rate limiting
        location /api/ {
            limit_req zone=api burst=100 nodelay;

            proxy_pass http://taillight;
            proxy_http_version 1.1;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Request-ID $request_id;
        }

        # Health check (no rate limiting)
        location /health {
            proxy_pass http://taillight;
        }

        # Prometheus metrics (restrict to internal network)
        location /metrics {
            allow 10.0.0.0/8;
            allow 172.16.0.0/12;
            allow 192.168.0.0/16;
            deny all;
            proxy_pass http://taillight;
        }

        # Frontend static files (if serving from same domain)
        location / {
            root /var/www/taillight;
            try_files $uri $uri/ /index.html;
        }
    }

    # Redirect HTTP to HTTPS
    server {
        listen 80;
        server_name taillight.example.com;
        return 301 https://$server_name$request_uri;
    }
}
```

**Key points:**
- `client_max_body_size`: Limits request body to prevent memory exhaustion
- SSE endpoints: Disable buffering, long timeouts for persistent connections
- Rate limiting: Separate zones for ingest vs regular API calls
- `/metrics`: Restricted to internal networks only
- `X-Request-ID`: Propagated for log correlation

## Backup & Restore

TimescaleDB hypertables require special handling for backups:

```sh
# Backup (includes TimescaleDB catalog)
pg_dump -Fc -U taillight -d taillight > taillight.dump

# Restore (TimescaleDB extension must already exist)
pg_restore -U taillight -d taillight taillight.dump
```

For production, consider `timescaledb-backup` or WAL archiving for point-in-time recovery.

## Reference Data

Juniper syslog XLSX files are not included in the repository. Download them from Juniper's documentation site and place them in the `api/` directory before running the import command.

## Documentation

- [Overview](docs/OVERVIEW.md) -- features, quickstart, configuration, CLI reference
- [Architecture](docs/ARCHITECTURE.md) -- system design, data flow diagrams, deployment topology
- [Internals](docs/INTERNALS.md) -- deep dive into SSE brokers, LISTEN/NOTIFY, schema, auth, frontend stores
- [Notifications](docs/NOTIFICATIONS.md) -- notification system setup, channels, rules, anti-spam, and API reference
- [Notification Design](docs/NOTIFICATION_SYSTEM_DESIGN.md) -- original system design document and architecture decisions
- [AI Analysis Setup](docs/ai-analysis-setup.md) -- local Ollama-based AI briefings from syslog data
- [API Reference](api/API.md) -- HTTP endpoints
- [rsyslog README](rsyslog/README.md) -- rsyslog configuration and deployment

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, commit conventions, and PR guidelines.

## License

This project is licensed under the GNU General Public License v3.0 — see the [LICENSE](LICENSE) file for details.
