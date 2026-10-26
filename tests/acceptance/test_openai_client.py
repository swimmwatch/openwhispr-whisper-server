"""Regression tests for actual SDK transport with generated, non-sensitive audio.

Only the probe's expected fixture digest is replaced in test-owned processes.
The bounded input validation and genuine SDK are unchanged. Real acceptance
continues to require the unpatched public JFK fixture and actual service.
"""

import hashlib
import io
import json
import os
import subprocess
import sys
import tempfile
import threading
import time
import unittest
import wave
from collections.abc import Iterator
from contextlib import contextmanager, redirect_stderr
from email import policy
from email.message import MIMEPart
from email.parser import BytesParser
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch

from tests.acceptance import openai_client

ROOT = Path(__file__).resolve().parents[2]
FIXTURE_BYTES = 352078
TEXT = "PRIVATE_RESPONSE_MARKER: synthetic transport test transcript"
BOOTSTRAP = """
import sys
from unittest.mock import patch
from tests.acceptance import openai_client
with patch.object(openai_client, "JFK_SHA256", sys.argv.pop(1)):
    raise SystemExit(openai_client.main())
"""


def synthetic_wav() -> bytes:
    output = io.BytesIO()
    with wave.open(output, "wb") as audio:
        audio.setnchannels(1)
        audio.setsampwidth(2)
        audio.setframerate(16000)
        audio.writeframes(b"\0" * (FIXTURE_BYTES - 44))
    return output.getvalue()


SYNTHETIC_AUDIO = synthetic_wav()
SYNTHETIC_SHA256 = hashlib.sha256(SYNTHETIC_AUDIO).hexdigest()


class FaultServer(ThreadingHTTPServer):
    def __init__(self, mode: str) -> None:
        super().__init__(("127.0.0.1", 0), FaultHandler)
        self.mode = mode
        self.requests: list[tuple[str, str, bytes, str | None]] = []
        self.stop = threading.Event()


class FaultHandler(BaseHTTPRequestHandler):
    def do_POST(self) -> None:
        server = self.server
        assert isinstance(server, FaultServer)
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        server.requests.append(
            (
                self.path,
                self.headers.get("Content-Type", ""),
                body,
                self.headers.get("Authorization"),
            )
        )
        status = HTTPStatus.OK
        content_type = "application/json"
        response = json.dumps({"text": TEXT}).encode()
        if server.mode == "text":
            content_type, response = "text/plain", TEXT.encode()
        elif server.mode == "rate-limit":
            status = HTTPStatus.TOO_MANY_REQUESTS
            response = json.dumps({"error": {"message": TEXT, "type": "rate_limit_error"}}).encode()
        elif server.mode == "empty":
            response = b'{"text": " "}'
        elif server.mode == "timeout":
            server.stop.wait(2)
        elif server.mode == "redirect":
            status = HTTPStatus.TEMPORARY_REDIRECT
        elif server.mode == "malformed":
            response = b'{"text": 123}'
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(response)))
        self.send_header("Retry-After", "0")
        if server.mode == "redirect":
            self.send_header("Location", f"http://127.0.0.1:{server.server_port}/must-not-follow")
        self.end_headers()
        try:
            if server.mode == "drip":
                for byte in response:
                    self.wfile.write(bytes([byte]))
                    self.wfile.flush()
                    if server.stop.wait(0.05):
                        break
            else:
                self.wfile.write(response)
        except BrokenPipeError, ConnectionResetError:
            pass

    def log_message(self, format: str, *args: object) -> None:
        pass


@contextmanager
def fault_server(mode: str) -> Iterator[FaultServer]:
    server = FaultServer(mode)
    thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.02})
    thread.start()
    try:
        yield server
    finally:
        server.stop.set()
        server.shutdown()
        server.server_close()
        thread.join(timeout=3)
        if thread.is_alive():
            raise RuntimeError("Fault server did not stop")


def invoke(
    server: FaultServer, audio: Path, *extra: str, timeout: float = 15
) -> subprocess.CompletedProcess[str]:
    env = dict(os.environ, OPENAI_LOG="debug", PYTHONUNBUFFERED="1")
    return subprocess.run(
        [
            sys.executable,
            "-c",
            BOOTSTRAP,
            SYNTHETIC_SHA256,
            "--base-url",
            f"http://127.0.0.1:{server.server_port}/v1",
            "--audio",
            str(audio),
            *extra,
        ],
        cwd=ROOT,
        env=env,
        capture_output=True,
        text=True,
        timeout=timeout,
    )


