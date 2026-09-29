"""HTTP surface for the local STT service.

Endpoints:
    GET  /healthz   — liveness; never blocked on model or queue.
    GET  /readyz    — readiness; engine_ready=True means the Python
                     inference bindings are importable. weights_ready=True
                     means at least one /transcribe has succeeded. The
                     first real request still pays the weight download.
    POST /transcribe — multipart upload of audio bytes + a language hint.

Concurrency: transcribe calls run inside a single worker thread so the
MLX engine's GPU state is not shared. Requests block until the worker
returns, so /readyz and /healthz stay responsive while inference is in
flight.
"""

from __future__ import annotations

import asyncio
import logging
import os
import tempfile
import threading
import time
from concurrent.futures import Future
from contextlib import asynccontextmanager
from dataclasses import dataclass, field
from typing import Any, Callable

from fastapi import FastAPI, File, Form, HTTPException, UploadFile

logger = logging.getLogger(__name__)

DEFAULT_MODEL_REPO = os.environ.get(
    "RUSHES_LOCAL_STT_MODEL", "mlx-community/whisper-large-v3-mlx"
)
DEFAULT_ALIGNER_VERSION = "v1"
MAX_UPLOAD_BYTES = int(os.environ.get("RUSHES_LOCAL_STT_MAX_UPLOAD_BYTES", 25 * 1024 * 1024))

# Decode knobs mirror the prototype harness; centralise so we can tune without
# hunting through the engine code.
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
    engine_ready: bool = False
    engine_error: str | None = None
    weights_ready: bool = False
    weights_error: str | None = None
    load_started_at: float | None = None
    load_finished_at: float | None = None
    model_module: Any = None
    worker_lock: threading.Lock = field(default_factory=threading.Lock)
    worker_busy: bool = False

    def snapshot(self) -> dict[str, Any]:
        return {
            "engine_ready": self.engine_ready,
            "weights_ready": self.weights_ready,
            "engine_error": self.engine_error,
            "weights_error": self.weights_error,
            "model": DEFAULT_MODEL_REPO,
            "aligner_version": DEFAULT_ALIGNER_VERSION,
            "load_started_at": self.load_started_at,
            "load_finished_at": self.load_finished_at,
            "worker_busy": self.worker_busy,
        }


@dataclass
class InferenceJob:
    audio_path: str
    language: str
    future: "Future[dict[str, Any]]"


def create_app(
    model_repo: str | None = None,
    model_module: Any | None = None,
) -> FastAPI:
    state = ModelState()
    state.model_module = model_module  # None means lazy import on first call.
    repo = model_repo or DEFAULT_MODEL_REPO
    job_queue: "queue.Queue[InferenceJob]" = queue.Queue()

    def ensure_engine() -> None:
        with state.worker_lock:
            if state.engine_ready:
                return
            state.load_started_at = time.time()
            try:
                if state.model_module is None:
                    import mlx_whisper

                    state.model_module = mlx_whisper
                state.engine_ready = True
            except Exception as exc:  # noqa: BLE001
                state.engine_error = repr(exc)
                logger.error("mlx_whisper import failed: %s", state.engine_error)
                raise
            finally:
                state.load_finished_at = time.time()
                logger.info(
                    "whisper engine ready in %.1fs", state.load_finished_at - state.load_started_at
                )

    # If a model module was injected at construction time, mark the engine
    # ready immediately so /readyz reflects the truth without a /transcribe
    # warm-up call. Real callers without an injected module still go through
    # ensure_engine() on first /transcribe.
    if model_module is not None:
        state.engine_ready = True

    def worker_loop() -> None:
        while True:
            job = job_queue.get()
            if job is None:
                job_queue.task_done()
                return
            state.worker_busy = True
            try:
                result = run_inference(state.model_module, repo, job.audio_path, job.language)
            except Exception as exc:  # noqa: BLE001
                logger.exception("inference worker failed")
                job.future.set_exception(exc)
            else:
                job.future.set_result(result)
            finally:
                state.worker_busy = False
                job_queue.task_done()

    # The worker thread is started here so it runs whether or not the lifespan
    # is honored: tests using httpx.ASGITransport do not fire lifespan events,
    # so they would hang on /transcribe otherwise. The lifespan teardown still
    # signals the worker to exit cleanly when it does run.
    threading.Thread(target=worker_loop, name="stt-worker", daemon=True).start()

    @asynccontextmanager
    async def _lifespan(app: FastAPI):
        try:
            yield
        finally:
            job_queue.put(None)

    app = FastAPI(title="Rushes Local STT", version="0.1.0", lifespan=_lifespan)

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
        try:
            ensure_engine()
        except Exception as exc:  # noqa: BLE001
            raise HTTPException(status_code=503, detail=f"本地 STT 引擎未就绪: {exc!r}") from exc

        contents = await audio.read()
        if not contents:
            raise HTTPException(status_code=400, detail="音频为空")
        if len(contents) > MAX_UPLOAD_BYTES:
            raise HTTPException(
                status_code=413,
                detail=f"音频超过 {MAX_UPLOAD_BYTES} 字节",
            )

        try:
            tmp_path = write_temp_audio(contents)
        except OSError as exc:
            raise HTTPException(status_code=500, detail=f"无法创建临时音频: {exc!r}") from exc

        future: "Future[dict[str, Any]]" = Future()
        job_queue.put(InferenceJob(audio_path=tmp_path, language=language, future=future))

        try:
            # Wait outside the event loop so healthz stays responsive.
            result = await asyncio_run_in_executor(future.result)
        except HTTPException:
            safe_remove(tmp_path)
            raise
        except Exception as exc:  # noqa: BLE001
            safe_remove(tmp_path)
            raise HTTPException(status_code=500, detail=f"本地 STT 失败: {exc!r}") from exc

        safe_remove(tmp_path)

        # Promote the engine to weights-ready after a successful pass.
        with state.worker_lock:
            if not state.weights_ready:
                state.weights_ready = True
                state.weights_error = None

        return result

    return app


