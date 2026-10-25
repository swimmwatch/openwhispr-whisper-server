#!/usr/bin/env python3
"""Select accepted images and the current chart without rebuilding for promotion."""

import argparse
import json
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any

import yaml

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from tools.release.artifacts import ROOT, inspect_layout, runtime_identity, verify_chart
from tools.release.check import require
from tools.release.storage import link_or_copy
from tools.security.check import VALUES_VARIANTS


def normalize_render(resources: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Ignore only chart-version labels; retain selectors and all runtime fields."""

    def visit(value: Any) -> None:
        if isinstance(value, dict):
            metadata = value.get("metadata")
            if isinstance(metadata, dict) and isinstance(metadata.get("labels"), dict):
                metadata["labels"].pop("helm.sh/chart", None)

            for child in value.values():
                visit(child)
        elif isinstance(value, list):
            for child in value:
                visit(child)

    visit(resources)

    return sorted(resources, key=lambda item: (item["kind"], item["metadata"]["name"]))


def rendered(
    helm: str | Path, archive: Path, values: dict[str, Any], images: dict[str, str]
) -> list[dict[str, Any]]:
    with tempfile.TemporaryDirectory(prefix="whisper-promotion-render-") as td:
        path = Path(td) / "values.json"
        values = json.loads(json.dumps(values))
        for role, identity in images.items():
            values.setdefault("images", {}).setdefault(role, {})["digest"] = identity

        path.write_text(json.dumps(values))

        result = subprocess.run(
            [
                str(helm),
                "template",
                "whisper",
                str(archive),
                "--namespace",
                "whisper-test",
                "--kube-version",
                "1.37.0",
                "--values",
                str(path),
            ],
            capture_output=True,
            timeout=90,
        )
        require(result.returncode == 0, "Promotion render failed; raw output suppressed")

        return normalize_render([item for item in yaml.safe_load_all(result.stdout) if item])


def equivalent(candidate: Path, stable: Path, helm: str | Path, images: dict[str, str]) -> None:
    old, new = verify_chart(candidate), verify_chart(stable)
    old_metadata = yaml.safe_load((candidate / "extracted/whisper-server/Chart.yaml").read_text())
    new_metadata = yaml.safe_load((stable / "extracted/whisper-server/Chart.yaml").read_text())
    for metadata in (old_metadata, new_metadata):
        metadata.pop("version", None)
        metadata.pop("appVersion", None)

    require(old_metadata == new_metadata, "Stable chart has changes beyond version metadata")

    for name in VALUES_VARIANTS:
        old_values = json.loads((candidate / "values" / (name + ".json")).read_bytes())
        new_values = json.loads((stable / "values" / (name + ".json")).read_bytes())
        require(old_values == new_values, "Promotion values matrix changed")
        require(
            rendered(helm, candidate / old["archive"], old_values, images)
            == rendered(helm, stable / new["archive"], new_values, images),
            "Stable chart changes rendered runtime behavior",
        )


def select(
    data: dict[str, Any],
    stage: str,
    current: Path,
    candidate: Path | None,
    output: Path,
    helm: str | Path,
    root: Path = ROOT,
) -> None:
    expected_runtime = runtime_identity(root)
    require(
        data["runtime_input_identity"] == expected_runtime,
        "Current runtime inputs differ from accepted selection",
    )

    chart = verify_chart(current / "chart")
    metadata = yaml.safe_load((current / "chart/extracted/whisper-server/Chart.yaml").read_text())
    require(
        chart["source_sha"] == data["release_source_sha"]
        and chart["chart_archive_sha256"] == data["chart_archive_sha256"]
        and metadata["version"] == data["chart_version"]
        and metadata["appVersion"]
        == data["candidate_version" if stage == "candidate" else "release_version"],
        "Selected chart identity/version drift",
    )

    chosen = candidate if candidate is not None else current
    require(stage == "candidate" or candidate is not None, "Accepted candidate run is required")

    for role in ("api", "backend"):
        saved = json.loads((chosen / role / "image.json").read_bytes())
        actual = inspect_layout(chosen / role / "layout", data["candidate_source_sha"])
        require(
            actual["manifest_digest"] == data["image_manifest_digests"][role]
            and saved["manifest_digest"] == actual["manifest_digest"]
            and saved["source_sha"] == data["candidate_source_sha"]
            and saved["runtime_input_identity"] == expected_runtime
            and saved["build_input_identity"] == data["build_input_identity"],
            "Accepted image/input drift",
        )

    if stage == "stable":
        require(
            data["accepted_image_manifest_digests"] == data["image_manifest_digests"],
            "Stable selection rebuilt accepted images",
        )
        equivalent(chosen / "chart", current / "chart", helm, data["image_manifest_digests"])

    require(not output.exists(), "Selection destination must be new")
    output.mkdir(parents=True)

    for role in ("api", "backend"):
        shutil.copytree(chosen / role, output / role, copy_function=link_or_copy)

    shutil.copytree(current / "chart", output / "chart", copy_function=link_or_copy)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inventory", type=Path, required=True)
    parser.add_argument("--stage", choices=("candidate", "stable"), required=True)
    parser.add_argument("--current", type=Path, required=True)
    parser.add_argument("--candidate", type=Path)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--helm", required=True)

    args = parser.parse_args()

    try:
        select(
            json.loads(args.inventory.read_bytes()),
            args.stage,
            args.current,
            args.candidate,
            args.output,
            args.helm,
        )
    except OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError, yaml.YAMLError:
        print("FAIL: release selection or runtime equivalence did not match", file=sys.stderr)
        return 1

    print("PASS: selected immutable images and chart; no promotion rebuild")
    return 0


if __name__ == "__main__":
    sys.exit(main())
