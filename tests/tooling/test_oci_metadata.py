"""OCI metadata survives export and cannot disagree across image components."""

import json
import tempfile
import unittest
from pathlib import Path
from typing import Any

from tools.release.oci import inspect_layout, sha

SOURCE = "1" * 40


class OCIMetadataTests(unittest.TestCase):
    def setUp(self) -> None:
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.layout = Path(temporary.name)
        (self.layout / "blobs/sha256").mkdir(parents=True)
        self.labels = {
            "org.opencontainers.image.revision": SOURCE,
            "org.opencontainers.image.title": "Whisper API",
            "org.opencontainers.image.licenses": "Apache-2.0 AND MIT",
        }

    def descriptor(self, value: dict[str, Any]) -> dict[str, Any]:
        data = json.dumps(value).encode()
        digest = sha(data)
        (self.layout / "blobs/sha256" / digest).write_bytes(data)

        return {"digest": "sha256:" + digest, "size": len(data)}

    def write_layout(self, component: str = "", title: str = "") -> None:
        index_annotations = dict(self.labels)
        platform_annotations = dict(self.labels)
        if component:
            selected = index_annotations if component == "index" else platform_annotations
            selected["org.opencontainers.image.title"] = title

        runtime = self.descriptor(
            {
                "config": self.descriptor({"config": {"Labels": self.labels}}),
                "layers": [],
                "annotations": platform_annotations,
            }
        )
        runtime["platform"] = {"architecture": "amd64", "os": "linux"}

        attestations = self.descriptor(
            {
                "config": self.descriptor({}),
                "layers": [
                    self.descriptor({"subject": [], "predicateType": predicate})
                    for predicate in (
                        "https://spdx.dev/Document",
                        "https://slsa.dev/provenance/v0.2",
                    )
                ],
            }
        )
        attestations["annotations"] = {
            "vnd.docker.reference.type": "attestation-manifest",
            "vnd.docker.reference.digest": runtime["digest"],
        }

        index = self.descriptor(
            {"manifests": [runtime, attestations], "annotations": index_annotations}
        )
        (self.layout / "index.json").write_text(json.dumps({"manifests": [index]}))

    def test_inspection_exposes_matching_labels_and_both_annotation_levels(self) -> None:
        self.write_layout()

        metadata = inspect_layout(self.layout, SOURCE)

        self.assertEqual(metadata["labels"], self.labels)
        self.assertEqual(metadata["index_annotations"], self.labels)
        self.assertEqual(metadata["platform_annotations"], self.labels)

    def test_conflicting_registry_metadata_is_rejected(self) -> None:
        for component in ("index", "manifest"):
            with self.subTest(component=component):
                self.write_layout(component, "Conflicting description")

                with self.assertRaisesRegex(ValueError, "OCI annotation conflicts"):
                    inspect_layout(self.layout, SOURCE)


if __name__ == "__main__":
    unittest.main()
