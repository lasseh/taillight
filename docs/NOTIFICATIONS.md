# Notification System

Taillight includes a pluggable notification engine that evaluates incoming syslog and applog events against user-defined rules and dispatches alerts to Slack, webhooks, or email. This document covers configuration, rule management, anti-spam mechanisms, and the full API reference.

## Overview

The notification pipeline has four stages:

```
Event arrives (syslog or applog)
  |
  v
Rule evaluation — each enabled rule's filter is tested against the event
  |
  v
GroupTracker — burst accumulation, cooldown, and escalation
  |
  v
Dispatch workers — send to backends (Slack, webhook, email)
  with per-channel rate limiting and circuit breakers
```

1. **Rule evaluation.** When a syslog or applog event arrives, the engine iterates over all enabled rules of the matching event kind and tests the event against each rule's filter fields. If the event satisfies a rule, it enters the group tracker.

2. **Group tracking.** Events are grouped by rule ID and a configurable group key (default: hostname for syslog, host for applog). The group tracker implements burst accumulation and cooldown to prevent alert storms.

3. **Dispatch.** When a group flushes (burst window expires or cooldown fires a digest), the engine resolves the rule's associated channels and places a dispatch job on a buffered queue. A pool of dispatch workers processes jobs concurrently.

4. **Backend delivery.** Each dispatch worker sends the notification to each channel's backend. Per-channel rate limiting and circuit breakers protect against downstream failures.

Every send attempt (success or failure) is logged to the `notification_log` table for auditing.

## Configuration

Notification settings live in `config.yml` under the `notification` key. SMTP settings for the email backend live under the `smtp` key.

```yaml
notification:
  enabled: true
  rule_refresh_interval: 30s
  dispatch_workers: 4
  dispatch_buffer: 1024
  default_burst_window: 30s
  default_cooldown: 5m
  default_max_cooldown: 1h
  send_timeout: 10s

smtp:
  host: "smtp.example.com"
  port: 587
  username: "alerts@example.com"
  password: "secret"
  from: "taillight@example.com"
  tls: true
  auth_type: "plain"
```

### Notification settings

| Key | Default | Description |
|-----|---------|-------------|
| `notification.enabled` | `false` | Master switch. When false, no rules are evaluated and no alerts are sent. |
| `notification.rule_refresh_interval` | `30s` | How often the engine reloads rules and channels from the database. Changes made via the API take effect within this interval. |
| `notification.dispatch_workers` | `4` | Number of concurrent goroutines processing the dispatch queue. Increase if you have many channels or slow backends. |
| `notification.dispatch_buffer` | `1024` | Size of the internal dispatch queue. If the queue fills up, new notifications are dropped with a warning log. |
| `notification.default_burst_window` | `30s` | Default burst accumulation window applied when a rule does not specify its own `burst_window`. |
| `notification.default_cooldown` | `1m` | Default minimum time between alerts for the same rule+group, applied when a rule does not specify `cooldown_seconds`. |
| `notification.default_max_cooldown` | `1h` | Maximum cooldown duration after exponential backoff escalation. Applied when a rule does not specify `max_cooldown_seconds`. |
| `notification.send_timeout` | `10s` | HTTP timeout for each individual backend send operation. |

### SMTP settings

The email backend is only registered when `smtp.host` is set. If `smtp.host` is empty, email channels cannot be created.

| Key | Default | Description |
|-----|---------|-------------|
| `smtp.host` | `""` | SMTP server hostname. Empty disables the email backend. |
| `smtp.port` | `587` | SMTP server port. |
| `smtp.username` | `""` | SMTP authentication username. |
| `smtp.password` | `""` | SMTP authentication password. |
| `smtp.from` | `taillight@localhost` | Sender address for outgoing email. |
| `smtp.tls` | `true` | Use STARTTLS when connecting to the SMTP server. |
| `smtp.auth_type` | `plain` | Authentication mechanism: `plain`, `crammd5`, or `""` (no auth). |