async def asyncio_run_in_executor(func: Callable[[], Any]) -> Any:
    """Offload a blocking call so the event loop can keep serving /healthz."""
    loop = asyncio.get_running_loop()
    return await loop.run_in_executor(None, func)


def write_temp_audio(contents: bytes) -> str:
    """Persist uploaded audio to a 0600 temp file. Caller must unlink."""
    fd, path = tempfile.mkstemp(prefix="rushes-stt-", suffix=".audio")
    try:
        os.chmod(path, 0o600)
        with os.fdopen(fd, "wb") as handle:
            handle.write(contents)
    except BaseException:
        safe_remove(path)
        raise
    return path


def safe_remove(path: str) -> None:
    try:
        os.remove(path)
    except OSError:
        pass


def run_inference(
    mlx_whisper: Any, repo: str, audio_path: str, language: str
) -> dict[str, Any]:
    options = dict(DECODE_OPTIONS)
    hint = (language or "").strip()
    if hint:
        options["language"] = hint

    try:
        result = mlx_whisper.transcribe(audio_path, path_or_hf_repo=repo, **options)
    except Exception as exc:  # noqa: BLE001
        logger.exception("transcribe failed")
        raise RuntimeError(f"transcribe 调用失败: {exc!r}") from exc

    if not isinstance(result, dict):
        raise RuntimeError("transcribe 返回结构不是字典")

    raw_text = result.get("text")
    if not isinstance(raw_text, str):
        raise RuntimeError("transcribe 返回缺少 text 字段")
    text = raw_text.strip()
    raw_language = result.get("language", "")

    if not text:
        return {
            "text": "",
            "language": hint or (raw_language if isinstance(raw_language, str) else ""),
            "provider": build_provider_id(repo),
            "segments": [],
            "status": "no_speech",
        }

    raw_segments = result.get("segments") or []
    if not isinstance(raw_segments, list):
        raise RuntimeError("transcribe 返回 segments 不是列表")

    segments: list[dict[str, Any]] = []
    for seg in raw_segments:
        segments.append(_normalise_segment(seg))

    return {
        "text": text,
        "language": hint or (raw_language if isinstance(raw_language, str) else ""),
        "provider": build_provider_id(repo),
        "segments": segments,
        "status": "succeeded",
    }


def build_provider_id(repo: str) -> str:
    # The Go side composes the per-request language hint onto this base
    # identity via recognizer.LanguageIdentity(). The Python response must
    # therefore carry the static part only, with the configured aligner
    # version so cache busts land when the alignment model changes.
    name = os.path.basename(repo.rstrip("/"))
    return f"whisper:{name}:{DEFAULT_ALIGNER_VERSION}"


def _normalise_segment(seg: Any) -> dict[str, Any]:
    if not isinstance(seg, dict):
        raise RuntimeError("segment 不是字典")
    seg_text = seg.get("text")
    if not isinstance(seg_text, str):
        raise RuntimeError("segment 缺少 text 字段")
    seg_start = _ms(seg.get("start"))
    seg_end = _ms(seg.get("end"))
    if seg_end <= seg_start:
        raise RuntimeError(f"segment 时间无效: start={seg_start} end={seg_end}")
    raw_words = seg.get("words") or []
    if not isinstance(raw_words, list):
        raise RuntimeError("segment.words 不是列表")
    words: list[dict[str, Any]] = []
    for word in raw_words:
        words.append(_normalise_word(word))
    return {
        "text": seg_text.strip(),
        "begin_ms": seg_start,
        "end_ms": seg_end,
        "words": words,
    }


def _normalise_word(word: Any) -> dict[str, Any]:
    if not isinstance(word, dict):
        raise RuntimeError("word 不是字典")
    text = word.get("word")
    if not isinstance(text, str):
        raise RuntimeError("word 缺少 word 字段")
    start = _ms(word.get("start"))
    end = _ms(word.get("end"))
    if end <= start:
        raise RuntimeError(f"word 时间无效: start={start} end={end} text={text!r}")
    punctuation = word.get("punctuation", "")
    if not isinstance(punctuation, str):
        punctuation = ""
    return {
        "text": text,
        "begin_ms": start,
        "end_ms": end,
        "punctuation": punctuation,
    }


def _ms(value: Any) -> int:
    if value is None:
        raise RuntimeError("缺少时间戳字段")
    if isinstance(value, bool):  # bool is an int subclass; reject
        raise RuntimeError(f"时间戳不是数值: {value!r}")
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        return int(round(value * 1000))
    raise RuntimeError(f"时间戳类型不支持: {type(value).__name__}")


import queue  # noqa: E402  (placed after dataclass to keep the section above readable)


app = create_app()
