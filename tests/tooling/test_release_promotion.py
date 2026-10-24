import copy
import hashlib
import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path
from typing import Any
from unittest.mock import patch

from tests.tooling.test_release import DIGEST, NOW, inventory
from tools.release import check, prepare, publish


def stable_inventory() -> dict[str, Any]:
    data = inventory()
    data.update(
        chart_version=data["release_version"],
        accepted_image_manifest_digests=copy.deepcopy(data["image_manifest_digests"]),
        documentation={"complete": True, "source_sha": data["release_source_sha"]},
        rendered_equivalence={
            "verified": True,
            "runtime_input_identity": data["runtime_input_identity"],
        },
        gpu_report_refs=[
            {
                "target": target,
                "conclusion": "success",
                "runtime_input_identity": data["runtime_input_identity"],
                "image_manifest_digests": copy.deepcopy(data["image_manifest_digests"]),
            }
            for target in sorted(check.GPU_TARGETS)
        ],
    )
    return data


class PromotionTests(unittest.TestCase):
    @unittest.skipUnless(
        os.environ.get("C02_PUBLISHER_TESTS") == "1", "Requires actual Helm artifact fixture"
    )
    def test_actual_helm_equivalence_allows_only_metadata_promotion(self) -> None:
        import yaml

        from tools.release.artifacts import extract, sha

        artifacts = Path(os.environ["C02_ARTIFACTS"]).resolve()
        helm = Path(os.environ["C02_HELM"]).resolve()
        with tempfile.TemporaryDirectory(prefix="whisper-chart-promotion-") as td:
            root = Path(td)

            def package(name: str, version: str) -> Path:
                destination = root / name
                shutil.copytree(artifacts / "chart", destination)
                source = destination / "extracted/whisper-server"
                metadata_path = source / "Chart.yaml"
                metadata = yaml.safe_load(metadata_path.read_text())
                metadata.update(version=version, appVersion=version)
                metadata_path.write_text(yaml.safe_dump(metadata, sort_keys=False))
                repack(destination)
                return destination

            def repack(destination: Path) -> None:
                source = destination / "extracted/whisper-server"
                version = yaml.safe_load((source / "Chart.yaml").read_text())["version"]
                result = subprocess.run(
                    [str(helm), "package", str(source), "--destination", str(destination)],
                    capture_output=True,
                    timeout=90,
                )

                self.assertEqual(result.returncode, 0, "Actual Helm package failed")

                saved = json.loads((destination / "chart.json").read_bytes())
                saved["archive"] = "whisper-server-" + version + ".tgz"
                saved["chart_archive_sha256"] = sha((destination / saved["archive"]).read_bytes())
                (destination / "chart.json").write_text(json.dumps(saved))
                # Helm normalizes Chart.yaml, including multiline annotations.
                # Compare and promote the actual archived bytes, not the input formatting.
                shutil.rmtree(destination / "extracted")
                extract(destination / saved["archive"], destination / "extracted")

            candidate = package("candidate", "0.1.0-rc.1")
            stable = package("stable", "0.1.0")
            prepare.equivalent(candidate, stable, helm, {"api": DIGEST, "backend": DIGEST})
            extra = stable / "extracted/whisper-server/templates/promotion-probe.yaml"
            extra.write_text(
                "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: unexpected-runtime-change\n"
            )
            repack(stable)

            with self.assertRaises(ValueError):
                prepare.equivalent(candidate, stable, helm, {"api": DIGEST, "backend": DIGEST})

    def test_inventory_approval_commit_can_only_change_the_selected_inventory(self) -> None:
        from tools.release.verify_ci import verify_inventory_revision

        with tempfile.TemporaryDirectory(prefix="whisper-approval-test-") as td:
            root = Path(td)

            def git(*args: str) -> str:
                result = subprocess.run(
                    ["git", "-C", str(root), *args], capture_output=True, text=True, timeout=30
                )

                self.assertEqual(result.returncode, 0, "Isolated Git fixture failed")

                return result.stdout.strip()

            git("init", "--quiet")
            git("config", "user.name", "Release Fixture")
            git("config", "user.email", "fixture@example.invalid")
            source = root / "main.go"
            source.write_text("package main\n")
            git("add", ".")
            git("commit", "--quiet", "-m", "fixture: source revision")
            artifact_sha = git("rev-parse", "HEAD")
            inventory_path = Path("docs/releases/fixture.json")
            (root / inventory_path).parent.mkdir(parents=True)
            (root / inventory_path).write_text("{}\n")
            git("add", ".")
            git("commit", "--quiet", "-m", "fixture: inventory approval")
            data = {"release_source_sha": artifact_sha}
            verify_inventory_revision(data, inventory_path, git("rev-parse", "HEAD"), root)
            source.write_text("package different\n")
            git("add", ".")
            git("commit", "--quiet", "-m", "fixture: unverified runtime change")

            with self.assertRaises(ValueError):
                verify_inventory_revision(data, inventory_path, git("rev-parse", "HEAD"), root)

    def test_stable_scan_binds_old_image_source_and_new_chart_source(self) -> None:
        from tools.security.run import input_binding

        chart = {
            "source_sha": "1" * 40,
            "chart_archive_sha256": "2" * 64,
            "values_matrix_identity": DIGEST,
        }
        builds = {
            role: {"source_sha": "3" * 40, "manifest_digest": DIGEST} for role in ("api", "backend")
        }

        with self.assertRaises(ValueError):
            input_binding(chart, builds)

        bound = input_binding(chart, builds, "3" * 40)

        self.assertEqual(bound["source_sha"], chart["source_sha"])
        self.assertEqual(bound["image_source_sha"], "3" * 40)

        builds["backend"]["source_sha"] = "4" * 40

        with self.assertRaises(ValueError):
            input_binding(chart, builds, "3" * 40)

    def test_stable_cannot_complete_before_alias_readback(self) -> None:
        data = stable_inventory()

        with self.assertRaises(ValueError):
            check.validate(data, "stable", NOW)

    def test_aliases_advance_only_after_all_channels_and_restore_on_failure(self) -> None:
        data = stable_inventory()
        data["channel_status"]["chart-pages"]["status"] = "pending"
        with patch.object(publish, "manifest") as read, patch.object(publish, "command") as write:
            with self.assertRaises(ValueError):
                publish.finalize_aliases(data, Path("/fixture"), NOW)
            read.assert_not_called()
            write.assert_not_called()

    def test_version_label_normalization_never_hides_selector_or_runtime_changes(self) -> None:
        resource: dict[str, Any] = {
            "kind": "StatefulSet",
            "metadata": {"name": "api", "labels": {"helm.sh/chart": "whisper-server-rc"}},
            "spec": {
                "selector": {"matchLabels": {"helm.sh/chart": "selector-must-not-disappear"}},
                "template": {"metadata": {"labels": {"helm.sh/chart": "whisper-server-rc"}}},
            },
        }
        normalized = prepare.normalize_render([copy.deepcopy(resource)])[0]

        self.assertNotIn("helm.sh/chart", normalized["metadata"]["labels"])
        self.assertNotIn("helm.sh/chart", normalized["spec"]["template"]["metadata"]["labels"])
        self.assertEqual(normalized["spec"]["selector"], resource["spec"]["selector"])

    def test_runtime_fingerprint_changes_with_source_but_not_release_metadata(self) -> None:
        from tools.release.artifacts import runtime_identity

        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            for name in (
                "go.mod",
                "go.sum",
                "LICENSE",
                "THIRD_PARTY_NOTICES.md",
                "containers/api.Dockerfile",
                "containers/backend.Dockerfile",
                "containers/build-inputs.json",
                "containers/scripts/build-jobs.sh",
                "charts/whisper-server/values.yaml",
                "charts/whisper-server/values.schema.json",
            ):
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("fixture\n")
            source = root / "cmd/api/main.go"
            source.parent.mkdir(parents=True)
            source.write_text("package main\n")
            chart = root / "charts/whisper-server/Chart.yaml"
            chart.parent.mkdir(parents=True, exist_ok=True)
            chart.write_text("version: 0.1.0-rc.1\n")
            first = runtime_identity(root)
            helper = root / "containers/scripts/build-jobs.sh"
            helper.write_text("changed compiler concurrency policy\n")

            self.assertNotEqual(runtime_identity(root), first)

            helper.write_text("fixture\n")
            chart.write_text("version: 0.1.0\n")

            self.assertEqual(runtime_identity(root), first)

            source.write_text("package main\nfunc main() {}\n")

            self.assertNotEqual(runtime_identity(root), first)

    def test_alias_failure_restores_every_touched_existing_alias(self) -> None:
        old, new = b"previous-index", b"qualified-index"
        identity = "sha256:" + hashlib.sha256(new).hexdigest()
        data = stable_inventory()
        images = dict.fromkeys(("api", "backend"), identity)
        data["image_manifest_digests"] = images
        data["accepted_image_manifest_digests"] = images
        data["security_report_refs"][0]["image_manifest_digests"] = images
        for report in data["gpu_report_refs"]:
            report["image_manifest_digests"] = images
        for channel in publish.REPOSITORIES:
            data["channel_status"][channel]["identity"] = identity
        stored = {
            (repo, alias): old
            for repo in publish.REPOSITORIES.values()
            for alias in check.stable_aliases(data["release_version"])
        }
        calls = []

        def write(args: list[str]) -> None:
            repository, alias = args[args.index("--tag") + 1].rsplit(":", 1)
            calls.append((repository, alias))
            stored[repository, alias] = new if args[-1].startswith("oci-layout://") else old
            if len(calls) == 2:
                raise ValueError("Injected failure after remote write")

        with (
            patch.object(publish, "inspect_layout", return_value={"manifest_digest": identity}),
            patch.object(publish, "manifest", side_effect=lambda repo, alias: stored[repo, alias]),
            patch.object(publish, "command", side_effect=write),
            self.assertRaises(ValueError),
        ):
            publish.finalize_aliases(data, Path("/fixture"), NOW)
        self.assertTrue(all(value == old for value in stored.values()))
        self.assertEqual(len(calls), 4)
        self.assertTrue(
            all(state["status"] != "verified" for state in data["alias_status"].values())
        )

    def test_alias_snapshot_is_checkpointed_before_remote_writes(self) -> None:
        old, new = b"previous-index", b"qualified-index"
        identity = "sha256:" + hashlib.sha256(new).hexdigest()
        data = stable_inventory()
        data["image_manifest_digests"] = dict.fromkeys(("api", "backend"), identity)
        data["accepted_image_manifest_digests"] = data["image_manifest_digests"]
        data["security_report_refs"][0]["image_manifest_digests"] = data["image_manifest_digests"]
        for report in data["gpu_report_refs"]:
            report["image_manifest_digests"] = data["image_manifest_digests"]
        for channel in publish.REPOSITORIES:
            data["channel_status"][channel]["identity"] = identity
        snapshots: list[dict[str, Any]] = []
        stored = {
            (repository, alias): old
            for repository in publish.REPOSITORIES.values()
            for alias in check.stable_aliases(data["release_version"])
        }

        def write(args: list[str]) -> None:
            self.assertTrue(snapshots, "Alias write preceded the durable checkpoint")

            repository, alias = args[args.index("--tag") + 1].rsplit(":", 1)
            channel = next(
                name for name, value in publish.REPOSITORIES.items() if value == repository
            )
            state = snapshots[-1]["alias_status"][channel]

            self.assertIn(alias, state["touched"])
            self.assertEqual(
                state["previous_identities"][alias], "sha256:" + hashlib.sha256(old).hexdigest()
            )

            stored[repository, alias] = new

        with (
            patch.object(publish, "inspect_layout", return_value={"manifest_digest": identity}),
            patch.object(publish, "manifest", side_effect=lambda repo, alias: stored[repo, alias]),
            patch.object(publish, "command", side_effect=write),
        ):
            publish.finalize_aliases(
                data,
                Path("/fixture"),
                NOW,
                checkpoint=lambda: snapshots.append(copy.deepcopy(data)),
            )

        self.assertEqual(check.validate(data, "stable", NOW), "stable_complete")

    def test_stable_gate_requires_exact_aliases_and_digests(self) -> None:
        data = stable_inventory()
        data["alias_status"] = {
            channel: {
                "status": "verified",
                "identities": dict.fromkeys(check.stable_aliases(data["release_version"]), DIGEST),
            }
            for channel in publish.REPOSITORIES
        }

        self.assertEqual(check.validate(data, "stable", NOW), "stable_complete")

        for change in ("missing", "wrong_digest", "failed"):
            changed = copy.deepcopy(data)
            observed = changed["alias_status"]["api-ghcr"]
            if change == "missing":
                observed["identities"].pop("latest")
            elif change == "wrong_digest":
                observed["identities"]["latest"] = "sha256:" + "9" * 64
            else:
                observed["status"] = "failed"

            with self.subTest(change=change), self.assertRaises(ValueError):
                check.validate(changed, "stable", NOW)


if __name__ == "__main__":
    unittest.main()