## Channels

A channel defines a notification destination. Each channel has a type, a name, and a type-specific configuration object.

### Slack

Sends notifications via Slack Incoming Webhooks. Messages use Block Kit with color-coded attachments based on event severity.

Channel config:

```json
{
  "webhook_url": "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"
}
```

Requirements:
- `webhook_url` must use HTTPS.

Severity color mapping for syslog events:
- Emergency, Alert, Critical (0-2): red
- Error (3): orange
- Warning (4): yellow
- Notice, Info (5-6): green
- Debug (7): gray

Applog level colors: FATAL = red, ERROR = orange, WARN = yellow, others = green.

Rate limit: 1 notification/second with burst of 3.

### Webhook

Sends an HTTP POST (configurable method) to a custom URL with a JSON payload. Supports custom headers and Go template-based payload customization.

Channel config:

```json
{
  "url": "https://alerting.example.com/hook",
  "method": "POST",
  "headers": {
    "Authorization": "Bearer my-token",
    "X-Source": "taillight"
  },
  "template": ""
}
```

Fields:
- `url` (required) — the endpoint to call. Must be HTTP or HTTPS. Internal/private IP ranges are blocked (SSRF protection).
- `method` (optional) — HTTP method, defaults to `POST`.
- `headers` (optional) — additional HTTP headers to include in the request.
- `template` (optional) — a Go `text/template` string for custom payload formatting. When empty, the default template is used.

Default payload format:

```json
{
  "source": "taillight",
  "rule": "critical-syslog",
  "kind": "syslog",
  "event_count": 5,
  "is_digest": false,
  "group_key": "router1",
  "window_seconds": 30,
  "timestamp": "2025-01-15T10:30:00Z",
  "hostname": "router1",
  "program": "rpd",
  "severity": 2,
  "severity_label": "critical",
  "message": "BGP peer 10.0.0.2 state changed to DOWN"
}
```

For applog events the syslog fields are replaced with `service`, `level`, `host`, and `message`.

The template has access to the full `Payload` struct and a `marshal` function for JSON-safe string escaping.

Rate limit: 5 notifications/second with burst of 10.

### Email

Sends HTML email via SMTP. Requires global SMTP settings in `config.yml` (see Configuration above).

Channel config:

```json
{
  "to": ["oncall@example.com", "team-lead@example.com"],
  "subject_template": "[ALERT] Server issue detected"
}
```

Fields:
- `to` (required) — list of recipient email addresses. Each must be a valid RFC 5322 address.
- `subject_template` (optional) — static subject line override. When empty, the subject is auto-generated as `[Taillight] <hostname> - <SEVERITY>`.

Emails are sent as `text/html` with a responsive layout including a severity-colored header bar, the log message in a monospace block, and a footer with the rule name and timestamp.

## Rules

A rule defines when to trigger a notification. Rules match events by type (syslog or applog) and a set of filter fields.

### Rule structure

```json
{
  "name": "critical-syslog",
  "enabled": true,
  "event_kind": "syslog",
  "hostname": "router*",
  "severity": 2,
  "channel_ids": [1, 3],
  "burst_window": 30,
  "cooldown_seconds": 300,
  "max_cooldown_seconds": 3600,
  "group_by": "hostname,programname"
}
```

### Filter fields

**Syslog rules** (`event_kind: "syslog"`):

| Field | Type | Description |
|-------|------|-------------|
| `hostname` | string | Exact match or wildcard (e.g. `router*`). |
| `programname` | string | Exact match on the program name. |
| `severity` | int | Exact syslog severity (0=emergency through 7=debug). |
| `severity_max` | int | Maximum severity level. Events with severity > this value are excluded. Useful for matching "warning and above" by setting `severity_max: 4`. |
| `facility` | int | Exact syslog facility code. |
| `syslogtag` | string | Exact match on the syslog tag. |
| `msgid` | string | Exact match on the structured data message ID. |
| `search` | string | Case-insensitive substring search within the message body. |

