import hashlib
import io
import json
import tempfile
import unittest
from pathlib import Path
from typing import Any
from unittest.mock import patch

from tests.tooling.test_release import NOW
from tools.security import advisories, run
from tools.security.advisories import NativeAdvisoryReview


class NativeAdvisoryTests(unittest.TestCase):
    def setUp(self) -> None:
        temporary = tempfile.TemporaryDirectory(prefix="whisper-native-review-test-")
        self.addCleanup(temporary.cleanup)
        self.path = Path(temporary.name) / "advisories.json"
        self.identity = "sha256:" + "a" * 64
        self.source = b"pinned advisory source fixture"
        self.data: dict[str, Any] = {
            "schema_version": 1,
            "build_input_identity": self.identity,
            "components": [
                {
                    "role": "api",
                    "name": "ffmpeg",
                    "owner": "fixture",
                    "expires": "2026-10-31",
                    "conclusion": "no_applicable_blocking_advisories",
                    "rationale": "Fixture review retains applicable medium findings.",
                    "sources": [
                        {
                            "url": "https://example.invalid/advisory",
                            "sha256": hashlib.sha256(self.source).hexdigest(),
                        }
                    ],
                    "advisories": [
                        {
                            "id": "CVE-2026-12345",
                            "severity": "MEDIUM",
                            "disposition": "applicable",
                            "rationale": "Fixture affected component is enabled.",
                        }
                    ],
                }
            ],
        }

    def coverage(self) -> list[str]:
        self.path.write_text(json.dumps(self.data))

        with patch.object(
            advisories.urllib.request,
            "urlopen",
            side_effect=lambda *args, **kwargs: io.BytesIO(self.source),
        ):
            return run.component_coverage(self.path, "api", self.identity, NOW)

    def test_applicable_medium_finding_is_reported_without_a_coverage_gap(self) -> None:
        self.assertEqual(self.coverage(), [])

        findings = run.component_findings(self.path, "api")

        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0]["advisory"], "CVE-2026-12345")
        self.assertEqual(findings[0]["severity"], "MEDIUM")
        self.assertEqual(findings[0]["status"], "applicable")

    def test_blocking_or_unresolved_advisory_cannot_be_declared_clean(self) -> None:
        entry = self.data["components"][0]["advisories"][0]
        for severity, disposition in (
            ("HIGH", "applicable"),
            ("CRITICAL", "applicable"),
            ("UNKNOWN", "applicable"),
            ("MEDIUM", "unresolved"),
        ):
            with self.subTest(severity=severity, disposition=disposition):
                entry.update(severity=severity, disposition=disposition)

                self.assertEqual(self.coverage(), ["ffmpeg:CVE-2026-12345:blocking-or-unresolved"])

    def test_changed_primary_source_requires_a_new_review(self) -> None:
        self.data["components"][0]["sources"][0]["sha256"] = "b" * 64

        with self.assertRaises(ValueError):
            self.coverage()

    def test_missing_inventory_remains_an_explicit_coverage_gap(self) -> None:
        self.assertEqual(
            run.component_coverage(self.path, "api", self.identity),
            ["custom-native-advisory-reconciliation-required"],
        )
        self.assertEqual(run.component_findings(self.path, "api"), [])

    def test_review_owns_a_snapshot_of_the_selected_inventory(self) -> None:
        self.path.write_text(json.dumps(self.data))
        review = NativeAdvisoryReview.load(self.path, NOW)
        self.path.write_text("{}")

        self.assertEqual(review.findings("api")[0]["advisory"], "CVE-2026-12345")

    def test_expired_or_duplicate_component_inventory_is_not_accepted(self) -> None:
        self.data["components"][0]["expires"] = "2000-01-01"

        with self.assertRaises(ValueError):
            self.coverage()

        self.data["components"][0]["expires"] = "2026-10-31"
        self.data["components"].append(self.data["components"][0])

        with self.assertRaises(ValueError):
            self.coverage()
