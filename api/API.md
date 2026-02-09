# Taillight — API Reference

API documentation for the Taillight log aggregation API.

## Overview

| Property       | Value                                |
|----------------|--------------------------------------|
| Base URL       | `http://<host>:<port>`               |
| API prefix     | `/api/v1`                            |
| Auth           | Session cookie or API key (Bearer token). Auth can be enabled for all endpoints via config. |
| Content type   | `application/json` (REST endpoints)  |
| Streaming      | `text/event-stream` (SSE endpoints)  |
| CORS           | Configurable via `cors_allowed_origins` (defaults to `localhost:5173, localhost:3000`) |

All timestamps use **RFC 3339** format (`2026-02-01T15:23:00Z`).

---

## Data Models

### SyslogEvent

Every event returned by the API has this shape:

```json
{
  "id": 12345,
  "received_at": "2026-02-01T15:23:00Z",
  "reported_at": "2026-02-01T15:23:00Z",
  "hostname": "server-01",
  "fromhost_ip": "192.168.1.100",
  "programname": "sshd",
  "msgid": "AUTH_FAILURE",
  "severity": 4,
  "severity_label": "warning",
  "facility": 4,
  "facility_label": "auth",
  "syslogtag": "sshd[12345]",
  "structured_data": null,
  "message": "Failed password for user from 192.168.1.50"
}
```

| Field             | Type      | Notes                                      |
|-------------------|-----------|--------------------------------------------|
| `id`              | `number`  | Unique row ID (int64)                      |
| `received_at`     | `string`  | RFC 3339 — when rsyslog received the event |
| `reported_at`     | `string`  | RFC 3339 — when the event was generated    |
| `hostname`        | `string`  | Source hostname                             |
| `fromhost_ip`     | `string`  | Source IP address                           |
| `programname`     | `string`  | Name of the program that emitted the event |
| `msgid`           | `string`  | Message identifier                         |
| `severity`        | `number`  | RFC 5424 severity code (0–7)               |
| `severity_label`  | `string`  | Human-readable severity name               |
| `facility`        | `number`  | RFC 5424 facility code (0–23)              |
| `facility_label`  | `string`  | Human-readable facility name               |
| `syslogtag`       | `string`  | Syslog tag (often includes PID)            |
| `structured_data` | `string?` | Nullable — RFC 5424 structured data, omitted from JSON when null |
| `message`         | `string`  | The log message body                       |

### Severity Codes (RFC 5424)

| Code | Label     | Meaning                    |
|------|-----------|----------------------------|
| 0    | `emerg`   | System is unusable          |
| 1    | `alert`   | Action must be taken immediately |
| 2    | `crit`    | Critical conditions         |
| 3    | `err`     | Error conditions            |
| 4    | `warning` | Warning conditions          |
| 5    | `notice`  | Normal but significant      |
| 6    | `info`    | Informational               |
| 7    | `debug`   | Debug-level messages        |

Lower numbers are more severe.

### Facility Codes (RFC 5424)

| Code | Label      | Code | Label    |
|------|------------|------|----------|
| 0    | `kern`     | 12   | `ntp`    |
| 1    | `user`     | 13   | `security` |
| 2    | `mail`     | 14   | `console`  |
| 3    | `daemon`   | 15   | `clock`    |
| 4    | `auth`     | 16   | `local0`   |
| 5    | `syslog`   | 17   | `local1`   |
| 6    | `lpr`      | 18   | `local2`   |
| 7    | `news`     | 19   | `local3`   |
| 8    | `uucp`     | 20   | `local4`   |
| 9    | `cron`     | 21   | `local5`   |
| 10   | `authpriv` | 22   | `local6`   |
| 11   | `ftp`      | 23   | `local7`   |

---

## Endpoints Reference

### Health Check

```
GET /health
```

Returns database connectivity status.

**Response (200):**
```json
{ "status": "healthy" }
```

**Response (503):**
```json
{ "status": "unhealthy" }
```

