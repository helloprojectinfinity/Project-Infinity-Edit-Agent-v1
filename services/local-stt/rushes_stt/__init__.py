"""Rushes local STT service — wraps mlx-whisper behind a small HTTP API.

The Go side talks to this service at /healthz, /readyz, and /transcribe. Audio
is uploaded as multipart bytes so the Go side controls the temp file lifecycle.
"""

from .server import app, create_app

__all__ = ["app", "create_app"]
