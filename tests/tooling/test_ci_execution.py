import contextlib
import hashlib
import json
import os
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from concurrent.futures import ThreadPoolExecutor
from datetime import UTC
from pathlib import Path
from typing import Any

from tools.security import check


class SecurityExecutionTests(unittest.TestCase):
    def test_sigterm_stops_the_owned_scanner_group(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pid_path = root / "child.pid"
            child = (
                "import os, pathlib, time\n"
                f"pathlib.Path({str(pid_path)!r}).write_text(str(os.getpid()))\n"
                "time.sleep(60)\n"
            )
            wrapper = (
                "import sys\nfrom tools.security.run import execute\n"
                f"execute([sys.executable, '-c', {child!r}], timeout=60)\n"
            )
            process = subprocess.Popen(
                [sys.executable, "-c", wrapper], stdout=subprocess.PIPE, stderr=subprocess.PIPE
            )
            try:
                deadline = time.monotonic() + 5
                while not pid_path.exists():
                    if time.monotonic() >= deadline:
                        self.fail("Scanner did not start before cancellation")
                    time.sleep(0.01)
                process.terminate()
                process.communicate(timeout=5)
                self.assertNotEqual(process.returncode, 0)
                proc = Path(f"/proc/{int(pid_path.read_text())}/stat")
                self.assertTrue(not proc.exists() or proc.read_text().split()[2] == "Z")
            finally:
                process.kill()
                process.communicate(timeout=5)
                if pid_path.exists():
                    with contextlib.suppress(ProcessLookupError):
                        os.kill(int(pid_path.read_text()), signal.SIGKILL)

    def test_scanner_worker_and_failed_spawn_preserve_signal_ownership(self) -> None:
        from tools.security.run import execute

        previous = signal.getsignal(signal.SIGTERM)
        with ThreadPoolExecutor(max_workers=1) as executor:
            self.assertEqual(
                executor.submit(execute, [sys.executable, "-c", "print('{}')"]).result(),
                (0, "{}\n"),
            )
        with (
            tempfile.TemporaryDirectory() as directory,
            self.assertRaises(FileNotFoundError),
        ):
            execute([str(Path(directory) / "missing-scanner")])
        self.assertEqual(signal.getsignal(signal.SIGTERM), previous)

    def test_scanner_descendants_stop_on_timeout_and_normal_exit(self) -> None:
        from tools.security.run import execute

        for wait_for_timeout in (True, False):
            with self.subTest(timeout=wait_for_timeout), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                pid_path = root / "child.pid"
                command = [
                    sys.executable,
                    "-c",
                    "import pathlib, subprocess, sys, time\n"
                    "child = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(60)'])\n"
                    f"pathlib.Path({str(pid_path)!r}).write_text(str(child.pid))\n"
                    + ("time.sleep(60)\n" if wait_for_timeout else "print('{}')\n"),
                ]
                try:
                    if wait_for_timeout:
                        with self.assertRaises(subprocess.TimeoutExpired):
                            execute(command, root, timeout=1)
                    else:
                        self.assertEqual(execute(command, root, timeout=1)[0], 0)
                    pid = int(pid_path.read_text())
                    proc = Path(f"/proc/{pid}/stat")
                    deadline = time.monotonic() + 1
                    while proc.exists() and proc.read_text().split()[2] != "Z":
                        if time.monotonic() >= deadline:
                            self.fail("Scanner descendant survived owned process cleanup")
                        time.sleep(0.01)
                finally:
                    if pid_path.exists():
                        with contextlib.suppress(ProcessLookupError):
                            os.kill(int(pid_path.read_text()), signal.SIGKILL)

    def test_native_applicable_findings_are_retained_without_waiving_blockers(self) -> None:
        import io
        from datetime import datetime
        from unittest.mock import patch

        from tools.security.run import component_coverage, component_findings

        content = b"public-advisory-fixture"
        record: dict[str, Any] = {
            "role": "api",
            "name": "ffmpeg",
            "owner": "fixture",
            "expires": "2030-01-01",
            "rationale": "Exact native component review fixture",
            "sources": [
                {
                    "url": "https://example.invalid/advisory",
                    "sha256": hashlib.sha256(content).hexdigest(),
                }
            ],
            "conclusion": "no_applicable_blocking_advisories",
            "advisories": [
                {
                    "id": "CVE-2026-12345",
                    "severity": "MEDIUM",
                    "disposition": "applicable",
                    "rationale": "Retain the actual finding",
                }
            ],
        }
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "review.json"
            data: dict[str, Any] = {
                "schema_version": 1,
                "build_input_identity": "sha256:" + "a" * 64,
                "components": [record],
            }
            path.write_text(json.dumps(data))

            with patch(
                "urllib.request.urlopen", side_effect=lambda *args, **kwargs: io.BytesIO(content)
            ):
                self.assertEqual(
                    component_coverage(
                        path,
                        "api",
                        data["build_input_identity"],
                        datetime(2026, 10, 24, tzinfo=UTC),
                    ),
                    [],
                )

                findings = component_findings(path, "api")

                self.assertEqual(len(findings), 1)
                self.assertEqual(findings[0]["severity"], "MEDIUM")

                record["advisories"][0]["severity"] = "HIGH"
                path.write_text(json.dumps(data))

                self.assertTrue(
                    component_coverage(
                        path,
                        "api",
                        data["build_input_identity"],
                        datetime(2026, 10, 24, tzinfo=UTC),
                    )
                )

    def test_missing_or_unresolved_native_evidence_cannot_be_clean(self) -> None:
        from tools.security.run import component_coverage

        self.assertTrue(component_coverage(None, "api", "sha256:" + "a" * 64))

        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "advisories.json"
            path.write_text(
                json.dumps({"schema_version": 1, "build_input_identity": "sha256:" + "c" * 64})
            )

            with self.assertRaises(ValueError):
                component_coverage(path, "api", "sha256:" + "a" * 64)

    def test_json_stream_retains_exit_zero_called_finding(self) -> None:
        from tools.security.run import json_stream

        raw = json_stream(
            '{"config":{"scanner_name":"govulncheck","protocol_version":"v1.0.0"}}\n'
            '{"finding":{"osv":"GO-2026-1234","trace":[{"module":"fixture","function":"Unsafe"}]}}'
        )

        self.assertEqual(check.evaluate("govulncheck", raw, 0)["blocking_count"], 1)
        with self.assertRaises(ValueError):
            json_stream('{"finding":{}}\ntruncated')

    def test_private_runner_never_returns_stderr_or_secret_text(self) -> None:
        import sys

        from tools.security.run import execute

        marker = "PRIVATE_FIXTURE_MARKER"
        result = execute(
            [
                sys.executable,
                "-c",
                "import sys; print('{\"safe\":1}'); sys.stderr.write(sys.argv[1])",
                marker,
            ]
        )

        self.assertEqual(result, (0, '{"safe":1}\n'))
        with self.assertRaises(ValueError) as caught:
            execute(
                [
                    sys.executable,
                    "-c",
                    "import sys; sys.stderr.write(sys.argv[1]); sys.exit(2)",
                    marker,
                ]
            )

        self.assertNotIn(marker, str(caught.exception))

    def test_helm_installer_accepts_actual_manifest_layout(self) -> None:
        import io
        import tarfile

        from tools.install_ci import archive_binary

        stream = io.BytesIO()
        with tarfile.open(fileobj=stream, mode="w:gz") as archive:
            info = tarfile.TarInfo("linux-amd64/helm")
            info.size = 4
            archive.addfile(info, io.BytesIO(b"helm"))

        self.assertEqual(archive_binary(stream.getvalue(), "helm"), b"helm")


class OCIArtifactTests(unittest.TestCase):
    def test_unnamed_buildkit_export_is_bound_by_its_attestation_reference(self) -> None:
        from tools.release.artifacts import inspect_layout

        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            (root / "blobs/sha256").mkdir(parents=True)

            def store(value: dict[str, Any]) -> dict[str, Any]:
                raw = json.dumps(value).encode()
                identity = hashlib.sha256(raw).hexdigest()
                (root / "blobs/sha256" / identity).write_bytes(raw)
                return {"digest": "sha256:" + identity, "size": len(raw)}

            config = store({"config": {"Labels": {"org.opencontainers.image.revision": "1" * 40}}})
            runtime = store({"config": config, "layers": []})
            runtime["platform"] = {"os": "linux", "architecture": "amd64"}
            layers = [
                store({"subject": [], "predicateType": predicate})
                for predicate in ("https://spdx.dev/Document", "https://slsa.dev/provenance/v1")
            ]
            attestation = store({"config": store({}), "layers": layers})
            attestation["annotations"] = {
                "vnd.docker.reference.type": "attestation-manifest",
                "vnd.docker.reference.digest": runtime["digest"],
            }

            def index() -> None:
                selected = store({"manifests": [runtime, attestation]})
                (root / "index.json").write_text(json.dumps({"manifests": [selected]}))

            index()

            self.assertEqual(inspect_layout(root, "1" * 40)["platform_digest"], runtime["digest"])

            attestation["annotations"]["vnd.docker.reference.digest"] = "sha256:" + "0" * 64
            index()

            with self.assertRaises(ValueError):
                inspect_layout(root, "1" * 40)

    def test_failed_and_skipped_jobs_fail_required_aggregate(self) -> None:
        from tools.release.artifacts import required_jobs

        required_jobs({"source": {"result": "success"}}, {"source"})
        for state in ("failure", "skipped", "cancelled"):
            with self.assertRaises(ValueError):
                required_jobs({"source": {"result": state}}, {"source"})

        with self.assertRaises(ValueError):
            required_jobs({}, {"source"})


if __name__ == "__main__":
    unittest.main()