**Applog rules** (`event_kind: "applog"`):

| Field | Type | Description |
|-------|------|-------------|
| `service` | string | Exact match on the service name. |
| `component` | string | Exact match on the component name. |
| `host` | string | Exact match or wildcard (e.g. `web-*`). |
| `level` | string | Minimum log level. Matches events at this level or above (TRACE < DEBUG < INFO < WARN < ERROR < FATAL). |
| `search` | string | Case-insensitive substring search within the message and attributes. |

All filter fields are optional. An empty filter matches all events of that kind. Multiple fields are AND-combined: the event must satisfy every non-empty field.

### Group-by

The `group_by` field controls how events are grouped for burst/cooldown tracking. It is a comma-separated list of field names.

For syslog: `hostname`, `programname`, `syslogtag`, `severity`. Default: `hostname`.

For applog: `host`, `service`, `component`, `level`. Default: `host`.

Example: `group_by: "hostname,programname"` means events from the same host and program are grouped together, so a burst of `rpd` errors on `router1` does not suppress `sshd` errors on the same host.

### Channel association

Rules are linked to channels via the `channel_ids` array. When a rule fires, the notification is sent to all enabled channels in the list. Channel associations are stored in a separate join table (`notification_rule_channels`) and managed transactionally.

## Anti-Spam Mechanisms

The notification system has multiple layers to prevent alert fatigue.

### Burst window

When an event first matches a rule, the engine does not immediately send a notification. Instead, it starts accumulating matching events for the duration of the burst window (default 30 seconds, configurable per rule via `burst_window`).

When the burst window expires, the engine sends a single notification containing the first matched event and the total event count.

```
Time  Event    Action
0s    match    Start burst window (30s)
5s    match    Accumulate (count=2)
12s   match    Accumulate (count=3)
30s   --       Burst window expires: send notification (3 events)
```

### Cooldown

After the burst window fires, the engine enters a cooldown period (default 5 minutes, configurable per rule via `cooldown_seconds`). During cooldown, matching events continue to accumulate silently.

When the cooldown expires:
- **If events accumulated:** send a digest notification summarizing the count, then start a new cooldown with doubled duration.
- **If no events accumulated:** the group resets to idle (clean slate for the next match).

```
Time    Event    Action
0s      match    Start burst window (30s)
30s     --       Send initial notification (burst flush)
30s     --       Start cooldown (5m)
1m      match    Accumulate silently
3m      match    Accumulate silently
5m30s   --       Cooldown expires: 2 events accumulated -> send digest
5m30s   --       Start new cooldown (10m, doubled)
15m30s  --       Cooldown expires: 0 events -> reset to idle
```

### Max cooldown (exponential backoff cap)

Each time a cooldown fires with accumulated events, the cooldown duration doubles. This prevents a continuously triggering rule from sending a digest every few minutes for hours. The escalation is capped at `max_cooldown_seconds` (default 1 hour).

Cooldown progression example (base cooldown 5m, max cooldown 1h):
- 1st cooldown: 5 minutes
- 2nd cooldown: 10 minutes
- 3rd cooldown: 20 minutes
- 4th cooldown: 40 minutes
- 5th cooldown: 60 minutes (capped at max)
- 6th+ cooldown: 60 minutes (stays at max)

Once a cooldown expires with zero accumulated events, the group is deleted entirely. The next matching event starts fresh.

### Per-channel rate limiting

Each channel has a token bucket rate limiter that caps how fast notifications can be sent to a particular destination, regardless of how many rules target it.

| Channel type | Rate | Burst |
|-------------|------|-------|
| Slack | 1/second | 3 |
| Webhook | 5/second | 10 |
| Email | 5/second | 10 |

If a notification is rate-limited, it is silently dropped (logged as suppressed).

### Circuit breakers

Each channel has a circuit breaker (powered by `sony/gobreaker`) that protects against cascading failures when a backend is down.

