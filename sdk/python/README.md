# taillight-sdk

Python SDK for shipping logs to [Taillight](https://github.com/lasseh/taillight) — a real-time log viewer.

- Zero external dependencies (stdlib only)
- Background-thread batching with configurable batch size and flush interval
- Drop-on-overflow — never blocks or crashes your application
- Exponential backoff on send failures
- Bearer token authentication

## Install

```sh
pip install taillight-sdk
```

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

# Flush remaining logs on shutdown
handler.shutdown()
```

## Server limits

The Taillight ingest API enforces per-request limits; the handler covers the
ones it can so a single oversized entry never sinks a whole batch:

- **`service` is required** — an empty service raises `ValueError` at
  construction, since every shipped entry would be rejected by the server.
- **Messages are capped at 64 KB** — longer messages are truncated
  client-side (marked with `…[truncated]`) instead of the server rejecting
  the batch.
- **Batches are capped at 1000 entries per request** — keep `batch_size` at
  or below 1000 (the default is 100).

## Documentation

See the full [Python logshipper guide](https://github.com/lasseh/taillight/blob/main/docs/python-logshipper.md) for configuration reference, structured logging, Django/Flask integration, and API details.

## License

MIT — see [LICENSE](LICENSE).
