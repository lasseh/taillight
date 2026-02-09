# rsyslog Juniper Filtering

Modular rsyslog configuration for receiving, filtering, and routing syslog from Juniper network devices (MX, SRX, EX, QFX). Designed to reduce noise from high-volume senders while preserving operationally important events.

## Overview

Juniper devices emit thousands of syslog messages per minute -- chassis polls, SNMP traps, session logs, scheduler ticks, and routine daemon output. This configuration applies layered filters to drop the noise and route what matters to local logs, LibreNMS, Oxidized, and optional remote collectors.

### Processing pipeline

```
UDP/TCP input (514, 1514)
  -> parse RFC 5424 structured data (mmpstrucdata)
  -> capture critical severity (emerg/alert/crit) before any filtering
  -> filter by $msgid        (fastest, exact event name match)
  -> route UI_COMMIT events  (Oxidized, Resin, commit log)
  -> filter by $programname  (daemon-level drops)
  -> filter by facility      (local7 info noise)
  -> filter by severity      (drop debug globally)
  -> filter by hostname/IP   (optional, all commented out)
  -> output to LibreNMS, per-host log files, PostgreSQL, remote forwarding
```

## Project structure

```
rsyslog.conf              Main config -- global settings, queue tuning, includes
conf.d/
  00-modules.conf         Module loading (imudp, imtcp, mmpstrucdata, omprog, ...)
  01-templates.conf       Output format templates (syslog, JSON, LibreNMS, PostgreSQL, debug)
  02-outputs.conf         Output rulesets (local files, remote, LibreNMS, JSON, PostgreSQL)
  10-inputs.conf          UDP/TCP listeners bound to network_devices ruleset
  20-ruleset.conf         Main processing ruleset -- filter chain and output routing
filters/
  05-by-msgid.conf        Primary filter by RFC 5424 msgid (event name)
  06-ui-commit-trigger.conf  Route UI_COMMIT to Oxidized/Resin/commit log
  10-by-programname.conf  Daemon-level drops (cron, ntpd, mib2d, sshd, pfed, ...)
  30-by-facility.conf     Facility-based filters (local7 info)
  40-by-severity.conf     Global severity threshold (drop debug)
  50-by-hostname.conf     Per-host filters (examples only, all commented out)
tests/
  test-messages.txt       RFC 5424 test fixtures -- messages to drop and to keep
  test-filters.sh         Validation test suite (5 tests)
Makefile                  Build, test, deploy, and service management
Dockerfile.test           Docker-based test environment
docker-compose.yml        Run tests in Docker
```

### Include order

Files are included explicitly in `rsyslog.conf`, not by glob. The numeric prefixes match the actual load order. Output rulesets (`02-outputs.conf`) must load before the main ruleset (`20-ruleset.conf`) because `call` requires the target ruleset to already be defined.

## Usage

### Validate configuration

```sh
make validate
```

Copies config to a temp directory, rewrites paths, and runs `rsyslogd -N1` syntax check.

### Run tests

```sh
make test
```

Runs `tests/test-filters.sh` which validates:
1. Full configuration syntax
2. Individual filter file syntax (each wrapped in a minimal rsyslog config)
3. Test fixture file exists and has content
4. All `include(file=...)` references resolve to real files
5. Behavioral validation -- starts rsyslog, sends test messages, verifies correct messages are dropped/kept

### Run tests in Docker

```sh
docker compose run --rm test
```

Builds a Debian container with rsyslog installed, validates config on build, and runs the full test suite. No local rsyslog installation required.

### Deploy

```sh
make deploy        # backs up current config, copies files to /etc/rsyslog.d/
make check         # validates deployed config in-place
make safe-restart  # validates then restarts rsyslog
```

### Other targets

```sh
make help          # list all targets
make diff          # compare local vs deployed config
make status        # service status and listening ports
make logs          # tail per-host log files
make firewall      # open syslog ports (ufw/firewalld auto-detect)
make install-deps  # install rsyslog packages (Debian/RHEL auto-detect)
```

## Filter design

Filters are applied cheapest-first:

