"""Disposable registry and Helm index acceptance; never contacts release channels."""

import hashlib
import json
import os
import shutil
import subprocess
import tempfile
import time
import unittest
import urllib.error
import urllib.request
from datetime import datetime
from pathlib import Path
from unittest.mock import patch

from tools.contract_yaml import load_yaml
from tools.release import publish

ROOT = Path(__file__).resolve().parents[2]


@unittest.skipUnless(
    os.environ.get("C02_PUBLISHER_TESTS") == "1",
    "Requires Docker, pinned tools and actual OCI/chart artifacts",
)
class ActualPublisherTests(unittest.TestCase):
    def test_chart_scan_inputs_cannot_drift_from_the_archive_or_matrix(self) -> None:
        from tools.release.artifacts import verify_chart

        original = Path(os.environ["C02_ARTIFACTS"]).resolve() / "chart"
        with tempfile.TemporaryDirectory() as td:
            target = Path(td) / "chart"
            shutil.copytree(original, target)
            verify_chart(target)
            for path in (
                target / "rendered/defaults/manifest.yaml",
                target / "values/defaults.json",
                next((target / "extracted").rglob("Chart.yaml")),
            ):
                saved = path.read_bytes()
                try:
                    path.write_bytes(b'{"unexpected":true}\n')

                    with self.subTest(path=path.name), self.assertRaises(ValueError):
                        verify_chart(target)
                finally:
                    path.write_bytes(saved)

    def start_registry(self) -> str:
        inputs = json.loads((ROOT / "tools/versions.yaml").read_bytes())
        container = subprocess.check_output(
            [
                "docker",
                "run",
                "--rm",
                "--detach",
                "--publish",
                "127.0.0.1::5000",
                inputs["ci"]["fixture_registry"]["image"],
            ],
            text=True,
        ).strip()
        self.addCleanup(
            subprocess.run, ["docker", "rm", "--force", container], check=True, capture_output=True
        )
        state = json.loads(subprocess.check_output(["docker", "inspect", container]))[0]
        port = state["NetworkSettings"]["Ports"]["5000/tcp"][0]["HostPort"]
        address = "127.0.0.1:" + port
        base = "http://" + address + "/v2/"
        for _attempt in range(50):
            try:
                urllib.request.urlopen(base, timeout=1).close()
                break
            except OSError:
                time.sleep(0.1)
        else:
            self.fail("Disposable registry did not start")
        return address

    def test_selected_oci_index_is_identical_after_local_registry_copy_and_retry(self) -> None:
        address = self.start_registry()
        repository = address + "/fixture"
        base = "http://" + address + "/v2/"

        def read_manifest(repo: str, version: str) -> bytes | None:
            self.assertEqual(repo, repository)

            request = urllib.request.Request(
                base + "fixture/manifests/" + version, headers={"Accept": publish.ACCEPT}
            )
            try:
                with urllib.request.urlopen(request, timeout=15) as response:
                    return response.read()
            except urllib.error.HTTPError as error:
                if error.code == 404:
                    return None
                raise

        artifact = Path(os.environ["C02_ARTIFACTS"]).resolve() / "api"
        expected = json.loads((artifact / "image.json").read_bytes())["manifest_digest"]
        with patch.object(publish, "manifest", side_effect=read_manifest):
            publish.publish_image(artifact / "layout", repository, "fixture", expected)
            with patch.object(publish, "command") as writer:
                publish.publish_image(artifact / "layout", repository, "fixture", expected)

                writer.assert_not_called()
                with self.assertRaises(ValueError):
                    publish.publish_image(
                        artifact / "layout", repository, "fixture", "sha256:" + "0" * 64
                    )
                writer.assert_not_called()
        observed = read_manifest(repository, "fixture")
        if observed is None:
            self.fail("Published manifest is missing")

        self.assertEqual("sha256:" + hashlib.sha256(observed).hexdigest(), expected)

        recorded = json.loads((artifact / "image.json").read_bytes())

        self.assertEqual(json.loads(observed)["annotations"], recorded["index_annotations"])

    def test_helm_metadata_survives_oci_push_and_pull(self) -> None:
        address = self.start_registry()
        chart = Path(os.environ["C02_ARTIFACTS"]).resolve() / "chart"
        recorded = json.loads((chart / "chart.json").read_bytes())
        archive = chart / recorded["archive"]
        helm = os.environ["C02_HELM"]
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            environment = {
                **os.environ,
                "HELM_CACHE_HOME": str(root / "cache"),
                "HELM_CONFIG_HOME": str(root / "config"),
                "HELM_DATA_HOME": str(root / "data"),
                "HELM_REGISTRY_CONFIG": str(root / "registry.json"),
            }

            def command(*args: str) -> bytes:
                return subprocess.check_output(
                    [helm, *args], env=environment, stderr=subprocess.STDOUT, timeout=60
                )

            metadata = load_yaml(command("show", "chart", str(archive)).decode())
            command("push", str(archive), "oci://" + address + "/helm", "--plain-http")
            request = urllib.request.Request(
                "http://" + address + "/v2/helm/whisper-server/manifests/" + metadata["version"],
                headers={"Accept": "application/vnd.oci.image.manifest.v1+json"},
            )
            with urllib.request.urlopen(request, timeout=15) as response:
                manifest = json.load(response)
            annotations = manifest["annotations"]
            for field, value in (
                ("title", metadata["name"]),
                ("description", metadata["description"]),
                ("version", metadata["version"]),
                ("url", metadata["home"]),
                ("source", metadata["sources"][0]),
                ("authors", metadata["maintainers"][0]["name"]),
            ):
                self.assertEqual(annotations["org.opencontainers.image." + field], value)

            self.assertIsNotNone(
                datetime.fromisoformat(annotations["org.opencontainers.image.created"]).tzinfo
            )

            for key, value in metadata["annotations"].items():
                self.assertEqual(annotations[key], value)

            self.assertEqual(annotations["org.opencontainers.image.licenses"], "Apache-2.0")
            self.assertTrue(annotations["org.opencontainers.image.documentation"])

            config_url = (
                "http://"
                + address
                + "/v2/helm/whisper-server/blobs/"
                + manifest["config"]["digest"]
            )
            with urllib.request.urlopen(config_url, timeout=15) as response:
                config = json.load(response)
            for field in ("keywords", "home", "sources", "maintainers", "annotations"):
                self.assertEqual(config[field], metadata[field])
            command(
                "pull",
                "oci://" + address + "/helm/whisper-server",
                "--version",
                metadata["version"],
                "--plain-http",
                "--destination",
                str(root),
            )

            self.assertEqual((root / archive.name).read_bytes(), archive.read_bytes())

    def test_oci_metadata_is_readable_and_consistent_at_every_registry_level(self) -> None:
        from tools.release.oci import inspect_layout

        artifacts = Path(os.environ["C02_ARTIFACTS"]).resolve()
        fields = (
            "title",
            "description",
            "authors",
            "vendor",
            "url",
            "documentation",
            "source",
            "version",
            "revision",
            "licenses",
            "created",
        )
        for role in ("api", "backend"):
            with self.subTest(role=role):
                saved = json.loads((artifacts / role / "image.json").read_bytes())
                metadata = inspect_layout(artifacts / role / "layout", saved["source_sha"])
                for field in fields:
                    key = "org.opencontainers.image." + field
                    value = metadata["labels"][key]

                    self.assertIsInstance(value, str)
                    self.assertTrue(value.strip())
                    self.assertEqual(metadata["index_annotations"][key], value)
                    self.assertEqual(metadata["platform_annotations"][key], value)

                self.assertEqual(
                    metadata["labels"]["org.opencontainers.image.title"],
                    "openwhispr-whisper-server-" + role,
                )
                self.assertIsNotNone(
                    datetime.fromisoformat(
                        metadata["labels"]["org.opencontainers.image.created"]
                    ).tzinfo
                )

    def test_actual_helm_index_preserves_previous_versions_and_archive_bytes(self) -> None:
        chart = Path(os.environ["C02_ARTIFACTS"]).resolve() / "chart"
        identity = json.loads((chart / "chart.json").read_bytes())
        archive = chart / identity["archive"]
        version = identity["archive"].removeprefix("whisper-server-").removesuffix(".tgz")
        with tempfile.TemporaryDirectory() as td:
            output = Path(td)
            (output / "index.yaml").write_text(
                "apiVersion: v1\nentries:\n  whisper-server:\n  - version: 0.0.1\n    digest: "
                + "a" * 64
                + "\n"
            )
            publish.prepare_pages(
                archive, version, identity["chart_archive_sha256"], output, os.environ["C02_HELM"]
            )
            first = (output / "index.yaml").read_bytes()
            publish.prepare_pages(
                archive, version, identity["chart_archive_sha256"], output, os.environ["C02_HELM"]
            )

            self.assertEqual(first, (output / "index.yaml").read_bytes())

            index = load_yaml(first.decode())

            self.assertEqual(
                {item["version"] for item in index["entries"]["whisper-server"]}, {"0.0.1", version}
            )
            self.assertEqual((output / archive.name).read_bytes(), archive.read_bytes())


if __name__ == "__main__":
    unittest.main()
