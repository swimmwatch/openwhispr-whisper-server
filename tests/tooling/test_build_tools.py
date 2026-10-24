import hashlib
import io
import json
import tempfile
import threading
import unittest
from datetime import timedelta
from pathlib import Path
from typing import Any
from unittest.mock import patch

from tests.tooling.test_release import NOW
from tools import install_ci
from tools.release import artifacts
from tools.security.run import validate_database_metadata


class DownloadCacheTests(unittest.TestCase):
    def test_warm_install_avoids_network_and_still_checks_archive_hash(self) -> None:
        payload = b"#!/bin/sh\nexit 0\n"
        metadata = {
            "url": "https://example.invalid/tool",
            "sha256": hashlib.sha256(payload).hexdigest(),
        }
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "bin").mkdir()
            cache = root / "cache"
            with patch.object(
                install_ci.urllib.request, "urlopen", return_value=io.BytesIO(payload)
            ) as network:
                install_ci.install("fixture", metadata, root / "bin", cache=cache)

                self.assertEqual(network.call_count, 1)

            with patch.object(install_ci.urllib.request, "urlopen") as network:
                install_ci.install("fixture", metadata, root / "bin", cache=cache)

                network.assert_not_called()

            self.assertEqual((root / "bin/fixture").read_bytes(), payload)

            (cache / metadata["sha256"]).write_bytes(b"corrupted cached input")

            with self.assertRaises(ValueError):
                install_ci.install("fixture", metadata, root / "bin", cache=cache)

    def test_independent_installations_run_concurrently_and_failures_propagate(self) -> None:
        barrier = threading.Barrier(2)
        completed = []

        def install(name: str, *args: Any) -> None:
            barrier.wait(timeout=5)
            completed.append(name)

        with patch.object(install_ci, "install_tool", side_effect=install):
            install_ci.install_tools(["first", "second"], {}, Path("/fixture"), None, jobs=2)

        self.assertEqual(set(completed), {"first", "second"})
        with (
            patch.object(
                install_ci, "install_tool", side_effect=ValueError("Injected invalid checksum")
            ),
            self.assertRaises(ValueError),
        ):
            install_ci.install_tools(["first"], {}, Path("/fixture"), None, jobs=2)


class DatabaseCacheTests(unittest.TestCase):
    def test_cached_database_requires_current_schema_recent_content_and_future_expiry(self) -> None:
        metadata = {
            "VulnerabilityDB": {
                "Version": 2,
                "UpdatedAt": (NOW - timedelta(hours=2)).isoformat(),
                "NextUpdate": (NOW + timedelta(hours=4)).isoformat(),
            }
        }
        validate_database_metadata(metadata, NOW)

        for field, value in (
            ("Version", 1),
            ("UpdatedAt", (NOW - timedelta(days=2)).isoformat()),
            ("NextUpdate", (NOW - timedelta(seconds=1)).isoformat()),
        ):
            with self.subTest(field=field):
                changed = {"VulnerabilityDB": dict(metadata["VulnerabilityDB"], **{field: value})}

                with self.assertRaises(ValueError):
                    validate_database_metadata(changed, NOW)

    def test_successful_scanner_process_cannot_hide_missing_database_metadata(self) -> None:
        with self.assertRaises(ValueError):
            validate_database_metadata({"Version": "0.75.0"}, NOW)


class BuildCacheTests(unittest.TestCase):
    def setUp(self) -> None:
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        (self.root / "tools").mkdir()
        (self.root / "containers").mkdir()
        versions = {
            "ci": {"buildkit": {"image": "fixture:builder", "sbom_generator": "fixture:scanner"}}
        }
        (self.root / "tools/versions.yaml").write_text(json.dumps(versions))
        (self.root / "containers/build-inputs.json").write_text("{}")
        self.calls: list[list[str]] = []

    def build(self, cold: bool, **options: Any) -> None:
        with (
            patch.object(artifacts, "ROOT", self.root),
            patch.object(artifacts, "command", side_effect=lambda args: self.calls.append(args)),
            patch.object(artifacts, "extract"),
            patch.object(
                artifacts, "inspect_layout", return_value={"manifest_digest": "sha256:" + "a" * 64}
            ),
            patch.object(artifacts, "runtime_identity", return_value="sha256:" + "b" * 64),
        ):
            artifacts.build("api", self.root / "output", "1" * 40, cold, **options)

    def test_cold_build_rejects_imported_or_exported_caches_before_creating_builder(self) -> None:
        for option in ("cache_from", "cache_to"):
            with self.subTest(option=option), self.assertRaises(ValueError):
                self.build(True, **{option: self.root / "cache"})

        self.assertEqual(self.calls, [])

    def test_warm_build_uses_local_layer_cache_and_bounded_builder_parallelism(self) -> None:
        cache = self.root / "cache"
        cache.mkdir()
        (cache / "index.json").write_text("{}")

        self.build(False, cache_from=cache, cache_to=self.root / "next", parallelism=3)
        build = next(call for call in self.calls if call[:2] == ["buildx", "build"])

        self.assertIn("type=local,src=" + str(cache), build)
        self.assertIn(
            "type=local,dest="
            + str(self.root / "next")
            + ",mode=max,compression=zstd,compression-level=3",
            build,
        )
        self.assertNotIn("--no-cache", build)
        self.assertEqual(
            (self.root / "output/buildkitd.toml").read_text(),
            "[worker.oci]\n  max-parallelism = 3\n",
        )
        self.assertEqual(self.calls[-1][:2], ["buildx", "rm"])

    def test_cold_build_remains_uncached(self) -> None:
        self.build(True)
        build = next(call for call in self.calls if call[:2] == ["buildx", "build"])

        self.assertIn("--no-cache", build)
        self.assertNotIn("--cache-from", build)
        self.assertNotIn("--cache-to", build)
