"""Taillight logging handler for Python."""

from taillight_handler._handler import TaillightHandler

from importlib.metadata import version, PackageNotFoundError

try:
    __version__ = version("taillight-handler")
except PackageNotFoundError:
    __version__ = "0.0.0-dev"

__all__ = ["TaillightHandler", "__version__"]
