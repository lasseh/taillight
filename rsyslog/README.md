# rsyslog Juniper Filtering

Modular rsyslog configuration for receiving, filtering, and routing syslog from Juniper network devices (MX, SRX, EX, QFX) into the srvlog feed. Designed to reduce noise from high-volume senders while preserving operationally important events.

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
  -> output to LibreNMS, per-host log files, PostgreSQL (srvlog), remote forwarding
```

## Project structure

```
rsyslog.conf              Main config -- global settings, queue tuning, includes
conf.d/
  00-modules.conf         Module loading (imudp, imtcp, mmpstrucdata, omprog, ...)
  01-templates.conf       Output format templates (srvlog, JSON, LibreNMS, PostgreSQL, debug)
  02-outputs.conf         Output rulesets (local files, remote, LibreNMS, JSON, PostgreSQL srvlog)
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
- **Enable PostgreSQL output**: Uncomment `module(load="ompgsql")` in `conf.d/00-modules.conf`, the `PgSQLSrvlogInsert` template in `conf.d/01-templates.conf`, the `output_pgsql_srvlog` ruleset in `conf.d/02-outputs.conf`, and `call output_pgsql_srvlog` in `conf.d/20-ruleset.conf`. Requires `rsyslog-pgsql` package and the DDL below.
- **Enable remote forwarding**: Uncomment `call output_remote` in `conf.d/20-ruleset.conf`
- **Enable JSON output**: Add `call output_json` to the output phase in `conf.d/20-ruleset.conf`
- **Enable Prometheus stats**: Uncomment `impstats` and `rsyslog_stats` in `conf.d/00-modules.conf` (requires [rsyslog_exporter](https://github.com/digitalocean/rsyslog_exporter))
- **Log dropped messages**: Uncomment `output_dropped` in `conf.d/02-outputs.conf`, then replace `stop` with `call output_dropped` in any filter to log what it discards to `/var/log/network/dropped.log`. See the example in `filters/40-by-severity.conf`. High-volume — use temporarily for tuning only.
- **Debug a device**: Uncomment the debug block in `conf.d/20-ruleset.conf` and set the target IP

## Juniper device configuration

All Juniper devices must send RFC 5424 structured-data syslog for the filters to work. Without `structured-data`, the `$msgid`, `$app-name`, and `$structured-data` fields are empty and all msgid-based filters silently match nothing.

**Remote syslog host (required):**

```
set system syslog host 10.0.0.50 any notice
set system syslog host 10.0.0.50 port 514
set system syslog host 10.0.0.50 source-address 10.0.1.1
set system syslog host 10.0.0.50 structured-data
set system syslog host 10.0.0.50 allow-duplicates
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