---

### List Events

```
GET /api/v1/syslog
```

Returns a paginated, filtered list of syslog events ordered **newest first**.

#### Query Parameters

| Param          | Type     | Default | Description                                  |
|----------------|----------|---------|----------------------------------------------|
| `hostname`     | `string` | —       | Exact match on hostname                       |
| `programname`  | `string` | —       | Exact match on program name                   |
| `syslogtag`    | `string` | —       | Exact match on syslog tag                     |
| `msgid`        | `string` | —       | Exact match on message ID                     |
| `search`       | `string` | —       | Full-text search on message body (PostgreSQL `plainto_tsquery`) |
| `fromhost_ip`  | `string` | —       | Exact match on source IP (must be valid IP)   |
| `severity`     | `int`    | —       | Exact severity match (0–7)                    |
| `severity_max` | `int`    | —       | Maximum severity level, inclusive (0–7). Returns events with `severity <= severity_max`. Use to show "warnings and above" (`severity_max=4`). |
| `facility`     | `int`    | —       | Exact facility match (0–23)                   |
| `from`         | `string` | —       | Start of time range, RFC 3339. Filters on `received_at >= from`. |
| `to`           | `string` | —       | End of time range, RFC 3339. Filters on `received_at <= to`.     |
| `cursor`       | `string` | —       | Opaque pagination cursor from a previous response |
| `limit`        | `int`    | `100`   | Results per page (1–1000)                     |

All filter parameters combine with **AND** logic. Omitted parameters are not applied.

#### Response (200)

```json
{
  "data": [ /* array of SyslogEvent */ ],
  "cursor": "base64_encoded_string",
  "has_more": true
}
```

| Field      | Type             | Description                                              |
|------------|------------------|----------------------------------------------------------|
| `data`     | `SyslogEvent[]`  | Array of events (empty `[]` when no results, never null) |
| `cursor`   | `string?`        | Present only when more pages exist. Pass as `?cursor=` for the next page. |
| `has_more` | `boolean`        | `true` if there are more events beyond this page         |

#### Example

```
GET /api/v1/syslog?hostname=web-01&severity_max=4&limit=50
```

---

### Get Single Event

```
GET /api/v1/syslog/{id}
```

Returns a single event by its ID.

#### Path Parameters

| Param | Type  | Description   |
|-------|-------|---------------|
| `id`  | `int` | The event ID  |

#### Response (200)

```json
{
  "data": { /* SyslogEvent */ }
}
```

#### Errors

| Status | Code         | Message            |
|--------|--------------|--------------------|
| 400    | `invalid_id` | `invalid event id` |
| 404    | `not_found`  | `event not found`  |
| 500    | `query_failed` | `failed to get event` |

---

### Stream Events (SSE)

```
GET /api/v1/syslog/stream
```

Opens a Server-Sent Events stream. The server sends a backfill of recent events, then pushes new events in real time as they arrive.

#### Query Parameters

