# journald Remote Ingestion — Research & Design

Centralize journald logs from multiple Linux servers into Taillight. Covers the journal export format, three ingestion approaches, and a recommended implementation using a native `systemd-journal-upload` compatible endpoint.

---

## Table of Contents

1. [Overview & Goals](#1-overview--goals)
2. [Ingestion Approaches](#2-ingestion-approaches)
3. [Journal Export Format](#3-journal-export-format)
4. [systemd-journal-upload Protocol](#4-systemd-journal-upload-protocol)
5. [Implementation Sketch — Option A](#5-implementation-sketch--option-a)
6. [Existing Go Parsers](#6-existing-go-parsers)
7. [Client Setup](#7-client-setup)
8. [References](#8-references)

---

## 1. Overview & Goals

### Problem

Linux servers use journald for local logging. In a multi-server environment, these logs stay isolated on each host — there is no built-in way to search or stream them centrally. Taillight already handles syslog and applog ingestion; adding journald support completes the picture.

### Goals

- Accept journal entries from remote servers with minimal client-side tooling
- Preserve structured journal fields (unit name, PID, UID, boot ID, etc.)
- Support cursor tracking so clients resume after disconnect without duplicates
- Integrate with Taillight's existing broker/SSE fan-out for real-time streaming
- No custom agent required on clients — use `systemd-journal-upload` where possible

---

## 2. Ingestion Approaches

### Option A: Native `systemd-journal-upload` Endpoint (Recommended)

Implement an HTTP endpoint that speaks the same protocol as `systemd-journal-remote`. Clients use the standard `systemd-journal-upload` service — no custom software needed.

| Pros | Cons |
|------|------|
| Zero client-side tooling — ships with systemd | Must implement the journal export format parser |
| Cursor tracking built into the protocol | Chunked transfer encoding handling required |
| Battle-tested client with retry and backoff | Binary field support adds parser complexity |
| Preserves all native journal fields | |

### Option B: JSON Batch API with Custom Shipper

Expose a `/api/v1/journal/ingest` endpoint accepting JSON arrays. Write a small shipper (or shell script using `journalctl --output=json`) that periodically POSTs batches.

| Pros | Cons |
|------|------|
| Simple JSON parsing on the server | Requires a custom shipper on every client |
| Flexible — clients can filter before sending | Cursor tracking must be implemented client-side |
| Easy to test with `curl` | Another moving part to deploy and maintain |

### Option C: Syslog Forwarding via `ForwardToSyslog`

Set `ForwardToSyslog=yes` in `journald.conf` and let rsyslog forward to Taillight's existing syslog pipeline.

| Pros | Cons |
|------|------|
| No server changes — uses existing syslog path | Loses all structured journal fields |
| Simple to configure | Message truncation at syslog length limits |
| | Double-logging (journal + syslog) wastes disk |
| | No cursor tracking — gaps on disconnect |

**Verdict:** Option A gives the best fidelity and operational simplicity. Option B is a reasonable fallback if the export format parser proves too complex. Option C is a last resort.

---

## 3. Journal Export Format

The journal export format is a binary-safe serialization used by `systemd-journal-gatewayd`, `systemd-journal-upload`, and `systemd-journal-remote`. Defined in the [systemd documentation](https://systemd.io/JOURNAL_EXPORT_FORMATS/).

### Entry Structure

Each journal entry is a sequence of fields, terminated by a double newline (`\n\n`). The stream is a concatenation of entries.

### Field Encoding

Fields come in two forms:

#### Text Fields

```
FIELD_NAME=value\n
```

UTF-8 safe values are encoded as a single line. The field name is uppercase with underscores, followed by `=`, the value, and a newline.

Example:
```
MESSAGE=Started user session 42\n
_HOSTNAME=web-01\n
PRIORITY=6\n
```

#### Binary Fields

```
FIELD_NAME\n
<8-byte little-endian uint64 length>\n
<raw bytes>\n
```

When a value contains non-UTF-8 bytes (or a newline), the format switches to binary encoding: the field name alone on a line, followed by an 8-byte little-endian unsigned integer giving the payload length, followed by the raw payload bytes, followed by a newline.

### Entry Separator

Two consecutive newlines (`\n\n`) separate entries. A parser reads fields until it hits a blank line, then emits the entry.

### Common Metadata Fields

These fields are added by journald automatically (prefixed with double underscore):

| Field | Description |
|-------|-------------|
| `__CURSOR` | Opaque string uniquely identifying the entry position |
| `__REALTIME_TIMESTAMP` | Wall-clock time in microseconds since epoch (UTC) |
| `__MONOTONIC_TIMESTAMP` | Monotonic time in microseconds since boot |

### Common Source Fields

Single-underscore prefix indicates trusted fields set by journald (not the client):

| Field | Description |
|-------|-------------|
| `_HOSTNAME` | Originating hostname |
| `_TRANSPORT` | How the entry arrived (`journal`, `syslog`, `stdout`, `kernel`) |
| `_SYSTEMD_UNIT` | Systemd unit name (e.g., `nginx.service`) |
| `_PID` | Process ID |
| `_UID` | User ID of the logging process |
| `_GID` | Group ID |
| `_COMM` | Process command name |
| `_EXE` | Process executable path |
| `_BOOT_ID` | Boot UUID |
| `_MACHINE_ID` | Machine UUID |

### User-Set Fields

No underscore prefix — set by the logging application:

| Field | Description |
|-------|-------------|
| `MESSAGE` | The log message body |
| `PRIORITY` | Syslog priority level (0–7) |
| `SYSLOG_IDENTIFIER` | Program name (like syslog tag) |
| `SYSLOG_FACILITY` | Syslog facility number |
| `CODE_FILE` | Source file (if structured logging is used) |
| `CODE_LINE` | Source line number |
| `CODE_FUNC` | Function name |

### Example Entry (Text Representation)

```
__CURSOR=s=abc123;i=42;b=boot-id;m=12345;t=67890;x=deadbeef
__REALTIME_TIMESTAMP=1700000000000000
__MONOTONIC_TIMESTAMP=123456789
_BOOT_ID=a1b2c3d4-e5f6-7890-abcd-ef1234567890
_MACHINE_ID=deadbeef12345678
_HOSTNAME=web-01
_TRANSPORT=journal
_SYSTEMD_UNIT=nginx.service
_PID=1234
_UID=0
_GID=0
_COMM=nginx
PRIORITY=6
SYSLOG_IDENTIFIER=nginx
MESSAGE=upstream response time 0.042s

```

(Note the trailing blank line marking end of entry.)

---

## 4. systemd-journal-upload Protocol

`systemd-journal-upload` is the standard client that ships journal entries to a remote server over HTTP(S).

### HTTP Protocol

- **Method:** `POST`
- **Path:** `/upload`
- **Content-Type:** `application/vnd.fdo.journal`
- **Transfer-Encoding:** `chunked` (stream of entries, connection held open)
- **Accept:** The client sends `Accept: application/vnd.fdo.journal`

The client opens a long-lived HTTP connection and streams entries in the journal export format as a chunked request body. Entries continue flowing as new journal entries appear.

### Cursor Tracking

After processing a batch, the server responds with:

```http
HTTP/1.1 200 OK
```

The upload service persists its cursor locally (by default in `/var/lib/systemd/journal-upload/state`). On restart, it resumes from the last successfully acknowledged cursor — no duplicates, no gaps.

The cursor file contains a single line:

```
LAST_CURSOR=s=abc123;i=42;b=boot-id;m=12345;t=67890;x=deadbeef
```

### Connection Lifecycle

1. Client connects and sends `POST /upload` with chunked encoding
2. Client streams journal entries continuously
3. On disconnect, the client retries with exponential backoff
4. On reconnect, streaming resumes from the last cursor

### TLS / Authentication

`systemd-journal-upload` supports HTTPS with client certificates:

- `--url=https://taillight.example.com:19532/upload`
- `--server-certificate=` — CA to verify the server
- `--client-certificate=` and `--client-key=` — mTLS client auth

For environments without mTLS, the upload can run behind a reverse proxy that terminates TLS and adds an API key header, or Taillight can accept plain HTTP on a trusted network.

---

## 5. Implementation Sketch — Option A

### Database Table

```sql
CREATE TABLE journal_events (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY,
    received_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    timestamp           TIMESTAMPTZ NOT NULL,  -- from __REALTIME_TIMESTAMP
    hostname            TEXT        NOT NULL,
    machine_id          TEXT        NOT NULL DEFAULT '',
    boot_id             TEXT        NOT NULL DEFAULT '',
    transport            TEXT        NOT NULL DEFAULT '',
    systemd_unit        TEXT        NOT NULL DEFAULT '',
    syslog_identifier   TEXT        NOT NULL DEFAULT '',
    pid                 INT,
    uid                 INT,
    priority            SMALLINT    NOT NULL DEFAULT 6,
    message             TEXT        NOT NULL,
    cursor              TEXT        NOT NULL,
    extra               JSONB       NOT NULL DEFAULT '{}'
);

-- TimescaleDB hypertable
SELECT create_hypertable('journal_events', 'timestamp');

-- Indexes
CREATE INDEX ON journal_events (hostname, timestamp DESC);
CREATE INDEX ON journal_events (systemd_unit, timestamp DESC);
CREATE INDEX ON journal_events (priority, timestamp DESC);

-- Trigger for LISTEN/NOTIFY
CREATE OR REPLACE FUNCTION notify_journal_event() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('journal_ingest', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER journal_event_notify
    AFTER INSERT ON journal_events
    FOR EACH ROW EXECUTE FUNCTION notify_journal_event();
```

The `extra` JSONB column captures all fields not mapped to dedicated columns, preserving full fidelity without schema changes for every possible journal field.

### Handler

New handler at `POST /upload` accepting `application/vnd.fdo.journal`:

```go
func (h *Handler) JournalUpload(w http.ResponseWriter, r *http.Request) {
    // 1. Verify Content-Type is application/vnd.fdo.journal
    // 2. Read chunked body through journal export format parser
    // 3. For each parsed entry:
    //    a. Map known fields to JournalEvent struct
    //    b. Collect remaining fields into Extra map
    //    c. Send to batch inserter channel
    // 4. Return 200 OK (client tracks cursor locally)
}
```

### Parser

The parser is a streaming state machine:

```go
type JournalParser struct {
    reader io.Reader
}

type JournalEntry struct {
    Fields map[string]string
    // Binary fields stored as base64 in the map
}

func (p *JournalParser) Next() (*JournalEntry, error) {
    // Read lines until blank line (entry separator)
    // For each line:
    //   - If contains '=' → text field: split on first '='
    //   - If no '=' → binary field: read 8-byte length, then payload
    // Return assembled entry
}
```

### Model

```go
type JournalEvent struct {
    ID              int64              `json:"id" db:"id"`
    ReceivedAt      time.Time          `json:"received_at" db:"received_at"`
    Timestamp       time.Time          `json:"timestamp" db:"timestamp"`
    Hostname        string             `json:"hostname" db:"hostname"`
    MachineID       string             `json:"machine_id" db:"machine_id"`
    BootID          string             `json:"boot_id" db:"boot_id"`
    Transport       string             `json:"transport" db:"transport"`
    SystemdUnit     string             `json:"systemd_unit" db:"systemd_unit"`
    SyslogIdentifier string           `json:"syslog_identifier" db:"syslog_identifier"`
    PID             *int               `json:"pid" db:"pid"`
    UID             *int               `json:"uid" db:"uid"`
    Priority        int                `json:"priority" db:"priority"`
    Message         string             `json:"message" db:"message"`
    Cursor          string             `json:"cursor" db:"cursor"`
    Extra           map[string]string  `json:"extra" db:"extra"`
}
```

### Broker Integration

Follow the same pattern as applog:

1. Add a `journal_ingest` LISTEN channel in the postgres listener
2. Create a `JournalBroker` with per-client filtering (hostname, unit, priority)
3. Add an SSE endpoint at `GET /api/v1/journal/stream`
4. Reuse the existing batch inserter pattern for writes

### Route Registration

```go
r.Route("/api/v1/journal", func(r chi.Router) {
    r.Post("/upload", h.JournalUpload)        // systemd-journal-upload endpoint
    r.Get("/stream", h.JournalStream)          // SSE stream
    r.Get("/", h.ListJournalEvents)            // paginated query
})
```

---

## 6. Existing Go Parsers

### github.com/coreos/go-systemd/sdjournal

The CoreOS library provides Go bindings to `libsystemd`. It can read local journal files but requires CGo and `libsystemd-dev` — not ideal for a cross-compiled static binary.

### Custom Parser

The journal export format is simple enough that a purpose-built parser (as sketched above) is the better choice for Taillight:

- No CGo dependency
- Works with streaming HTTP bodies
- Only needs to parse the export wire format, not local `.journal` files
- Approximately 80–120 lines of Go

### github.com/ssgreg/journald (write-only)

Provides Go bindings for writing to journald, not reading. Not applicable here.

---

## 7. Client Setup

### Install

`systemd-journal-upload` is packaged separately on most distributions:

```bash
# Debian/Ubuntu
apt install systemd-journal-remote

# RHEL/Fedora
dnf install systemd-journal-remote
```

The package provides both `systemd-journal-upload` (client) and `systemd-journal-remote` (server). We only need the client.

### Configuration

`/etc/systemd/journal-upload.conf`:

```ini
[Upload]
URL=http://taillight.example.com:8080/upload
# For HTTPS with client certificates:
# URL=https://taillight.example.com:19532/upload
# ServerCertificateFile=/etc/ssl/certs/taillight-ca.pem
# ClientCertificateFile=/etc/ssl/certs/journal-upload.pem
# ClientKeyFile=/etc/ssl/private/journal-upload.key
```

### Systemd Unit

The service is included in the package. Enable and start it:

```bash
systemctl enable --now systemd-journal-upload.service
```

The default unit file:

```ini
[Unit]
Description=Journal Remote Upload Service
Documentation=man:systemd-journal-upload(8)
After=network-online.target
Wants=network-online.target

[Service]
User=systemd-journal-upload
Group=systemd-journal-upload
DynamicUser=yes
ExecStart=/usr/lib/systemd/systemd-journal-upload --save-state
WatchdogSec=3min
Restart=on-failure
PrivateTmp=yes
ProtectSystem=full
StateDirectory=systemd/journal-upload

[Install]
WantedBy=multi-user.target
```

### Cursor State

The upload service saves its cursor to `/var/lib/systemd/journal-upload/state`. This file persists across restarts, ensuring no entries are lost or duplicated.

### Verify

Check the service is running and streaming:

```bash
systemctl status systemd-journal-upload
journalctl -u systemd-journal-upload -f
```

---

## 8. References

- [Journal Export Format](https://systemd.io/JOURNAL_EXPORT_FORMATS/) — wire format specification
- [systemd-journal-upload(8)](https://www.freedesktop.org/software/systemd/man/systemd-journal-upload.html) — client man page
- [systemd-journal-remote(8)](https://www.freedesktop.org/software/systemd/man/systemd-journal-remote.html) — reference server
- [systemd-journal-gatewayd(8)](https://www.freedesktop.org/software/systemd/man/systemd-journal-gatewayd.html) — HTTP gateway for local queries
- [coreos/go-systemd](https://github.com/coreos/go-systemd) — Go bindings (CGo required)