- **Closed** (normal): notifications flow through.
- **Open** (tripped): after 5 consecutive failures, the breaker opens. All notifications to this channel are immediately rejected for 60 seconds.
- **Half-open** (probing): after 60 seconds, the breaker allows up to 2 probe requests through. If they succeed, the breaker closes. If they fail, it re-opens.

Circuit breaker events are logged and counted in Prometheus metrics (`notif_suppressed_total{reason="circuit_breaker"}`).

## API Reference

All notification endpoints live under `/api/v1/notifications/`. Read endpoints (GET) require the `read` scope. Write endpoints (POST, PUT, DELETE) require the `admin` scope.

### Channels

#### List channels

```
GET /api/v1/notifications/channels
```

```sh
curl -s http://localhost:8080/api/v1/notifications/channels | jq
```

Response:

```json
{
  "data": [
    {
      "id": 1,
      "name": "ops-slack",
      "type": "slack",
      "config": {"webhook_url": "https://hooks.slack.com/services/..."},
      "enabled": true,
      "created_at": "2025-01-15T10:00:00Z",
      "updated_at": "2025-01-15T10:00:00Z"
    }
  ]
}
```

#### Get channel

```
GET /api/v1/notifications/channels/{id}
```

```sh
curl -s http://localhost:8080/api/v1/notifications/channels/1 | jq
```

#### Create channel

```
POST /api/v1/notifications/channels
```

```sh
curl -s -X POST http://localhost:8080/api/v1/notifications/channels \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "ops-slack",
    "type": "slack",
    "config": {"webhook_url": "https://hooks.slack.com/services/T00/B00/xxx"},
    "enabled": true
  }' | jq
```

Returns `201 Created` with the created channel including its assigned `id`.

The channel config is validated against the backend before insertion. For Slack, the `webhook_url` must use HTTPS. For webhooks, the `url` is validated against SSRF blocklists. For email, all addresses in `to` must be valid RFC 5322 addresses.

#### Update channel

```
PUT /api/v1/notifications/channels/{id}
```

```sh
curl -s -X PUT http://localhost:8080/api/v1/notifications/channels/1 \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "ops-slack-updated",
    "type": "slack",
    "config": {"webhook_url": "https://hooks.slack.com/services/T00/B00/new"},
    "enabled": true
  }' | jq
```

#### Delete channel

```
DELETE /api/v1/notifications/channels/{id}
```

```sh
curl -s -X DELETE http://localhost:8080/api/v1/notifications/channels/1
```

Returns `204 No Content` on success. Returns `404` if the channel does not exist.

#### Test channel

```
POST /api/v1/notifications/channels/{id}/test
```

Sends a test notification to the channel, bypassing burst/cooldown. Useful for validating that Slack webhooks, webhook endpoints, or SMTP settings are working.

```sh
curl -s -X POST http://localhost:8080/api/v1/notifications/channels/1/test | jq
```

Response:

```json
{
  "success": true,
  "status_code": 200,
  "duration_ms": 245
}
```

If the send fails:

```json
{
  "success": false,
  "status_code": 403,
  "error": "slack webhook returned status 403",
  "duration_ms": 120
}
```

### Rules

#### List rules

```
GET /api/v1/notifications/rules
```

```sh
curl -s http://localhost:8080/api/v1/notifications/rules | jq
```

Response:

```json
{
  "data": [
    {
      "id": 1,
      "name": "critical-syslog",
      "enabled": true,
      "event_kind": "syslog",
      "hostname": "",
      "severity_max": 3,
      "channel_ids": [1, 2],
      "burst_window": 30,
      "cooldown_seconds": 300,
      "max_cooldown_seconds": 3600,
      "group_by": "hostname",
      "created_at": "2025-01-15T10:00:00Z",
      "updated_at": "2025-01-15T10:00:00Z"
    }
  ]
}
```

#### Get rule

```
GET /api/v1/notifications/rules/{id}
```

