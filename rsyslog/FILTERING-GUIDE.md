# How to Create rsyslog Filter Rules

A practical guide for writing rules that drop noisy syslog messages before they hit your database. No prior rsyslog experience needed.

## How filtering works

rsyslog processes messages top-to-bottom through a chain of filter files. Each filter checks a condition and either **drops** the message (`stop`) or lets it continue to the next filter.

```
Message arrives
  -> 05-by-msgid.conf        Check the event name (e.g. CHASSISD_TEMP_ZONE)
  -> 10-by-programname.conf   Check the daemon name (e.g. cron, ntpd)
  -> 30-by-facility.conf      Check the facility (e.g. local7)
  -> 40-by-severity.conf      Check the severity (e.g. debug)
  -> 50-by-hostname.conf      Check the sender (e.g. lab-router-01)
  -> outputs                   Message is stored/forwarded
```

If a filter says `stop`, the message is gone. Everything else passes through.

## The basic pattern

Every filter rule follows this structure:

```
if (CONDITION) then { stop }
```

That's it. `stop` means "drop this message and don't process it further."

### Drop with safety net

Most of the time you want to drop routine noise but keep anything that smells like a problem. Add an exception check:

```
if (CONDITION) then {
    if (not re_match(tolower($msg), "error|fail|critical|down")) then { stop }
}
```

This reads as: "If the condition matches AND the message does NOT contain any of those keywords, drop it."

`tolower($msg)` makes the keyword match case-insensitive, so it catches `Error`, `ERROR`, and `error`.

## Filter types (with real examples)

### 1. Filter by event name (`$msgid`)

The most precise filter. Matches the exact event type from the syslog header. Add these to `filters/05-by-msgid.conf`.

**Drop chassis hardware polling (temperature, fans, PSU):**
```
if ($msgid == "CHASSISD_BLOWERS_SPEED" or
    $msgid == "CHASSISD_TEMP_ZONE" or
    $msgid == "CHASSISD_FAN" or
    $msgid == "CHASSISD_PSU" or
    $msgid == "CHASSISD_SENSORS") then {
    if (not re_match(tolower($msg), "major|critical|alarm|failed")) then { stop }
}
```
Drops thousands of routine hardware checks per hour, but keeps messages about actual hardware failures.

**Drop SRX firewall session create/close logs:**
```
if ($msgid == "RT_FLOW_SESSION_CREATE" or
    $msgid == "RT_FLOW_SESSION_CLOSE") then {
    if (not re_match(tolower($msg), "deny|error")) then { stop }
}
```
On a busy SRX, session logs can be 90%+ of all syslog volume. This keeps denied traffic and errors.

**Drop BGP I/O noise (keep state changes):**
```
if ($msgid == "BGP_CONNECT" or
    $msgid == "BGP_READ" or
    $msgid == "BGP_WRITE" or
    $msgid == "BGP_SEND" or
    $msgid == "BGP_RECV") then {
    if (not re_match(tolower($msg), "established|down|error|notification")) then { stop }
}
```
Keeps the important stuff: peer going up (`established`), peer going down, and BGP notifications.

**Drop SNMP traps (cold/warm start, auth failures):**
```
if ($msgid == "SNMPD_TRAP_COLD_START" or
    $msgid == "SNMPD_TRAP_WARM_START" or
    $msgid == "SNMPD_AUTH_FAILURE" or
    $msgid == "SNMPD_TRAP_QUEUE") then { stop }
```
No exception check here -- these are never operationally useful in syslog.

**Drop LLDP neighbor-up (keep neighbor-down):**
```
if ($msgid == "LLDP_NEIGHBOR_UP") then {
    if (not re_match(tolower($msg), "error")) then { stop }
}
```
Neighbor going away is interesting. Neighbor showing up is routine.

**Drop license routine checks (keep expiry warnings):**
```
if ($msgid startswith "LICENSE_" and
    not re_match(tolower($msg), "expir|invalid")) then { stop }
```
Uses `startswith` to catch all license-related events in one rule.

### 2. Filter by daemon name (`$programname`)

Drops all output from a specific daemon. Broader than msgid -- use when a daemon is entirely noisy. Add these to `filters/10-by-programname.conf`.

**Drop all cron messages:**
```
if ($programname == "cron" or $programname == "CRON") then { stop }
```
Cron logs are never useful from network devices. No exception needed.

**Drop NTP sync noise (keep errors):**
```
if ($programname == "ntpd") then {
    if (not re_match(tolower($msg), "error|unreachable|no server")) then { stop }
}
```

