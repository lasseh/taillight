# Notification System

Taillight's notification system alerts operators when log events match configurable rules. It supports Slack incoming webhooks and generic HTTP webhooks, with built-in anti-spam protections to prevent alert fatigue.

## Overview

```
Log event arrives
  -> Engine evaluates all enabled rules
    -> Rule filters match? (hostname, severity, search, etc.)
      -> Burst window collects events for N seconds
        -> Cooldown check (suppress if recently fired)
          -> Rate limiter (per-channel token bucket)
            -> Circuit breaker (back off on repeated failures)
              -> Backend sends notification (Slack / Webhook)
                -> Result logged to notification_log audit table
```

**Key concepts:**

| Concept | Description |
|---------|-------------|
| **Channel** | A configured notification destination (Slack webhook, HTTP endpoint) |
| **Rule** | A set of filter conditions + linked channels. When a matching event arrives, the linked channels are notified |
| **Burst window** | Collects multiple matching events into a single notification (default: 30s) |
| **Cooldown** | Suppresses repeat notifications for the same rule after firing (default: 5m) |
| **Rate limiter** | Per-channel token bucket to prevent flooding backends |
| **Circuit breaker** | Stops sending to a channel after 5 consecutive failures, retries after 60s |

## Enabling Notifications

Add the following to `api/config.yml`:

```yaml
notification:
  enabled: true
```

That's the only required change. All other settings have sensible defaults:

```yaml
notification:
  enabled: true
  rule_refresh_interval: 30s    # How often rules/channels are reloaded from the database
  dispatch_workers: 4           # Number of concurrent notification sender goroutines
  dispatch_buffer: 1024         # Size of the internal dispatch queue
  default_burst_window: 30s     # Default burst collection window per rule
  default_cooldown: 5m          # Default post-send cooldown per rule
  send_timeout: 10s             # HTTP timeout for each backend send
  global_rate_limit: 100        # Reserved for future use
```

After enabling, run the database migration:

```sh
./taillight migrate up
```

This creates four tables: `notification_channels`, `notification_rules`, `notification_rule_channels`, and `notification_log` (a TimescaleDB hypertable with 7-day chunks and 30-day retention).

## Using the Web UI

Navigate to **ALERTS** in the top navigation bar. The page has three tabs:

### Channels Tab

Channels define where notifications are sent. Click **+ add channel** to create one.

**Slack channel:**
1. Enter a name (e.g. `ops-slack`)
2. Select type **Slack**
3. Paste the Slack incoming webhook URL
4. Click **create channel**
5. Click **test** to verify it works

**Webhook channel:**
1. Enter a name (e.g. `pagerduty-webhook`)
2. Select type **Webhook**
3. Enter the destination URL
4. Optionally change the HTTP method (POST or PUT)
5. Optionally add custom headers as JSON (e.g. `{"Authorization": "Bearer ..."}`)
6. Optionally provide a custom Go `text/template` for the request body
7. Click **create channel**

Each channel shows an enable/disable status dot, type badge, and last updated time. Use the **test** button to send a synthetic notification without creating a rule.

### Rules Tab

Rules define which events trigger notifications. Click **+ add rule** to create one.

1. **Name** — a descriptive label (e.g. `critical-bgp-alerts`)
2. **Event Kind** — choose Syslog or AppLog
3. **Filters** — vary by event kind (see below)
4. **Message Search** — substring search in the message body (shared between both kinds)
5. **Channels** — select one or more channels to notify
6. **Anti-Spam** — configure burst window and cooldown durations

**Syslog filters:**

| Filter | Description | Example |
|--------|-------------|---------|
| Hostname | Exact match or glob pattern | `router*`, `switch1.dc1.example.com` |
| Program | Exact match | `rpd`, `sshd`, `mgd` |
| Severity (exact) | Match a specific syslog severity level | `3` (Error) |
| Severity max | Match this severity and all more severe (lower numbers are more severe) | `4` (Warning) matches 0-4 |
| Syslog Tag | Exact match on the syslog tag field | `rpd[1234]` |
| Message ID | Exact match on structured data message ID | `BGP_PREFIX_THRESH_EXCEEDED` |

