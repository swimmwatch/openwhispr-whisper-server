"""Exercise the official OpenAI SDK against an explicitly selected loopback API.

This is a Linux main-thread CLI, not a GPU qualification or performance gate.
Run through the locked contributor environment; only the pinned public JFK WAV
is accepted. Responses and API credentials are never emitted in diagnostics.
"""

import argparse
import hashlib
import json
import logging
import math
import signal
import sys
import time
from collections.abc import Iterator, Sequence
from contextlib import contextmanager
from importlib.metadata import version
from pathlib import Path
from typing import Literal
from urllib.parse import urlsplit

from openai import APIError, APIStatusError, APITimeoutError, DefaultHttpxClient, OpenAI

JFK_SHA256 = "59dfb9a4acb36fe2a2affc14bacbee2920ff435cb13cc314a08c13f66ba7860e"
JFK_BYTES = 352078
ResponseFormat = Literal["json", "text"]


def _base_url(value: str) -> str:
    parsed = urlsplit(value)
    if (
        parsed.scheme != "http"
        or parsed.hostname not in {"127.0.0.1", "localhost", "::1"}
        or parsed.username is not None
        or parsed.password is not None
        or parsed.query
        or parsed.fragment
        or parsed.path.rstrip("/") != "/v1"
    ):
        raise ValueError("An explicit credential-free loopback HTTP /v1 URL is required")
    # Accessing port validates its numeric syntax and supported range.
    _ = parsed.port
    return value.rstrip("/")


def _timeout_handler(_signum: int, _frame: object) -> None:
    raise TimeoutError("Client deadline exceeded")


@contextmanager
def _deadline(seconds: float) -> Iterator[None]:
    """Bound the entire synchronous Linux client call, including trickled bodies."""
    previous_handler = signal.signal(signal.SIGALRM, _timeout_handler)
    previous_timer = signal.getitimer(signal.ITIMER_REAL)
    started = time.monotonic()
    signal.setitimer(signal.ITIMER_REAL, seconds)
    try:
        yield
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous_handler)
        if previous_timer[0] > 0:
            remaining = max(previous_timer[0] - (time.monotonic() - started), 0.000001)
            signal.setitimer(signal.ITIMER_REAL, remaining, previous_timer[1])


def probe(
    base_url: str, audio: Path, response_format: ResponseFormat, timeout_seconds: float
) -> dict[str, str | int | bool]:
    """Return safe measured client evidence; never infer model or GPU correctness."""
    base_url = _base_url(base_url)
    if not math.isfinite(timeout_seconds) or not 0 < timeout_seconds <= 300:
        raise ValueError("Timeout must be finite, positive and at most 300 seconds")
    for name in ("openai", "httpx", "httpcore"):
        logging.getLogger(name).setLevel(logging.CRITICAL + 1)
    with _deadline(timeout_seconds):
        with audio.open("rb") as source:
            payload = source.read(JFK_BYTES + 1)
        if (
            len(payload) != JFK_BYTES
            or audio.suffix.lower() != ".wav"
            or hashlib.sha256(payload).hexdigest() != JFK_SHA256
        ):
            raise ValueError("The pinned public JFK WAV is required")
        with OpenAI(
            api_key="unused-local-test",
            base_url=base_url,
            max_retries=0,
            timeout=timeout_seconds,
            http_client=DefaultHttpxClient(trust_env=False, follow_redirects=False),
        ) as client:
            text: object
            if response_format == "text":
                text = client.audio.transcriptions.create(
                    file=(audio.name, payload, "audio/wav"),
                    model="base",
                    language="en",
                    response_format="text",
                )
            else:
                result = client.audio.transcriptions.create(
                    file=(audio.name, payload, "audio/wav"),
                    model="base",
                    language="en",
                    response_format="json",
                )
                text = getattr(result, "text", None)
        if not isinstance(text, str) or not text.strip():
            raise ValueError("Client did not receive a nonempty transcript")
        return {
            "success": True,
            "sdk": "openai-python",
            "sdk_version": version("openai"),
            "response_format": response_format,
            "fixture_sha256": JFK_SHA256,
            "transcript_characters": len(text),
            "transcript_sha256": hashlib.sha256(text.encode()).hexdigest(),
            "qualification": "not_established",
        }


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", required=True, help="Explicit loopback API URL ending in /v1")
    parser.add_argument("--audio", type=Path, required=True, help="Pinned public JFK WAV")
    parser.add_argument("--response-format", choices=("json", "text"), default="json")
    parser.add_argument("--timeout-seconds", type=float, default=120.0)
    args = parser.parse_args(argv)
    error: dict[str, str | int | bool]
    try:
        result = probe(args.base_url, args.audio, args.response_format, args.timeout_seconds)
    except APIStatusError as failure:
        error = {"code": "sdk_http_error", "status_code": failure.status_code}
    except APITimeoutError, TimeoutError:
        error = {"code": "client_timeout"}
    except APIError:
        error = {"code": "sdk_error"}
    except OSError, ValueError:
        error = {"code": "invalid_input"}
    else:
        print(json.dumps(result))
        return 0
    print(json.dumps(dict(error, success=False, qualification="not_established")), file=sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
