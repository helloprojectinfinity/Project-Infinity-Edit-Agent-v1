"""Tests for the local STT HTTP surface.

The whisper model itself is not exercised in tests — it is huge and not
always available offline. We verify that the response handling, queueing,
and validation contract hold for a stubbed model module.
"""

from __future__ import annotations

import asyncio
import sys
import tempfile
import time
from pathlib import Path
from typing import Any

import httpx
import pytest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

import rushes_stt.server as server  # noqa: E402


def _build_fake_mlx_whisper(transcripts: list[dict[str, Any]] | dict[str, Any]):
    """Returns a fake mlx_whisper module that yields the given transcribe responses."""
    queue = [transcripts] if isinstance(transcripts, dict) else list(transcripts)

    class _Fake:
        transcribe_call_count = 0

        def transcribe(self, path, **_kwargs):  # noqa: ANN001
            _Fake.transcribe_call_count += 1
            if not queue:
                raise AssertionError("fake transcribe exhausted")
            return queue.pop(0)

    return _Fake()


def _build_failing_mlx_whisper(exc: BaseException):
    class _Fake:
        def transcribe(self, path, **_kwargs):  # noqa: ANN001
            raise exc

    return _Fake()


@pytest.fixture
def app_factory():
    """Returns a function `make_app(model)` that builds an app with the fake model.

    Every test must rebuild the app so the model closure is fresh.
    """
    def make_app(model: Any):
        return server.create_app(
            model_repo="mlx-community/whisper-large-v3-mlx",
            model_module=model,
        )

    return make_app


async def _post_audio(client: httpx.AsyncClient, contents: bytes, language: str = ""):
    return await client.post(
        "/transcribe",
        files={"audio": ("clip.wav", contents, "audio/wav")},
        data={"language": language},
    )


def _client(app):
    return httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://test")


async def test_transcribe_returns_clean_response_on_success(app_factory):
    app = app_factory(_build_fake_mlx_whisper({
        "text": "你好世界",
        "language": "zh",
        "segments": [
            {"text": "你好世界", "start": 0.0, "end": 1.0, "words": [
                {"word": "你好", "start": 0.0, "end": 0.5, "punctuation": ""},
                {"word": "世界", "start": 0.5, "end": 1.0, "punctuation": ""},
            ]},
        ],
    }))
    async with _client(app) as client:
        response = await _post_audio(client, b"\x00\x00" * 100, language="zh")
    assert response.status_code == 200
    body = response.json()
    assert body["text"] == "你好世界"
    assert body["language"] == "zh"
    assert body["status"] == "succeeded"
    assert body["provider"] == "whisper:whisper-large-v3-mlx:v1"
    assert len(body["segments"]) == 1
    seg = body["segments"][0]
    assert seg["begin_ms"] == 0 and seg["end_ms"] == 1000
    assert seg["words"][0]["begin_ms"] == 0


async def test_transcribe_distinguishes_empty_from_malformed(app_factory):
    app = app_factory(_build_fake_mlx_whisper({"text": "", "language": "en"}))
    async with _client(app) as client:
        response = await _post_audio(client, b"x")
    assert response.status_code == 200
    body = response.json()
    assert body["text"] == ""
    assert body["status"] == "no_speech"
    assert body["segments"] == []


async def test_transcribe_returns_500_when_segments_not_a_list(app_factory):
    app = app_factory(_build_fake_mlx_whisper({
        "text": "hi",
        "language": "en",
        "segments": {"not": "a list"},
    }))
    async with _client(app) as client:
        response = await _post_audio(client, b"x")
    assert response.status_code == 500
    assert "segments" in response.json()["detail"]


async def test_transcribe_returns_500_when_word_timestamp_invalid(app_factory):
    app = app_factory(_build_fake_mlx_whisper({
        "text": "hi",
        "language": "en",
        "segments": [
            {"text": "hi", "start": 1.0, "end": 0.5, "words": []},
        ],
    }))
    async with _client(app) as client:
        response = await _post_audio(client, b"x")
    assert response.status_code == 500
    assert "时间无效" in response.json()["detail"]


async def test_transcribe_returns_500_when_word_field_missing(app_factory):
    app = app_factory(_build_fake_mlx_whisper({
        "text": "hi",
        "language": "en",
        "segments": [{"text": "hi", "start": 0.0, "end": 1.0, "words": [{}]}],
    }))
    async with _client(app) as client:
        response = await _post_audio(client, b"x")
    assert response.status_code == 500


async def test_transcribe_returns_500_when_engine_raises(app_factory):
    app = app_factory(_build_failing_mlx_whisper(RuntimeError("boom")))
    async with _client(app) as client:
        response = await _post_audio(client, b"x")
    assert response.status_code == 500
    assert "boom" in response.json()["detail"]


