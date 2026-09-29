"""Entry point: `python -m rushes_stt` or `uvicorn rushes_stt:app`."""

from __future__ import annotations

import logging
import os

import uvicorn


def main() -> None:
    host = os.environ.get("RUSHES_LOCAL_STT_HOST", "127.0.0.1")
    port = int(os.environ.get("RUSHES_LOCAL_STT_PORT", "8013"))
    log_level = os.environ.get("RUSHES_LOCAL_STT_LOG_LEVEL", "info")
    logging.basicConfig(
        level=log_level.upper(),
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    )
    uvicorn.run("rushes_stt:app", host=host, port=port, log_level=log_level.lower())


if __name__ == "__main__":
    main()