**Drop MIB polling noise:**
```
if ($programname == "mib2d") then {
    if (not re_match(tolower($msg), "error")) then { stop }
}
```
`mib2d` polls SNMP MIBs constantly. Only errors matter.

**Drop SSH session noise (keep failed logins):**
```
if ($programname == "sshd") then {
    if (not re_match(tolower($msg), "failed|invalid|error|refused")) then { stop }
}
```
Keeps failed login attempts for security audit.

**Drop LACP routine messages (keep state changes):**
```
if ($programname == "lacpd") then {
    if (not re_match(tolower($msg), "error|warning|down")) then { stop }
}
```

### 3. Filter by severity

Global noise reduction. Add to `filters/40-by-severity.conf`.

**Drop all debug messages:**
```
if ($syslogseverity == 7) then { stop }
```

Severity numbers for reference:
| Number | Name | When to drop |
|--------|------|-------------|
| 0 | emerg | Never |
| 1 | alert | Never |
| 2 | crit | Never |
| 3 | err | Never |
| 4 | warning | Rarely |
| 5 | notice | Sometimes, per-daemon |
| 6 | info | Often, per-daemon |
| 7 | debug | Almost always |

### 4. Filter by hostname or IP

Target specific devices. Add to `filters/50-by-hostname.conf`.

**Drop everything from a lab device:**
```
if ($hostname == "lab-router-01") then { stop }
```

**Drop a whole subnet (e.g. lab network):**
```
if ($fromhost-ip startswith "10.0.99.") then { stop }
```

**Only accept messages from specific devices (allowlist):**
```
if (not ($fromhost-ip == "10.0.1.1" or
         $fromhost-ip == "10.0.1.2" or
         $fromhost-ip == "10.0.2.1")) then { stop }
```
Use with caution -- this drops everything not in the list.

### 5. Filter by facility

Facility-level filtering. Add to `filters/30-by-facility.conf`.

**Drop local7 info noise (common Juniper default):**
```
if ($syslogfacility == 23 and $syslogseverity == 6) then { stop }
```

Common facility numbers:
| Number | Name | Typical use |
|--------|------|------------|
| 4 | auth | Login/auth events |
| 10 | authpriv | Privileged auth |
| 19 | local3 | Juniper firewall logs |
| 23 | local7 | Juniper daemon default |

### 6. Filter by message content (`$msg`)

Last resort when msgid or programname aren't specific enough.

**Drop messages containing a specific string:**
```
if ($msg contains "SNMP_TRAP_LINK_UP") then { stop }
```

**Drop messages matching a regex pattern:**
```
if re_match($msg, "scheduled backup completed") then { stop }
```

**Case-insensitive content match:**
```
if re_match(tolower($msg), "scheduled backup completed") then { stop }
```

Content matching is slower than msgid or programname checks. Use it only when the other methods don't work.

## Writing your own filter: step by step

### 1. Find the noisy message

Look at your logs and identify what you want to drop. Note the key fields:

```
2024-01-15T10:00:01 core-rtr-01 rpd[1234] CHASSISD_TEMP_ZONE: ...
                     ^hostname   ^program  ^msgid
```

### 2. Pick the right filter type

| You know the... | Use | File |
|---|---|---|
| Event name (e.g. `CHASSISD_TEMP_ZONE`) | `$msgid ==` | `05-by-msgid.conf` |
| Daemon name (e.g. `rpd`, `sshd`) | `$programname ==` | `10-by-programname.conf` |
| Facility number | `$syslogfacility ==` | `30-by-facility.conf` |
| Severity level | `$syslogseverity ==` | `40-by-severity.conf` |
| Hostname or IP | `$hostname ==` / `$fromhost-ip ==` | `50-by-hostname.conf` |

Prefer msgid over programname, and programname over message content. More specific = fewer accidental drops.

### 3. Decide on exception keywords

Ask yourself: "If this event type contained the word `error` or `fail`, would I want to see it?" If yes, add the safety net:

```
if (not re_match(tolower($msg), "error|fail|critical|down")) then { stop }
```

Common exception keyword sets:
- **Hardware:** `major|critical|alarm|failed`
- **Network:** `error|fail|down|unreachable`
- **Security:** `failed|invalid|error|refused|denied`
- **General:** `error|fail|critical`

### 4. Add the rule to the correct file

Open the matching filter file and add your rule. Keep related rules grouped together with a comment:

```
# --- My new filter: drop routine widget checks ---
if ($msgid == "WIDGET_CHECK_OK") then {
    if (not re_match(tolower($msg), "error|fail")) then { stop }
}
```

### 5. Test it

```sh
# Validate config syntax
make validate

# Run the full test suite
make test

# Or test in Docker (no local rsyslog needed)
docker compose run --rm test
```