async def test_transcribe_rejects_oversize_payload(app_factory):
    app = app_factory(_build_fake_mlx_whisper({"text": "ok", "segments": []}))
    async with _client(app) as client:
        response = await _post_audio(client, b"\x00" * (25 * 1024 * 1024 + 1))
    assert response.status_code == 413


async def test_transcribe_rejects_empty_audio(app_factory):
    app = app_factory(_build_fake_mlx_whisper({"text": "ok", "segments": []}))
    async with _client(app) as client:
        response = await _post_audio(client, b"")
    assert response.status_code == 400


async def test_transcribe_serializes_concurrent_requests(app_factory):
    started: list[float] = []
    finished: list[float] = []

    class _Slow:
        def transcribe(self, path, **_kwargs):  # noqa: ANN001
            started.append(time.time())
            time.sleep(0.2)
            finished.append(time.time())
            return {"text": "ok", "language": "en", "segments": []}

    app = app_factory(_Slow())
    async with _client(app) as client:
        results = await asyncio.gather(
            _post_audio(client, b"a"),
            _post_audio(client, b"b"),
            _post_audio(client, b"c"),
        )
    assert all(r.status_code == 200 for r in results)
    # Serialised: each start is after the previous finish.
    assert started[1] >= finished[0] - 0.01
    assert started[2] >= finished[1] - 0.01


async def test_transcribe_uses_secure_temp_file(app_factory):
    captured: dict[str, Any] = {}

    class _Spy:
        def transcribe(self, path, **_kwargs):  # noqa: ANN001
            captured["path"] = path
            try:
                captured["mode"] = oct(Path(path).stat().st_mode & 0o777)
            except OSError:
                captured["mode"] = None
            return {"text": "ok", "language": "en", "segments": []}

    app = app_factory(_Spy())
    async with _client(app) as client:
        response = await _post_audio(client, b"a")
    assert response.status_code == 200
    assert Path(captured["path"]).name.startswith("rushes-stt-")
    assert Path(captured["path"]).parent == Path(tempfile.gettempdir())
    assert captured["mode"] == "0o600"
    assert not Path(captured["path"]).exists(), "temp file should be unlinked after transcribe"


async def test_transcribe_deletes_temp_on_engine_failure(app_factory):
    captured: dict[str, Any] = {}

    class _Fail:
        def transcribe(self, path, **_kwargs):  # noqa: ANN001
            captured["path"] = path
            raise RuntimeError("boom")

    app = app_factory(_Fail())
    async with _client(app) as client:
        response = await _post_audio(client, b"a")
    assert response.status_code == 500
    assert "path" in captured
    assert not Path(captured["path"]).exists(), "temp file should be unlinked on failure too"


async def test_healthz_responds_while_inference_runs(app_factory):
    class _Slow:
        def transcribe(self, path, **_kwargs):  # noqa: ANN001
            time.sleep(0.4)
            return {"text": "ok", "language": "en", "segments": []}

    app = app_factory(_Slow())
    async with _client(app) as client:
        async with client.stream(
            "POST",
            "/transcribe",
            files={"audio": ("x", b"a", "audio/wav")},
            data={"language": ""},
        ) as post_resp:
            post_resp.raise_for_status()
            health = await client.get("/healthz")
            assert health.status_code == 200
            await post_resp.aread()


async def test_readyz_reports_engine_and_weights_state(app_factory):
    app = app_factory(_build_fake_mlx_whisper({"text": "ok", "segments": []}))
    async with _client(app) as client:
        ready = await client.get("/readyz")
        assert ready.status_code == 200
        before = ready.json()
        assert before["engine_ready"] is True
        assert before["weights_ready"] is False
        await _post_audio(client, b"x")
        ready = await client.get("/readyz")
        after = ready.json()
        assert after["weights_ready"] is True


# Startup must not block on the engine. The dev script waits 15s on /healthz
# before declaring the service dead, and importing mlx_whisper alone takes
# ~30s on a cold start. A test that boots the app and pings /healthz in well
# under a second catches any future regression that pulls engine work back
# into startup.
async def test_startup_does_not_load_engine(app_factory):
    fake = _build_fake_mlx_whisper({"text": "ok", "segments": []})

    import time

    started = time.perf_counter()
    app = app_factory(fake)
    async with _client(app) as client:
        # Block on /healthz, the liveness gate the dev script relies on.
        for _ in range(50):
            response = await client.get("/healthz")
            if response.status_code == 200:
                break
            await asyncio.sleep(0.02)
    elapsed = time.perf_counter() - started
    assert response.status_code == 200
    assert response.json()["status"] == "ok"
    assert elapsed < 2.0, f"startup took {elapsed:.2f}s; /healthz should not wait on the engine"
    # The fake's transcribe must never have been called during startup.
    assert fake.transcribe_call_count == 0
