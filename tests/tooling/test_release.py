import copy
import hashlib
import tempfile
import unittest
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from tools.release import check

NOW = datetime(2026, 10, 23, 12, tzinfo=UTC)
SHA = "1" * 40
DIGEST = "sha256:" + "2" * 64
CHART = "3" * 64


def inventory() -> dict[str, Any]:
    return {
        "schema_version": 1,
        "candidate_version": "0.1.0-rc.1",
        "candidate_source_sha": SHA,
        "release_version": "0.1.0",
        "release_source_sha": SHA,
        "build_input_identity": DIGEST,
        "runtime_input_identity": DIGEST,
        "upstream_revision": "6ac14546046ffb17d144a08bcad65337e535cf5",
        "base_digests": {"api": DIGEST, "backend": DIGEST},
        "toolchain_versions": {"go": "1.27.3", "cuda": "12.8"},
        "image_manifest_digests": {"api": DIGEST, "backend": DIGEST},
        "chart_version": "0.1.0-rc.1",
        "chart_archive_sha256": CHART,
        "chart_oci_manifest_digest": DIGEST,
        "ci_run_refs": [
            {
                "run_id": 1,
                "source_sha": SHA,
                "build_input_identity": DIGEST,
                "conclusion": "success",
                "jobs": dict.fromkeys(check.REQUIRED_JOBS, "success"),
            }
        ],
        "security_report_refs": [
            {
                "source_sha": SHA,
                "build_input_identity": DIGEST,
                "image_manifest_digests": {"api": DIGEST, "backend": DIGEST},
                "chart_archive_sha256": CHART,
                "conclusion": "success",
                "scan_time": NOW.isoformat(),
                "coverage_gaps": [],
            }
        ],
        "gpu_report_refs": [],
        "channel_status": {
            name: {"status": "verified", "identity": CHART if name.startswith("chart-") else DIGEST}
            for name in check.CHANNELS
        },
    }


class ReleaseGateTests(unittest.TestCase):
    def test_candidate_can_be_published_without_claiming_gpu_qualification(self) -> None:
        self.assertEqual(
            check.validate(inventory(), "candidate", NOW), "candidate_published_unqualified"
        )

    def test_version_numbers_with_leading_zeroes_are_not_semver(self) -> None:
        for field, value in (("candidate_version", "0.01.0-rc.1"), ("release_version", "00.1.0")):
            data = inventory()
            data[field] = value
            if field == "candidate_version":
                data["chart_version"] = value

            with self.subTest(field=field), self.assertRaises(ValueError):
                check.validate(data, "candidate", NOW)

    def test_failed_cancelled_skipped_or_missing_ci_denies_candidate(self) -> None:
        for state in ("failure", "cancelled", "skipped", None):
            data = inventory()
            if state is None:
                data["ci_run_refs"][0]["jobs"].pop(check.REQUIRED_JOBS[0])
            else:
                data["ci_run_refs"][0]["jobs"][check.REQUIRED_JOBS[0]] = state

            with self.subTest(state=state), self.assertRaises(ValueError):
                check.validate(data, "candidate", NOW)

    def test_drift_wrong_pulled_identity_or_partial_channel_denies(self) -> None:
        for mutate in (
            lambda x: x["ci_run_refs"][0].update(source_sha="4" * 40),
            lambda x: x["security_report_refs"][0].update(chart_archive_sha256="5" * 64),
            lambda x: x["channel_status"]["api-dockerhub"].update(identity="sha256:" + "6" * 64),
            lambda x: x["channel_status"]["chart-pages"].update(status="failed"),
            lambda x: x["security_report_refs"][0].update(scan_time="2026-10-20T00:00:00+00:00"),
        ):
            data = inventory()
            mutate(data)

            with self.subTest(data=data), self.assertRaises(ValueError):
                check.validate(data, "candidate", NOW)

    def test_stable_requires_both_current_gpu_reports_and_finished_docs(self) -> None:
        data = inventory()
        data.update(
            chart_version="0.1.0",
            documentation={"complete": True, "source_sha": SHA},
            accepted_image_manifest_digests=copy.deepcopy(data["image_manifest_digests"]),
            rendered_equivalence={"verified": True, "runtime_input_identity": DIGEST},
        )
        reports = [
            {
                "target": target,
                "conclusion": "success",
                "runtime_input_identity": DIGEST,
                "image_manifest_digests": copy.deepcopy(data["image_manifest_digests"]),
            }
            for target in ("docker-rtx5070ti", "homelab-rtx2060")
        ]
        for count in (0, 1):
            data["gpu_report_refs"] = reports[:count]

            with self.assertRaises(ValueError):
                check.validate(data, "stable", NOW)
        data["gpu_report_refs"] = reports
        data["alias_status"] = {
            channel: {
                "status": "verified",
                "identities": dict.fromkeys(check.stable_aliases(data["release_version"]), DIGEST),
            }
            for channel in check.CHANNELS
            if not channel.startswith("chart-")
        }

        self.assertEqual(check.validate(data, "stable", NOW), "stable_complete")

        for mutate in (
            lambda x: x["documentation"].update(complete=False),
            lambda x: x["gpu_report_refs"][1].update(runtime_input_identity="sha256:" + "7" * 64),
            lambda x: x["accepted_image_manifest_digests"].update(api="sha256:" + "8" * 64),
            lambda x: x["rendered_equivalence"].update(verified=False),
        ):
            invalid = copy.deepcopy(data)
            mutate(invalid)

            with self.assertRaises(ValueError):
                check.validate(invalid, "stable", NOW)

    def test_index_merge_preserves_old_version_and_denies_byte_collision(self) -> None:
        old: dict[str, Any] = {
            "apiVersion": "v1",
            "entries": {"whisper-server": [{"version": "0.0.1", "digest": "a" * 64}]},
        }
        new: dict[str, Any] = {
            "apiVersion": "v1",
            "entries": {"whisper-server": [{"version": "0.1.0-rc.1", "digest": CHART}]},
        }
        merged = check.merge_index(old, new)

        self.assertEqual(len(merged["entries"]["whisper-server"]), 2)
        self.assertEqual(old["entries"]["whisper-server"][0]["digest"], "a" * 64)
        self.assertEqual(check.merge_index(merged, new), merged)

        changed = copy.deepcopy(new)
        changed["entries"]["whisper-server"][0]["digest"] = "b" * 64

        with self.assertRaises(ValueError):
            check.merge_index(merged, changed)

    def test_immutable_copy_retries_without_overwriting_a_used_version(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            source, target = Path(td) / "source", Path(td) / "public" / "chart.tgz"
            source.write_bytes(b"chart-fixture")
            expected = hashlib.sha256(source.read_bytes()).hexdigest()
            check.immutable_copy(source, target, expected)
            check.immutable_copy(source, target, expected)
            source.write_bytes(b"changed")

            with self.assertRaises(ValueError):
                check.immutable_copy(
                    source, target, hashlib.sha256(source.read_bytes()).hexdigest()
                )
            self.assertEqual(target.read_bytes(), b"chart-fixture")


if __name__ == "__main__":
    unittest.main()