| Filter file | Method | What it drops |
|---|---|---|
| `05-by-msgid.conf` | `$msgid ==` exact match | Chassis polls, RPD scheduler, RT_FLOW sessions, SNMP traps, config audit, LLDP neighbor-up, PFE stats, license checks, BGP I/O, kernel routine |
| `06-ui-commit-trigger.conf` | `$msgid ==` | Routes UI_COMMIT/UI_COMMIT_COMPLETED to commit log, Oxidized, and Resin |
| `10-by-programname.conf` | `$programname ==` | cron, ntpd, mib2d, dcd, lacpd, cosd, alarmd, sshd, pfed |
| `30-by-facility.conf` | `$syslogfacility ==` | local7 info-level messages |
| `40-by-severity.conf` | `$syslogseverity ==` | All debug (severity 7) |
| `50-by-hostname.conf` | `$hostname`/`$fromhost-ip` | Nothing (examples only) |

Every filter that drops messages has exception keywords checked via `re_match(tolower($msg), "...")` for case-insensitive matching. Messages containing words like `error`, `fail`, `critical`, `down`, `denied`, or `alarm` pass through even if the event type is normally dropped.

## Customization

- **Add a new msgid filter**: Add a block to `filters/05-by-msgid.conf`
- **Add a new daemon filter**: Add a block to `filters/10-by-programname.conf`
- **Filter specific hosts**: Uncomment and edit examples in `filters/50-by-hostname.conf`
- **Enable PostgreSQL output**: Uncomment `module(load="ompgsql")` in `conf.d/00-modules.conf`, the `PgSQLInsert` template in `conf.d/01-templates.conf`, the `output_pgsql` ruleset in `conf.d/02-outputs.conf`, and `call output_pgsql` in `conf.d/20-ruleset.conf`. Requires `rsyslog-pgsql` package and the DDL below.
- **Enable remote forwarding**: Uncomment `call output_remote` in `conf.d/20-ruleset.conf`
- **Enable JSON output**: Add `call output_json` to the output phase in `conf.d/20-ruleset.conf`
- **Enable Prometheus stats**: Uncomment `impstats` and `rsyslog_stats` in `conf.d/00-modules.conf` (requires [rsyslog_exporter](https://github.com/digitalocean/rsyslog_exporter))
- **Debug a device**: Uncomment the debug block in `conf.d/20-ruleset.conf` and set the target IP

## PostgreSQL output

Optional output that writes filtered syslog events to PostgreSQL for a live log viewer SSE backend. All four config sections (`00-modules`, `01-templates`, `02-outputs`, `20-ruleset`) are commented out by default.

### Schema

```sql
CREATE TABLE syslog_events (
    id              BIGINT GENERATED ALWAYS AS IDENTITY,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    reported_at     TIMESTAMPTZ NOT NULL,
    hostname        TEXT        NOT NULL,
    fromhost_ip     INET        NOT NULL,
    programname     TEXT        NOT NULL DEFAULT '',
    msgid           TEXT        NOT NULL DEFAULT '',
    severity        SMALLINT    NOT NULL,
    facility        SMALLINT    NOT NULL,
    syslogtag       TEXT        NOT NULL DEFAULT '',
    structured_data TEXT,
    message         TEXT        NOT NULL,
    raw_message     TEXT
);
-- No PRIMARY KEY: append-only log table does not need uniqueness enforcement,
-- and omitting it avoids the TimescaleDB requirement that unique constraints
-- must include the partition column (received_at).

-- Per-host drill-down
CREATE INDEX idx_syslog_host_received
    ON syslog_events (hostname, received_at DESC);

-- Fast severity filter (only crit/alert/emerg, severity <= 3)
CREATE INDEX idx_syslog_severity_received
    ON syslog_events (severity, received_at DESC)
    WHERE severity <= 3;
```

For standalone PostgreSQL (no TimescaleDB) you may want a time index on `received_at`. A BRIN index is ideal for this append-mostly workload -- much smaller than a B-tree while still enabling partition pruning on time ranges:

```sql
CREATE INDEX idx_syslog_received_brin
    ON syslog_events USING brin (received_at);
```

### Retention

Simple cron-based retention (delete rows older than 30 days). On large unpartitioned tables a single `DELETE` generates massive WAL and holds locks; batch the deletes or use TimescaleDB retention policies (below) which drop whole chunks instantly.

```sql
DELETE FROM syslog_events
WHERE id IN (
    SELECT id FROM syslog_events
    WHERE received_at < now() - interval '30 days'
    LIMIT 10000
);
-- Run in a loop until 0 rows affected.
```

### TimescaleDB (optional upgrade)

Quick start with Docker (uses the `-ha` image which bundles toolkit, pg_stat_statements, and other useful extensions):

```yaml
# docker-compose.yml
services:
  syslog-db:
    image: timescale/timescaledb-ha:pg18
    ports:
      - "5432:5432"
    environment:
      POSTGRES_DB: taillight
      POSTGRES_USER: taillight
      POSTGRES_PASSWORD: "set-a-strong-password"
    volumes:
      - syslog_pgdata:/var/lib/postgresql/data

volumes:
  syslog_pgdata:
```

```sh
docker compose up -d
```

Then enable the extension and create the schema:

```sql
CREATE EXTENSION IF NOT EXISTS timescaledb;
-- Run the CREATE TABLE and indexes from the Schema section above, then:
```

Convert the table into a hypertable for automatic partitioning, built-in retention, and native compression:

```sql
SELECT create_hypertable('syslog_events', by_range('received_at'));
-- TimescaleDB auto-creates a time index on received_at, so the plain-Postgres
-- idx_syslog_received B-tree is redundant and can be dropped after conversion.

-- Automatic drop of chunks older than 30 days
SELECT add_retention_policy('syslog_events', drop_after => INTERVAL '30 days');

-- Enable columnstore and compress chunks older than 3 days (~10x space savings)
ALTER TABLE syslog_events SET (
    timescaledb.enable_columnstore,
    timescaledb.segmentby = 'hostname',
    timescaledb.orderby   = 'received_at DESC'
);
SELECT add_columnstore_policy('syslog_events', after => INTERVAL '3 days');
```

### LISTEN/NOTIFY for push-based SSE

Instead of polling, the Go SSE backend can LISTEN on a channel and push new events instantly. This trigger fires on every INSERT and sends the new row's ID over the `syslog_ingest` channel:

```sql
CREATE OR REPLACE FUNCTION notify_syslog_insert()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('syslog_ingest', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_syslog_notify
    AFTER INSERT ON syslog_events
    FOR EACH ROW EXECUTE FUNCTION notify_syslog_insert();
```

The Go backend calls `LISTEN syslog_ingest`, receives each ID, fetches the row, and pushes it to connected SSE clients -- zero polling.

### Storage estimates

| msgs/day | row avg | daily  | monthly | monthly (compressed) |
|----------|---------|--------|---------|----------------------|
| 100 k    | 512 B   | 49 MB  | 1.4 GB  | ~150 MB              |
| 500 k    | 512 B   | 244 MB | 7.2 GB  | ~720 MB              |
| 1 M      | 512 B   | 488 MB | 14.3 GB | ~1.4 GB              |

## Juniper device configuration

All Juniper devices must send RFC 5424 structured-data syslog for the filters to work. Without `structured-data`, the `$msgid`, `$app-name`, and `$structured-data` fields are empty and all msgid-based filters silently match nothing.

**Remote syslog host (required):**

```
set system syslog host 10.0.0.50 any notice
set system syslog host 10.0.0.50 port 514
set system syslog host 10.0.0.50 source-address 10.0.1.1
set system syslog host 10.0.0.50 structured-data
```

Replace `10.0.0.50` with the rsyslog collector IP and `10.0.1.1` with the device's loopback/management address.

**Why `any notice`?** Sends severity 0-5 (emergency through notice), dropping info/debug chatter that rsyslog would filter out anyway. This covers BGP state changes, interface up/down, UI_COMMIT, hardware alarms, OSPF/IS-IS adjacency changes, and authentication failures. Use `any info` per-device when debugging.

**Optional: suppress trailing English text:**

```
set system syslog host 10.0.0.50 structured-data brief
```

`brief` saves bandwidth but removes the English message text that exception keyword filters (`$msg contains "error"`) match against. Start without `brief` until all filters use `$msgid`-only matching.

**Notes:**

- When `structured-data` is set, `explicit-priority` and `time-format` statements are ignored (structured format includes priority, year, and milliseconds by default)
- Starting in Junos 19.2R1, you cannot combine `structured-data` with other format statements on the same host (commit error)
- `structured-data` applies per-host, so you can enable it for the rsyslog collector while keeping a different format on console/local files

## Requirements

- rsyslog 8.x with `mmpstrucdata` module (`rsyslog-mmpstrucdata` package)
- Juniper devices sending RFC 5424 structured-data syslog (see above)
- PostgreSQL and `rsyslog-pgsql` package (optional, for live log viewer SSE backend)
- Docker (optional, for isolated testing)
