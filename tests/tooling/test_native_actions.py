"""Native CI operations retain project verification and publication boundaries."""

import hashlib
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from tools import install_ci
from tools.release import artifacts
from tools.security import check_secrets


class ActionOutputTests(unittest.TestCase):
    def test_restored_native_tool_must_match_the_reviewed_binary_hash(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            binary = root / "trivy"
            binary.write_bytes(b"reviewed executable")
            versions = {
                "scanners": {
                    "trivy": {"binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest()}
                }
            }
            install_ci.verify_existing(["trivy"], versions, root)

            binary.write_bytes(b"modified restored executable")

            with self.assertRaises(ValueError):
                install_ci.verify_existing(["trivy"], versions, root)

    def test_action_digest_mismatch_cannot_produce_qualified_image_metadata(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)

            with (
                patch.object(artifacts, "extract"),
                patch.object(
                    artifacts,
                    "inspect_layout",
                    return_value={"manifest_digest": "sha256:" + "a" * 64},
                ),
                self.assertRaises(ValueError),
            ):
                artifacts.record_image(output, "1" * 40, False, "sha256:" + "b" * 64)

            self.assertFalse((output / "image.json").exists())

    def test_native_secret_report_is_sanitized_and_missing_or_malformed_reports_fail(self) -> None:
        raw = {
            "SchemaVersion": 2,
            "Results": [
                {"Secrets": [{"RuleID": "fixture", "Match": "PRIVATE", "Code": "PRIVATE"}]}
            ],
        }
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            report = root / "raw.json"
            report.write_text(json.dumps(raw))

            with (
                patch.object(
                    check_secrets, "scanner_identity", return_value=("verified", "0.75.0")
                ),
                patch.object(
                    check_secrets, "enrich_report", side_effect=lambda value, *args: value
                ),
            ):
                safe, status = check_secrets.read_report(report, "trivy", root, root)

                self.assertEqual(status, 1)
                self.assertEqual(safe["finding_count"], 1)
                self.assertNotIn("PRIVATE", json.dumps(safe))

                report.write_text("{}")

                with self.assertRaises(ValueError):
                    check_secrets.read_report(report, "trivy", root, root)

                report.unlink()

                with self.assertRaises(OSError):
                    check_secrets.read_report(report, "trivy", root, root)


if __name__ == "__main__":
    unittest.main()
