"""Exercise CI download metadata and archive handling."""

import io
import tarfile
import unittest

from tools.install_ci import DownloadInput, archive_binary


class DownloadInputTests(unittest.TestCase):
    def test_download_url_has_priority_over_url_alias(self) -> None:
        value = DownloadInput.from_metadata({"download": "primary", "url": "alias", "sha256": "a"})

        self.assertEqual(value.url, "primary")

    def test_missing_download_url_is_rejected(self) -> None:
        with self.assertRaisesRegex(ValueError, "download URL is missing"):
            DownloadInput.from_metadata({"sha256": "a"})

    def test_duplicate_archive_binaries_are_rejected(self) -> None:
        stream = io.BytesIO()
        with tarfile.open(fileobj=stream, mode="w:gz") as archive:
            for name in ("helm", "linux-amd64/helm"):
                info = tarfile.TarInfo(name)
                info.size = 1
                archive.addfile(info, io.BytesIO(b"x"))

        with self.assertRaisesRegex(ValueError, "no unique regular binary"):
            archive_binary(stream.getvalue(), "helm")
