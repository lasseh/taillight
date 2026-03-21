# taillight-handler

Python logging handler that ships logs to [Taillight](https://github.com/lasseh/taillight) — a real-time log viewer.

Zero external dependencies. Batches entries in a background thread and ships them via HTTP POST.

## Install

```sh
pip install taillight-handler
```

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

# On shutdown
handler.shutdown()
```

## Documentation

See the full [Python logshipper guide](https://github.com/lasseh/taillight/blob/main/docs/python-logshipper.md) for configuration reference, structured logging, Django/Flask integration, and API details.

## License

GPL-3.0 — see [LICENSE](LICENSE).
