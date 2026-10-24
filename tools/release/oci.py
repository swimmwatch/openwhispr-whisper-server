"""Verify OCI byte identities and safely extract release archives."""

import hashlib
import json
import tarfile
from pathlib import Path
from typing import Any, cast

from tools.release.validation import digest, require


def sha(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def blob(layout: Path, descriptor: dict[str, Any]) -> bytes:
    identity = descriptor.get("digest")
    digest(identity)

    identity = cast(str, identity)
    data = (layout / "blobs/sha256" / identity.split(":")[1]).read_bytes()
    require(
        len(data) == descriptor.get("size") and "sha256:" + sha(data) == identity,
        "OCI blob identity mismatch",
    )

    return data


def inspect_layout(layout: Path, source_sha: str) -> dict[str, Any]:
    index = json.loads((layout / "index.json").read_bytes())
    require(len(index["manifests"]) == 1, "Expected one selected OCI index")

    selected = index["manifests"][0]
    selected_data = blob(layout, selected)
    manifest = json.loads(selected_data)
    require("manifests" in manifest, "OCI index must preserve attestations")

    platforms = [
        item
        for item in manifest["manifests"]
        if item.get("platform") == {"architecture": "amd64", "os": "linux"}
    ]
    require(len(platforms) == 1, "Exactly one Linux amd64 runtime is required")

    platform = platforms[0]
    image = json.loads(blob(layout, platform))
    config = json.loads(blob(layout, image["config"]))
    require(
        config["config"]["Labels"]["org.opencontainers.image.revision"] == source_sha,
        "OCI source revision mismatch",
    )

    labels = config["config"]["Labels"]
    for component in (manifest, image):
        for key, value in component.get("annotations", {}).items():
            if key.startswith("org.opencontainers.image.") and key in labels:
                require(value == labels[key], "OCI annotation conflicts with image label: " + key)

    # Hash layers incrementally: CUDA layers are too large for an in-memory read.
    for layer in image["layers"]:
        digest(layer["digest"])

        path = layout / "blobs/sha256" / layer["digest"].split(":")[1]

        with path.open("rb") as stream:
            require(
                path.stat().st_size == layer["size"]
                and "sha256:" + hashlib.file_digest(stream, "sha256").hexdigest()
                == layer["digest"],
                "OCI layer identity mismatch",
            )

    predicates = set()
    for item in manifest["manifests"]:
        if item.get("annotations", {}).get("vnd.docker.reference.type") != "attestation-manifest":
            continue

        require(
            item["annotations"].get("vnd.docker.reference.digest") == platform["digest"],
            "Attestation target mismatch",
        )

        attestation = json.loads(blob(layout, item))
        blob(layout, attestation["config"])

        for layer in attestation["layers"]:
            statement = json.loads(blob(layout, layer))
            # An unnamed OCI export has an empty in-toto subject. BuildKit binds
            # it through the checked attestation-manifest reference above.
            subjects = statement.get("subject")
            require(
                isinstance(subjects, list)
                and (
                    not subjects
                    or any(
                        subject.get("digest", {}).get("sha256") == platform["digest"].split(":")[1]
                        for subject in subjects
                    )
                ),
                "Attestation subject mismatch",
            )
            predicates.add(statement.get("predicateType"))

    require(
        "https://spdx.dev/Document" in predicates
        and any(value and value.startswith("https://slsa.dev/provenance/") for value in predicates),
        "Missing actual BuildKit SBOM or provenance",
    )

    return {
        "manifest_digest": selected["digest"],
        "platform_digest": platform["digest"],
        "config_digest": image["config"]["digest"],
        "source_sha": source_sha,
        "attestation_predicates": sorted(predicates),
        "labels": labels,
        "index_annotations": manifest.get("annotations", {}),
        "platform_annotations": image.get("annotations", {}),
    }


def extract(archive: Path, destination: Path) -> None:
    destination.mkdir(parents=True, exist_ok=True)

    with tarfile.open(archive) as package:
        members = package.getmembers()
        for item in members:
            require(
                (item.isfile() or item.isdir())
                and not Path(item.name).is_absolute()
                and ".." not in Path(item.name).parts,
                "Unsafe artifact archive",
            )

        package.extractall(destination, members=members, filter="data")