**AppLog filters:**

| Filter | Description | Example |
|--------|-------------|---------|
| Service | Exact match on service name | `api-gateway` |
| Component | Exact match on component | `auth` |
| Host | Exact match on host | `web1.example.com` |
| Level (minimum) | Match this level and all more severe | `WARN` matches WARN, ERROR, FATAL |

All filters are AND-combined: every non-empty filter must match for the rule to fire. Leave a filter empty to skip it.

Each rule row shows its event kind badge, linked channels, burst/cooldown settings, and a summary of active filters.

### Log Tab

The notification log shows the audit trail of all notification attempts. Filter by:

- **Time range** — 1h, 6h, 24h, 7d, or 30d
- **Rule** — filter to a specific rule
- **Channel** — filter to a specific channel
- **Status** — `sent`, `failed`, `rate_limited`, or `circuit_open`

Each entry shows the timestamp, status, rule name, channel name, event count (how many events were in the burst), duration, and failure reason (if applicable).

The log is stored in a TimescaleDB hypertable with automatic 30-day retention.

## API Reference

All endpoints are under `/api/v1/notifications/`. The notification API works with or without authentication (follows the same auth model as the rest of the API).

### Channels

#### List channels

```
GET /api/v1/notifications/channels
```

```json
{
  "data": [
    {
      "id": 1,
      "name": "ops-slack",
      "type": "slack",
      "config": {"webhook_url": "https://hooks.slack.com/services/T.../B.../xxx"},
      "enabled": true,
      "created_at": "2025-02-10T12:00:00Z",
      "updated_at": "2025-02-10T12:00:00Z"
    }
  ]
}
```

#### Create channel

```
POST /api/v1/notifications/channels
Content-Type: application/json

{
  "name": "ops-slack",
  "type": "slack",
  "enabled": true,
  "config": {
    "webhook_url": "https://hooks.slack.com/services/T.../B.../xxx"
  }
}
```

Returns `201 Created` with the created channel.

#### Get channel

```
GET /api/v1/notifications/channels/{id}
```

#### Update channel

```
PUT /api/v1/notifications/channels/{id}
Content-Type: application/json

{
  "name": "ops-slack-renamed",
  "type": "slack",
  "enabled": false,
  "config": {
    "webhook_url": "https://hooks.slack.com/services/T.../B.../new"
  }
}
```

#### Delete channel

```
DELETE /api/v1/notifications/channels/{id}
```

Returns `204 No Content`.

#### Test channel

Send a synthetic test notification to verify the channel works:

```
POST /api/v1/notifications/channels/{id}/test
```

```json
{
  "success": true,
  "status_code": 200,
  "duration_ms": 142
}
```

On failure:

```json
{
  "success": false,
  "status_code": 403,
  "error": "slack webhook returned status 403",
  "duration_ms": 89
}
```

### Rules

#### List rules

```
GET /api/v1/notifications/rules
```

```json
{
  "data": [
    {
      "id": 1,
      "name": "critical-syslog",
      "enabled": true,
      "event_kind": "syslog",
      "hostname": "router*",
      "severity_max": 3,
      "channel_ids": [1, 2],
      "burst_window": 30,
      "cooldown_seconds": 300,
      "created_at": "2025-02-10T12:00:00Z",
      "updated_at": "2025-02-10T12:00:00Z"
    }
  ]
}
```

#### Create rule

```
POST /api/v1/notifications/rules
Content-Type: application/json

{
  "name": "critical-syslog",
  "enabled": true,
  "event_kind": "syslog",
  "hostname": "router*",
  "severity_max": 3,
  "search": "BGP",
  "channel_ids": [1],
  "burst_window": 30,
  "cooldown_seconds": 300
}
```

