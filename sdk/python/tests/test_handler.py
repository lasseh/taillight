"""Tests for TaillightHandler."""

import json
import logging
import threading
import time
import unittest
from http.server import HTTPServer, BaseHTTPRequestHandler

from taillight_handler import TaillightHandler


class CaptureHandler(BaseHTTPRequestHandler):
    """HTTP handler that captures POST request bodies and headers."""

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)
        self.server.captured.append({
            "body": json.loads(body),
            "headers": dict(self.headers),
        })
        self.send_response(202)
        self.end_headers()
        self.wfile.write(b'{"accepted": 1}')

    def log_message(self, format, *args):
        pass  # Suppress stderr output during tests.


class FailHandler(BaseHTTPRequestHandler):
    """HTTP handler that always returns 500."""

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        self.rfile.read(length)
        self.send_response(500)
        self.end_headers()
        self.wfile.write(b"internal server error")

    def log_message(self, format, *args):
        pass


def start_server(handler_class):
    """Start a local HTTP server and return (server, url)."""
    server = HTTPServer(("127.0.0.1", 0), handler_class)
    server.captured = []
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    port = server.server_address[1]
    return server, f"http://127.0.0.1:{port}/ingest"


class TestTaillightHandler(unittest.TestCase):

    def _make_handler(self, endpoint, **kwargs):
        """Create a handler with fast flush settings for tests."""
        defaults = {
            "service": "test-svc",
            "batch_size": 10,
            "flush_interval": 0.1,
            "buffer_size": 64,
        }
        defaults.update(kwargs)
        return TaillightHandler(endpoint=endpoint, **defaults)

    def test_batch_send(self):
        server, url = start_server(CaptureHandler)
        try:
            handler = self._make_handler(url, batch_size=3, flush_interval=5.0)
            logger = logging.getLogger("test_batch")
            logger.addHandler(handler)
            logger.setLevel(logging.DEBUG)

            for i in range(3):
                logger.info("msg %d", i)

            # Wait for the batch to flush (triggered by batch_size=3).
            time.sleep(0.5)
            handler.shutdown()

            self.assertGreaterEqual(len(server.captured), 1)
            batch = server.captured[0]["body"]
            self.assertIn("logs", batch)
            self.assertEqual(len(batch["logs"]), 3)
            for entry in batch["logs"]:
                self.assertEqual(entry["service"], "test-svc")
                self.assertEqual(entry["level"], "INFO")
                self.assertIn("timestamp", entry)
                self.assertIn("msg", entry)
                self.assertIn("host", entry)
        finally:
            logger.removeHandler(handler)
            server.shutdown()

    def test_flush_on_interval(self):
        server, url = start_server(CaptureHandler)
        try:
            handler = self._make_handler(url, batch_size=100, flush_interval=0.2)
            logger = logging.getLogger("test_interval")
            logger.addHandler(handler)
            logger.setLevel(logging.DEBUG)

            logger.info("single entry")

            # Wait for the interval flush.
            time.sleep(0.5)
            handler.shutdown()

            self.assertGreaterEqual(len(server.captured), 1)
            total = sum(len(c["body"]["logs"]) for c in server.captured)
            self.assertEqual(total, 1)
        finally:
            logger.removeHandler(handler)
            server.shutdown()

    def test_shutdown_flushes(self):
        server, url = start_server(CaptureHandler)
        try:
            handler = self._make_handler(url, batch_size=1000, flush_interval=60.0)
            logger = logging.getLogger("test_shutdown")
            logger.addHandler(handler)
            logger.setLevel(logging.DEBUG)

            for i in range(5):
                logger.info("entry %d", i)

            handler.shutdown()

            total = sum(len(c["body"]["logs"]) for c in server.captured)
            self.assertEqual(total, 5)
        finally:
            logger.removeHandler(handler)
            server.shutdown()

    def test_dropped_on_overflow(self):
        server, url = start_server(CaptureHandler)
        try:
            # Tiny buffer + large batch + long interval = queue fills up.
            handler = self._make_handler(url, buffer_size=2, batch_size=1000, flush_interval=60.0)
            logger = logging.getLogger("test_dropped")
            logger.addHandler(handler)
            logger.setLevel(logging.DEBUG)

            for i in range(20):
                logger.info("flood %d", i)

            handler.shutdown()
            self.assertGreater(handler.dropped, 0)
        finally:
            logger.removeHandler(handler)
            server.shutdown()

    def test_level_mapping(self):
        server, url = start_server(CaptureHandler)
        try:
            handler = self._make_handler(url, batch_size=5, flush_interval=5.0)
            logger = logging.getLogger("test_levels")
            logger.addHandler(handler)
            logger.setLevel(logging.DEBUG)

            logger.debug("d")
            logger.info("i")
            logger.warning("w")
            logger.error("e")
            logger.critical("c")

            time.sleep(0.5)
            handler.shutdown()

            all_logs = []
            for c in server.captured:
                all_logs.extend(c["body"]["logs"])
            levels = [entry["level"] for entry in all_logs]
            self.assertEqual(levels, ["DEBUG", "INFO", "WARN", "ERROR", "FATAL"])
        finally:
            logger.removeHandler(handler)
            server.shutdown()

    def test_extra_fields_in_attrs(self):
        server, url = start_server(CaptureHandler)
        try:
            handler = self._make_handler(url, flush_interval=0.2)
            logger = logging.getLogger("test_extras")
            logger.addHandler(handler)
            logger.setLevel(logging.DEBUG)

            logger.info("req", extra={"method": "GET", "status": 200})

            time.sleep(0.5)
            handler.shutdown()

            entry = server.captured[0]["body"]["logs"][0]
            self.assertIn("attrs", entry)
            self.assertEqual(entry["attrs"]["method"], "GET")
            self.assertEqual(entry["attrs"]["status"], 200)
        finally:
            logger.removeHandler(handler)
            server.shutdown()

    def test_bearer_auth_header(self):
        server, url = start_server(CaptureHandler)
        try:
            handler = self._make_handler(url, api_key="secret-key", flush_interval=0.2)
            logger = logging.getLogger("test_auth")
            logger.addHandler(handler)
            logger.setLevel(logging.DEBUG)

            logger.info("auth test")

            time.sleep(0.5)
            handler.shutdown()

            self.assertGreaterEqual(len(server.captured), 1)
            auth = server.captured[0]["headers"].get("Authorization")
            self.assertEqual(auth, "Bearer secret-key")
        finally:
            logger.removeHandler(handler)
            server.shutdown()

    def test_component_field(self):
        server, url = start_server(CaptureHandler)
        try:
            handler = self._make_handler(url, component="worker", flush_interval=0.2)
            logger = logging.getLogger("test_component")
            logger.addHandler(handler)
            logger.setLevel(logging.DEBUG)

            logger.info("with component")

            time.sleep(0.5)
            handler.shutdown()

            entry = server.captured[0]["body"]["logs"][0]
            self.assertEqual(entry["component"], "worker")
        finally:
            logger.removeHandler(handler)
            server.shutdown()

    def test_source_populated(self):
        server, url = start_server(CaptureHandler)
        try:
            handler = self._make_handler(url, flush_interval=0.2)
            logger = logging.getLogger("test_source")
            logger.addHandler(handler)
            logger.setLevel(logging.DEBUG)

            logger.info("source test")

            time.sleep(0.5)
            handler.shutdown()

            entry = server.captured[0]["body"]["logs"][0]
            self.assertIn("source", entry)
            self.assertIn(":", entry["source"])  # filename:lineno
        finally:
            logger.removeHandler(handler)
            server.shutdown()

    def test_send_failed_counter(self):
        server, url = start_server(FailHandler)
        try:
            handler = self._make_handler(url, flush_interval=0.2)
            logger = logging.getLogger("test_fail")
            logger.addHandler(handler)
            logger.setLevel(logging.DEBUG)

            logger.info("will fail")

            time.sleep(0.5)
            handler.shutdown()

            self.assertGreater(handler.send_failed, 0)
        finally:
            logger.removeHandler(handler)
            server.shutdown()


if __name__ == "__main__":
    unittest.main()
