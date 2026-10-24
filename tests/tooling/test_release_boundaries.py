"""Exercise release filesystem and publication boundaries without remote writes."""

import hashlib
import io
import json
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from tools.release import oci, publish, storage
from tools.release.artifacts import BuildOptions


class BuildOptionsTests(unittest.TestCase):
    def test_cold_build_rejects_each_external_cache(self) -> None:
        for cache_from, cache_to in ((Path("cache"), None), (None, Path("cache"))):
            with (
                self.subTest(cache_from=cache_from, cache_to=cache_to),
                self.assertRaisesRegex(ValueError, "Cold builds"),
            ):
                BuildOptions(True, cache_from, cache_to)

    def test_parallelism_requires_a_positive_integer(self) -> None:
        for invalid in (0, -1, True):
            with self.subTest(parallelism=invalid), self.assertRaisesRegex(ValueError, "positive"):
                BuildOptions(False, parallelism=invalid)

    def test_cache_paths_cannot_inject_buildx_fields(self) -> None:
        with self.assertRaisesRegex(ValueError, "commas"):
            BuildOptions(False, cache_to=Path("cache,mode=min"))


class ArtifactBoundaryTests(unittest.TestCase):
    def test_blob_checks_both_content_identity_and_size(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            content = b"verified OCI payload"
            identity = hashlib.sha256(content).hexdigest()
            path = root / "blobs/sha256" / identity
            path.parent.mkdir(parents=True)
            path.write_bytes(content)
            descriptor = {"digest": "sha256:" + identity, "size": len(content)}

            self.assertEqual(oci.blob(root, descriptor), content)
            with self.assertRaisesRegex(ValueError, "identity mismatch"):
                oci.blob(root, dict(descriptor, size=len(content) + 1))

            path.write_bytes(b"x" * len(content))

            with self.assertRaisesRegex(ValueError, "identity mismatch"):
                oci.blob(root, descriptor)

    def test_archive_rejects_traversal_and_links_before_extraction(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / "artifact.tar"
            for name, kind in (("../outside", tarfile.REGTYPE), ("link", tarfile.SYMTYPE)):
                with self.subTest(name=name):
                    with tarfile.open(archive, "w") as package:
                        item = tarfile.TarInfo(name)
                        item.type = kind
                        item.linkname = "../outside" if kind == tarfile.SYMTYPE else ""
                        package.addfile(item)

                    with self.assertRaisesRegex(ValueError, "Unsafe artifact archive"):
                        oci.extract(archive, root / "extracted")
                    self.assertFalse((root / "outside").exists())

    def test_immutable_transfer_falls_back_when_hardlink_is_unavailable(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "source"
            target = Path(directory) / "target"
            source.write_bytes(b"immutable archive")

            with patch.object(storage.os, "link", side_effect=OSError("Cross-device link")):
                storage.link_or_copy(str(source), str(target))

            self.assertEqual(target.read_bytes(), source.read_bytes())

    def test_saved_state_replaces_previous_json_without_leaving_temporary_files(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "state.json"
            storage.save(target, {"status": "pending"})
            storage.save(target, {"status": "verified"})

            self.assertEqual(json.loads(target.read_bytes()), {"status": "verified"})
            self.assertEqual(list(root.iterdir()), [target])


class PagesReadbackTests(unittest.TestCase):
    def test_public_archive_and_index_must_both_match(self) -> None:
        content = b"chart bytes"
        expected = hashlib.sha256(content).hexdigest()
        index = {"entries": {"whisper-server": [{"version": "1.0.0", "digest": expected}]}}
        responses = [io.BytesIO(content), io.BytesIO(json.dumps(index).encode())]

        with patch.object(publish.urllib.request, "urlopen", side_effect=responses):
            publish.verify_pages(Path("whisper-server-1.0.0.tgz"), "1.0.0", expected)

    def test_archive_mismatch_stops_before_index_lookup(self) -> None:
        with (
            patch.object(
                publish.urllib.request, "urlopen", return_value=io.BytesIO(b"wrong")
            ) as read,
            self.assertRaisesRegex(ValueError, "Pages read-back mismatch"),
        ):
            publish.verify_pages(Path("chart.tgz"), "1.0.0", "a" * 64)
        read.assert_called_once()

    def test_index_mismatch_cannot_complete_publication(self) -> None:
        content = b"chart bytes"
        index = {"entries": {"whisper-server": [{"version": "0.9.0", "digest": "a" * 64}]}}
        responses = [io.BytesIO(content), io.BytesIO(json.dumps(index).encode())]

        with (
            patch.object(publish.urllib.request, "urlopen", side_effect=responses),
            self.assertRaisesRegex(ValueError, "Pages index read-back mismatch"),
        ):
            publish.verify_pages(Path("chart.tgz"), "1.0.0", hashlib.sha256(content).hexdigest())
