# Python Log Shipper

Ship logs from any Python application to Taillight's applog ingest endpoint
using the built-in `logging` module — no external dependencies required.

This is the Python equivalent of the Go [`logshipper`](../api/pkg/logshipper/README.md)
package. Same batching strategy, same API contract, same drop-on-overflow behavior.

## Requirements

- Python 3.7+
- No external dependencies (stdlib only)

## Quick start

```python
import logging
from taillight_handler import TaillightHandler

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

## TaillightHandler

Copy this class into your project. It has zero external dependencies.

```python
"""Taillight applog handler for Python's logging module."""

import atexit
import json
import logging
import queue
import socket
import threading
import time
from datetime import datetime, timezone
from urllib.request import Request, urlopen
from urllib.error import URLError

# Maps Python log levels to Taillight levels.
_LEVEL_MAP = {
    logging.DEBUG: "DEBUG",
    logging.INFO: "INFO",
    logging.WARNING: "WARN",
    logging.ERROR: "ERROR",
    logging.CRITICAL: "FATAL",
}

# Fields that belong to LogRecord itself, not user-supplied extras.
_RESERVED = frozenset(logging.LogRecord("", 0, "", 0, "", (), None).__dict__)


class TaillightHandler(logging.Handler):
    """Batching log handler that ships entries to a Taillight applog ingest endpoint.

    Logs are buffered in a queue and flushed by a background thread, either when
    the batch reaches ``batch_size`` or every ``flush_interval`` seconds.

    If the queue is full, new entries are silently dropped (non-blocking).
    """

    def __init__(
        self,
        endpoint: str,
        api_key: str = "",
        service: str = "",
        component: str = "",
        host: str = "",
        batch_size: int = 100,
        flush_interval: float = 1.0,
        buffer_size: int = 1024,
        timeout: float = 5.0,
    ):
        super().__init__()
        self.endpoint = endpoint
        self.api_key = api_key
        self.service = service
        self.component = component
        self.host = host or socket.gethostname()
        self.batch_size = batch_size
        self.flush_interval = flush_interval
        self.timeout = timeout
        self._queue: queue.Queue = queue.Queue(maxsize=buffer_size)
        self._shutdown_event = threading.Event()
        self._dropped = 0
        self._send_failed = 0
        self._lock = threading.Lock()
        self._thread = threading.Thread(target=self._run, daemon=True, name="taillight-shipper")
        self._thread.start()
        atexit.register(self.shutdown)

    def emit(self, record: logging.LogRecord) -> None:
        """Convert a LogRecord to an ingest entry and enqueue it."""
        entry: dict = {
            "timestamp": datetime.fromtimestamp(record.created, tz=timezone.utc).isoformat(),
            "level": _LEVEL_MAP.get(record.levelno, "INFO"),
            "msg": record.getMessage(),
            "service": self.service,
            "host": self.host,
        }
        if self.component:
            entry["component"] = self.component
        if record.pathname and record.lineno:
            entry["source"] = f"{record.pathname}:{record.lineno}"

        # Extract user-supplied extra= fields into attrs.
        extras = {k: v for k, v in record.__dict__.items() if k not in _RESERVED}
        if extras:
            entry["attrs"] = extras

        try:
            self._queue.put_nowait(entry)
        except queue.Full:
            with self._lock:
                self._dropped += 1

    def _run(self) -> None:
        """Background loop: drain the queue and flush in batches."""
        batch: list[dict] = []
        while not self._shutdown_event.is_set():
            deadline = time.monotonic() + self.flush_interval
            while len(batch) < self.batch_size:
                remaining = max(0, deadline - time.monotonic())
                try:
                    batch.append(self._queue.get(timeout=remaining))
                except queue.Empty:
                    break
            if batch:
                self._flush(batch)
                batch = []

    def _flush(self, batch: list[dict]) -> None:
        """POST a batch of log entries to the ingest endpoint."""
        body = json.dumps({"logs": batch}).encode("utf-8")
        req = Request(self.endpoint, data=body, method="POST")
        req.add_header("Content-Type", "application/json")
        if self.api_key:
            req.add_header("Authorization", f"Bearer {self.api_key}")
        try:
            with urlopen(req, timeout=self.timeout) as resp:
                resp.read()
        except (URLError, OSError):
            with self._lock:
                self._send_failed += 1

    def shutdown(self, timeout: float = 5.0) -> None:
        """Signal the background thread to stop and drain remaining entries."""
        self._shutdown_event.set()
        self._thread.join(timeout=timeout)
        # Drain anything left in the queue.
        remaining: list[dict] = []
        while not self._queue.empty():
            try:
                remaining.append(self._queue.get_nowait())
            except queue.Empty:
                break
        if remaining:
            self._flush(remaining)

    @property
    def dropped(self) -> int:
        """Number of entries dropped due to a full buffer."""
        with self._lock:
            return self._dropped

    @property
    def send_failed(self) -> int:
        """Number of batch sends that failed."""
        with self._lock:
            return self._send_failed
```

### Config reference

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
            "()": "myproject.taillight_handler.TaillightHandler",
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
from taillight_handler import TaillightHandler

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