class OfficialOpenAIProbeTests(unittest.TestCase):
    def setUp(self) -> None:
        temporary_root = ROOT / ".local/p07/clients"
        temporary_root.mkdir(parents=True, exist_ok=True)
        temporary = tempfile.TemporaryDirectory(dir=temporary_root)
        self.addCleanup(temporary.cleanup)
        self.work = Path(temporary.name)
        self.audio = self.work / "synthetic.wav"
        self.audio.write_bytes(SYNTHETIC_AUDIO)

    def test_real_sdk_json_and_text_wire_contract_without_transcript_logs(self) -> None:
        for fmt in ("json", "text"):
            with self.subTest(fmt=fmt), fault_server(fmt) as server:
                result = invoke(server, self.audio, "--response-format", fmt)
                self.assertEqual(result.returncode, 0, result.stderr)
                evidence = json.loads(result.stdout)
                self.assertEqual(evidence["sdk_version"], "2.11.0")
                self.assertEqual(evidence["fixture_sha256"], SYNTHETIC_SHA256)
                self.assertEqual(evidence["transcript_characters"], len(TEXT))
                self.assertEqual(
                    evidence["transcript_sha256"], hashlib.sha256(TEXT.encode()).hexdigest()
                )
                self.assertEqual(evidence["qualification"], "not_established")
                self.assertNotIn(TEXT, result.stdout + result.stderr)
                self.assertNotIn("unused-local-test", result.stdout + result.stderr)
                self.assertEqual(len(server.requests), 1)
                endpoint, content_type, body, auth = server.requests[0]
                self.assertEqual(endpoint, "/v1/audio/transcriptions")
                self.assertEqual(auth, "Bearer unused-local-test")
                message = BytesParser(policy=policy.default).parsebytes(
                    f"Content-Type: {content_type}\r\nMIME-Version: 1.0\r\n\r\n".encode() + body
                )
                parts: dict[str, MIMEPart] = {}
                for part in message.iter_parts():
                    name = part.get_param("name", header="content-disposition")
                    assert isinstance(name, str)
                    parts[name] = part
                self.assertEqual(set(parts), {"file", "model", "language", "response_format"})
                self.assertEqual(parts["model"].get_payload(decode=True), b"base")
                self.assertEqual(parts["language"].get_payload(decode=True), b"en")
                self.assertEqual(parts["response_format"].get_payload(decode=True), fmt.encode())
                payload = parts["file"].get_payload(decode=True)
                assert isinstance(payload, bytes)
                self.assertEqual(hashlib.sha256(payload).hexdigest(), SYNTHETIC_SHA256)

    def test_wrong_fixture_hash_never_uploads(self) -> None:
        with fault_server("json") as server:
            invalid = self.work / "invalid.wav"
            invalid.write_bytes(bytes([SYNTHETIC_AUDIO[0] ^ 1]) + SYNTHETIC_AUDIO[1:])
            result = invoke(server, invalid)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(server.requests, [])
            self.assertEqual(json.loads(result.stderr)["code"], "invalid_input")

    def test_oversized_fixture_never_uploads(self) -> None:
        with fault_server("json") as server:
            oversized = self.work / "oversized.wav"
            oversized.write_bytes(SYNTHETIC_AUDIO + b"x")
            result = invoke(server, oversized)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(json.loads(result.stderr)["code"], "invalid_input")
            self.assertEqual(server.requests, [])

    def test_oversized_stream_rejects_without_waiting_for_end_of_input(self) -> None:
        with fault_server("json") as server:
            fifo = self.work / "stream.wav"
            os.mkfifo(fifo)
            stop = threading.Event()

            def write() -> None:
                descriptor = os.open(fifo, os.O_RDWR | os.O_NONBLOCK)
                try:
                    pending = memoryview(SYNTHETIC_AUDIO + b"x")
                    while pending and not stop.is_set():
                        try:
                            pending = pending[os.write(descriptor, pending) :]
                        except BlockingIOError:
                            stop.wait(0.01)
                    stop.wait()
                finally:
                    os.close(descriptor)

            writer = threading.Thread(target=write)
            writer.start()
            try:
                result = invoke(server, fifo, "--timeout-seconds", "0.3", timeout=3)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(json.loads(result.stderr)["code"], "invalid_input")
                self.assertEqual(server.requests, [])
            finally:
                stop.set()
                writer.join(timeout=3)
                self.assertFalse(writer.is_alive(), "Fixture writer did not stop")

    def test_deadline_also_bounds_opening_stalled_fixture(self) -> None:
        with fault_server("json") as server:
            fifo = self.work / "stalled.wav"
            os.mkfifo(fifo)
            result = invoke(server, fifo, "--timeout-seconds", "0.15", timeout=3)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(json.loads(result.stderr)["code"], "client_timeout")
            self.assertEqual(server.requests, [])

    def test_non_main_thread_cli_deadline_failure_is_safe(self) -> None:
        with fault_server("json") as server:
            errors = io.StringIO()
            codes: list[int] = []

            def run() -> None:
                with (
                    redirect_stderr(errors),
                    patch.object(openai_client, "JFK_SHA256", SYNTHETIC_SHA256),
                ):
                    codes.append(
                        openai_client.main(
                            [
                                "--base-url",
                                f"http://127.0.0.1:{server.server_port}/v1",
                                "--audio",
                                str(self.audio),
                            ]
                        )
                    )

            thread = threading.Thread(target=run)
            thread.start()
            thread.join(timeout=3)
            self.assertFalse(thread.is_alive())
            self.assertEqual(codes, [1])
            self.assertEqual(json.loads(errors.getvalue())["code"], "invalid_input")
            self.assertNotIn("Traceback", errors.getvalue())
            self.assertEqual(server.requests, [])

    def test_actual_sdk_does_not_retry_rate_limit(self) -> None:
        with fault_server("rate-limit") as server:
            result = invoke(server, self.audio)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(len(server.requests), 1)
            self.assertEqual(json.loads(result.stderr)["status_code"], HTTPStatus.TOO_MANY_REQUESTS)
            self.assertNotIn(TEXT, result.stdout + result.stderr)
            self.assertNotIn("unused-local-test", result.stdout + result.stderr)

    def test_actual_sdk_timeout_is_bounded_and_does_not_retry(self) -> None:
        with fault_server("timeout") as server:
            before = time.monotonic()
            result = invoke(server, self.audio, "--timeout-seconds", "0.15")
            self.assertNotEqual(result.returncode, 0)
            self.assertLess(time.monotonic() - before, 5)
            self.assertEqual(len(server.requests), 1)
            self.assertNotIn(TEXT, result.stdout + result.stderr)

    def test_trickled_response_cannot_extend_absolute_deadline(self) -> None:
        with fault_server("drip") as server:
            before = time.monotonic()
            result = invoke(server, self.audio, "--timeout-seconds", "0.15")
            self.assertNotEqual(result.returncode, 0)
            self.assertLess(time.monotonic() - before, 5)
            self.assertEqual(len(server.requests), 1)
            self.assertNotIn(TEXT, result.stdout + result.stderr)

    def test_nonlocal_route_shapes_and_credentials_never_upload(self) -> None:
        with fault_server("json") as server:
            for url in (
                f"http://127.0.0.1:{server.server_port}/v2",
                f"http://127.0.0.1:{server.server_port}/v1?token=PRIVATE_CONFIG_MARKER",
                f"http://PRIVATE_CONFIG_MARKER@127.0.0.1:{server.server_port}/v1",
                "http://127.0.0.1:PRIVATE_CONFIG_MARKER/v1",
                "http://127.0.0.1:70000/v1",
            ):
                with self.subTest(url=url):
                    result = invoke(server, self.audio, "--base-url", url)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(server.requests, [])
                    self.assertNotIn("PRIVATE_CONFIG_MARKER", result.stdout + result.stderr)

    def test_redirect_empty_and_malformed_results_fail_safely(self) -> None:
        for mode in ("redirect", "empty", "malformed"):
            with self.subTest(mode=mode), fault_server(mode) as server:
                result = invoke(server, self.audio)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(len(server.requests), 1)
                self.assertNotIn(TEXT, result.stdout + result.stderr)
                self.assertNotIn("Traceback", result.stderr)

    def test_invalid_timeouts_never_upload(self) -> None:
        for value in ("0", "-1", "nan", "inf", "301"):
            with self.subTest(value=value), fault_server("json") as server:
                result = invoke(server, self.audio, "--timeout-seconds", value)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(server.requests, [])


if __name__ == "__main__":
    unittest.main()
