import copy
import json
import tempfile
import unittest
from pathlib import Path
from typing import Any
from unittest.mock import MagicMock, patch

from tools.release import prepare


class ReleaseSelectionTests(unittest.TestCase):
    def setUp(self) -> None:
        temporary = tempfile.TemporaryDirectory(prefix="whisper-selection-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.current = self.root / "current"
        self.candidate = self.root / "accepted"
        self.output = self.root / "selected"
        self.data: dict[str, Any] = {
            "runtime_input_identity": "sha256:" + "a" * 64,
            "build_input_identity": "sha256:" + "b" * 64,
            "candidate_source_sha": "1" * 40,
            "release_source_sha": "2" * 40,
            "image_manifest_digests": {
                "api": "sha256:" + "c" * 64,
                "backend": "sha256:" + "d" * 64,
            },
            "chart_archive_sha256": "e" * 64,
            "chart_version": "0.1.0",
            "release_version": "0.1.0",
        }
        self.data["accepted_image_manifest_digests"] = dict(self.data["image_manifest_digests"])

        chart = self.current / "chart/extracted/whisper-server"
        chart.mkdir(parents=True)
        (chart / "Chart.yaml").write_text("version: 0.1.0\nappVersion: 0.1.0\n")
        (self.current / "chart/marker").write_text("current stable chart")

        for role, identity in self.data["image_manifest_digests"].items():
            image = self.candidate / role
            (image / "layout").mkdir(parents=True)
            (image / "layout/marker").write_text("accepted " + role)
            (image / "image.json").write_text(
                json.dumps(
                    {
                        "manifest_digest": identity,
                        "source_sha": self.data["candidate_source_sha"],
                        "runtime_input_identity": self.data["runtime_input_identity"],
                        "build_input_identity": self.data["build_input_identity"],
                    }
                )
            )

        self.chart = {
            "source_sha": self.data["release_source_sha"],
            "chart_archive_sha256": self.data["chart_archive_sha256"],
        }

    def select(
        self,
        data: dict[str, Any] | None = None,
        chart: dict[str, Any] | None = None,
        manifest: str | None = None,
    ) -> MagicMock:
        def inspect(layout: Path, source: str) -> dict[str, str]:
            self.assertEqual(layout.parent.parent, self.candidate)
            self.assertEqual(source, self.data["candidate_source_sha"])

            return {
                "manifest_digest": manifest
                or self.data["image_manifest_digests"][layout.parent.name]
            }

        with (
            patch.object(
                prepare, "runtime_identity", return_value=self.data["runtime_input_identity"]
            ),
            patch.object(prepare, "verify_chart", return_value=chart or self.chart),
            patch.object(prepare, "inspect_layout", side_effect=inspect),
            patch.object(prepare, "equivalent") as equivalent,
        ):
            prepare.select(
                data or self.data,
                "stable",
                self.current,
                self.candidate,
                self.output,
                Path("/fixture/helm"),
                self.root,
            )

        return equivalent

    def test_stable_selects_accepted_images_and_current_chart(self) -> None:
        equivalent = self.select()

        for role in ("api", "backend"):
            self.assertEqual((self.output / role / "layout/marker").read_text(), "accepted " + role)

        self.assertEqual((self.output / "chart/marker").read_text(), "current stable chart")
        equivalent.assert_called_once_with(
            self.candidate / "chart",
            self.current / "chart",
            Path("/fixture/helm"),
            self.data["image_manifest_digests"],
        )

    def test_runtime_or_chart_source_drift_prevents_selection(self) -> None:
        changed = copy.deepcopy(self.data)
        changed["runtime_input_identity"] = "sha256:" + "f" * 64

        with self.assertRaises(ValueError):
            self.select(data=changed)

        for field in ("source_sha", "chart_archive_sha256"):
            with self.subTest(field=field):
                chart = dict(self.chart)
                chart[field] = "unverified"

                with self.assertRaises(ValueError):
                    self.select(chart=chart)

        self.assertFalse(self.output.exists())

    def test_image_metadata_or_actual_manifest_drift_prevents_selection(self) -> None:
        for role in ("api", "backend"):
            path = self.candidate / role / "image.json"
            original = json.loads(path.read_text())
            for field in original:
                with self.subTest(role=role, field=field):
                    changed = dict(original)
                    changed[field] = "unverified"
                    path.write_text(json.dumps(changed))

                    with self.assertRaises(ValueError):
                        self.select()

                    path.write_text(json.dumps(original))

        with self.assertRaises(ValueError):
            self.select(manifest="sha256:" + "f" * 64)
        self.assertFalse(self.output.exists())

    def test_stable_rejects_images_outside_accepted_inventory(self) -> None:
        changed = copy.deepcopy(self.data)
        changed["accepted_image_manifest_digests"]["api"] = "sha256:" + "f" * 64

        with self.assertRaises(ValueError):
            self.select(data=changed)
        self.assertFalse(self.output.exists())
