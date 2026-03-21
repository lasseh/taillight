# Python Log Shipper

Ship logs from any Python application to Taillight's applog ingest endpoint
using the built-in `logging` module — no external dependencies required.

This is the Python equivalent of the Go [`logshipper`](../api/pkg/logshipper/README.md)
package. Same batching strategy, same API contract, same drop-on-overflow behavior.

## Install

```sh
pip install taillight-sdk
```

## Requirements

- Python 3.9+
- No external dependencies (stdlib only)

## Quick start

```python
import logging
from taillight_sdk import TaillightHandler

handler = TaillightHandler(
    endpoint="https://taillight.example.com/api/v1/applog/ingest",
    api_key="your-api-key",
    service="my-python-app",
)

logger = logging.getLogger("myapp")
logger.addHandler(handler)
logger.setLevel(logging.DEBUG)

logger.info("server started", extra={"port": 8080})

# On shutdown — flush remaining logs
handler.shutdown()
```

## Config reference

The source is at [`sdk/python/src/taillight_sdk/_handler.py`](../sdk/python/src/taillight_sdk/_handler.py).

| Parameter        | Type    | Default            | Description                                    |
|------------------|---------|--------------------|------------------------------------------------|
| `endpoint`       | `str`   | —                  | Ingest URL (required)                          |
| `api_key`        | `str`   | `""`               | Bearer token for authentication                |
| `service`        | `str`   | `""`               | Service name attached to every entry           |
| `component`      | `str`   | `""`               | Optional component label                       |
| `host`           | `str`   | `hostname()`       | Host/instance identifier                       |
| `batch_size`     | `int`   | `100`              | Flush when batch reaches this size             |
| `flush_interval` | `float` | `1.0`              | Flush at least this often (seconds)            |
| `buffer_size`    | `int`   | `1024`             | Queue capacity (entries dropped when full)     |
| `timeout`        | `float` | `5.0`              | HTTP request timeout (seconds)                 |

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

This ships as:

```json
{
    "timestamp": "2026-01-15T10:30:00+00:00",
    "level": "INFO",
    "msg": "request handled",
    "service": "my-python-app",
    "host": "prod-1",
    "attrs": {
        "method": "GET",
        "path": "/api/users",
        "status": 200,
        "duration_ms": 42
    }
}
```

## Dual output (stderr + Taillight)

Python's `logging` natively supports multiple handlers on a single logger.
No special fan-out helper needed:

```python
import logging
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
    endpoint="https://taillight.example.com/api/v1/applog/ingest",
    api_key="your-api-key",
    service="my-python-app",
))
```

## Django integration

Add the handler to Django's `LOGGING` dict in `settings.py`:

```python
LOGGING = {
    "version": 1,
    "disable_existing_loggers": False,
    "handlers": {
        "console": {
            "class": "logging.StreamHandler",
        },
        "taillight": {
            "()": "taillight_sdk.TaillightHandler",
            "endpoint": "https://taillight.example.com/api/v1/applog/ingest",
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

## Flask integration

```python
from flask import Flask
from taillight_sdk import TaillightHandler

app = Flask(__name__)

handler = TaillightHandler(
    endpoint="https://taillight.example.com/api/v1/applog/ingest",
    api_key=app.config.get("TAILLIGHT_API_KEY", ""),
    service="my-flask-app",
    component="web",
)
app.logger.addHandler(handler)
```

## Ingest API reference

**Endpoint:** `POST /api/v1/applog/ingest`

**Headers:**
```
Content-Type: application/json
Authorization: Bearer <API_KEY>
```

**Request body:**
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

| Field       | Required | Constraints        |
|-------------|----------|--------------------|
| `timestamp` | yes      | RFC 3339           |
| `level`     | yes      | DEBUG, INFO, WARN, ERROR, FATAL |
| `msg`       | yes      | max 64 KB          |
| `service`   | yes      | max 128 chars      |
| `host`      | yes      | max 256 chars      |
| `component` | no       | max 128 chars      |
| `source`    | no       | max 256 chars      |
| `attrs`     | no       | JSON object, max 64 KB |

**Limits:** max 1000 entries per batch, 5 MB request body.

**Response:** `202 Accepted` with `{"accepted": N}` on success.