Returns `201 Created` with the created rule.

#### Get rule

```
GET /api/v1/notifications/rules/{id}
```

#### Update rule

```
PUT /api/v1/notifications/rules/{id}
Content-Type: application/json

{
  "name": "critical-syslog-updated",
  "enabled": true,
  "event_kind": "syslog",
  "severity_max": 4,
  "channel_ids": [1, 2],
  "burst_window": 60,
  "cooldown_seconds": 600
}
```

#### Delete rule

```
DELETE /api/v1/notifications/rules/{id}
```

Returns `204 No Content`.

### Notification Log

#### List log entries

```
GET /api/v1/notifications/log?from=2025-02-10T00:00:00Z&to=2025-02-10T23:59:59Z
```

Optional query parameters:

| Parameter | Type | Description |
|-----------|------|-------------|
| `from` | RFC3339 timestamp | Start of time range |
| `to` | RFC3339 timestamp | End of time range |
| `rule_id` | integer | Filter by rule ID |
| `channel_id` | integer | Filter by channel ID |
| `status` | string | Filter by status (`sent`, `failed`, `suppressed`, `rate_limited`, `circuit_open`) |

```json
{
  "data": [
    {
      "id": 42,
      "created_at": "2025-02-10T14:30:00Z",
      "rule_id": 1,
      "channel_id": 1,
      "event_kind": "syslog",
      "event_id": 12345,
      "status": "sent",
      "event_count": 7,
      "status_code": 200,
      "duration_ms": 142
    }
  ]
}
```

## Channel Configuration

### Slack

