import copy
import hashlib
import json
import os
import tempfile
import unittest
from pathlib import Path
from typing import Any
from unittest.mock import patch

import yaml

from tests.tooling import test_security
from tests.tooling.test_release import CHART, DIGEST, NOW, SHA, inventory
from tools.release import artifacts, check, publish
from tools.release.inventory import assemble, qualify


class ReleaseInventoryTests(unittest.TestCase):
    def security(self) -> dict[str, Any]:
        return dict(test_security.SecurityAggregateTests().inventory(), build_input_identity=DIGEST)

    def inputs(self) -> tuple[dict[str, Any], dict[str, Any], dict[str, Any], dict[str, Any]]:
        previous = inventory()
        metadata = {
            "version": previous["candidate_version"],
            "stage": "candidate",
            "source_sha": SHA,
            "candidate_run_id": 42,
            "ci_run_ref": previous["ci_run_refs"][0],
        }
        builds = {
            role: {
                "manifest_digest": DIGEST,
                "cold": True,
                "runtime_input_identity": DIGEST,
                "build_input_identity": DIGEST,
            }
            for role in ("api", "backend")
        }
        chart = {"source_sha": SHA, "chart_archive_sha256": CHART}
        pins = {
            "go": {"version": "1.27.3", "builder": "go@" + DIGEST},
            "api": {"builder_runtime": "ubuntu@" + DIGEST},
            "backend": {
                "cuda_runtime": "cuda@" + DIGEST,
                "nvcc": "12.8",
                "upstream_requested_ref": previous["upstream_revision"],
                "upstream_commit": "6" * 40,
            },
        }
        return metadata, builds, chart, pins

    def test_candidate_qualification_requires_actual_matching_security_evidence(self) -> None:
        data = assemble(*self.inputs())
        qualify(data, self.security(), "candidate", NOW)
        self.assertEqual(
            check.validate(data, "candidate", NOW, require_channels=False), "candidate_building"
        )
        self.assertEqual(data["candidate_run_id"], 42)
        self.assertIsNone(data["chart_oci_manifest_digest"])
        with self.assertRaises(ValueError):
            check.validate(data, "candidate", NOW)
        mismatched = copy.deepcopy(data)
        mismatched["image_manifest_digests"]["api"] = "sha256:" + "9" * 64
        with self.assertRaises(ValueError):
            qualify(mismatched, self.security(), "candidate", NOW)

    def test_warm_build_or_different_runtime_cannot_be_assembled(self) -> None:
        metadata, builds, chart, pins = self.inputs()
        for invalid in (False, "false", 1, None):
            builds["api"]["cold"] = invalid
            with self.subTest(cold=invalid), self.assertRaises(ValueError):
                assemble(metadata, builds, chart, pins)
        builds["api"]["cold"] = True
        builds["backend"]["runtime_input_identity"] = "sha256:" + "9" * 64
        with self.assertRaises(ValueError):
            assemble(metadata, builds, chart, pins)

    def test_stable_does_not_reuse_an_old_rendering_approval(self) -> None:
        metadata, builds, chart, pins = self.inputs()
        metadata.update(stage="stable", version="0.1.0")
        approved = inventory()
        approved.update(
            accepted_image_manifest_digests=approved["image_manifest_digests"],
            documentation={"complete": True, "source_sha": SHA},
            rendered_equivalence={"verified": True, "runtime_input_identity": DIGEST},
        )
        data = assemble(metadata, builds, chart, pins, approved)
        self.assertEqual(data["rendered_equivalence"], {})
        with self.assertRaises(ValueError):
            qualify(data, self.security(), "stable", NOW)

    def test_chart_manifest_is_recorded_only_after_its_archive_readback(self) -> None:
        content = b"selected chart archive"
        archive_sha = hashlib.sha256(content).hexdigest()
        manifest = json.dumps(
            {
                "layers": [
                    {
                        "mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
                        "digest": "sha256:" + archive_sha,
                    }
                ]
            }
        ).encode()
        expected = "sha256:" + hashlib.sha256(manifest).hexdigest()
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "chart.tgz"
            archive.write_bytes(content)
            with (
                patch.object(publish, "manifest", return_value=manifest),
                patch.object(publish, "registry_get", return_value=content),
                patch.object(publish, "command") as write,
            ):
                self.assertEqual(
                    publish.publish_chart(archive, "0.1.0-rc.1", archive_sha, None, "helm"),
                    expected,
                )
                write.assert_not_called()
            with (
                patch.object(publish, "manifest", return_value=manifest),
                patch.object(publish, "registry_get", return_value=b"different archive"),
                self.assertRaises(ValueError),
            ):
                publish.publish_chart(archive, "0.1.0-rc.1", archive_sha, None, "helm")

    @unittest.skipUnless(os.environ.get("C02_HELM"), "Requires the pinned Helm executable")
    def test_tag_version_is_applied_to_the_produced_chart(self) -> None:
        helm = os.environ["C02_HELM"]
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            artifacts.chart(output, helm, SHA, "0.1.0-rc.7")
            saved = artifacts.verify_chart(output)
            self.assertEqual(saved["archive"], "whisper-server-0.1.0-rc.7.tgz")
            metadata = yaml.safe_load((output / "extracted/whisper-server/Chart.yaml").read_bytes())
            self.assertEqual(metadata["version"], "0.1.0-rc.7")
            self.assertEqual(metadata["appVersion"], "0.1.0-rc.7")
            resources = yaml.safe_load_all(
                (output / "rendered/defaults/manifest.yaml").read_bytes()
            )
            images = {
                container["image"]
                for resource in resources
                if resource and resource["kind"] == "StatefulSet"
                for container in resource["spec"]["template"]["spec"]["containers"]
            }
            self.assertEqual(
                images,
                {
                    f"ghcr.io/swimmwatch/openwhispr-whisper-server-{role}:0.1.0-rc.7"
                    for role in ("api", "backend")
                },
            )
