"""HTTP surface for the local STT service.

Endpoints:
    GET  /healthz  — liveness; never blocked on the model.
    GET  /readyz   — readiness; 200 once the model is loaded.
    POST /transcribe — multipart upload of audio bytes + a language hint.

The model is loaded lazily on first /transcribe request and held in process
memory so subsequent requests reuse the loaded weights.
"""

from __future__ import annotations

import logging
import os
import threading
import time
from dataclasses import dataclass
from typing import Any

from fastapi import FastAPI, File, Form, HTTPException, UploadFile

logger = logging.getLogger(__name__)

DEFAULT_MODEL_REPO = os.environ.get(
    "RUSHES_LOCAL_STT_MODEL", "mlx-community/whisper-large-v3-mlx"
)
DEFAULT_ALIGNER_VERSION = "v1"
MAX_UPLOAD_BYTES = int(os.environ.get("RUSHES_LOCAL_STT_MAX_UPLOAD_BYTES", 25 * 1024 * 1024))

# Decode knobs that mirror the prototype harness; centralised so we can tune
# without hunting through the engine code.
DECODE_OPTIONS: dict[str, Any] = {
    "fp16": True,
    "word_timestamps": True,
    "condition_on_previous_text": False,
    "compression_ratio_threshold": 2.4,
    "logprob_threshold": -1.0,
    "no_speech_threshold": 0.6,
}


@dataclass
class ModelState:
    loaded: bool = False
    loading: bool = False
    error: str | None = None
    model: Any = None
    load_started_at: float | None = None
    load_finished_at: float | None = None

    def snapshot(self) -> dict[str, Any]:
        return {
            "loaded": self.loaded,
            "loading": self.loading,
            "error": self.error,
            "model": DEFAULT_MODEL_REPO,
            "aligner_version": DEFAULT_ALIGNER_VERSION,
            "load_started_at": self.load_started_at,
            "load_finished_at": self.load_finished_at,
        }


def create_app(model_repo: str | None = None) -> FastAPI:
    state = ModelState()
    state_lock = threading.Lock()
    repo = model_repo or DEFAULT_MODEL_REPO

    def ensure_loaded() -> Any:
        with state_lock:
            if state.loaded:
                return state.model
            if state.loading:
                # Another thread is loading; busy-wait briefly then re-check.
                pass
            elif state.error is None:
                state.loading = True
                state.load_started_at = time.time()
                try:
                    logger.info("loading whisper model %s", repo)
                    import mlx_whisper

                    state.model = mlx_whisper
                    state.loaded = True
                    state.load_finished_at = time.time()
                    logger.info("whisper model loaded in %.1fs", state.load_finished_at - state.load_started_at)
                except Exception as exc:  # noqa: BLE001
                    state.error = repr(exc)
                    logger.error("whisper model load failed: %s", state.error)
                finally:
                    state.loading = False
            if not state.loaded:
                raise RuntimeError(state.error or "模型未就绪")
        # Re-check outside the lock to avoid re-entering while holding it.
        if not state.loaded:
            raise RuntimeError(state.error or "模型未就绪")
        return state.model

    app = FastAPI(title="Rushes Local STT", version="0.1.0")

    @app.get("/healthz")
    def healthz() -> dict[str, Any]:
        return {"status": "ok"}

    @app.get("/readyz")
    def readyz() -> dict[str, Any]:
        return state.snapshot()

    @app.post("/transcribe")
    async def transcribe(
        audio: UploadFile = File(...),
        language: str = Form(""),
    ) -> dict[str, Any]:
        contents = await audio.read()
        if not contents:
            raise HTTPException(status_code=400, detail="音频为空")
        if len(contents) > MAX_UPLOAD_BYTES:
            raise HTTPException(
                status_code=413,
                detail=f"音频超过 {MAX_UPLOAD_BYTES} 字节",
            )

        try:
            mlx_whisper = ensure_loaded()
        except RuntimeError as exc:
            raise HTTPException(status_code=503, detail=str(exc)) from exc

        options = dict(DECODE_OPTIONS)
        hint = (language or "").strip()
        if hint:
            options["language"] = hint

        tmp_path = f"/tmp/rushes-stt-{int(time.time() * 1000)}-{os.getpid()}.bin"
        with open(tmp_path, "wb") as handle:
            handle.write(contents)
        try:
            try:
                result = mlx_whisper.transcribe(
                    tmp_path,
                    path_or_hf_repo=repo,
                    **options,
                )
            except Exception as exc:  # noqa: BLE001
                logger.exception("transcribe failed")
                raise HTTPException(status_code=500, detail=f"本地 STT 失败: {exc!r}") from exc
        finally:
            try:
                os.remove(tmp_path)
            except OSError:
                pass

        text = (result.get("text") or "").strip()
        if not text:
            # Empty text → caller maps to ErrSpeechNoWords.
            return {
                "text": "",
                "language": hint or result.get("language", ""),
                "provider": f"whisper:{os.path.basename(repo)}",
                "segments": [],
            }

        segments: list[dict[str, Any]] = []
        for seg in result.get("segments") or []:
            words: list[dict[str, Any]] = []
            for w in seg.get("words") or []:
                words.append(
                    {
                        "text": w.get("word", ""),
                        "begin_ms": int(round((w.get("start") or 0.0) * 1000)),
                        "end_ms": int(round((w.get("end") or 0.0) * 1000)),
                        "punctuation": "",
                    }
                )
            segments.append(
                {
                    "text": (seg.get("text") or "").strip(),
                    "begin_ms": int(round((seg.get("start") or 0.0) * 1000)),
                    "end_ms": int(round((seg.get("end") or 0.0) * 1000)),
                    "words": words,
                }
            )

        return {
            "text": text,
            "language": hint or result.get("language", ""),
            "provider": f"whisper:{os.path.basename(repo)}",
            "segments": segments,
        }

    return app


app = create_app()