The Slack backend uses [Incoming Webhooks](https://api.slack.com/messaging/webhooks). Messages are formatted using Block Kit with severity-colored attachments.

**Config fields:**

| Field | Required | Description |
|-------|----------|-------------|
| `webhook_url` | Yes | The Slack incoming webhook URL (must use HTTPS) |

**Example config:**

```json
{
  "webhook_url": "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"
}
```

**How to get a webhook URL:**

1. Go to [api.slack.com/apps](https://api.slack.com/apps) and create a new app (or select existing)
2. Click **Incoming Webhooks** and activate it
3. Click **Add New Webhook to Workspace** and select a channel
4. Copy the webhook URL

**Message format:**

Slack notifications include:
- A severity-colored sidebar (red for critical, orange for error, yellow for warning, green for info)
- A header with the rule name
- A field grid showing host, program, severity, and facility (syslog) or service, level, host, component (applog)
- The log message in a code block
- A footer with timestamp and event count

### Webhook

The generic webhook backend sends an HTTP request with a JSON body to any URL. It supports custom templates for the request body.

**Config fields:**

| Field | Required | Description |
|-------|----------|-------------|
| `url` | Yes | The destination URL |
| `method` | No | HTTP method (default: `POST`) |
| `headers` | No | Custom HTTP headers as a JSON object |
| `template` | No | Go `text/template` for the request body |

**Example — simple webhook:**

```json
{
  "url": "https://example.com/alerts"
}
```

**Example — webhook with auth header:**

```json
{
  "url": "https://api.pagerduty.com/v2/enqueue",
  "headers": {
    "Authorization": "Token token=YOUR_PAGERDUTY_TOKEN"
  }
}
```

**Example — webhook with custom template:**

```json
{
  "url": "https://example.com/alerts",
  "headers": {"Content-Type": "application/json"},
  "template": "{\"text\": \"Alert: {{.RuleName}} - {{.EventCount}} events\", \"severity\": \"{{if .SyslogEvent}}{{.SyslogEvent.SeverityLabel}}{{else}}{{.AppLogEvent.Level}}{{end}}\"}"
}
```

**Default template** (used when no custom template is provided):

```json
{
  "source": "taillight",
  "rule": "{{.RuleName}}",
  "kind": "{{.Kind}}",
  "event_count": {{.EventCount}},
  "timestamp": "{{.Timestamp.Format \"2006-01-02T15:04:05Z07:00\"}}",
  "hostname": "{{.SyslogEvent.Hostname}}",
  "program": "{{.SyslogEvent.Programname}}",
  "severity": {{.SyslogEvent.Severity}},
  "severity_label": "{{.SyslogEvent.SeverityLabel}}",
  "message": {{marshal .SyslogEvent.Message}}
}
```

**Template variables** (available in Go `text/template`):

| Variable | Type | Description |
|----------|------|-------------|
| `.RuleName` | string | Name of the matched rule |
| `.Kind` | string | `"syslog"` or `"applog"` |
| `.EventCount` | int | Number of events in the burst window |
| `.Timestamp` | time.Time | Timestamp of the first event |
| `.SyslogEvent` | pointer | Syslog event data (nil for applog rules) |
| `.AppLogEvent` | pointer | AppLog event data (nil for syslog rules) |

**SyslogEvent fields:** `.Hostname`, `.Programname`, `.Severity`, `.SeverityLabel`, `.Facility`, `.FacilityLabel`, `.SyslogTag`, `.MsgID`, `.Message`

**AppLogEvent fields:** `.Service`, `.Component`, `.Host`, `.Level`, `.Msg`

The `marshal` template function is available for JSON-safe string escaping.

## Anti-Spam Protection

The notification engine has four layers of protection to prevent alert storms:

### Layer 1: Burst Window

When a rule matches an event, the engine doesn't immediately send a notification. Instead, it opens a **burst window** (default: 30 seconds) and collects all matching events during that period. When the window closes, a single notification is sent with the first event's details and the total event count.

This means if 100 syslog events match a rule within 30 seconds, you get one notification that says "100 events matched" rather than 100 individual notifications.

Configure per-rule with the `burst_window` field (in seconds). Set to `0` to use the server default.

### Layer 2: Cooldown

After a notification fires, the rule enters a **cooldown period** (default: 5 minutes). During cooldown, matching events are counted but no notification is sent. When the cooldown expires, a summary is logged: "47 events suppressed during cooldown."

Configure per-rule with the `cooldown_seconds` field. Set to `0` to use the server default.

### Layer 3: Per-Channel Rate Limiting

Each channel has a token-bucket rate limiter that prevents flooding the backend:

| Channel Type | Rate | Burst |
|-------------|------|-------|
| Slack | 1 req/sec | 3 |
| Webhook | 5 req/sec | 10 |

Rate-limited notifications are logged with status `rate_limited`.

### Layer 4: Circuit Breaker

Each channel has a circuit breaker that opens after **5 consecutive failures**, preventing further sends for 60 seconds. After the timeout, the breaker enters half-open state and allows 2 trial requests. If they succeed, the circuit closes; if they fail, it re-opens.

Circuit-broken notifications are logged with status `circuit_open`.

## Examples

### Example 1: Alert on critical syslog events from routers

Goal: Get a Slack notification when any router sends an error-level or worse syslog message.

**Step 1: Create a Slack channel**

```sh
curl -X POST http://localhost:8080/api/v1/notifications/channels \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "network-ops",
    "type": "slack",
    "enabled": true,
    "config": {
      "webhook_url": "https://hooks.slack.com/services/T.../B.../xxx"
    }
  }'
```

**Step 2: Create a rule**

```sh
curl -X POST http://localhost:8080/api/v1/notifications/rules \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "router-critical",
    "enabled": true,
    "event_kind": "syslog",
    "hostname": "router*",
    "severity_max": 3,
    "channel_ids": [1],
    "burst_window": 60,
    "cooldown_seconds": 300
  }'
```

This rule:
- Matches syslog events from any host starting with `router`
- Only fires for severity 0 (Emergency) through 3 (Error)
- Collects events for 60 seconds before sending
- Waits 5 minutes between notifications

### Example 2: Alert on application errors

Goal: Get a Slack and webhook notification when the `api-gateway` service logs ERROR or FATAL messages.

```sh
# Create a webhook channel for PagerDuty
curl -X POST http://localhost:8080/api/v1/notifications/channels \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "pagerduty",
    "type": "webhook",
    "enabled": true,
    "config": {
      "url": "https://events.pagerduty.com/v2/enqueue",
      "headers": {"Authorization": "Token token=YOUR_TOKEN"}
    }
  }'

# Create a rule that sends to both channels
curl -X POST http://localhost:8080/api/v1/notifications/rules \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "api-gateway-errors",
    "enabled": true,
    "event_kind": "applog",
    "service": "api-gateway",
    "level": "ERROR",
    "channel_ids": [1, 2],
    "burst_window": 30,
    "cooldown_seconds": 600
  }'
```

### Example 3: Alert on specific BGP messages

Goal: Catch BGP-related syslog messages from any host.

```sh
curl -X POST http://localhost:8080/api/v1/notifications/rules \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "bgp-alerts",
    "enabled": true,
    "event_kind": "syslog",
    "programname": "rpd",
    "search": "BGP",
    "channel_ids": [1],
    "burst_window": 30,
    "cooldown_seconds": 300
  }'
```

This matches syslog events where the program is `rpd` AND the message contains "BGP".

## Prometheus Metrics

The notification engine exports the following metrics:

| Metric | Type | Description |
|--------|------|-------------|
| `taillight_notif_rules_evaluated_total` | counter | Total number of rule evaluations |
| `taillight_notif_rules_matched_total` | counter | Total number of rule matches |
| `taillight_notif_dispatched_total` | counter | Total notifications queued for dispatch |
| `taillight_notif_sent_total` | counter (labels: `channel_type`, `status`) | Total notifications sent (success/failed) |
| `taillight_notif_suppressed_total` | counter (labels: `reason`) | Suppressed notifications (cooldown/rate_limit/circuit_breaker) |
| `taillight_notif_send_duration_seconds` | histogram | Backend send latency |
| `taillight_notif_dispatch_queue_length` | gauge | Current dispatch queue depth |

## Database Schema

The notification system adds four tables:

**notification_channels** — configured backends

| Column | Type | Description |
|--------|------|-------------|
| `id` | bigint | Auto-generated primary key |
| `name` | text | Unique channel name |
| `type` | text | `slack` or `webhook` |
| `config` | jsonb | Backend-specific configuration |
| `enabled` | boolean | Whether the channel is active |
| `created_at` | timestamptz | Creation timestamp |
| `updated_at` | timestamptz | Last update timestamp |

**notification_rules** — alert conditions

| Column | Type | Description |
|--------|------|-------------|
| `id` | bigint | Auto-generated primary key |
| `name` | text | Unique rule name |
| `enabled` | boolean | Whether the rule is active |
| `event_kind` | text | `syslog` or `applog` |
| `hostname`, `programname`, etc. | text/smallint | Filter fields (null = don't filter) |
| `burst_window` | integer | Seconds to collect events (default: 30) |
| `cooldown_seconds` | integer | Seconds between notifications (default: 300) |

**notification_rule_channels** — many-to-many join table (rule_id, channel_id)

**notification_log** — TimescaleDB hypertable audit trail

| Column | Type | Description |
|--------|------|-------------|
| `id` | bigint | Auto-generated |
| `created_at` | timestamptz | Partition column (7-day chunks, 30-day retention) |
| `rule_id` | bigint | Which rule triggered |
| `channel_id` | bigint | Which channel was targeted |
| `status` | text | `sent`, `suppressed`, or `failed` |
| `reason` | text | Failure/suppression reason |
| `event_count` | int | Events in the burst window |
| `status_code` | integer | HTTP status from the backend |
| `duration_ms` | integer | Send duration in milliseconds |
| `payload` | jsonb | Full notification payload for debugging |
