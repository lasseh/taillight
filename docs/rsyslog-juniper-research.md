# rsyslog Juniper Syslog Filtering -- Research & Design Guide

A comprehensive reference for building a modular rsyslog filtering solution for Juniper network devices sending RFC 5424 structured-data syslog. Covers RainerScript syntax, all available filter properties, real-world Juniper noise patterns, output flexibility, and a Makefile-driven workflow.

---

## Table of Contents

1. [Overview & Goals](#1-overview--goals)
2. [rsyslog Filter Reference](#2-rsyslog-filter-reference)
3. [Project File Structure](#3-project-file-structure)
4. [Core Configuration Examples](#4-core-configuration-examples)
5. [Filter File Examples](#5-filter-file-examples)
6. [Complete Makefile](#6-complete-makefile)
7. [Testing Strategy](#7-testing-strategy)
8. [Platform Notes (Debian vs RHEL)](#8-platform-notes-debian-vs-rhel)
9. [Common Juniper Noise Patterns Reference](#9-common-juniper-noise-patterns-reference)
10. [Tips & Best Practices](#10-tips--best-practices)

---

## 1. Overview & Goals

### Problem

Large Juniper networks (MX, EX, SRX, QFX) generate enormous syslog volumes. A single MX router can produce thousands of messages per minute from daemons like `rpd`, `chassisd`, `pfed`, and `mib2d` -- most of which are routine operational noise. Without filtering, log collectors (LibreNMS, Graylog, Elasticsearch) drown in irrelevant data, increasing storage costs and making real alerts harder to find.

### Design Principles

- **RFC 5424 structured-data required** -- all Juniper devices send structured-data format, giving us parsed `$msgid` fields for precise event matching
- **Modular files** -- each filter category in its own file, easy to enable/disable
- **Plain RainerScript** -- no legacy syslog syntax, no BSD blocks; everything uses modern `if/then/stop` syntax
- **Validate before restart** -- `rsyslogd -N1` catches syntax errors before they take down syslog
- **Preserve important messages** -- every filter has exception keywords (`error`, `fail`, `down`, `alarm`, `critical`) to avoid dropping real problems
- **Performance first** -- `$msgid ==` for event names, `$programname ==` for daemons, `$msg contains` only for exception keywords in the message body; filter ordering matters
- **Flexible outputs** -- swap between local files, remote forwarding, LibreNMS, or any combination

---

## 2. rsyslog Filter Reference

### 2.1 Message Properties

Every syslog message exposes properties that filters can match against. These are the most useful ones for network device filtering.

#### RFC 5424 Header Fields

These fields are parsed directly from the RFC 5424 structured-data header. They are the primary filter targets.

| Property | Description | Example Value |
|---|---|---|
| `$msgid` | RFC 5424 MSGID -- the Juniper event name | `CHASSISD_BLOWERS_SPEED` |
| `$app-name` | RFC 5424 APP-NAME -- the daemon name (same as `$programname`) | `chassisd` |
| `$procid` | RFC 5424 PROCID -- the process ID | `5678` |
| `$structured-data` | Raw RFC 5424 structured data string | `[junos@2636.1.1.1.2.18 username="user"]` |

#### Standard syslog Properties

| Property | Description | Example Value |
|---|---|---|
| `$msg` | The MSG part of the syslog message (after header) | `Fan speed normal at 50%` |
| `$rawmsg` | The complete raw message as received | `<165>1 2024-11-28T12:00:00.000Z router1 chassisd 5678 ...` |
| `$hostname` | Hostname from the syslog header | `juniper-mx1` |
| `$fromhost` | DNS name of the sending system | `juniper-mx1.example.com` |
| `$fromhost-ip` | IP address of the sender | `10.0.1.1` |
| `$programname` | Static program name from the syslog tag (same as `$app-name`) | `chassisd` |
| `$syslogtag` | Full tag field including PID | `chassisd[5678]:` |
| `$syslogfacility` | Numeric facility (0-23) | `23` (local7) |
| `$syslogfacility-text` | Facility name | `local7` |
| `$syslogseverity` | Numeric severity (0-7, lower = more severe) | `5` (notice) |
| `$syslogseverity-text` | Severity name | `notice` |
| `$pri` | Raw PRI value | `189` |
| `$pri-text` | Formatted PRI | `local7.notice` |
| `$timereported` | Timestamp from the message (ISO 8601, milliseconds, timezone) | `2024-11-28T12:00:00.123Z` |
| `$timegenerated` | When rsyslog received it | `2024-11-28T12:00:00.123456` |
| `$inputname` | Input module that received the message | `imudp` |

#### The `junos@2636` Structured Data Block

`2636` is Juniper Networks' IANA Private Enterprise Number (PEN). The numbers after it (e.g., `.1.1.1.2.18`) identify the specific message type/module. The block contains key-value pairs specific to each event type:

```
[junos@2636.1.1.1.2.18 username="user"]
```

After `mmpstrucdata` parsing, individual fields are available as JSON properties:

| Property | Source | Example |
|---|---|---|
| `$!rfc5424-sd!junos@2636.1.1.1.2.18!username` | Parsed by mmpstrucdata | `user` |

System-level events typically have a small number of fields:

| Event | Example SD fields |
|---|---|
| `UI_DBASE_LOGOUT_EVENT` | `username` |
| `UI_COMMIT` | `username` |
| `UI_LOGIN_EVENT` | `username` |
| `CHASSISD_BLOWERS_SPEED` | (varies by platform) |
| `RPD_SCHED_CALLBACK` | (minimal or none) |
| `SNMPD_AUTH_FAILURE` | (community/source info) |

The SD-ID suffix (`.1.1.1.2.18`) varies per event type -- you do not need to memorize these for filtering because `$msgid` is the main value for filter rules.

**Filtering notes:**

- `$msgid` contains the Juniper event name (e.g., `RPD_SCHED_CALLBACK`) -- use `$msgid ==` for event name matching
- `$programname` (equivalent to `$app-name`) reliably contains the daemon name (`rpd`, `chassisd`, `mgd`, etc.) -- use for dropping entire daemons
- `$msg` contains the trailing message text -- use `$msg contains` only for exception keywords (`error`, `fail`, `down`, etc.)
- Juniper typically uses `local0` through `local7` for facility

### 2.2 Filter Syntax

#### String Comparison

```conf
# Exact equality on MSGID (primary method for event name matching)
if ($msgid == "CHASSISD_BLOWERS_SPEED") then { ... }

# Exact equality on programname (for daemon-level drops)
if ($programname == "chassisd") then { ... }

# Not equal
if ($programname != "rpd") then { ... }

# Contains substring (case-sensitive) -- for exception keywords in message body
if ($msg contains "error") then { ... }

# Starts with prefix
if ($msgid startswith "CHASSISD_") then { ... }
```

#### Regex Matching

```conf
# Basic regex match (returns true/false)
if re_match($msg, "RT_FLOW_SESSION_(CREATE|CLOSE)") then { ... }

# Regex extract (capture groups)
set $.device_type = re_extract($syslogtag, "(\\w+)\\[", 0, 1, "unknown");

# Case-insensitive matching (tolower + lowercase pattern)
# POSIX ERE (rsyslog default) does not support (?i)
if re_match(tolower($msg), "error|fail|down") then { ... }
```

#### Logical Operators

```conf
# AND -- both conditions must be true
if ($msgid == "CHASSISD_BLOWERS_SPEED" and $msg contains "alarm") then { ... }

# OR -- either condition
if ($msg contains "error" or $msg contains "Error") then { ... }

# NOT -- negate
if (not ($msg contains "error")) then { ... }

# Grouped conditions with parentheses
if (($msgid == "CHASSISD_BLOWERS_SPEED" or $msgid == "CHASSISD_TEMP_ZONE") and
    not ($msg contains "error" or $msg contains "fail")) then {
    stop
}
```

#### Numeric Comparison (severity/facility)

```conf
# Drop everything less severe than warning (severity > 4)
# Severity scale: 0=emerg, 1=alert, 2=crit, 3=err, 4=warning, 5=notice, 6=info, 7=debug
if ($syslogseverity > 4) then { stop }

# Only keep facility local0 (facility == 16)
if ($syslogfacility == 16) then { ... }
```

#### The `stop` Directive

`stop` immediately terminates processing of the current message. It replaces the legacy `~` discard action.

```conf
# Drop message, no further processing
if ($msgid == "RT_FLOW_SESSION_CREATE") then { stop }

# With logging before drop (useful for debugging filters)
if ($msgid == "RT_FLOW_SESSION_CREATE") then {
    action(type="omfile" file="/var/log/rsyslog-dropped.log" template="RSYSLOG_TraditionalFileFormat")
    stop
}
```

### 2.3 Performance: String vs Regex

| Method | Relative Speed | Use When |
|---|---|---|
| `$msgid ==` | Fastest | Filtering by event name (primary method) |
| `$programname ==` | Fastest | Filtering by daemon name (daemon-level drops) |
| `$msg contains` | Fast | Exception keywords in message body |
| `$msg startswith` | Fast | Message starts with known prefix |
| `re_match()` | 5-10x slower | Need alternation or patterns |
| `re_extract()` | Slowest | Need to capture groups |

**Rule of thumb:** Use `$msgid ==` for event name matching, `$programname ==` for daemon-level drops, `$msg contains` for exception keywords in the message body. Use `re_match()` only when you need alternation (`CREATE|CLOSE`) or character classes. Chain multiple `contains` with `or` instead of a single regex when practical.

---

## 3. Project File Structure

```
rsyslog-filters/
├── Makefile                        # Build, validate, deploy, test
├── Dockerfile.test                 # Docker-based test environment
├── docker-compose.yml              # Run tests in Docker
├── rsyslog.conf                    # Main config (minimal, includes conf.d/)
├── conf.d/
│   ├── 00-modules.conf             # Module loading (imudp, imtcp, mmpstrucdata, etc.)
│   ├── 01-templates.conf           # Output format templates
│   ├── 02-outputs.conf             # Output destinations (files, remote, etc.)
│   ├── 10-inputs.conf              # UDP/TCP listener definitions
│   └── 20-ruleset.conf             # Main processing ruleset
├── filters/
│   ├── 05-by-msgid.conf            # Filter by $msgid event name (fastest, primary)
│   ├── 06-ui-commit-trigger.conf   # Route UI_COMMIT to backup triggers, then stop
│   ├── 10-by-programname.conf      # Daemon-level drops ($programname)
│   ├── 30-by-facility.conf         # Filter by syslog facility
│   ├── 40-by-severity.conf         # Filter by severity threshold
│   └── 50-by-hostname.conf         # Filter by source device/subnet
└── tests/
    ├── test-messages.txt           # Sample RFC 5424 syslog messages (fixtures)
    └── test-filters.sh             # Validation and functional tests
```

**Naming convention:** Numeric prefixes control load order. Lower numbers load first. `05` is `$msgid` matching (fastest, most precise). `06` is Oxidized routing. `10` is daemon-level `$programname` drops. `30`+ are broader facility, severity, and hostname filters.

---

## 4. Core Configuration Examples

### 4.0 Juniper Device Configuration (Prerequisite)

All Juniper devices MUST be configured to send structured-data syslog to the rsyslog collector. This is what populates the `$msgid`, `$app-name`, `$procid`, and `$structured-data` fields.

**Remote syslog host with structured-data (required):**

```
set system syslog host 10.0.0.50 any notice
set system syslog host 10.0.0.50 port 514
set system syslog host 10.0.0.50 source-address 10.0.1.1
set system syslog host 10.0.0.50 structured-data
```

**With `brief` (suppress trailing English text, saves bandwidth):**

```
set system syslog host 10.0.0.50 structured-data brief
```

**For local file logging:**

```
set system syslog file messages any notice
set system syslog file messages structured-data
```

**Why `any notice` (not `any any`)?**

Junos severity levels: emergency(0), alert(1), critical(2), error(3), warning(4), notice(5), info(6), debug(7). Setting `any notice` sends severity 0-5 and drops info/debug. This is the sweet spot because:

- **Keeps:** BGP state changes (`Established`, `Down`), interface up/down, UI_COMMIT events, hardware alarms, OSPF/IS-IS adjacency changes, authentication failures -- all at notice or higher
- **Drops:** routine info-level chatter (SNMP polling, scheduler callbacks, PFE statistics, periodic health checks) and debug messages that rsyslog would filter out anyway
- **Result:** significantly less bandwidth and processing on the rsyslog side, without losing operationally relevant events
- If you need info-level messages temporarily (e.g., debugging), use `any info` on a per-device basis rather than fleet-wide

**Notes:**

- When `structured-data` is set, `explicit-priority` and `time-format` statements are ignored -- structured format includes priority, year, and milliseconds by default
- Starting in Junos 19.2R1, you cannot combine `structured-data` with other format statements on the same file/host (commit error)
- `structured-data` applies per-host or per-file, so you can enable it on the rsyslog collector host while keeping a different format on console/local files
- **Recommended: start without `brief`** so exception keyword filters (`$msg contains "error"`) still work against the trailing English text. Switch to `brief` once filters are fully converted to `$msgid`-only matching with no `$msg` exception checks

### 4.1 `rsyslog.conf` -- Main Config

Minimal main config that loads modules, sets globals, and includes modular files.

```conf
#############################
# rsyslog.conf -- Main Configuration
# Network device syslog with Juniper filtering
#############################

# Global settings
global(
    maxMessageSize="64k"
    preserveFQDN="on"
    processInternalMessages="on"
    workDirectory="/var/spool/rsyslog"
)

# Main message queue -- tuned for high-volume network syslog
main_queue(
    queue.type="LinkedList"
    queue.filename="main_queue"
    queue.size="50000"
    queue.discardMark="45000"
    queue.discardSeverity="6"          # Discard info/debug first under pressure
    queue.workerThreads="4"
    queue.workerThreadMinimumMessages="1000"
    queue.timeoutEnqueue="0"           # Non-blocking enqueue
    queue.maxDiskSpace="5g"            # Disk-assisted queue for overflow
    queue.saveOnShutdown="on"
)

# Include modular configuration files (order matters)
# Outputs load before inputs/ruleset because `call` requires the target
# ruleset to already be defined when the calling ruleset is parsed.
include(file="/etc/rsyslog.d/conf.d/00-modules.conf")
include(file="/etc/rsyslog.d/conf.d/01-templates.conf")
include(file="/etc/rsyslog.d/conf.d/02-outputs.conf")
include(file="/etc/rsyslog.d/conf.d/10-inputs.conf")
include(file="/etc/rsyslog.d/conf.d/20-ruleset.conf")
```

### 4.2 `00-modules.conf` -- Module Loading

```conf
#############################
# Module Loading
#############################

# UDP syslog input (primary for network devices)
module(load="imudp" threads="8" batchSize="128")

# TCP syslog input (reliable delivery)
module(load="imtcp")

# Unix socket -- receives local system logs via /dev/log.
# Required for rsyslog's own internal messages (processInternalMessages="on")
# and any local programs that log via syslog(3).
module(load="imuxsock")

# Mark messages -- writes "-- MARK --" every interval (seconds) to prove
# rsyslog is alive and log flow hasn't silently stopped.
# Useful for monitoring and verifying log pipeline health.
module(load="immark" interval="600")

# Program output (for local LibreNMS syslog.php)
module(load="omprog")

# Parse RFC 5424 structured data into JSON tree
module(load="mmpstrucdata")

# Statistics -- feeds Prometheus exporter via omprog
# Uncomment when rsyslog_exporter is installed:
#   https://github.com/digitalocean/rsyslog_exporter
#
# module(load="impstats"
#     interval="10"
#     format="json"
#     resetCounters="off"
#     log.syslog="off"
#     ruleset="rsyslog_stats"
# )

######################
# Prometheus Stats Exporter
# Defined here so the ruleset exists before impstats starts emitting.
# Uncomment together with impstats above.
######################

# ruleset(name="rsyslog_stats") {
#     action(type="omprog"
#         name="prometheus_exporter"
#         binary="/usr/local/bin/rsyslog_exporter"
#         queue.type="LinkedList"
#         queue.size="1000"
#         queue.workerThreads="1"
#         action.resumeRetryCount="10"
#         action.resumeInterval="30"
#     )
# }
```

### 4.3 `01-templates.conf` -- Output Format Templates

```conf
#############################
# Output Templates
#############################

# Standard syslog format with high-resolution timestamp
template(name="NetworkDeviceFormat" type="string"
    string="%timereported:::date-rfc3339% %fromhost-ip% %hostname% %syslogtag% %msg:2:$%\n"
)

# Traditional syslog format (compatible with most log parsers)
template(name="TraditionalFormat" type="string"
    string="%timereported% %hostname% %syslogtag% %msg:2:$%\n"
)

# JSON output (for Elasticsearch, Graylog, etc.)
# All string properties use format="jsonr" to escape special characters
template(name="JSONFormat" type="list") {
    constant(value="{")
    constant(value="\"@timestamp\":\"")     property(name="timereported" dateFormat="rfc3339")
    constant(value="\",\"host\":\"")        property(name="hostname" format="jsonr")
    constant(value="\",\"fromhost_ip\":\"") property(name="fromhost-ip" format="jsonr")
    constant(value="\",\"program\":\"")     property(name="programname" format="jsonr")
    constant(value="\",\"msgid\":\"")       property(name="msgid" format="jsonr")
    constant(value="\",\"severity\":\"")    property(name="syslogseverity-text" format="jsonr")
    constant(value="\",\"facility\":\"")    property(name="syslogfacility-text" format="jsonr")
    constant(value="\",\"tag\":\"")         property(name="syslogtag" format="jsonr")
    constant(value="\",\"structured_data\":\"") property(name="structured-data" format="jsonr")
    constant(value="\",\"message\":\"")     property(name="msg" position.from="2" format="jsonr")
    constant(value="\"}\n")
}

# Per-host dynamic filename template
template(name="PerHostFile" type="string"
    string="/var/log/network/%hostname%.log"
)

# Commit log -- hostname with domain suffix, tag, and message
template(name="CommitFormat" type="string"
    string="%hostname%.osl.as207788.net %syslogtag% %msg:2:$%\n"
)

# Resin trigger -- hostname, source IP, and message
template(name="ResinTemplate" type="string"
    string="%hostname% %fromhost-ip% %msg:2:$%\n"
)

# LibreNMS local syslog.php input format (pipe-delimited)
template(name="LibreNMSFormat" type="string"
    string="%fromhost%||%syslogfacility%||%syslogpriority%||%syslogseverity%||%syslogtag%||%$year%-%$month%-%$day% %timegenerated:8:25%||%msg:2:$%||%programname%\n"
)

# Debug template -- all parsed fields, one block per message
template(name="DebugFields" type="string"
    string="---\nTIME:       %timereported:::date-rfc3339%\nFROM_IP:    %fromhost-ip%\nFROM_HOST:  %fromhost%\nHOSTNAME:   %hostname%\nPROGRAM:    %programname%\nTAG:        %syslogtag%\nPID:        %procid%\nMSGID:      %msgid%\nFACILITY:   %syslogfacility% (%syslogfacility-text%)\nSEVERITY:   %syslogseverity% (%syslogseverity-text%)\nPRI:        %pri% (%pri-text%)\nAPP_NAME:   %app-name%\nSD:         %structured-data%\nINPUT:      %inputname%\nMSG:        %msg:2:$%\n\n"
)

# Oxidized webhook -- hostname, event, user, and timestamp
# All string properties use format="jsonr" to escape special characters
template(name="OxidizedJSON" type="list") {
    constant(value="{\"hostname\":\"")
    property(name="hostname" format="jsonr")
    constant(value="\",\"event\":\"")
    property(name="msgid" format="jsonr")
    constant(value="\",\"user\":\"")
    property(name="$!rfc5424-sd!junos@2636.1.1.1.2.18!username" format="jsonr")
    constant(value="\",\"timestamp\":\"")
    property(name="timereported" dateFormat="rfc3339")
    constant(value="\"}\n")
}
```

### 4.4 `10-inputs.conf` -- Listener Definitions

```conf
#############################
# Input Listeners
#############################

# Standard UDP syslog (port 514)
input(type="imudp"
    port="514"
    ruleset="network_devices"
    ratelimit.interval="0"
)

# Standard TCP syslog (port 514)
input(type="imtcp"
    port="514"
    ruleset="network_devices"
)

# High-performance UDP (port 1514) -- for high-volume senders
input(type="imudp"
    port="1514"
    ruleset="network_devices"
    ratelimit.interval="0"
)
```

### 4.5 `20-ruleset.conf` -- Main Processing Ruleset

This is the central routing logic. It parses structured data, includes filters, then forwards surviving messages to outputs.

```conf
#############################
# Main Processing Ruleset
#############################

ruleset(name="network_devices"
    queue.type="LinkedList"
    queue.filename="network_q"
    queue.size="10000"
    queue.maxDiskSpace="1g"
    queue.saveOnShutdown="on"
    queue.workerThreads="2"
) {
    # --- Phase 0: Parse structured data ---
    action(type="mmpstrucdata")

    # --- Debug: dump all fields for one device ---
    # Change the IP to the device you want to inspect.
    # Comment out or remove when done.
    # if ($fromhost-ip == "10.0.1.1") then {
    #     action(type="omfile"
    #         file="/var/log/network/debug.log"
    #         template="DebugFields"
    #         fileCreateMode="0644"
    #         dirCreateMode="0755"
    #     )
    # }

    # --- Critical severity logging (before filters) ---
    call output_critical           # Always capture crit/alert/emerg

    # --- Phase 1: Apply filters (cheapest first) ---

    # Filter by $msgid event name (fastest, most precise)
    include(file="/etc/rsyslog.d/filters/05-by-msgid.conf")

    # Route UI_COMMIT events to backup triggers (Oxidized, Resin), then stop
    include(file="/etc/rsyslog.d/filters/06-ui-commit-trigger.conf")

    # Filter by $programname -- daemon-level drops (cron, ntpd, mib2d, etc.)
    include(file="/etc/rsyslog.d/filters/10-by-programname.conf")

    # Filter by facility
    include(file="/etc/rsyslog.d/filters/30-by-facility.conf")

    # Filter by severity threshold
    include(file="/etc/rsyslog.d/filters/40-by-severity.conf")

    # Filter by hostname/IP (if you only want specific devices)
    include(file="/etc/rsyslog.d/filters/50-by-hostname.conf")

    # --- Phase 2: Route surviving messages to outputs ---
    call output_librenms           # Feed to local LibreNMS syslog.php
    call output_local              # Also write to per-host log files
    # call output_remote           # Uncomment to enable remote forwarding
}
```

### 4.6 `02-outputs.conf` -- Output Destinations

```conf
#############################
# Output Destinations
#############################

# Critical severity output -- captures sev <= 2 (crit, alert, emerg)
ruleset(name="output_critical") {
    if ($syslogseverity <= 2) then {
        action(type="omfile"
            file="/var/log/network/critical_logs.log"
            template="NetworkDeviceFormat"
            asyncWriting="on"
            flushOnTXEnd="on"
            fileCreateMode="0644"
            dirCreateMode="0755"
        )
    }
}

# Local file output -- per-host log files
ruleset(name="output_local") {
    action(type="omfile"
        DynaFile="PerHostFile"
        template="NetworkDeviceFormat"
        asyncWriting="on"
        flushOnTXEnd="off"
        ioBufferSize="64k"
        flushInterval="1"
        dirCreateMode="0755"
        fileCreateMode="0644"
    )
}

# Remote forwarding via UDP (to central collector)
ruleset(name="output_remote") {
    action(type="omfwd"
        target="syslog-collector.example.com"
        port="514"
        protocol="udp"
        template="NetworkDeviceFormat"
        queue.type="LinkedList"
        queue.size="5000"
        queue.filename="fwd_remote"
        queue.maxDiskSpace="1g"
        queue.saveOnShutdown="on"
        action.resumeRetryCount="-1"
        action.resumeInterval="30"
    )
}

# Remote forwarding via TCP (reliable, with disk-assisted queue)
ruleset(name="output_remote_tcp") {
    action(type="omfwd"
        target="syslog-collector.example.com"
        port="6514"
        protocol="tcp"
        template="NetworkDeviceFormat"
        TCP_Framing="octet-counted"
        queue.type="LinkedList"
        queue.size="5000"
        queue.filename="fwd_remote_tcp"
        queue.maxDiskSpace="2g"
        queue.saveOnShutdown="on"
        action.resumeRetryCount="-1"
        action.resumeInterval="30"
    )
}

# LibreNMS local integration (omprog -> syslog.php on this server)
ruleset(name="output_librenms") {
    action(type="omprog"
        binary="/opt/librenms/syslog.php"
        template="LibreNMSFormat"
        queue.type="LinkedList"
        queue.size="5000"
        queue.workerThreads="2"
        queue.filename="librenms_omprog"
        queue.maxDiskSpace="500m"
        queue.saveOnShutdown="on"
        action.resumeRetryCount="-1"
        action.resumeInterval="30"
    )
}

# JSON output (for Elasticsearch/Graylog/Loki)
ruleset(name="output_json") {
    action(type="omfile"
        file="/var/log/network/syslog.json"
        template="JSONFormat"
        asyncWriting="on"
        flushOnTXEnd="off"
        ioBufferSize="64k"
        flushInterval="1"
    )
}
```

---

## 5. Filter File Examples

This is the core value of the project. Each filter file handles one category and uses exception keywords to preserve important messages.

### 5.1 `filters/05-by-msgid.conf` -- Filter by Event Name

The primary filter. `$msgid` is a parsed RFC 5424 header field -- exact match, no string scanning, zero ambiguity.

```conf
#############################
# MSGID-Based Filters (Event Name)
# Primary filter: $msgid is parsed from RFC 5424 header, exact match
# Fastest and most precise method for event name filtering
#
# Exception keywords use re_match(tolower($msg), ...) for case-insensitive
# matching -- POSIX ERE (rsyslog default) does not support (?i)
#############################

# --- Chassis hardware noise ---
if ($msgid == "CHASSISD_BLOWERS_SPEED" or
    $msgid == "CHASSISD_TEMP_ZONE" or
    $msgid == "CHASSISD_FAN" or
    $msgid == "CHASSISD_PSU" or
    $msgid == "CHASSISD_SENSORS") then {
    if (not re_match(tolower($msg), "major|critical|alarm|failed")) then { stop }
}

# --- RPD scheduler noise ---
if ($msgid == "RPD_SCHED_CALLBACK" or
    $msgid == "RPD_SCHED_MODULE_INFO" or
    $msgid == "RPD_SCHED_SLIP") then { stop }

# --- RT_FLOW session logs (SRX firewall) ---
# Drop session create/close, keep denies and errors
if ($msgid == "RT_FLOW_SESSION_CREATE" or
    $msgid == "RT_FLOW_SESSION_CLOSE") then {
    if (not re_match(tolower($msg), "deny|error")) then { stop }
}

# --- SNMP traps and auth failures ---
if ($msgid == "SNMPD_TRAP_COLD_START" or
    $msgid == "SNMPD_TRAP_WARM_START" or
    $msgid == "SNMPD_AUTH_FAILURE" or
    $msgid == "SNMPD_TRAP_QUEUE") then { stop }

# --- Login/logout ---
# Kept for audit trail -- all logins and logouts are logged
# if ($msgid == "UI_LOGIN_EVENT" or
#     $msgid == "UI_LOGOUT_EVENT") then {
#     if (not re_match(tolower($msg), "fail|invalid")) then { stop }
# }

# --- Config mode noise ---
# Drop routine config audit; keep errors/failures for audit trail.
# To log ALL config changes, uncomment the action below to route
# UI_CFG_AUDIT_SET to a separate audit log:
# if ($msgid == "UI_CFG_AUDIT_SET") then {
#     action(type="omfile"
#         file="/var/log/network/audit.log"
#         template="NetworkDeviceFormat"
#         fileCreateMode="0644"
#         dirCreateMode="0755"
#     )
# }
if ($msgid == "UI_CFG_AUDIT_SET" or
    $msgid == "UI_CMDLINE_READ_LINE" or
    $msgid == "UI_CONFIGURATION_MODE") then {
    if (not re_match(tolower($msg), "error|fail|denied")) then { stop }
}

# --- Management daemon routine login/auth ---
if ($msgid == "UI_DBASE_LOGIN_EVENT" or
    $msgid == "UI_AUTH_EVENT" or
    $msgid == "UI_AUTH_INVALID_CHALLENGE") then {
    if (not re_match(tolower($msg), "fail|error|denied")) then { stop }
}

# --- LLDP discovery ---
# Drop routine neighbor-up events; keep neighbor-down (operationally significant)
if ($msgid == "LLDP_NEIGHBOR_UP") then {
    if (not re_match(tolower($msg), "error")) then { stop }
}

# --- PFE stats ---
if ($msgid == "PFED_FW_SYSLOG_IP" or
    $msgid == "PFED_FW_SYSLOG_IP6" or
    $msgid == "PFE_FW_SYSLOG") then {
    if (not re_match(tolower($msg), "error|fail|critical")) then { stop }
}

# --- Interface statistics (not state changes) ---
if ($msgid == "IFINFO" and $msg contains "statistics") then { stop }

# --- License routine checks ---
if ($msgid startswith "LICENSE_" and
    not re_match(tolower($msg), "expir|invalid")) then { stop }

# --- BGP routine updates (keep state changes) ---
if ($msgid == "BGP_CONNECT" or
    $msgid == "BGP_READ" or
    $msgid == "BGP_WRITE" or
    $msgid == "BGP_SEND" or
    $msgid == "BGP_RECV") then {
    if (not re_match(tolower($msg), "established|down|error|notification")) then { stop }
}

# --- Kernel routine messages ---
if ($msgid == "IF_FLAGS_SNMP" or
    $msgid startswith "JNX_" or
    $msgid startswith "COS_" or
    $msgid startswith "PFE_") then {
    if (not re_match(tolower($msg), "panic|error|fail|critical|down|trap")) then { stop }
}
```

### 5.2 `filters/06-ui-commit-trigger.conf` -- Route Commits to Backup Triggers

`UI_COMMIT` and `UI_COMMIT_COMPLETED` events are useful for triggering Oxidized config backups and Resin notifications. Instead of dropping these, forward them to the relevant outputs, then stop.

This filter runs **before** the general noise filters so commits are routed regardless of other filter rules.

```conf
#############################
# filters/06-ui-commit-trigger.conf
# Route commit events to backup triggers, log locally, then drop
# Must be included BEFORE general noise filters
#############################

if ($msgid == "UI_COMMIT" or
    $msgid == "UI_COMMIT_COMPLETED") then {

    # Log all commits to a local file
    action(type="omfile"
        file="/var/log/remote-commit.log"
        template="CommitFormat"
    )

    # Forward to Oxidized webhook (HTTP POST via omprog or omhttp)
    # Option A: HTTP POST to Oxidized REST API
    # action(type="omhttp"
    #     server="oxidized.example.com"
    #     serverport="8888"
    #     restpath="hook/juniper"
    #     template="OxidizedJSON"
    #     action.resumeRetryCount="3"
    #     action.resumeInterval="10"
    # )

    # Option B: Forward to a local/remote syslog port that Oxidized watches
    action(type="omfwd"
        target="oxidized.example.com"
        port="1515"
        protocol="udp"
        template="OxidizedJSON"
    )

    # Option C: Write to a file that Oxidized watches
    # action(type="omfile"
    #     file="/var/log/oxidized/commits.json"
    #     template="OxidizedJSON"
    #     asyncWriting="on"
    #     flushOnTXEnd="on"
    # )

    # Option D: Execute a script directly
    # action(type="omprog"
    #     binary="/usr/local/bin/trigger-oxidized.sh"
    #     template="OxidizedJSON"
    # )

    # Trigger Resin on completed commits only
    if ($msgid == "UI_COMMIT_COMPLETED") then {
        action(type="omprog"
            binary="/opt/resin/resin"
            template="ResinTemplate"
            output="/var/log/resin/omprog.log"
            queue.type="LinkedList"
            queue.workerThreads="5"
            queue.size="1000"
            queue.saveOnShutdown="on"
        )
    }

    # Uncomment to prevent commit events from reaching the normal output pipeline.
    # Left open so commits also appear in per-host logs and LibreNMS.
    # stop
}
```

**How it works:**

1. The filter runs early in the ruleset (prefix `06-`) -- before any general noise filters
2. All commit events are logged to `/var/log/remote-commit.log` (local audit trail)
3. All commit events are forwarded to Oxidized via one of the output options
4. `UI_COMMIT_COMPLETED` events additionally trigger Resin via `omprog`
5. Messages continue through the pipeline so commits also appear in per-host logs and LibreNMS
6. Oxidized receives a JSON payload with `hostname`, `event`, `user`, and `timestamp`

**Oxidized integration methods:**

| Method | Module | Notes |
|---|---|---|
| HTTP POST to REST API | `omhttp` | Cleanest. Requires rsyslog `omhttp` module. POST to Oxidized's hook endpoint. |
| Forward to syslog port | `omfwd` | Oxidized listens on a dedicated port, parses incoming JSON. |
| Write to watched file | `omfile` | Oxidized or a cron job watches the file for new entries. |
| Execute script | `omprog` | Script calls `curl` or the Oxidized CLI. Slowest but most flexible. |

**Example trigger script (`/usr/local/bin/trigger-oxidized.sh`):**

```bash
#!/usr/bin/env bash
# Read JSON from stdin (one line per event), trigger Oxidized node refresh
while IFS= read -r line; do
    hostname=$(echo "$line" | jq -r '.hostname // empty')
    if [ -n "$hostname" ]; then
        curl -s -X PUT "http://oxidized.example.com:8888/node/next/${hostname}" \
            -H "Content-Type: application/json" >/dev/null 2>&1
    fi
done
```

### 5.3 `filters/10-by-programname.conf` -- Daemon-Level Drops

Drop entire daemons where every message from the daemon is noise (or where the daemon is so noisy that per-event filtering is not worth the effort). `$programname` is a pre-parsed field -- no string scanning needed.

Note: `$programname` is equivalent to `$app-name` from the RFC 5424 header.

```conf
#############################
# Programname-Based Filters (Daemon-Level Drops)
# Use for daemons where you want to drop ALL messages (or all non-error messages)
# Event-specific filtering belongs in 05-by-msgid.conf
#
# Exception keywords use re_match(tolower($msg), ...) for case-insensitive
# matching -- POSIX ERE (rsyslog default) does not support (?i)
#############################

# --- CRON: Drop all cron messages unconditionally ---
if ($programname == "cron" or $programname == "CRON") then { stop }

# --- NTP: Drop routine sync, keep errors ---
if ($programname == "ntpd") then {
    if (not re_match(tolower($msg), "error|unreachable|no server")) then { stop }
}

# --- MIB2D: Drop routine MIB polling, keep errors ---
if ($programname == "mib2d") then {
    if (not re_match(tolower($msg), "error")) then { stop }
}

# --- DCD: Drop routine device control, keep errors/failures ---
if ($programname == "dcd") then {
    if (not re_match(tolower($msg), "error|fail")) then { stop }
}

# --- LACPD: Drop routine LACP, keep errors and state changes ---
if ($programname == "lacpd") then {
    if (not re_match(tolower($msg), "error|warning|down")) then { stop }
}

# --- COSD: Drop class-of-service daemon noise ---
if ($programname == "cosd") then {
    if (not re_match(tolower($msg), "error")) then { stop }
}

# --- ALARMD: Drop routine alarm checks, keep actual alarms ---
if ($programname == "alarmd") then {
    if (not re_match(tolower($msg), "major|critical|alarm|failed")) then { stop }
}

# --- SSHD: Drop routine sessions, keep failures ---
if ($programname == "sshd") then {
    if (not re_match(tolower($msg), "failed|invalid|error|refused")) then { stop }
}

# --- PFED: Drop PFE stats, keep errors ---
# Aligned with 05-by-msgid.conf PFE msgid filter keywords
if ($programname == "pfed") then {
    if (not re_match(tolower($msg), "error|fail|critical")) then { stop }
}
```

### 5.4 `filters/30-by-facility.conf` -- Filter by Syslog Facility

Juniper maps different subsystems to facilities. Useful when you want to suppress an entire subsystem.

```conf
#############################
# Facility-Based Filters
# Juniper typically uses local0-local7
#############################

# Facility reference:
#   0  = kern        8  = uucp       16 = local0
#   1  = user        9  = cron       17 = local1
#   2  = mail       10  = authpriv   18 = local2
#   3  = daemon     11  = ftp        19 = local3
#   4  = auth       12  = ntp        20 = local4
#   5  = syslog     13  = audit      21 = local5
#   6  = lpr        14  = alert      22 = local6
#   7  = news       15  = clock      23 = local7

# Drop local7 (Juniper default daemon facility) info messages
# Debug is already dropped globally by 40-by-severity.conf
# This catches remaining info-level noise from Juniper daemons
# not handled by specific msgid or programname filters
if ($syslogfacility == 23 and $syslogseverity == 6) then { stop }

# Example: Juniper sends firewall logs on local3 (facility 19)
# Drop all local3 firewall logs except denies
# if ($syslogfacility == 19) then {
#     if (not ($msg contains "DENY" or
#              $msg contains "deny" or
#              $msg contains "error")) then { stop }
# }

# Example: Keep only auth-related facilities at or above warning
# if ($syslogfacility == 4 or $syslogfacility == 10) then {
#     if ($syslogseverity > 4) then { stop }
# }
```

### 5.5 `filters/40-by-severity.conf` -- Filter by Severity Threshold

Global severity filter -- drop anything less severe than a threshold. This is a blunt instrument; use per-event `$msgid` filters for more control.

```conf
#############################
# Severity-Based Filters
# Global threshold to cut low-priority noise
#############################

# Severity reference (lower number = more severe):
#   0 = emerg     (system unusable)
#   1 = alert     (immediate action needed)
#   2 = crit      (critical conditions)
#   3 = err       (error conditions)
#   4 = warning   (warning conditions)
#   5 = notice    (normal but significant)
#   6 = info      (informational)
#   7 = debug     (debug-level)

# Drop all debug messages globally (severity 7)
if ($syslogseverity == 7) then { stop }

# More aggressive: drop info and debug (severity > 5)
# WARNING: This will drop a lot of messages. Use per-event filters instead
#          unless you really want a hard global cutoff.
# if ($syslogseverity > 5) then { stop }

# Example: Drop debug from specific facility only
# if ($syslogfacility == 23 and $syslogseverity == 7) then { stop }
```

### 5.6 `filters/50-by-hostname.conf` -- Filter by Source Device

Filter by hostname or IP address. Useful for suppressing noisy devices or isolating specific ones for debugging.

```conf
#############################
# Hostname/IP-Based Filters
# Filter messages from specific devices or subnets
#############################

# --- Drop all messages from a known-noisy lab device ---
# if ($hostname == "lab-router-01") then { stop }

# --- Drop messages from a specific IP ---
# if ($fromhost-ip == "10.0.99.1") then { stop }

# --- Filter by IP prefix (subnet) using startswith ---
# Drop all messages from the 10.0.99.x management lab subnet
# if ($fromhost-ip startswith "10.0.99.") then { stop }

# --- Filter by IP range using regex ---
# Drop messages from 10.0.10.1 through 10.0.10.254
# if re_match($fromhost-ip, "^10\\.0\\.10\\.") then { stop }

# --- Only keep messages from specific devices (allowlist approach) ---
# WARNING: This will drop everything NOT in the list. Use with caution.
# if (not ($fromhost-ip == "10.0.1.1" or
#          $fromhost-ip == "10.0.1.2" or
#          $fromhost-ip == "10.0.2.1")) then { stop }

# --- Route specific hosts to separate log files ---
# if ($hostname == "core-rtr-01") then {
#     action(type="omfile"
#         file="/var/log/network/core-rtr-01-debug.log"
#         template="NetworkDeviceFormat")
#     # Don't stop -- let message continue through other filters
# }
```

### 5.7 Combined Filter Patterns

Real-world filters often combine multiple conditions. These patterns show common combinations.

```conf
#############################
# Combined Filter Examples
# Multi-condition patterns for fine-grained control
#############################

# --- Pattern: msgid + severity ---
# Drop chassis noise at notice/info, keep warnings and above
if ($msgid startswith "CHASSISD_" and $syslogseverity > 4) then { stop }

# --- Pattern: programname + severity ---
# Drop info/notice from rpd, keep warnings and above
if ($programname == "rpd" and $syslogseverity > 4) then { stop }

# --- Pattern: hostname + msgid ---
# Only filter chassis noise from access switches, keep core router chassis
if ($hostname startswith "access-sw-" and
    $msgid startswith "CHASSISD_") then {
    if ($syslogseverity > 4) then { stop }
}

# --- Pattern: "drop X unless Y" (exception-based filtering) ---
# Drop all SNMPD messages UNLESS they contain error/fail keywords
if ($programname == "snmpd") then {
    if (not ($msg contains "error" or
             $msg contains "Error" or
             $msg contains "fail" or
             $msg contains "Fail")) then { stop }
}

# --- Pattern: rate-based noise from specific daemon ---
# Drop mib2d from devices that poll every 5 seconds (high volume)
if ($programname == "mib2d" and
    $fromhost-ip startswith "10.0.1.") then { stop }

# --- Pattern: tag-based filtering with PID ---
# Drop specific daemon PIDs (useful for isolating a known-bad process)
# if ($syslogtag startswith "rpd[" and $msgid == "RPD_SCHED_CALLBACK") then { stop }
```

---

## 6. Complete Makefile

Copy-paste ready Makefile for managing the rsyslog filter project.

```makefile
DEPLOY_DIR ?= /etc/rsyslog.d

.PHONY: test validate deploy restart clean help

##@ General
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Quality
test: ## Build and test config in Docker
	docker build --no-cache -f Dockerfile.test .

validate: ## Validate config syntax locally (requires rsyslogd)
	@TMPDIR=$$(mktemp -d) && \
	cp rsyslog.conf $$TMPDIR/rsyslog.conf && \
	cp -r conf.d $$TMPDIR/conf.d && \
	cp -r filters $$TMPDIR/filters && \
	sed -i.bak 's|/etc/rsyslog.d/|'$$TMPDIR'/|g' $$TMPDIR/rsyslog.conf && \
	for f in $$TMPDIR/conf.d/*.conf; do \
		sed -i.bak 's|/etc/rsyslog.d/|'$$TMPDIR'/|g' "$$f" 2>/dev/null; \
	done && \
	rsyslogd -N1 -f $$TMPDIR/rsyslog.conf && \
	echo "Configuration OK" && \
	rm -rf $$TMPDIR || \
	(echo "VALIDATION FAILED" && rm -rf $$TMPDIR && exit 1)

##@ Deploy
deploy: ## Deploy config files (backs up existing first)
	@BACKUP=/tmp/rsyslog-backup-$$(date +%Y%m%d-%H%M%S) && \
	echo "Backing up to $$BACKUP" && \
	sudo mkdir -p $$BACKUP && \
	[ -f /etc/rsyslog.conf ] && sudo cp /etc/rsyslog.conf $$BACKUP/ || true && \
	[ -d $(DEPLOY_DIR) ] && sudo cp -r $(DEPLOY_DIR) $$BACKUP/rsyslog.d || true && \
	sudo mkdir -p $(DEPLOY_DIR)/conf.d $(DEPLOY_DIR)/filters && \
	sudo cp rsyslog.conf /etc/rsyslog.conf && \
	sudo cp conf.d/*.conf $(DEPLOY_DIR)/conf.d/ && \
	sudo cp filters/*.conf $(DEPLOY_DIR)/filters/ && \
	sudo chown -R root:root /etc/rsyslog.conf $(DEPLOY_DIR) && \
	sudo chmod 644 /etc/rsyslog.conf $(DEPLOY_DIR)/conf.d/*.conf $(DEPLOY_DIR)/filters/*.conf && \
	echo "Deployed. Run 'make validate' on host, then 'make restart'."

##@ Service
restart: ## Restart rsyslog service
	sudo systemctl restart rsyslog
	@sudo systemctl status rsyslog --no-pager

##@ Utility
clean: ## Remove backup files
	rm -rf /tmp/rsyslog-backup-*

.DEFAULT_GOAL := help
```

---

## 7. Testing Strategy

### 7.1 Sample Test Messages (`tests/test-messages.txt`)

A fixture file with RFC 5424 structured-data messages that should and should not be filtered:

```text
# Messages that SHOULD be filtered (dropped)
<165>1 2024-11-28T12:00:00.000Z juniper-mx1 sshd 1234 SSHD_LOGIN_ACCEPTED [junos@2636.1.1.1.2.18 username="admin"] Accepted publickey for admin from 10.0.0.1
<165>1 2024-11-28T12:00:01.000Z juniper-mx1 chassisd 5678 CHASSISD_BLOWERS_SPEED [junos@2636.1.1.1.2.18] Fan speed normal at 50%
<165>1 2024-11-28T12:00:02.000Z juniper-mx1 chassisd 5678 CHASSISD_TEMP_ZONE [junos@2636.1.1.1.2.18] Zone 1 temperature 42C
<165>1 2024-11-28T12:00:03.000Z juniper-srx1 RT_FLOW 0 RT_FLOW_SESSION_CREATE [junos@2636.1.1.1.2.18] 10.0.1.1/1234->10.0.2.1/443 TCP
<165>1 2024-11-28T12:00:04.000Z juniper-srx1 RT_FLOW 0 RT_FLOW_SESSION_CLOSE [junos@2636.1.1.1.2.18] 10.0.1.1/1234->10.0.2.1/443 TCP closed
<165>1 2024-11-28T12:00:05.000Z juniper-mx1 rpd 1234 RPD_SCHED_CALLBACK [junos@2636.1.1.1.2.18] scheduled callback fired
<165>1 2024-11-28T12:00:06.000Z juniper-mx1 rpd 1234 RPD_SCHED_MODULE_INFO [junos@2636.1.1.1.2.18] module status update
<165>1 2024-11-28T12:00:07.000Z juniper-mx1 pfed 5678 PFED_FW_SYSLOG_IP [junos@2636.1.1.1.2.18] packet forwarding stats
<165>1 2024-11-28T12:00:08.000Z juniper-mx1 mib2d 1234 MIB2D_POLL [junos@2636.1.1.1.2.18] SNMP polling interface ge-0/0/1
<165>1 2024-11-28T12:00:09.000Z juniper-mx1 cron 9999 CRON_JOB [junos@2636.1.1.1.2.18] (root) CMD (/usr/local/bin/cleanup.sh)
<165>1 2024-11-28T12:00:10.000Z juniper-mx1 ntpd 5678 NTP_SYNC [junos@2636.1.1.1.2.18] synchronized to 10.0.0.1, stratum 2
<165>1 2024-11-28T12:00:11.000Z juniper-ex1 lacpd 1234 LACP_INTF [junos@2636.1.1.1.2.18] ae0 member ge-0/0/0 added
<165>1 2024-11-28T12:00:12.000Z juniper-mx1 snmpd 5678 SNMPD_TRAP_COLD_START [junos@2636.1.1.1.2.18] trap generated
<165>1 2024-11-28T12:00:13.000Z juniper-mx1 snmpd 5678 SNMPD_AUTH_FAILURE [junos@2636.1.1.1.2.18] community mismatch
<165>1 2024-11-28T12:00:14.000Z juniper-mx1 dcd 1234 DCD_INTF_INIT [junos@2636.1.1.1.2.18] interface ge-0/0/1 speed 1000mbps
<165>1 2024-11-28T12:00:15.000Z juniper-mx1 mgd 3046 UI_CFG_AUDIT_SET [junos@2636.1.1.1.2.18 username="admin"] config change
<165>1 2024-11-28T12:00:17.000Z juniper-mx1 lldpd 2345 LLDP_NEIGHBOR_UP [junos@2636.1.1.1.2.18] neighbor discovered on ge-0/0/1: switch2.example.com

# Messages that MUST NOT be filtered (important -- keep these)
<161>1 2024-11-28T12:01:00.000Z juniper-mx1 sshd 1234 UI_LOGIN_EVENT [junos@2636.1.1.1.2.18 username="unknown"] Failed password for invalid user root from 10.99.0.1
<161>1 2024-11-28T12:01:01.000Z juniper-mx1 chassisd 5678 CHASSISD_TEMP_ZONE [junos@2636.1.1.1.2.18] Major alarm: Zone 1 temperature 85C
<161>1 2024-11-28T12:01:02.000Z juniper-srx1 RT_FLOW 0 RT_FLOW_SESSION_DENY [junos@2636.1.1.1.2.18] denied by policy block-all
<161>1 2024-11-28T12:01:03.000Z juniper-mx1 rpd 1234 RPD_BGP_NEIGHBOR_STATE_CHANGED [junos@2636.1.1.1.2.18] BGP peer 10.0.0.2 Down - hold timer expired
<161>1 2024-11-28T12:01:04.000Z juniper-mx1 pfed 5678 PFED_FW_ERROR [junos@2636.1.1.1.2.18] forwarding engine error failure
<161>1 2024-11-28T12:01:05.000Z juniper-mx1 mib2d 1234 MIB2D_ERROR [junos@2636.1.1.1.2.18] error: failed to read interface counters
<161>1 2024-11-28T12:01:06.000Z juniper-mx1 ntpd 5678 NTP_UNREACHABLE [junos@2636.1.1.1.2.18] no servers reachable
<161>1 2024-11-28T12:01:07.000Z juniper-ex1 lacpd 1234 LACP_ERROR [junos@2636.1.1.1.2.18] ERROR: member interface ge-0/0/0 down
<161>1 2024-11-28T12:01:08.000Z juniper-mx1 chassisd 5678 CHASSISD_PSU [junos@2636.1.1.1.2.18] Failed - power supply 1
<161>1 2024-11-28T12:01:09.000Z juniper-mx1 mgd 3046 UI_COMMIT [junos@2636.1.1.1.2.18 username="admin"] commit failed: syntax error in config
<165>1 2024-11-28T12:01:10.000Z juniper-mx1 rpd 5678 RPD_BGP_NEIGHBOR_STATE_CHANGED [junos@2636.1.1.1.2.18] BGP peer 10.0.0.3 (AS 65000) Established
<161>1 2024-11-28T12:01:11.000Z juniper-mx1 rpd 5678 RPD_BGP_NEIGHBOR_STATE_CHANGED [junos@2636.1.1.1.2.18] BGP peer 10.0.0.4 Down: Hold Timer Expired
<161>1 2024-11-28T12:01:12.000Z juniper-mx1 chassisd 5678 CHASSISD_FAN [junos@2636.1.1.1.2.18] Critical - fan tray 2 failed
<160>1 2024-11-28T12:01:13.000Z juniper-mx1 kernel 0 KERNEL_PANIC [junos@2636.1.1.1.2.18] PANIC: unrecoverable error
<165>1 2024-11-28T12:01:14.000Z juniper-mx1 license 0 LICENSE_EXPIRING [junos@2636.1.1.1.2.18] license expiring in 7 days
<161>1 2024-11-28T12:01:15.000Z juniper-mx1 dcd 1234 DCD_INTF_INIT [junos@2636.1.1.1.2.18] failed to initialize interface ge-0/0/2

# UI_COMMIT should route to Oxidized (not dropped, but forwarded then stopped)
<165>1 2024-11-28T12:02:00.000Z juniper-mx1 mgd 3046 UI_COMMIT [junos@2636.1.1.1.2.18 username="admin"] User 'admin' requested commit
<165>1 2024-11-28T12:02:01.000Z juniper-ex1 mgd 3046 UI_COMMIT_COMPLETED [junos@2636.1.1.1.2.18 username="admin"] commit complete
```

### 7.2 Test Script (`tests/test-filters.sh`)

```bash
#!/usr/bin/env bash
#
# test-filters.sh -- Feed sample messages through rsyslog validation
# Usage: make test  (or bash tests/test-filters.sh)
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
TEST_MESSAGES="$SCRIPT_DIR/test-messages.txt"
RSYSLOGD="rsyslogd"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass=0
fail=0
skip=0

# Check for rsyslogd availability
HAVE_RSYSLOGD=false
if command -v $RSYSLOGD >/dev/null 2>&1; then
    HAVE_RSYSLOGD=true
fi

echo "=== rsyslog Filter Tests ==="
echo ""

# Use WORK_DIR instead of TMPDIR to avoid clobbering the standard POSIX variable
WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT

# --- Test 1: Configuration syntax validation ---
echo -n "Test 1: Configuration syntax validation... "
if [ "$HAVE_RSYSLOGD" = false ]; then
    echo -e "${YELLOW}SKIP${NC} (rsyslogd not found)"
    (( skip += 1 ))
else
    cp "$PROJECT_DIR/rsyslog.conf" "$WORK_DIR/"
    cp -r "$PROJECT_DIR/conf.d" "$WORK_DIR/"
    cp -r "$PROJECT_DIR/filters" "$WORK_DIR/"

    # Rewrite paths to use work directory
    if [[ "$OSTYPE" == "darwin"* ]]; then
        find "$WORK_DIR" -name "*.conf" -exec sed -i '' "s|/etc/rsyslog.d/|${WORK_DIR}/|g" {} +
    else
        find "$WORK_DIR" -name "*.conf" -exec sed -i "s|/etc/rsyslog.d/|${WORK_DIR}/|g" {} +
    fi

    if $RSYSLOGD -N1 -f "$WORK_DIR/rsyslog.conf" > /dev/null 2>&1; then
        echo -e "${GREEN}PASS${NC}"
        (( pass += 1 ))
    else
        echo -e "${RED}FAIL${NC}"
        echo "  rsyslogd -N1 reported errors:"
        $RSYSLOGD -N1 -f "$WORK_DIR/rsyslog.conf" 2>&1 | head -20
        (( fail += 1 ))
    fi
fi

# --- Test 2: All filter files parse individually ---
echo -n "Test 2: Individual filter file syntax... "
if [ "$HAVE_RSYSLOGD" = false ]; then
    echo -e "${YELLOW}SKIP${NC} (rsyslogd not found)"
    (( skip += 1 ))
else
    filter_errors=0
    for f in "$PROJECT_DIR/filters/"*.conf; do
        # Wrap in minimal rsyslog config with all modules/templates used by filters
        cat > "$WORK_DIR/test-single.conf" <<CONFEOF
module(load="imtcp")
module(load="mmpstrucdata")
module(load="omprog")
template(name="CommitFormat" type="string" string="%msg%\n")
template(name="OxidizedJSON" type="string" string="%msg%\n")
template(name="ResinTemplate" type="string" string="%msg%\n")
template(name="NetworkDeviceFormat" type="string" string="%msg%\n")
input(type="imtcp" port="15140" ruleset="test_rs")
ruleset(name="test_rs") {
    action(type="mmpstrucdata")
$(cat "$f")
}
CONFEOF
        if ! $RSYSLOGD -N1 -f "$WORK_DIR/test-single.conf" > /dev/null 2>&1; then
            echo ""
            echo -e "  ${RED}FAIL${NC}: $(basename "$f")"
            (( filter_errors += 1 ))
        fi
    done

    if [ $filter_errors -eq 0 ]; then
        echo -e "${GREEN}PASS${NC} ($(ls "$PROJECT_DIR/filters/"*.conf | wc -l | tr -d ' ') files)"
        (( pass += 1 ))
    else
        echo -e "${RED}FAIL${NC} ($filter_errors files with errors)"
        (( fail += 1 ))
    fi
fi

# --- Test 3: Test messages file exists and has content ---
echo -n "Test 3: Test message fixtures exist... "
if [ -f "$TEST_MESSAGES" ] && [ -s "$TEST_MESSAGES" ]; then
    msg_count=$(grep -c '^<' "$TEST_MESSAGES" || true)
    echo -e "${GREEN}PASS${NC} ($msg_count messages)"
    (( pass += 1 ))
else
    echo -e "${RED}FAIL${NC} (missing or empty: $TEST_MESSAGES)"
    (( fail += 1 ))
fi

# --- Test 4: No orphaned include references ---
echo -n "Test 4: All included files exist... "
grep -rh 'include.*file=' "$PROJECT_DIR/rsyslog.conf" "$PROJECT_DIR/conf.d/" 2>/dev/null | \
    grep -oE 'file="[^"]+"' | \
    sed 's/file="//;s/"$//' > "$WORK_DIR/includes.txt" || true

include_errors=0
while read -r inc; do
    resolved="${inc/\/etc\/rsyslog.d\//$PROJECT_DIR/}"
    if [ ! -f "$resolved" ]; then
        echo ""
        echo -e "  ${RED}MISSING${NC}: $inc -> $resolved"
        (( include_errors += 1 ))
    fi
done < "$WORK_DIR/includes.txt"

if [ $include_errors -eq 0 ]; then
    inc_count=$(wc -l < "$WORK_DIR/includes.txt" | tr -d ' ')
    echo -e "${GREEN}PASS${NC} ($inc_count includes resolved)"
    (( pass += 1 ))
else
    echo -e "${RED}FAIL${NC} ($include_errors missing files)"
    (( fail += 1 ))
fi

# --- Test 5: Behavioral filter validation ---
echo -n "Test 5: Behavioral filter validation... "
if [ "$HAVE_RSYSLOGD" = false ]; then
    echo -e "${YELLOW}SKIP${NC} (rsyslogd not found)"
    (( skip += 1 ))
elif ! command -v nc >/dev/null 2>&1; then
    echo -e "${YELLOW}SKIP${NC} (nc not found)"
    (( skip += 1 ))
else
    TEST_PORT=15514
    OUTPUT_FILE="$WORK_DIR/behavioral-output.log"
    PID_FILE="$WORK_DIR/rsyslog-test.pid"

    # Build test config -- includes pure filter files only
    # Excludes 06-ui-commit-trigger.conf (has omprog/omfwd actions requiring external services)
    cat > "$WORK_DIR/behavioral.conf" <<CONFEOF
global(maxMessageSize="64k")
module(load="imudp")
module(load="mmpstrucdata")
template(name="TestOutput" type="string"
    string="KEPT: %msgid% | %programname% | %msg:2:\$%\n")
input(type="imudp" port="$TEST_PORT" ruleset="test_behavioral")

ruleset(name="test_behavioral") {
    action(type="mmpstrucdata")
$(cat "$PROJECT_DIR/filters/05-by-msgid.conf")
$(cat "$PROJECT_DIR/filters/10-by-programname.conf")
$(cat "$PROJECT_DIR/filters/30-by-facility.conf")
$(cat "$PROJECT_DIR/filters/40-by-severity.conf")
    action(type="omfile"
        file="$OUTPUT_FILE"
        template="TestOutput"
        flushOnTXEnd="on"
    )
}
CONFEOF

    # Start rsyslog in background
    $RSYSLOGD -n -f "$WORK_DIR/behavioral.conf" -i "$PID_FILE" >/dev/null 2>&1 &
    RSYSLOG_PID=$!
    sleep 2

    # Check if rsyslog started
    if ! kill -0 $RSYSLOG_PID 2>/dev/null; then
        echo -e "${YELLOW}SKIP${NC} (rsyslogd failed to start -- may need root for UDP listener)"
        (( skip += 1 ))
    else
        # Send test messages
        while IFS= read -r line; do
            [[ "$line" =~ ^#.*$ || -z "$line" ]] && continue
            echo "$line" | nc -u -w1 127.0.0.1 $TEST_PORT 2>/dev/null || true
        done < "$TEST_MESSAGES"
        sleep 2

        # Stop rsyslog
        kill $RSYSLOG_PID 2>/dev/null || true
        wait $RSYSLOG_PID 2>/dev/null || true

        if [ ! -f "$OUTPUT_FILE" ]; then
            echo -e "${YELLOW}SKIP${NC} (no output -- rsyslog may not have processed messages)"
            (( skip += 1 ))
        else
            behavioral_errors=0

            # Messages that SHOULD be dropped (noise)
            for msgid in CHASSISD_BLOWERS_SPEED RPD_SCHED_CALLBACK RPD_SCHED_MODULE_INFO \
                         RT_FLOW_SESSION_CREATE RT_FLOW_SESSION_CLOSE \
                         SNMPD_TRAP_COLD_START SNMPD_AUTH_FAILURE; do
                if grep -q "KEPT: ${msgid} " "$OUTPUT_FILE"; then
                    echo -e "\n  ${RED}LEAK${NC}: $msgid was not filtered (should be dropped)"
                    (( behavioral_errors += 1 ))
                fi
            done

            # Messages that MUST be kept (important)
            for msgid in RT_FLOW_SESSION_DENY RPD_BGP_NEIGHBOR_STATE_CHANGED \
                         KERNEL_PANIC LICENSE_EXPIRING UI_COMMIT; do
                if ! grep -q "KEPT: ${msgid} " "$OUTPUT_FILE"; then
                    echo -e "\n  ${RED}MISSING${NC}: $msgid was filtered (should be kept)"
                    (( behavioral_errors += 1 ))
                fi
            done

            if [ $behavioral_errors -eq 0 ]; then
                kept_count=$(wc -l < "$OUTPUT_FILE" | tr -d ' ')
                echo -e "${GREEN}PASS${NC} ($kept_count messages survived filters)"
                (( pass += 1 ))
            else
                echo -e "${RED}FAIL${NC} ($behavioral_errors errors)"
                (( fail += 1 ))
            fi
        fi
    fi
fi

# --- Summary ---
echo ""
echo "=== Results ==="
echo -e "  ${GREEN}Passed: $pass${NC}"
if [ $fail -gt 0 ]; then
    echo -e "  ${RED}Failed: $fail${NC}"
else
    echo "  Failed: 0"
fi
if [ $skip -gt 0 ]; then
    echo -e "  ${YELLOW}Skipped: $skip${NC}"
fi
echo ""

exit $fail
```

### 7.3 Docker-Based Test Environment (Optional)

For isolated testing without affecting production:

```dockerfile
# Dockerfile.test
FROM debian:trixie-slim

RUN apt-get update && apt-get install -y \
    rsyslog \
    netcat-openbsd \
    && rm -rf /var/lib/apt/lists/*

COPY rsyslog.conf /etc/rsyslog.conf
COPY rsyslog.conf /etc/rsyslog.d/rsyslog.conf
COPY conf.d/ /etc/rsyslog.d/conf.d/
COPY filters/ /etc/rsyslog.d/filters/
COPY tests/ /etc/rsyslog.d/tests/

# Validate config on build
RUN rsyslogd -N1 -f /etc/rsyslog.conf

# Create log and spool directories
RUN mkdir -p /var/log/network /var/spool/rsyslog

# Run tests
RUN bash /etc/rsyslog.d/tests/test-filters.sh

EXPOSE 514/udp 514/tcp 1514/udp

CMD ["rsyslogd", "-n", "-f", "/etc/rsyslog.conf"]
```

Usage:

```bash
# Build and test
docker build -f Dockerfile.test -t rsyslog-test .

# Run container
docker run -d --name rsyslog-test -p 5140:514/udp rsyslog-test

# Send test messages
while IFS= read -r line; do
    [[ "$line" =~ ^#.*$ || -z "$line" ]] && continue
    echo "$line" | nc -u -w0 localhost 5140
done < tests/test-messages.txt

# Check what passed through filters
docker exec rsyslog-test find /var/log/network -type f -exec cat {} +

# Cleanup
docker rm -f rsyslog-test
```

---

## 8. Platform Notes (Debian vs RHEL)

### 8.1 Package Installation

| Item | Debian/Ubuntu | RHEL/CentOS/Rocky |
|---|---|---|
| Install | `apt-get install rsyslog` | `dnf install rsyslog` |
| JSON module | `apt-get install rsyslog-mmjsonparse` | `dnf install rsyslog-mmjsonparse` |
| Elasticsearch | `apt-get install rsyslog-elasticsearch` | `dnf install rsyslog-elasticsearch` |
| PostgreSQL | `apt-get install rsyslog-pgsql` | `dnf install rsyslog-pgsql` |

### 8.2 Default Paths

| Item | Debian/Ubuntu | RHEL/CentOS |
|---|---|---|
| Main config | `/etc/rsyslog.conf` | `/etc/rsyslog.conf` |
| Include dir | `/etc/rsyslog.d/` | `/etc/rsyslog.d/` |
| Module dir | `/usr/lib/x86_64-linux-gnu/rsyslog/` | `/usr/lib64/rsyslog/` |
| Log dir | `/var/log/` | `/var/log/` |
| PID file | `/var/run/rsyslogd.pid` | `/var/run/rsyslogd.pid` |
| Service | `rsyslog.service` | `rsyslog.service` |

### 8.3 SELinux Considerations (RHEL)

SELinux is enforcing by default on RHEL-based systems and can block rsyslog from writing to custom log directories or listening on non-standard ports.

```bash
# Check if SELinux is blocking rsyslog
sudo ausearch -m avc -ts recent | grep rsyslog

# Allow rsyslog to write to /var/log/network/
sudo semanage fcontext -a -t var_log_t "/var/log/network(/.*)?"
sudo restorecon -Rv /var/log/network/

# Allow rsyslog to listen on port 1514 (non-standard)
sudo semanage port -a -t syslogd_port_t -p udp 1514
sudo semanage port -a -t syslogd_port_t -p tcp 1514

# If rsyslog needs to forward to remote hosts and SELinux blocks it
sudo setsebool -P nis_enabled 1
```

### 8.4 Firewall Rules

**Debian/Ubuntu (ufw):**

```bash
sudo ufw allow 514/udp comment "rsyslog UDP"
sudo ufw allow 514/tcp comment "rsyslog TCP"
sudo ufw allow 1514/udp comment "rsyslog high-perf"
```

**RHEL/CentOS (firewalld):**

```bash
sudo firewall-cmd --permanent --add-port=514/udp
sudo firewall-cmd --permanent --add-port=514/tcp
sudo firewall-cmd --permanent --add-port=1514/udp
sudo firewall-cmd --reload
```

---

## 9. Common Juniper Noise Patterns Reference

### 9.1 By Daemon

| Daemon | Noisy Events | Keep (Exceptions) | Typical Volume |
|---|---|---|---|
| `chassisd` | BLOWERS_SPEED, TEMP_ZONE, FAN, PSU, SENSORS | Major, Critical, ERROR, Failed, Alarm | Medium |
| `rpd` | RPD_SCHED_CALLBACK, RPD_SCHED_MODULE_INFO, task_job_create | error, Error, down, Down, fail, Fail | Very High |
| `pfed` | PFED_FW_SYSLOG_IP, PFED_FW_SYSLOG_IP6, PFE_FW_SYSLOG | error, Error | High |
| `snmpd` | SNMPD_TRAP, SNMPD_TRAP_COLD_START, SNMPD_AUTH_FAILURE | error, Error, fail, Fail | High |
| `mib2d` | Routine MIB polling responses | error, Error | Medium |
| `mgd` | UI_DBASE_LOGIN_EVENT, UI_AUTH_EVENT | fail, Fail, error, Error, denied | Medium |
| `dcd` | Routine interface initialization | error, Error, fail, Fail | Low |
| `lacpd` | LACP_INTF routine events | ERROR, WARNING, down, Down | Low |
| `cosd` | Class-of-service routine updates | error, Error | Low |
| `alarmd` | Routine alarm daemon checks | Major, Critical, Alarm, alarm set | Low |
| `sshd` | Accepted sessions, session opened/closed | Failed, failed, Invalid, invalid, refused | Medium |
| `ntpd` | Synchronized, adjust, peer status | error, Error, unreachable | Low |
| `cron` | All scheduled job execution | (none -- drop all) | Medium |

### 9.2 By Event Type

| Event Pattern | What It Is | Action | Notes |
|---|---|---|---|
| `RT_FLOW_SESSION_CREATE` | SRX firewall session opened | Drop | Extremely high volume on busy SRXs |
| `RT_FLOW_SESSION_CLOSE` | SRX firewall session closed | Drop | Equally high volume |
| `RT_FLOW_SESSION_DENY` | SRX firewall session denied | **Keep** | Security-relevant |
| `UI_COMMIT` | Configuration commit started | Route to Oxidized, then stop | Triggers config backup via Oxidized |
| `UI_COMMIT_COMPLETED` | Configuration commit finished | Route to Oxidized, then stop | Triggers config backup via Oxidized |
| `UI_LOGIN_EVENT` | User login | **Keep** | Audit trail |
| `UI_LOGOUT_EVENT` | User logout | **Keep** | Audit trail |
| `UI_CFG_AUDIT_SET` | Config change audit trail | Drop | Consider keeping for audit |
| `SNMPD_TRAP_COLD_START` | Device cold boot trap | Drop | Usually follows a known restart |
| `SNMPD_TRAP_WARM_START` | Device warm boot trap | Drop | Usually follows a known restart |
| `SNMPD_AUTH_FAILURE` | SNMP community mismatch | Drop | Usually misconfigured pollers |
| `RPD_SCHED_CALLBACK` | Internal rpd scheduler tick | Drop | Very high volume, zero value |
| `RPD_SCHED_MODULE_INFO` | rpd module status update | Drop | Internal bookkeeping |
| `CHASSISD_BLOWERS_SPEED` | Fan RPM readings | Drop (unless alarm) | Periodic readings |
| `CHASSISD_TEMP_ZONE` | Temperature sensor readings | Drop (unless alarm) | Periodic readings |
| `BGP_PREFIX_THRESH_EXCEEDED` | BGP prefix limit reached | **Keep** | Capacity planning alert |
| `KERN_ARP_ADDR_CHANGE` | ARP table change | Context-dependent | Can indicate issues |
| `OSPF_NBRUP` / `OSPF_NBRDOWN` | OSPF neighbor state change | **Keep** | Network topology change |
| `BFDD_TRAP_STATE_DOWN` | BFD session went down | **Keep** | Fast failure detection |
| `LLDP_NEIGHBOR_UP/DOWN` | LLDP discovery | Drop (unless error) | Routine |

### 9.3 Severity Guidance

| Severity | Numeric | Action | Rationale |
|---|---|---|---|
| emerg (0) | 0 | **Always keep** | System unusable |
| alert (1) | 1 | **Always keep** | Immediate action needed |
| crit (2) | 2 | **Always keep** | Hardware failure, routing crash |
| err (3) | 3 | **Always keep** | Error conditions |
| warning (4) | 4 | **Keep** | Degraded but functional |
| notice (5) | 5 | Filter selectively | Normal but significant -- most Juniper noise lives here |
| info (6) | 6 | Filter aggressively | Informational, usually noise |
| debug (7) | 7 | **Always drop** | Debug should never be on in production |

### 9.4 Exception Keywords

These keywords in a message body should prevent filtering, regardless of the daemon or event type.

The filter files use `re_match(tolower($msg), "...")` to handle all case variants with a single pattern instead of listing each variant separately. For example, `re_match(tolower($msg), "error|fail|down")` matches `error`, `Error`, `ERROR`, `fail`, `Fail`, `FAIL`, `down`, `Down`, `DOWN`, etc.

```
error
fail, failed
down
critical
major
alarm
panic
unreachable
denied, deny
expired, expir
exceeded
trap (context-dependent)
```

---

## 10. Tips & Best Practices

### 10.1 Filter Ordering for Performance

Process filters in order of cost (cheapest first):

1. **`$msgid ==`** -- Parsed RFC 5424 header field, exact match. Use this for all event name filtering.
2. **`$programname ==`** -- Pre-parsed, single comparison. Use this to drop entire daemons.
3. **`$syslogseverity`** -- Numeric comparison. Drop debug globally.
4. **`$msg contains`** -- Linear string scan, but fast. Use for exception keywords in the message body.
5. **`$msg startswith`** -- Only checks beginning of string. Faster than `contains` when applicable.
6. **`re_match()`** -- Compiled regex. Avoid unless absolutely necessary.
7. **`re_extract()`** -- Regex with capture. Only when you need to extract fields.

### 10.2 `$msgid` vs `$programname` vs `$msg contains`

```conf
# BEST -- exact match on parsed MSGID field, zero ambiguity
if ($msgid == "CHASSISD_BLOWERS_SPEED") then { ... }

# GOOD -- programname is pre-parsed, instant comparison (for daemon-level drops)
if ($programname == "chassisd") then { ... }

# ACCEPTABLE -- scans message body for exception keywords
if ($msg contains "error") then { ... }
```

Use `$msgid ==` for event name matching. Use `$programname ==` for dropping entire daemons. Use `$msg contains` only for exception keywords in the message body (error, fail, down, etc.).

### 10.3 Regex Performance

- Avoid `.*` at the start of patterns -- it forces scanning from every position
- Use `$msgid ==` chains instead of `re_match()` for event name alternation
- Compile-time cost: regex patterns are compiled once at config load, not per-message
- Run-time cost: still 5-10x slower than `contains` per message

```conf
# BAD -- regex just to check two event names
if re_match($msg, "CHASSISD_(BLOWERS_SPEED|TEMP_ZONE)") then { ... }

# GOOD -- two exact $msgid checks
if ($msgid == "CHASSISD_BLOWERS_SPEED" or $msgid == "CHASSISD_TEMP_ZONE") then { ... }
```

### 10.4 Log Rotation

Set up logrotate for filtered output files:

```
# /etc/logrotate.d/rsyslog-network
/var/log/network/*.log {
    weekly
    rotate 12
    compress
    delaycompress
    missingok
    notifempty
    create 0644 syslog adm
    sharedscripts
    postrotate
        /usr/lib/rsyslog/rsyslog-rotate
    endscript
}
```

### 10.5 Monitoring with Prometheus & Grafana

The `impstats` module exposes internal counters every N seconds. By piping these through [rsyslog_exporter](https://github.com/prometheus-community/rsyslog_exporter), Prometheus can scrape them and Grafana can visualize them.

#### Available Metrics

**Queue counters** (per queue -- main Q, each ruleset, each action queue):

| Counter | Meaning |
|---------|---------|
| `size` | Current messages waiting in queue |
| `enqueued` | Total messages that entered the queue |
| `full` | Times the queue hit capacity |
| `maxqsize` | Peak queue depth since start |
| `discarded.full` | Messages lost -- queue completely full |
| `discarded.nf` | Messages dropped -- queue nearly full (lower severity dropped first) |

**Action counters** (per action -- each `omfile`, `omprog`, `omfwd`):

| Counter | Meaning |
|---------|---------|
| `processed` | Total messages this action handled |
| `failed` | Messages that failed delivery |
| `suspended` | Times the action entered suspended state |
| `suspended.duration` | Total seconds spent suspended |
| `resumed` | Times the action recovered from suspension |

**Input counters** (per input listener):

| Counter | Meaning |
|---------|---------|
| `submitted` | Messages received by this input |

**Resource usage** (from `getrusage()`):

| Counter | Meaning |
|---------|---------|
| `utime` | User CPU time (microseconds) |
| `stime` | System CPU time (microseconds) |
| `maxrss` | Peak resident set size (KB) |

#### rsyslog Configuration

The `impstats` module and `rsyslog_stats` ruleset are defined in `00-modules.conf`, commented out by default. Uncomment both when `rsyslog_exporter` is installed:

```conf
######################
# Prometheus Stats Exporter
# Defined in 00-modules.conf (commented out by default)
# Uncomment together with impstats above.
######################

# ruleset(name="rsyslog_stats") {
#     action(type="omprog"
#         name="prometheus_exporter"
#         binary="/usr/local/bin/rsyslog_exporter"
#         queue.type="LinkedList"
#         queue.size="1000"
#         queue.workerThreads="1"
#         action.resumeRetryCount="10"
#         action.resumeInterval="30"
#     )
# }
```

This pipes JSON-formatted stats to the exporter binary, which exposes them as Prometheus metrics on `:9104/metrics`.

#### Installing the Exporter

```bash
# Download the latest release
curl -LO https://github.com/prometheus-community/rsyslog_exporter/releases/latest/download/rsyslog_exporter-linux-amd64
sudo install rsyslog_exporter-linux-amd64 /usr/local/bin/rsyslog_exporter

# Or build from source
go install github.com/prometheus-community/rsyslog_exporter@latest
sudo cp $(go env GOPATH)/bin/rsyslog_exporter /usr/local/bin/
```

The exporter is started by rsyslog via `omprog` -- it does not need a systemd service. It listens on `:9104` by default.

#### Prometheus Scrape Config

```yaml
scrape_configs:
  - job_name: rsyslog
    scrape_interval: 15s
    static_configs:
      - targets: ['librenms.example.com:9104']
        labels:
          instance: 'syslog-collector'
```

#### Grafana Dashboard

Import the pre-built dashboard from [Grafana Dashboard #13581](https://grafana.com/grafana/dashboards/13581-9107-rsyslog/) for a ready-made view of rsyslog metrics.

For a custom dashboard tailored to Juniper filtering, use these panels:

| Panel | PromQL | Purpose |
|-------|--------|---------|
| **Ingest rate** | `rate(rsyslog_input_submitted[5m])` | Messages/sec hitting the collector |
| **Queue depth** | `rsyslog_queue_size` | Current backlog per queue |
| **Peak queue** | `rsyslog_queue_maxqsize` | Highest backlog since restart |
| **Output throughput** | `rate(rsyslog_action_processed[5m])` | Delivery rate per action (LibreNMS, local files) |
| **Output failures** | `rsyslog_action_failed` | LibreNMS omprog or file write failures |
| **Discarded messages** | `rsyslog_queue_discarded_full` | Non-zero means data loss |
| **Filter drop rate** | `rate(rsyslog_input_submitted[5m]) - rate(rsyslog_action_processed{name="output_librenms"}[5m])` | Messages/sec dropped by filters |
| **Filter ratio** | `1 - (rate(rsyslog_action_processed{name="output_librenms"}[5m]) / rate(rsyslog_input_submitted[5m]))` | Percentage of messages filtered |
| **Action suspensions** | `rsyslog_action_suspended` | Detect downstream problems |
| **Memory** | `rsyslog_resource_maxrss` | RSS growth over time |

The **filter ratio** panel is the most useful for tuning -- it shows what percentage of incoming syslog your `$msgid` and `$programname` filters are dropping. A healthy filter set typically drops 60-80% of raw Juniper syslog.

#### Quick Verification Without Grafana

```bash
# Check the exporter is running and serving metrics
curl -s http://localhost:9104/metrics | grep rsyslog_input_submitted

# Check queue health (should be near zero)
curl -s http://localhost:9104/metrics | grep rsyslog_queue_size

# Check for failures
curl -s http://localhost:9104/metrics | grep rsyslog_action_failed
```

### 10.6 Debug Output for Building Filters

When building new filters you need to see how rsyslog parses each incoming message -- which field holds what -- so you know what to match on. This debug config writes all parsed properties to a single file, scoped to a specific device IP so you do not drown in traffic from the entire network.

#### Setup

The `DebugFields` template is already defined in `01-templates.conf`. To enable debug output, uncomment the debug block in `20-ruleset.conf` and set the IP to the device you want to inspect:

```conf
ruleset(name="network_devices" ...) {
    action(type="mmpstrucdata")

    # --- Debug: dump all fields for one device ---
    # Change the IP to the device you want to inspect.
    # Comment out or remove when done.
    if ($fromhost-ip == "10.0.1.1") then {
        action(type="omfile"
            file="/var/log/network/debug.log"
            template="DebugFields"
            fileCreateMode="0644"
            dirCreateMode="0755"
        )
    }
    # --- End debug ---

    # Phase 1: filters
    include(file="/etc/rsyslog.d/filters/05-by-msgid.conf")
    ...
}
```

Create the directory and restart:

```bash
sudo mkdir -p /var/log/network
sudo systemctl restart rsyslog
```

#### What You Get

```bash
tail -f /var/log/network/debug.log
```

```
---
TIME:       2024-11-28T12:00:01.234+00:00
FROM_IP:    10.0.1.1
FROM_HOST:  juniper-mx1
HOSTNAME:   juniper-mx1
PROGRAM:    chassisd
TAG:        chassisd[5678]:
PID:        5678
MSGID:      CHASSISD_BLOWERS_SPEED
FACILITY:   23 (local7)
SEVERITY:   5 (notice)
PRI:        189 (local7.notice)
APP_NAME:   chassisd
SD:         [junos@2636.1.1.1.2.18]
INPUT:      imudp
MSG:         Fan speed normal at 50%

---
TIME:       2024-11-28T12:00:01.456+00:00
FROM_IP:    10.0.1.1
FROM_HOST:  juniper-mx1
HOSTNAME:   juniper-mx1
PROGRAM:    rpd
TAG:        rpd[1234]:
PID:        1234
MSGID:      RPD_SCHED_CALLBACK
FACILITY:   23 (local7)
SEVERITY:   5 (notice)
PRI:        189 (local7.notice)
APP_NAME:   rpd
SD:         [junos@2636.1.1.1.2.18]
INPUT:      imudp
MSG:         scheduled callback fired
```

Every field is labeled. You can immediately see:

- `MSGID: CHASSISD_BLOWERS_SPEED` -- use `$msgid == "CHASSISD_BLOWERS_SPEED"` in `05-by-msgid.conf`
- `PROGRAM: chassisd` -- use `$programname == "chassisd"` in `10-by-programname.conf` for daemon-level drops
- `SEVERITY: 5 (notice)` -- filterable by `$syslogseverity > 4` in `40-by-severity.conf`
- `MSG:` -- the actual text to match exception keywords with `$msg contains`

#### Scoping to Multiple Devices or a Subnet

```conf
# Single device
if ($fromhost-ip == "10.0.1.1") then { ... }

# Multiple devices
if ($fromhost-ip == "10.0.1.1" or
    $fromhost-ip == "10.0.1.2") then { ... }

# Entire subnet
if ($fromhost-ip startswith "10.0.1.") then { ... }
```

#### Useful Grep Recipes

```bash
# List all unique event names (MSGID) from the device
grep '^MSGID:' /var/log/network/debug.log | sort | uniq -c | sort -rn

# List all unique daemons (PROGRAM)
grep '^PROGRAM:' /var/log/network/debug.log | sort | uniq -c | sort -rn

# Show only messages at severity notice (5) -- where most noise lives
grep -A14 '^SEVERITY:   5' /var/log/network/debug.log

# Show only messages from rpd
grep -B5 -A9 '^PROGRAM:    rpd' /var/log/network/debug.log

# Find all messages containing "error" in the body
grep -B13 'error\|Error\|ERROR' /var/log/network/debug.log | grep '^MSGID:'
```

#### Cleanup

Remove the debug block from the ruleset and restart when done. The file grows fast on a busy device.

```bash
sudo rm -f /var/log/network/debug.log
sudo systemctl restart rsyslog
```

### 10.7 Common Mistakes

| Mistake | Problem | Fix |
|---|---|---|
| Filters after outputs | Messages already forwarded | Put filter includes before output calls in ruleset |
| Missing `mmpstrucdata` | `$msgid` and structured-data fields are empty | Add `module(load="mmpstrucdata")` and `action(type="mmpstrucdata")` in ruleset |
| Case-sensitive keywords | `error` doesn't match `Error` | Use `re_match(tolower($msg), "error")` -- POSIX ERE doesn't support `(?i)` |
| Regex without anchoring | `"10.0.1"` matches `"110.0.1"` | Use `"10\\.0\\.1\\."` with escaped dots |
| `stop` in wrong scope | Stops the wrong ruleset | Verify `stop` is inside the correct `if` block |
| Missing exception keywords | Real errors get filtered | Always add `not ($msg contains "error" or ...)` |
| Using `$msg contains` for event names | Slower and risk of false matches | Use `$msgid ==` for event name matching |

### 10.8 Never Filter These

Regardless of volume, never filter these message types:

- **Hardware failures:** CHASSISD with `Critical`, `Major`, `Failed`, `Alarm`
- **Routing state changes:** BGP `Established`/`Down`, OSPF neighbor changes, BFD session down
- **Security events:** Failed logins, denied sessions, authentication failures
- **Kernel panics:** Any message with `PANIC`, `panic`, or `trap`
- **License expiration:** Messages containing `expir` or `invalid`
- **Link state changes:** Interface up/down (without "statistics" qualifier)

---

## References

- [rsyslog Filter Conditions](https://www.rsyslog.com/doc/configuration/filters.html)
- [RainerScript Language](https://www.rsyslog.com/doc/rainerscript/index.html)
- [rsyslog Message Properties](https://www.rsyslog.com/doc/configuration/properties.html)
- [rsyslog Basic Structure](https://www.rsyslog.com/doc/configuration/basic_structure.html)
- [rsyslog Templates](https://www.rsyslog.com/doc/configuration/templates.html)
- [rsyslog impstats Module](https://www.rsyslog.com/doc/configuration/modules/impstats.html)
- [rsyslog Statistic Counters](https://www.rsyslog.com/doc/configuration/rsyslog_statistic_counter.html)
- [rsyslog_exporter (Prometheus)](https://github.com/prometheus-community/rsyslog_exporter)
- [Grafana Dashboard #13581 -- Rsyslog](https://grafana.com/grafana/dashboards/13581-9107-rsyslog/)
- [rsyslog mmpstrucdata Module](https://www.rsyslog.com/doc/configuration/modules/mmpstrucdata.html)
- [Juniper Syslog Messages Reference](https://www.juniper.net/documentation/us/en/software/junos/network-mgmt/topics/ref/statement/syslog-edit-system.html)
- [Juniper: Interpreting Structured-Data Messages](https://www.juniper.net/documentation/en_US/junos/topics/reference/general/syslog-interpreting-msg-generated-structured-data-format.html)
- [Juniper: Configuring Structured-Data Format](https://www.juniper.net/documentation/en_US/junos/topics/task/configuration/syslog-single-chassis-system-structured-data-format-configuring.html)
- [Juniper: `structured-data` Statement Reference](https://www.juniper.net/documentation/us/en/software/junos/network-mgmt/topics/ref/statement/structured-data-edit-system.html)
- [Juniper System Log Explorer](https://apps.juniper.net/syslog-explorer/)