#### Create rule

```
POST /api/v1/notifications/rules
```

Required fields: `name`, `event_kind` (must be `syslog` or `applog`).

```sh
curl -s -X POST http://localhost:8080/api/v1/notifications/rules \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "critical-syslog",
    "enabled": true,
    "event_kind": "syslog",
    "severity_max": 3,
    "channel_ids": [1],
    "burst_window": 30,
    "cooldown_seconds": 300,
    "max_cooldown_seconds": 3600,
    "group_by": "hostname"
  }' | jq
```

Returns `201 Created` with the created rule.

#### Update rule

```
PUT /api/v1/notifications/rules/{id}
```

```sh
curl -s -X PUT http://localhost:8080/api/v1/notifications/rules/1 \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "critical-syslog-updated",
    "enabled": true,
    "event_kind": "syslog",
    "severity_max": 2,
    "channel_ids": [1, 2],
    "burst_window": 60,
    "cooldown_seconds": 600,
    "max_cooldown_seconds": 3600
  }' | jq
```

Updating a rule replaces its channel associations entirely. Include all desired `channel_ids` in the update payload.

#### Delete rule

```
DELETE /api/v1/notifications/rules/{id}
```

```sh
curl -s -X DELETE http://localhost:8080/api/v1/notifications/rules/1
```

Returns `204 No Content` on success.

### Notification log

#### List log entries

```
GET /api/v1/notifications/log
```

Query parameters (all optional):

| Parameter | Type | Description |
|-----------|------|-------------|
| `rule_id` | int | Filter by rule ID. |
| `channel_id` | int | Filter by channel ID. |
| `status` | string | Filter by status: `sent` or `failed`. |
| `from` | RFC 3339 | Start of time range. |
| `to` | RFC 3339 | End of time range. |

Returns the most recent 500 entries, newest first.

```sh
curl -s 'http://localhost:8080/api/v1/notifications/log?status=failed&from=2025-01-15T00:00:00Z' | jq
```

Response:

```json
{
  "data": [
    {
      "id": 42,
      "created_at": "2025-01-15T10:31:00Z",
      "rule_id": 1,
      "channel_id": 1,
      "event_kind": "syslog",
      "event_id": 98765,
      "status": "failed",
      "reason": "slack webhook returned status 403",
      "event_count": 3,
      "status_code": 403,
      "duration_ms": 120,
      "payload": {"kind": "syslog", "rule_name": "critical-syslog", "...": "..."}
    }
  ]
}
```

## Examples

### Alert on all critical syslog events via Slack

Create a Slack channel:

```sh
curl -s -X POST http://localhost:8080/api/v1/notifications/channels \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "ops-slack",
    "type": "slack",
    "config": {"webhook_url": "https://hooks.slack.com/services/T00/B00/xxx"},
    "enabled": true
  }' | jq '.data.id'
# Returns: 1
```

Test the channel:

```sh
curl -s -X POST http://localhost:8080/api/v1/notifications/channels/1/test | jq
```

Create a rule matching severity 0-3 (emergency through error):

```sh
curl -s -X POST http://localhost:8080/api/v1/notifications/rules \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "critical-alerts",
    "enabled": true,
    "event_kind": "syslog",
    "severity_max": 3,
    "channel_ids": [1],
    "burst_window": 30,
    "cooldown_seconds": 300,
    "group_by": "hostname"
  }' | jq
```

This will:
1. Accumulate matching events for 30 seconds before sending the first alert.
2. After the initial alert, suppress for 5 minutes.
3. If events keep arriving, send digests with exponentially increasing intervals up to 1 hour.

### Webhook integration for a specific host

Create a webhook channel that posts to your incident management system:

```sh
curl -s -X POST http://localhost:8080/api/v1/notifications/channels \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "pagerduty-hook",
    "type": "webhook",
    "config": {
      "url": "https://events.pagerduty.com/v2/enqueue",
      "headers": {"Authorization": "Bearer pdkey123"}
    },
    "enabled": true
  }' | jq '.data.id'
# Returns: 2
```