## Real-world example: silencing NTP noise from a specific host

You're seeing thousands of these from one firewall:

```
time: 2026-03-26T09:47:08.664113Z
hostname: fw-node1
ip: 10.0.1.10
program: xntpd
msgid: -
severity: err
facility: ntp
message: NTP Server 192.168.1.100 is Unreachable
```

The NTP server is known to be unreachable (decommissioned, firewalled, etc.) and you don't need the alerts. Here's how to filter it, from simplest to most targeted:

### Option A: Drop all NTP messages from this host

Add to `filters/50-by-hostname.conf`:

```
# --- fw-node1: NTP server is decommissioned, drop NTP noise ---
if ($fromhost-ip == "10.0.1.10" and $programname == "xntpd") then { stop }
```

This drops every `xntpd` message from that IP. Simple, but you'll miss if a *different* NTP problem starts.

### Option B: Drop only "Unreachable" messages from this host

Add to `filters/50-by-hostname.conf`:

```
# --- fw-node1: known-unreachable NTP server 192.168.1.100 ---
if ($fromhost-ip == "10.0.1.10" and
    $programname == "xntpd" and
    $msg contains "192.168.1.100") then { stop }
```

More targeted -- only drops messages about that specific NTP server. If a different NTP server becomes unreachable, you'll still see it.

### Option C: Drop NTP "Unreachable" globally (all hosts)

Add to `filters/10-by-programname.conf`:

```
# --- xntpd: drop known-unreachable NTP server across all devices ---
if ($programname == "xntpd" and
    $msg contains "192.168.1.100" and
    $msg contains "Unreachable") then { stop }
```

Use this when the NTP server is unreachable from *all* devices and you want to silence it everywhere.

### Which option to pick?

| Option | Scope | Risk of missing real problems |
|--------|-------|------------------------------|
| A | All NTP from one host | Medium -- misses new NTP issues on that host |
| B | One NTP server from one host | Low -- only silences the known-bad server |
| C | One NTP server from all hosts | Low -- but affects every device |

Option B is usually the best balance. Start there and broaden only if needed.

## Top 10 things to filter in a typical network

These are the highest-volume, lowest-value messages from Juniper devices:

| # | What | Why it's noisy | Filter type |
|---|------|---------------|-------------|
| 1 | `RT_FLOW_SESSION_CREATE/CLOSE` | SRX firewalls log every session | msgid |
| 2 | `CHASSISD_*` (temp, fan, PSU) | Hardware polls every few seconds | msgid |
| 3 | `cron` / `CRON` | Scheduled tasks on every device | programname |
| 4 | `mib2d` | SNMP MIB polling runs constantly | programname |
| 5 | Debug severity (7) | Verbose diagnostic output | severity |
| 6 | `SNMPD_TRAP_*` | Trap storms on reboot/failover | msgid |
| 7 | `BGP_READ/WRITE/SEND/RECV` | BGP I/O on every keepalive | msgid |
| 8 | `sshd` session open/close | Every SSH session to every device | programname |
| 9 | `LLDP_NEIGHBOR_UP` | Neighbor discovery runs constantly | msgid |
| 10 | `RPD_SCHED_*` | RPD scheduler ticks every second | msgid |

## Quick reference: rsyslog comparison operators

| Operator | Example | Meaning |
|----------|---------|---------|
| `==` | `$msgid == "FOO"` | Exact match |
| `!=` | `$msgid != "FOO"` | Not equal |
| `contains` | `$msg contains "text"` | Substring match |
| `startswith` | `$msgid startswith "BGP_"` | Starts with prefix |
| `re_match()` | `re_match($msg, "pat")` | Regex match (POSIX ERE) |
| `tolower()` | `tolower($msg)` | Lowercase for case-insensitive matching |
| `and` / `or` | `$a == "X" or $a == "Y"` | Combine conditions |
| `not` | `not re_match(...)` | Negate a condition |

## Common mistakes

- **Forgetting `then`**: every `if` needs `then { ... }`. Rsyslog won't warn you clearly.
- **Missing quotes**: values must be quoted -- `$msgid == "FOO"`, not `$msgid == FOO`.
- **Wrong severity number**: severity 6 is info, 7 is debug. Dropping `> 5` kills all info too.
- **Regex instead of exact match**: use `==` when you know the exact string. It's faster and can't accidentally match substrings.
- **No exception keywords**: dropping an entire msgid without checking for error/fail keywords means you'll miss actual problems silently.
- **Case sensitivity**: `$msg contains "Error"` won't match `ERROR`. Use `re_match(tolower($msg), "error")` instead.
