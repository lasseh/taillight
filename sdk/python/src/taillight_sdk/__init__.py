"""Taillight SDK for Python — ship logs to Taillight."""

from taillight_sdk._handler import TaillightHandler

from importlib.metadata import version, PackageNotFoundError

try:
    __version__ = version("taillight-sdk")
except PackageNotFoundError:
    __version__ = "0.0.0-dev"

__all__ = ["TaillightHandler", "__version__"]