Same filter parameters as [List Events](#list-events) (excluding `cursor` and `limit`):

`hostname`, `programname`, `syslogtag`, `msgid`, `search`, `fromhost_ip`, `severity`, `severity_max`, `facility`, `from`, `to`

Filters are applied both during backfill and on live events. Time-range filters (`from`/`to`) only apply to the initial backfill query, not to live events.

#### Request Headers

| Header          | Description                                                      |
|-----------------|------------------------------------------------------------------|
| `Last-Event-ID` | Optional. Resume from this event ID. The browser's `EventSource` sets this automatically on reconnect. |

#### Response Headers

```
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
Access-Control-Allow-Origin: *
```

#### SSE Wire Format

Each event is sent as:

```
id: 12345
event: syslog
data: {"id":12345,"received_at":"2026-02-01T15:23:00Z",...}

```

- **`id`** — The event's database ID. Used by `EventSource` for automatic reconnection via `Last-Event-ID`.
- **`event`** — Always `syslog`. Listen for this event name in your `EventSource`.
- **`data`** — A single JSON-encoded `SyslogEvent` on one line.
- Events are separated by a blank line (`\n\n`).

#### Backfill Behavior

| Scenario                          | Behavior                                                         |
|-----------------------------------|------------------------------------------------------------------|
| No `Last-Event-ID` header         | Sends up to **100** most recent matching events, oldest first     |
| `Last-Event-ID` present           | Sends up to **100** events with `id > Last-Event-ID`, oldest first |
| After backfill                    | Subscribes to live events via broker, pushed as they arrive      |

The backfill always delivers events in **chronological order** (oldest first) so the frontend can append them to the list in sequence.

#### Slow Client Behavior

The server uses a per-client buffer of 64 messages. If a client falls behind, messages are silently dropped. The client can detect gaps by comparing consecutive `id` values.

---

### Meta: List Hosts

```
GET /api/v1/meta/hosts
```

Returns distinct hostnames. Use this to populate a hostname filter dropdown.

**Response (200):**
```json
{
  "data": ["server-01", "server-02", "web-01"]
}
```

Returns an alphabetically sorted `string[]`. Empty strings are excluded. Capped at 10,000 values.

---

### Meta: List Programs

```
GET /api/v1/meta/programs
```

Returns distinct program names. Use this to populate a program filter dropdown.

**Response (200):**
```json
{
  "data": ["cron", "kernel", "sshd", "sudo"]
}
```

Returns an alphabetically sorted `string[]`. Empty strings are excluded. Capped at 10,000 values.

---

### Meta: List Facilities

```
GET /api/v1/meta/facilities
```

Returns distinct facility codes present in the data. Use this with the facility labels table to populate a facility filter dropdown.

**Response (200):**
```json
{
  "data": [0, 1, 4, 10, 16]
}
```

Returns a numerically sorted `int[]`. Capped at 10,000 values.

---

### Meta: List Tags

```
GET /api/v1/meta/tags
```

Returns distinct syslog tags. Use this to populate a tag filter dropdown.

**Response (200):**
```json
{
  "data": ["cron[456]", "kernel", "sshd[123]"]
}
```

Returns an alphabetically sorted `string[]`. Empty strings are excluded. Capped at 10,000 values.

---

## Cursor Pagination

The API uses **keyset (cursor-based) pagination** for stable, efficient paging through large result sets.

### How It Works

1. First request: `GET /api/v1/syslog?limit=50`
2. Response includes `"cursor": "abc123..."` and `"has_more": true`
3. Next page: `GET /api/v1/syslog?limit=50&cursor=abc123...`
4. Repeat until `"has_more": false` (and `cursor` is absent)

### Cursor Internals

The cursor is a **base64url-encoded** string containing `{received_at_unix_nanos},{id}`. It encodes the `(received_at, id)` tuple of the last visible row. The server fetches rows where `(received_at, id) < (cursor.received_at, cursor.id)` — i.e., events older than the cursor.

Cursors are **opaque** — treat them as strings. Do not parse or construct them client-side.

### Combining Pagination with Filters

Filters and cursors work together. A cursor always continues the exact same filtered query. If the user changes a filter, discard the current cursor and start from the first page.

### Vue Integration — Load More / Infinite Scroll

```ts
const events = ref<SyslogEvent[]>([])
const cursor = ref<string | null>(null)
const hasMore = ref(true)
const loading = ref(false)

async function loadEvents(reset = false) {
  if (loading.value) return
  if (!reset && !hasMore.value) return

  loading.value = true
  if (reset) {
    events.value = []
    cursor.value = null
  }

  const params = new URLSearchParams(activeFilters.value)
  params.set('limit', '100')
  if (cursor.value) params.set('cursor', cursor.value)

  const res = await fetch(`/api/v1/syslog?${params}`)
  const body = await res.json()

  events.value.push(...body.data)
  cursor.value = body.cursor ?? null
  hasMore.value = body.has_more
  loading.value = false
}
```

---

## Filtering

### Filter Parameters

All filter parameters are passed as query strings. They apply to both the List Events and Stream endpoints.

| Param          | Type     | Match Type   | Notes                                          |
|----------------|----------|--------------|-------------------------------------------------|
| `hostname`     | `string` | Exact        |                                                 |
| `programname`  | `string` | Exact        |                                                 |
| `syslogtag`    | `string` | Exact        |                                                 |
| `msgid`        | `string` | Exact        |                                                 |
| `fromhost_ip`  | `string` | Exact        | Must be a valid IP (e.g. `192.168.1.1`)         |
| `search`       | `string` | Full-text    | PostgreSQL full-text search on the message body |
| `severity`     | `int`    | Exact        | Single severity level (0–7)                     |
| `severity_max` | `int`    | Range        | Shows all severities from 0 up to this value    |
| `facility`     | `int`    | Exact        | Single facility code (0–23)                     |
| `from`         | `string` | Range (>=)   | RFC 3339 timestamp                              |
| `to`           | `string` | Range (<=)   | RFC 3339 timestamp                              |

### Combining Filters

Multiple filters are combined with **AND**. Example:

```
GET /api/v1/syslog?hostname=web-01&severity_max=3&programname=nginx
```

This returns events from host `web-01`, program `nginx`, with severity 0 (emerg), 1 (alert), 2 (crit), or 3 (err).

### Populating Filter Dropdowns from Meta Endpoints

Fetch meta values on mount to populate `<select>` or autocomplete inputs:

```ts
const hosts = ref<string[]>([])
const programs = ref<string[]>([])
const facilities = ref<number[]>([])
const tags = ref<string[]>([])

async function loadMeta() {
  const [h, p, f, t] = await Promise.all([
    fetch('/api/v1/meta/hosts').then(r => r.json()),
    fetch('/api/v1/meta/programs').then(r => r.json()),
    fetch('/api/v1/meta/facilities').then(r => r.json()),
    fetch('/api/v1/meta/tags').then(r => r.json()),
  ])
  hosts.value = h.data
  programs.value = p.data
  facilities.value = f.data
  tags.value = t.data
}
```

### Facility Label Mapping

The meta facilities endpoint returns integer codes. Map them to labels client-side:

```ts
const facilityLabels: Record<number, string> = {
  0: 'kern', 1: 'user', 2: 'mail', 3: 'daemon', 4: 'auth',
  5: 'syslog', 6: 'lpr', 7: 'news', 8: 'uucp', 9: 'cron',
  10: 'authpriv', 11: 'ftp', 12: 'ntp', 13: 'security',
  14: 'console', 15: 'clock', 16: 'local0', 17: 'local1',
  18: 'local2', 19: 'local3', 20: 'local4', 21: 'local5',
  22: 'local6', 23: 'local7',
}

const severityLabels: Record<number, string> = {
  0: 'emerg', 1: 'alert', 2: 'crit', 3: 'err',
  4: 'warning', 5: 'notice', 6: 'info', 7: 'debug',
}
```

### Severity Filter UX

Use `severity_max` for a "minimum severity" dropdown (show this level and above). Since lower numbers are more severe:
- "Errors and above" → `severity_max=3`
- "Warnings and above" → `severity_max=4`
- "All messages" → omit the parameter

Use `severity` (exact) for filtering to a single severity level.

---

## Error Handling

### Error Envelope

All errors return this structure:

```json
{
  "error": {
    "code": "machine_readable_code",
    "message": "Human-readable description"
  }
}
```

### Error Codes

| HTTP Status | Code                    | When                                    |
|-------------|-------------------------|-----------------------------------------|
| 400         | `invalid_filter`        | Malformed query parameter value         |
| 400         | `invalid_id`            | Event ID is not a valid integer         |
| 404         | `not_found`             | Event with given ID does not exist      |
| 500         | `query_failed`          | Database query error                    |
| 500         | `streaming_unsupported` | Server does not support HTTP flushing   |

### Frontend Error Handling

```ts
async function fetchAPI<T>(url: string): Promise<T> {
  const res = await fetch(url)
  if (!res.ok) {
    const body = await res.json().catch(() => null)
    const message = body?.error?.message ?? `HTTP ${res.status}`
    throw new Error(message)
  }
  return res.json()
}
```

---

## SSE Streaming — Vue Integration

### Basic EventSource Composable

```ts
import { ref, onUnmounted, watch } from 'vue'

interface SyslogEvent {
  id: number
  received_at: string
  reported_at: string
  hostname: string
  fromhost_ip: string
  programname: string
  msgid: string
  severity: number
  severity_label: string
  facility: number
  facility_label: string
  syslogtag: string
  structured_data: string | null
  message: string
}

export function useSyslogStream(filters: () => Record<string, string>) {
  const events = ref<SyslogEvent[]>([])
  const connected = ref(false)
  let es: EventSource | null = null

  function connect() {
    disconnect()

    const params = new URLSearchParams(filters())
    // Remove empty values
    for (const [key, val] of [...params.entries()]) {
      if (!val) params.delete(key)
    }

    const url = `/api/v1/syslog/stream?${params}`
    es = new EventSource(url)

    es.addEventListener('syslog', (e: MessageEvent) => {
      const event: SyslogEvent = JSON.parse(e.data)
      events.value.push(event)
    })

    es.onopen = () => {
      connected.value = true
    }

    es.onerror = () => {
      connected.value = false
      // EventSource reconnects automatically.
      // On reconnect it sends Last-Event-ID header,
      // so the server backfills missed events.
    }
  }

  function disconnect() {
    if (es) {
      es.close()
      es = null
    }
    connected.value = false
  }

  function clear() {
    events.value = []
  }

  onUnmounted(disconnect)

  return { events, connected, connect, disconnect, clear }
}
```

### Reconnection Behavior

`EventSource` handles reconnection automatically:

1. On disconnect, the browser waits ~3 seconds, then reconnects.
2. On reconnect, it sends the `Last-Event-ID` header with the `id` of the last received event.
3. The server backfills up to 100 events that occurred since that ID.
4. Live streaming resumes after the backfill.

No custom reconnection logic is needed. The browser handles it natively.

### Changing Filters on a Live Stream

When filters change, close the current `EventSource` and open a new one with updated query parameters. The new connection starts fresh with a new backfill.

```ts
// In a component
const filters = reactive({
  hostname: '',
  severity_max: '',
  programname: '',
})

const { events, connected, connect, disconnect, clear } = useSyslogStream(
  () => toRaw(filters)
)

// Reconnect when filters change
watch(filters, () => {
  clear()
  connect()
}, { deep: true })

// Initial connection
onMounted(() => connect())
```

### Combining SSE with Historical Pagination

A common pattern: show a live stream at the top with a "load older" button that fetches historical pages.

1. Open an SSE stream — new events prepend to the list.
2. Track the oldest event ID in the stream.
3. On "load older", use `GET /api/v1/syslog` with the same filters and cursor pagination to fetch older events and append them to the bottom of the list.
4. The two data sources (SSE for new, REST for old) share the same `SyslogEvent` type.

---

## Quick Reference

### Syslog Endpoints

| Endpoint                       | Method | Purpose                  | Auth |
|--------------------------------|--------|--------------------------|------|
| `/api/v1/syslog`               | GET    | List events (paginated)  | No   |
| `/api/v1/syslog/{id}`          | GET    | Get single event         | No   |
| `/api/v1/syslog/stream`        | GET    | SSE live stream          | No   |
| `/api/v1/meta/hosts`           | GET    | Distinct hostnames       | No   |
| `/api/v1/meta/programs`        | GET    | Distinct program names   | No   |
| `/api/v1/meta/facilities`      | GET    | Distinct facility codes  | No   |
| `/api/v1/meta/tags`            | GET    | Distinct syslog tags     | No   |
| `/api/v1/stats/volume`         | GET    | Event volume over time   | No   |
| `/api/v1/juniper/lookup`       | GET    | Juniper syslog reference | No   |

### Application Log Endpoints

| Endpoint                       | Method | Purpose                  | Auth |
|--------------------------------|--------|--------------------------|------|
| `/api/v1/applog`               | GET    | List app logs (paginated)| No   |
| `/api/v1/applog/{id}`          | GET    | Get single app log       | No   |
| `/api/v1/applog/stream`        | GET    | SSE live stream          | No   |
| `/api/v1/applog/ingest`        | POST   | Ingest log batch         | API Key |
| `/api/v1/applog/meta/services` | GET    | Distinct services        | No   |
| `/api/v1/applog/meta/components`| GET   | Distinct components      | No   |
| `/api/v1/applog/stats/volume`  | GET    | App log volume over time | No   |

### System Endpoints

| Endpoint                       | Method | Purpose                  | Auth |
|--------------------------------|--------|--------------------------|------|
| `/health`                      | GET    | Health check             | No   |
| `/metrics`                     | GET    | Prometheus metrics       | No   |

---

## Stats Endpoints

### Volume (Syslog)

```
GET /api/v1/stats/volume
```

Returns event counts bucketed by time interval.

#### Query Parameters

| Param      | Type     | Default    | Description                                      |
|------------|----------|------------|--------------------------------------------------|
| `interval` | `string` | `1h` | Bucket interval: `1m`, `5m`, `15m`, `30m`, `1h`, `6h`, `1d` |
| `range`    | `string` | `24h`      | Time range to query: `1h`, `6h`, `12h`, `24h`, `7d`, `30d` |

#### Response (200)

```json
{
  "data": [
    { "time": "2026-02-01T15:00:00Z", "total": 1234, "by_host": { "web-01": 800, "web-02": 434 } },
    { "time": "2026-02-01T16:00:00Z", "total": 987, "by_host": { "web-01": 600, "web-02": 387 } }
  ]
}
```

---

### Volume (App Logs)

```
GET /api/v1/applog/stats/volume
```

Same interface as syslog volume, but for application logs.

---

## Juniper Reference Lookup

```
GET /api/v1/juniper/lookup
```

Looks up Juniper syslog message reference information by message name.

#### Query Parameters

| Param  | Type     | Required | Description                    |
|--------|----------|----------|--------------------------------|
| `name` | `string` | Yes      | Juniper message name (msgid)   |

#### Response (200)

```json
{
  "data": [
    {
      "name": "RPD_BGP_NEIGHBOR_STATE_CHANGED",
      "message": "BGP peer %s (%s) changed state from %s to %s",
      "description": "A BGP peer changed state",
      "type": "Event",
      "severity": "notice",
      "cause": "BGP state machine transition",
      "action": "Monitor peer status",
      "os": "junos"
    }
  ]
}
```

---

## Application Log Endpoints

### AppLogEvent Data Model

```json
{
  "id": 12345,
  "received_at": "2026-02-01T15:23:00Z",
  "timestamp": "2026-02-01T15:23:00Z",
  "level": "ERROR",
  "service": "api-gateway",
  "component": "auth",
  "msg": "Authentication failed for user",
  "source": "auth/handler.go:123",
  "attrs": { "user_id": "abc123", "ip": "192.168.1.50" }
}
```

| Field         | Type      | Notes                                      |
|---------------|-----------|--------------------------------------------|
| `id`          | `number`  | Unique row ID (int64)                      |
| `received_at` | `string`  | RFC 3339 — when the server received it     |
| `timestamp`   | `string`  | RFC 3339 — when the log was generated      |
| `level`       | `string`  | Log level: DEBUG, INFO, WARN, ERROR        |
| `service`     | `string`  | Service name                               |
| `component`   | `string?` | Optional component within the service      |
| `msg`         | `string`  | Log message                                |
| `source`      | `string?` | Source file and line number                |
| `attrs`       | `object?` | Additional structured attributes (JSON)    |

### List App Logs

```
GET /api/v1/applog
```

Returns a paginated, filtered list of application log events ordered **newest first**.

#### Query Parameters

| Param       | Type     | Default | Description                                  |
|-------------|----------|---------|----------------------------------------------|
| `service`   | `string` | —       | Exact match on service name                   |
| `component` | `string` | —       | Exact match on component                      |
| `level`     | `string` | —       | Exact match on log level                      |
| `search`    | `string` | —       | Full-text search on message                   |
| `from`      | `string` | —       | Start of time range (RFC 3339)                |
| `to`        | `string` | —       | End of time range (RFC 3339)                  |
| `cursor`    | `string` | —       | Pagination cursor                             |
| `limit`     | `int`    | `100`   | Results per page (1–1000)                     |

### Get Single App Log

```
GET /api/v1/applog/{id}
```

Returns a single app log event by ID.

### Stream App Logs (SSE)

```
GET /api/v1/applog/stream
```

Opens a Server-Sent Events stream for real-time app log events. Same behavior as syslog stream (backfill + live events).

#### Query Parameters

Same filters as List App Logs (excluding `cursor` and `limit`).

### Ingest App Logs

```
POST /api/v1/applog/ingest
```

Batch ingest application log events.

#### Authentication

If `api_keys` is configured in the server config, this endpoint requires a Bearer token:

```
Authorization: Bearer <api-key>
```

#### Request Body

```json
{
  "logs": [
    {
      "timestamp": "2026-02-01T15:23:00Z",
      "level": "INFO",
      "msg": "User logged in",
      "service": "auth-service",
      "component": "login",
      "source": "auth/login.go:45",
      "attrs": { "user_id": "abc123" }
    }
  ]
}
```

| Field       | Type     | Required | Description                    |
|-------------|----------|----------|--------------------------------|
| `timestamp` | `string` | Yes      | RFC 3339 timestamp             |
| `level`     | `string` | Yes      | Log level                      |
| `msg`       | `string` | Yes      | Log message                    |
| `service`   | `string` | Yes      | Service name (max 128 chars)   |
| `component` | `string` | No       | Component name (max 128 chars) |
| `host`      | `string` | Yes      | Hostname (max 256 chars)       |
| `source`    | `string` | No       | Source file:line (max 256 chars) |
| `attrs`     | `object` | No       | Additional attributes (max 64 KB) |

#### Response (202 Accepted)

```json
{
  "accepted": 5
}
```

#### Errors

| Status | Code                | Message                           |
|--------|---------------------|-----------------------------------|
| 400    | `invalid_json`      | Malformed JSON body               |
| 400    | `empty_batch`       | Logs array is empty               |
| 400    | `batch_too_large`   | Max batch size is 1000 entries    |
| 400    | `validation_failed` | Field validation errors (details in message) |
| 401    | `unauthorized`      | Missing or invalid API key        |
| 413    | `body_too_large`    | Request body exceeds 5 MB limit   |
| 500    | `insert_failed`     | Database insert error             |

### Meta: List Services

```
GET /api/v1/applog/meta/services
```

Returns distinct service names.

### Meta: List Components

```
GET /api/v1/applog/meta/components
```

Returns distinct component names.

---

## Prometheus Metrics

```
GET /metrics
```

Exposes Prometheus metrics for monitoring. Available metrics include:

- `taillight_http_requests_total` — HTTP request count by method, path, status
- `taillight_http_request_duration_seconds` — Request latency histogram
- `taillight_sse_clients_active` — Current SSE client connections
- `taillight_events_broadcast_total` — Events broadcast to SSE clients
- `taillight_events_dropped_total` — Events dropped due to slow clients
- `taillight_notifications_received_total` — PostgreSQL NOTIFY received
- `taillight_db_pool_*` — Database connection pool stats