Create a rule targeting a specific host:

```sh
curl -s -X POST http://localhost:8080/api/v1/notifications/rules \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "core-router-down",
    "enabled": true,
    "event_kind": "syslog",
    "hostname": "core-rtr-*",
    "severity_max": 3,
    "search": "down",
    "channel_ids": [2],
    "burst_window": 10,
    "cooldown_seconds": 600
  }' | jq
```

### Email alerts on high applog error rate

Create an email channel:

```sh
curl -s -X POST http://localhost:8080/api/v1/notifications/channels \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "oncall-email",
    "type": "email",
    "config": {
      "to": ["oncall@example.com", "team-lead@example.com"]
    },
    "enabled": true
  }' | jq '.data.id'
# Returns: 3
```

Create a rule matching ERROR-level applog events with a longer burst window to batch spikes:

```sh
curl -s -X POST http://localhost:8080/api/v1/notifications/rules \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "applog-errors",
    "enabled": true,
    "event_kind": "applog",
    "level": "ERROR",
    "service": "payment-api",
    "channel_ids": [3],
    "burst_window": 60,
    "cooldown_seconds": 900,
    "max_cooldown_seconds": 3600,
    "group_by": "host,component"
  }' | jq
```

This groups errors by host and component, accumulates for 60 seconds before the initial email, then sends digests at 15 minute/30 minute/1 hour intervals while errors continue.

## Troubleshooting

### Notifications not sending

1. **Check the master switch.** Ensure `notification.enabled: true` in `config.yml`. When disabled, the engine does not start and no rules are evaluated.

2. **Check channel configuration.** Use the test endpoint to validate connectivity:
   ```sh
   curl -s -X POST http://localhost:8080/api/v1/notifications/channels/1/test | jq
   ```

3. **Check the rule is enabled.** Rules have an `enabled` field that must be `true`.

4. **Check the notification log.** Look for failed entries:
   ```sh
   curl -s 'http://localhost:8080/api/v1/notifications/log?status=failed' | jq
   ```

5. **Check rule refresh timing.** After creating or updating a rule via the API, it takes up to `rule_refresh_interval` (default 30s) for the engine to pick up the change.

### Too many alerts

- **Increase `burst_window`.** A longer burst window accumulates more events into a single notification. Try 60-120 seconds instead of 30.
- **Increase `cooldown_seconds`.** A longer cooldown reduces digest frequency. Try 900 (15 minutes) or 1800 (30 minutes).
- **Set `max_cooldown_seconds`.** This caps the exponential backoff. A value of 3600 (1 hour) means at most one digest per hour during sustained events.
- **Use `group_by` wisely.** Grouping by `hostname,programname` creates separate tracking per host-program pair. If you want fewer notifications, group by hostname only.

### Slow dispatch / backed-up queue

- **Increase `dispatch_workers`.** Default is 4. If you have many channels or slow backends, try 8-16.
- **Check `dispatch_buffer`.** If you see "dispatch queue full" warnings in the logs, increase the buffer size.
- **Check backend health.** A slow or failing backend can back up workers. Look for circuit breaker events in the logs.
- **Reduce `send_timeout`.** If a backend is unresponsive, a lower timeout (e.g. 5s) frees up workers faster.

### SMTP issues

- **Connection refused.** Verify `smtp.host` and `smtp.port` are correct and reachable from the Taillight server.
- **TLS errors.** If your SMTP server does not support STARTTLS, set `smtp.tls: false`. If using a self-signed certificate, the connection will fail (Go's TLS client verifies server certificates).
- **Authentication failures.** Check `smtp.auth_type` matches your server's requirements. Use `plain` for most providers, `crammd5` for servers that require it, or `""` for relay servers that do not require auth.
- **Sender rejected.** Some SMTP servers require the `smtp.from` address to match an authorized sender. Verify with your email provider.
