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
                    entry = self._queue.get(timeout=remaining)
                    if entry is None:  # Shutdown sentinel.
                        break
                    batch.append(entry)
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
        # Wake the background thread if it's blocked on queue.get().
        try:
            self._queue.put_nowait(None)
        except queue.Full:
            pass
        self._thread.join(timeout=timeout)
        # Drain anything left in the queue.
        remaining: list[dict] = []
        while not self._queue.empty():
            try:
                entry = self._queue.get_nowait()
                if entry is not None:
                    remaining.append(entry)
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
