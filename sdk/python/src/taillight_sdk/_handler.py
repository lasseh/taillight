"""Taillight applog handler for Python's logging module."""

import atexit
import json
import logging
import queue
import socket
import sys
import threading
import time
from datetime import datetime, timezone
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

# Maps Python log levels to Taillight levels.
_LEVEL_MAP = {
    logging.DEBUG: "DEBUG",
    logging.INFO: "INFO",
    logging.WARNING: "WARN",
    logging.ERROR: "ERROR",
    logging.CRITICAL: "FATAL",
}

# Fields that belong to LogRecord itself, not user-supplied extras.
# Built at import time so the set matches the running Python version.
_RESERVED = frozenset(logging.LogRecord("", 0, "", 0, "", (), None).__dict__)

# Backoff parameters for persistent send failures.
_BACKOFF_INITIAL = 1.0  # seconds
_BACKOFF_MAX = 60.0  # seconds
_BACKOFF_MULTIPLIER = 2.0

# The ingest API's per-entry cap on the msg field (64 KB). Messages longer
# than this are truncated client-side so one oversized entry cannot make the
# server reject the whole batch.
_MAX_MSG_BYTES = 64 * 1024
_TRUNCATION_SUFFIX = "…[truncated]"


def _truncate_msg(msg: str) -> str:
    """Cap msg at the server's 64 KB byte limit, marking the cut with a suffix."""
    raw = msg.encode("utf-8")
    if len(raw) <= _MAX_MSG_BYTES:
        return msg
    cut = raw[: _MAX_MSG_BYTES - len(_TRUNCATION_SUFFIX.encode("utf-8"))]
    # errors="ignore" drops a trailing partial character at the cut point.
    return cut.decode("utf-8", errors="ignore") + _TRUNCATION_SUFFIX


class TaillightHandler(logging.Handler):
    """Batching log handler that ships entries to a Taillight applog ingest endpoint.

    Logs are buffered in a queue and flushed by a background thread, either when
    the batch reaches ``batch_size`` or every ``flush_interval`` seconds.

    If the queue is full, new entries are silently dropped (non-blocking).

    After construction the handler never raises exceptions to the caller and
    never blocks the application. If Taillight is unreachable, entries are
    dropped and a warning is printed to stderr on the first failure.

    ``service`` is required — the ingest API rejects entries without one, so
    an empty service raises ``ValueError`` at construction. Messages longer
    than the server's 64 KB cap are truncated (marked with ``…[truncated]``)
    rather than letting the server reject the whole batch.
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
        if not service:
            # Raised before super().__init__() so a rejected handler is never
            # registered with the logging module.
            raise ValueError(
                "service is required: the ingest API rejects entries with an empty service"
            )
        super().__init__()
        self.endpoint = endpoint
        self.api_key = api_key
        self.service = service
        self.component = component
        self.host = host or socket.gethostname()
        self.batch_size = batch_size
        self.flush_interval = flush_interval
        self.timeout = timeout
        self._queue: queue.Queue[dict | None] = queue.Queue(maxsize=buffer_size)
        self._shutdown_event = threading.Event()
        self._dropped = 0
        self._send_failed = 0
        self._consecutive_failures = 0
        self._backoff_until = 0.0
        self._warned = False
        self._is_shut_down = False
        self._lock = threading.Lock()
        self._thread = threading.Thread(target=self._run, daemon=True, name="taillight-shipper")
        self._thread.start()
        atexit.register(self.shutdown)

    def emit(self, record: logging.LogRecord) -> None:
        """Convert a LogRecord to an ingest entry and enqueue it."""
        try:
            entry: dict[str, object] = {
                "timestamp": datetime.fromtimestamp(record.created, tz=timezone.utc).isoformat(),
                "level": _LEVEL_MAP.get(record.levelno, "INFO"),
                "msg": _truncate_msg(record.getMessage()),
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
        except Exception:
            self.handleError(record)

    def _run(self) -> None:
        """Background loop: drain the queue and flush in batches."""
        batch: list[dict] = []
        while not self._shutdown_event.is_set():
            try:
                # Respect backoff: sleep until backoff expires or shutdown.
                now = time.monotonic()
                with self._lock:
                    wait_until = self._backoff_until
                if wait_until > now:
                    delay = min(wait_until - now, self.flush_interval)
                    self._shutdown_event.wait(timeout=delay)
                    if self._shutdown_event.is_set():
                        break

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
            except Exception:
                # Never let the background thread die.
                batch = []

    def _flush(self, batch: list[dict]) -> None:
        """POST a batch of log entries to the ingest endpoint."""
        try:
            body = json.dumps({"logs": batch}, default=str).encode("utf-8")
        except Exception:
            # If serialization still fails somehow, drop the batch.
            with self._lock:
                self._send_failed += 1
            return

        req = Request(self.endpoint, data=body, method="POST")
        req.add_header("Content-Type", "application/json")
        if self.api_key:
            req.add_header("Authorization", f"Bearer {self.api_key}")
        try:
            with urlopen(req, timeout=self.timeout) as resp:
                resp.read()
            # Success: reset backoff.
            with self._lock:
                self._consecutive_failures = 0
                self._backoff_until = 0.0
        except HTTPError as exc:
            self._handle_send_error(f"HTTP {exc.code}")
        except (URLError, OSError) as exc:
            self._handle_send_error(str(exc))

    def _handle_send_error(self, reason: str) -> None:
        """Record a send failure, apply backoff, and warn once on stderr."""
        with self._lock:
            self._send_failed += 1
            self._consecutive_failures += 1
            delay = min(
                _BACKOFF_INITIAL * (_BACKOFF_MULTIPLIER ** (self._consecutive_failures - 1)),
                _BACKOFF_MAX,
            )
            self._backoff_until = time.monotonic() + delay
            if not self._warned:
                self._warned = True
                print(
                    f"taillight-sdk: first send failure ({reason}), "
                    f"will retry with backoff — check endpoint and API key",
                    file=sys.stderr,
                )

    def close(self) -> None:
        """Called by logging.shutdown(). Flushes and stops the background thread."""
        self.shutdown()
        super().close()

    def shutdown(self, timeout: float = 5.0) -> None:
        """Signal the background thread to stop and drain remaining entries."""
        if self._is_shut_down:
            return
        self._is_shut_down = True
        atexit.unregister(self.shutdown)
        self._shutdown_event.set()
        # Wake the background thread if it's blocked on queue.get() or backoff wait.
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
