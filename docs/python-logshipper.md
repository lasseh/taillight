<!--
AI_CONTEXT: This is the complete, self-contained reference for integrating
Python applications with Taillight log ingestion via the taillight-sdk package.

It contains everything needed to generate correct integration code:
SDK constructor API, configuration, level mapping, behavioral contracts,
wire format, error handling, framework recipes, and troubleshooting.

No external documents are required. All field names, defaults, and limits
are verified against the SDK source and server-side validation.
-->

# Python Log Shipper

Ship logs from any Python application to [Taillight](https://github.com/lasseh/taillight)'s
applog ingest endpoint using the built-in `logging` module — no external
dependencies required.

`taillight-sdk` provides `TaillightHandler`, a `logging.Handler` subclass
that batches log entries in a background thread and ships them via HTTP POST.
It is non-blocking, thread-safe, and drops entries on overflow rather than
slowing your application.

This is the Python equivalent of the Go
[logshipper](https://github.com/lasseh/taillight/blob/main/api/pkg/logshipper/README.md)
package. Same batching strategy, same API contract, same drop-on-overflow
behavior.

## Install

```sh
pip install taillight-sdk
```

- **Python 3.9+**
- **No external dependencies** (stdlib only)

## Quick start

```python
import logging
import os
from taillight_sdk import TaillightHandler

handler = TaillightHandler(
    endpoint=os.environ["TAILLIGHT_URL"],       # e.g. "https://taillight.example.com/api/v1/applog/ingest"
    api_key=os.environ["TAILLIGHT_API_KEY"],     # API key with "ingest" scope
    service="my-python-app",                     # appears as the service name in Taillight UI
)

logger = logging.getLogger("myapp")
logger.addHandler(handler)
logger.setLevel(logging.DEBUG)

logger.info("server started", extra={"port": 8080})

# On shutdown — flush remaining logs.
handler.shutdown()
```

## Constructor reference

```python
TaillightHandler(
    endpoint: str,            # Required
    api_key: str = "",
    service: str = "",
    component: str = "",
    host: str = "",           # Defaults to socket.gethostname()
    batch_size: int = 100,
    flush_interval: float = 1.0,
    buffer_size: int = 1024,
    timeout: float = 5.0,
)
```

All parameters are keyword arguments.

| Parameter        | Type    | Default              | Required | Description                                                                 |
|------------------|---------|----------------------|----------|-----------------------------------------------------------------------------|
| `endpoint`       | `str`   | —                    | **yes**  | Full ingest URL, must end with `/api/v1/applog/ingest`                      |
| `api_key`        | `str`   | `""`                 | no       | Bearer token sent as `Authorization: Bearer <key>`. Must have `ingest` scope when auth is enabled |
| `service`        | `str`   | `""`                 | no*      | Service name attached to every entry. *The server requires this field — set it here or entries fail validation |
| `component`      | `str`   | `""`                 | no       | Optional component label. Omitted from the JSON payload when empty          |
| `host`           | `str`   | `socket.gethostname()` | no     | Host/instance identifier. Auto-detected if not set                          |
| `batch_size`     | `int`   | `100`                | no       | Flush when the batch reaches this many entries                              |
| `flush_interval` | `float` | `1.0`                | no       | Flush at least this often (seconds). Whichever of `batch_size` or `flush_interval` is reached first triggers a flush |
| `buffer_size`    | `int`   | `1024`               | no       | Internal queue capacity. Entries are silently dropped when the queue is full |
| `timeout`        | `float` | `5.0`                | no       | HTTP request timeout in seconds                                             |

## Level mapping

The SDK maps Python logging levels to Taillight's five canonical levels:

| Python Level            | Python Constant          | Taillight Level |
|-------------------------|--------------------------|-----------------|
| `DEBUG`                 | `logging.DEBUG` (10)     | `DEBUG`         |
| `INFO`                  | `logging.INFO` (20)      | `INFO`          |
| `WARNING`               | `logging.WARNING` (30)   | `WARN`          |
| `ERROR`                 | `logging.ERROR` (40)     | `ERROR`         |
| `CRITICAL`              | `logging.CRITICAL` (50)  | `FATAL`         |

Any unmapped numeric level defaults to `INFO`.

The server also accepts these aliases (case-insensitive): `TRACE` → `DEBUG`,
`WARNING` → `WARN`, `CRITICAL` → `FATAL`, `PANIC` → `FATAL`.

## Structured logging

Pass structured data via the `extra=` keyword. These fields are serialized
into the `attrs` JSON object in the ingest payload:

```python
logger.info("request handled", extra={
    "method": "GET",
    "path": "/api/users",
    "status": 200,
    "duration_ms": 42,
})
```

### How extra fields work

Every key in `extra={}` that is **not** a standard `logging.LogRecord`
attribute is placed into the `attrs` object. Standard LogRecord fields
(`name`, `levelname`, `pathname`, `message`, `args`, `exc_info`, etc.) are
filtered out automatically.

**Key name collisions:** If you use a key that matches a LogRecord attribute
(e.g., `message`, `levelname`, `name`), it will be silently ignored because
it's treated as a reserved field. Rename your key to avoid this.

**Non-serializable values:** The SDK uses `json.dumps(default=str)`, so any
object that isn't JSON-serializable is converted to its `str()` representation
rather than raising an error.

### Nested and complex values

```python
logger.info("user action", extra={
    "user": {"id": 42, "role": "admin"},       # nested dicts are preserved
    "tags": ["important", "audit"],             # lists are preserved
    "request_id": uuid.uuid4(),                 # UUID → string via str()
    "elapsed": timedelta(milliseconds=150),     # timedelta → string via str()
})
```

## Entry wire format

Each log entry is serialized by the SDK into this JSON structure:

```jsonc
{
    "timestamp": "2026-01-15T10:30:00.123456+00:00",  // ISO 8601 UTC, from record.created
    "level": "INFO",                                    // mapped from Python level (see table above)
    "msg": "request handled",                           // record.getMessage()
    "service": "my-python-app",                         // from constructor
    "host": "prod-1",                                   // from constructor or socket.gethostname()
    "component": "worker",                              // only present if set in constructor
    "source": "app/main.py:42",                         // auto-populated from record.pathname:lineno
    "attrs": {                                          // only present if extra= fields were passed
        "method": "GET",
        "status": 200
    }
}
```

Entries are batched and sent as:

```json
{
    "logs": [
        { "timestamp": "...", "level": "...", "msg": "...", ... },
        { "timestamp": "...", "level": "...", "msg": "...", ... }
    ]
}
```

## Behavioral specification

Understanding these behaviors helps when tuning performance or debugging
integration issues.

### Threading model

The constructor starts a single **daemon thread** named `"taillight-shipper"`.
Because it is a daemon thread, it does not prevent the Python interpreter from
exiting. The handler registers an `atexit` hook to flush remaining entries on
normal shutdown.

### Non-blocking emit

`emit()` places entries onto an internal queue using `put_nowait()`. It
**never blocks** the calling thread. If the queue is full, the entry is
silently dropped and the `dropped` counter is incremented.

### Flush triggers

The background thread flushes a batch when **either** condition is met:

1. The batch accumulates `batch_size` entries, **or**
2. `flush_interval` seconds have elapsed since the last flush attempt

Whichever happens first triggers the HTTP POST.

### Backoff on failure

When a send fails (HTTP error, connection refused, timeout), the handler
applies exponential backoff:

| Consecutive failures | Backoff delay |
|----------------------|---------------|
| 1                    | 1 s           |
| 2                    | 2 s           |
| 3                    | 4 s           |
| 4                    | 8 s           |
| 5                    | 16 s          |
| 6                    | 32 s          |
| 7+                   | 60 s (cap)    |

Backoff resets to zero on the first successful send. During backoff, `emit()`
still accepts entries into the queue (they may drop if the queue fills up).

A **one-time warning** is printed to stderr on the first failure:
```
taillight-sdk: first send failure (HTTP 401), will retry with backoff — check endpoint and API key
```

## Shutdown and lifecycle

### Automatic cleanup

The handler registers an `atexit` hook in the constructor. On normal
interpreter exit, `shutdown()` is called automatically — remaining entries
are drained and flushed.

### Explicit shutdown

For web servers and long-running apps, call `shutdown()` explicitly in your
teardown logic:

```python
handler.shutdown(timeout=5.0)
```

**What `shutdown()` does:**
1. Signals the background thread to stop
2. Joins the thread (waits up to `timeout` seconds)
3. Drains any remaining entries from the queue
4. Flushes them synchronously in a final HTTP POST
5. Unregisters the atexit hook

`shutdown()` is **idempotent** — safe to call multiple times.

`close()` calls `shutdown()` then `super().close()`. It is invoked
automatically by `logging.shutdown()` during interpreter teardown.

## Monitoring

The handler exposes two read-only properties for health checks:

```python
handler.dropped      # int — entries dropped because the queue was full
handler.send_failed  # int — batch send attempts that failed (HTTP errors, timeouts, etc.)
```

Example health check:

```python
def check_logging_health(handler: TaillightHandler) -> dict:
    return {
        "dropped": handler.dropped,
        "send_failed": handler.send_failed,
        "healthy": handler.dropped == 0 and handler.send_failed == 0,
    }
```

If `dropped` is increasing, raise `buffer_size` or reduce log volume.
If `send_failed` is increasing, check the endpoint URL and API key.

## Dual output (stderr + Taillight)

Python's `logging` natively supports multiple handlers on a single logger.
No special fan-out helper needed:

```python
import logging
import os
import sys

logger = logging.getLogger("myapp")
logger.setLevel(logging.DEBUG)

# Print to stderr locally.
stderr_handler = logging.StreamHandler(sys.stderr)
stderr_handler.setFormatter(logging.Formatter(
    "%(asctime)s %(levelname)-5s %(message)s"
))
logger.addHandler(stderr_handler)

# Ship to Taillight.
logger.addHandler(TaillightHandler(
    endpoint=os.environ["TAILLIGHT_URL"],
    api_key=os.environ["TAILLIGHT_API_KEY"],
    service="my-python-app",
))
```

## Framework integration

### Django

Add the handler to Django's `LOGGING` dict in `settings.py`:

```python
import os

LOGGING = {
    "version": 1,
    "disable_existing_loggers": False,
    "handlers": {
        "console": {
            "class": "logging.StreamHandler",
        },
        "taillight": {
            "()": "taillight_sdk.TaillightHandler",
            "endpoint": os.environ.get("TAILLIGHT_URL", ""),
            "api_key": os.environ.get("TAILLIGHT_API_KEY", ""),
            "service": "my-django-app",
            "component": "web",
        },
    },
    "root": {
        "handlers": ["console", "taillight"],
        "level": "INFO",
    },
}
```

Django calls `logging.shutdown()` on exit, which triggers `close()` →
`shutdown()` automatically.

### Flask

```python
import os
from flask import Flask
from taillight_sdk import TaillightHandler

app = Flask(__name__)

handler = TaillightHandler(
    endpoint=os.environ["TAILLIGHT_URL"],
    api_key=os.environ["TAILLIGHT_API_KEY"],
    service="my-flask-app",
    component="web",
)
app.logger.addHandler(handler)
```

### FastAPI

Use FastAPI's lifespan to ensure clean shutdown:

```python
import logging
import os
from contextlib import asynccontextmanager

from fastapi import FastAPI
from taillight_sdk import TaillightHandler

handler = TaillightHandler(
    endpoint=os.environ["TAILLIGHT_URL"],
    api_key=os.environ["TAILLIGHT_API_KEY"],
    service="my-fastapi-app",
    component="web",
)

@asynccontextmanager
async def lifespan(app: FastAPI):
    logging.getLogger().addHandler(handler)
    yield
    handler.shutdown()

app = FastAPI(lifespan=lifespan)
logger = logging.getLogger("myapp")

@app.get("/")
async def root():
    logger.info("request received", extra={"path": "/"})
    return {"status": "ok"}
```

### Celery

Attach the handler in the worker init signal so each worker process ships
logs:

```python
import logging
import os
from celery import Celery
from celery.signals import worker_init, worker_shutdown
from taillight_sdk import TaillightHandler

app = Celery("tasks")
_handler = None

@worker_init.connect
def setup_logging(**kwargs):
    global _handler
    _handler = TaillightHandler(
        endpoint=os.environ["TAILLIGHT_URL"],
        api_key=os.environ["TAILLIGHT_API_KEY"],
        service="my-celery-app",
        component="worker",
    )
    logging.getLogger().addHandler(_handler)

@worker_shutdown.connect
def teardown_logging(**kwargs):
    if _handler:
        _handler.shutdown()
```

### structlog

Bridge structlog through stdlib logging so `TaillightHandler` picks up all
output:

```python
import logging
import os
import structlog
from taillight_sdk import TaillightHandler

# Configure structlog to render via stdlib logging.
structlog.configure(
    processors=[
        structlog.contextvars.merge_contextvars,
        structlog.processors.add_log_level,
        structlog.processors.StackInfoRenderer(),
        structlog.processors.format_exc_info,
        structlog.processors.TimeStamper(fmt="iso"),
        structlog.stdlib.ProcessorFormatter.wrap_for_formatter,
    ],
    logger_factory=structlog.stdlib.LoggerFactory(),
    wrapper_class=structlog.stdlib.BoundLogger,
)

# Attach TaillightHandler to the root stdlib logger.
handler = TaillightHandler(
    endpoint=os.environ["TAILLIGHT_URL"],
    api_key=os.environ["TAILLIGHT_API_KEY"],
    service="my-app",
)
logging.getLogger().addHandler(handler)
logging.getLogger().setLevel(logging.DEBUG)

# Now structlog calls ship to Taillight.
log = structlog.get_logger()
log.info("request handled", method="GET", path="/api/users", status=200)
```

**Note:** structlog places bound fields into `extra` on the LogRecord, so they
appear in `attrs` automatically.

## Environment variable configuration

A reusable factory pattern for configuring from environment variables:

```python
import os
from taillight_sdk import TaillightHandler

def make_taillight_handler(**overrides) -> TaillightHandler:
    """Create a TaillightHandler configured from environment variables.

    Environment variables:
        TAILLIGHT_URL       — Ingest endpoint URL (required)
        TAILLIGHT_API_KEY   — API key with ingest scope (required when auth is enabled)
        TAILLIGHT_SERVICE   — Service name (default: "python-app")
        TAILLIGHT_COMPONENT — Component label (default: "")
    """
    return TaillightHandler(
        endpoint=os.environ["TAILLIGHT_URL"],
        api_key=os.environ.get("TAILLIGHT_API_KEY", ""),
        service=os.environ.get("TAILLIGHT_SERVICE", "python-app"),
        component=os.environ.get("TAILLIGHT_COMPONENT", ""),
        **overrides,
    )
```

## Ingest API reference

Full specification of the HTTP endpoint that the SDK calls.

**Endpoint:** `POST /api/v1/applog/ingest`

**Headers:**
```
Content-Type: application/json
Authorization: Bearer <API_KEY>
```

The API key must have the **`ingest`** scope. Keys are created via the
`taillight apikey` CLI command. When authentication is disabled in the
server config, the `Authorization` header is optional.

### Request body

```json
{
    "logs": [
        {
            "timestamp": "2026-01-15T10:30:00Z",
            "level": "INFO",
            "msg": "hello world",
            "service": "my-app",
            "host": "prod-1",
            "component": "worker",
            "source": "app.py:42",
            "attrs": {"key": "value"}
        }
    ]
}
```

### Field constraints

| Field       | Required | Type   | Max Size   | Notes                                                      |
|-------------|----------|--------|------------|------------------------------------------------------------|
| `timestamp` | **yes**  | string | —          | RFC 3339 (e.g., `2026-01-15T10:30:00Z`). SDK handles this automatically |
| `level`     | **yes**  | string | —          | `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`. Case-insensitive. Aliases: `TRACE`→`DEBUG`, `WARNING`→`WARN`, `CRITICAL`/`PANIC`→`FATAL` |
| `msg`       | **yes**  | string | 64 KB      | The log message                                             |
| `service`   | **yes**  | string | 128 chars  | Service name                                                |
| `host`      | **yes**  | string | 256 chars  | Hostname or instance identifier                             |
| `component` | no       | string | 128 chars  | Component or subsystem label                                |
| `source`    | no       | string | 256 chars  | Source file and line (e.g., `app.py:42`)                    |
| `attrs`     | no       | object | 64 KB      | Arbitrary JSON key-value pairs                              |

### Batch limits

- Maximum **1,000 entries** per request
- Maximum **5 MB** request body

### Success response

**`202 Accepted`**

```json
{"accepted": 5}
```

The `accepted` field contains the number of entries stored.

### Error responses

All errors use this envelope:

```json
{
    "error": {
        "code": "validation_failed",
        "message": "logs[0]: service is required; logs[0]: host is required"
    }
}
```

Validation errors include the array index of each failing entry.

| HTTP Status | Error Code          | Cause                                                    |
|-------------|---------------------|----------------------------------------------------------|
| 400         | `invalid_json`      | Request body is not valid JSON                           |
| 400         | `empty_batch`       | `logs` array is empty                                    |
| 400         | `batch_too_large`   | More than 1,000 entries in the batch                     |
| 400         | `validation_failed` | One or more entries failed field validation (details in `message`) |
| 401         | `unauthorized`      | Missing or invalid API key (when auth is enabled)        |
| 403         | `forbidden`         | API key does not have the `ingest` scope                 |
| 413         | `body_too_large`    | Request body exceeds 5 MB                                |
| 500         | `insert_failed`     | Server-side database error (retrying may help)           |

## Troubleshooting

### No logs appearing in Taillight

1. Verify the endpoint URL ends with `/api/v1/applog/ingest`
2. Check that the API key has the `ingest` scope
3. Check `handler.send_failed` — if > 0, the SDK cannot reach the server
4. Look for the one-time stderr warning: `taillight-sdk: first send failure ...`
5. Ensure `service` and `host` are set (both are required by the server)

### Logs appear with a delay

The default `flush_interval` is 1 second. To reduce latency:
- Lower `flush_interval` (e.g., `0.25` for 250ms)
- Lower `batch_size` if entries arrive slowly (e.g., `10`)

### `dropped` counter is increasing

The internal queue is full — the app is producing logs faster than they can
be shipped. Options:
- Increase `buffer_size` (e.g., `4096` or `8192`)
- Reduce log volume (raise the handler's level with `handler.setLevel(logging.WARNING)`)
- Check if `send_failed` is also increasing (network issues cause a backlog)

### Extra fields not appearing in `attrs`

The field name likely collides with a Python `logging.LogRecord` attribute.
Common collisions: `message`, `name`, `levelname`, `filename`, `module`,
`funcName`, `args`. Rename your field to a non-reserved name.

### 401 Unauthorized or 403 Forbidden

- **401**: API key is missing or invalid. Check the `api_key` value.
- **403**: API key exists but lacks the `ingest` scope. Create a new key
  with `taillight apikey create --scope ingest`.

### `shutdown()` is slow

The handler waits up to `timeout` seconds (default 5.0) for the background
thread to finish. If the thread is stuck in an HTTP request:
- Lower the constructor `timeout` parameter (e.g., `2.0`)
- Pass a shorter timeout to `shutdown(timeout=2.0)`

## Version

```python
from taillight_sdk import __version__
print(__version__)  # e.g. "0.1.0"
```

**PyPI package:** [`taillight-sdk`](https://pypi.org/project/taillight-sdk/)
**Source:** [`sdk/python/`](https://github.com/lasseh/taillight/tree/main/sdk/python)
