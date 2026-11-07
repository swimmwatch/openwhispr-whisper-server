#!/usr/bin/env python3
"""Validate a selected release inventory; never infer missing acceptance evidence."""

import argparse
import json
import re
import sys
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any, cast

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from tools.release.storage import immutable_copy as immutable_copy
from tools.release.storage import merge_index as merge_index
from tools.release.validation import digest as digest
from tools.release.validation import require as require
from tools.release.validation import source as source

REQUIRED_JOBS = ("source", "chart", "build-api", "build-backend", "Security Check")
CHANNELS = (
    "api-ghcr",
    "api-dockerhub",
    "backend-ghcr",
    "backend-dockerhub",
    "chart-oci",
    "chart-pages",
)
GPU_TARGETS = {"docker-rtx5070ti", "homelab-rtx2060"}
VERSION_CORE = r"(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)"


def fresh(value: object, now: datetime) -> None:
    try:
        instant = datetime.fromisoformat(cast(str, value).replace("Z", "+00:00"))
        require(
            instant.tzinfo is not None and timedelta(0) <= now - instant <= timedelta(hours=24),
            "Stale or future security evidence",
        )
    except (AttributeError, TypeError, ValueError) as error:
        raise ValueError("Stale or invalid security timestamp") from error


def validate(
    data: dict[str, Any],
    stage: str,
    now: datetime | None = None,
    require_channels: bool = True,
    require_aliases: bool = True,
) -> str:
    now = now or datetime.now(UTC)
    require(
        isinstance(data, dict) and data.get("schema_version") == 1, "Unsupported release inventory"
    )
    require(stage in ("candidate", "stable"), "Unsupported release stage")

    for key in ("candidate_source_sha", "release_source_sha"):
        source(data.get(key))

    for key in ("build_input_identity", "runtime_input_identity"):
        digest(data.get(key))
    if require_channels or data.get("chart_oci_manifest_digest") is not None:
        digest(data.get("chart_oci_manifest_digest"))

    digest(data.get("chart_archive_sha256"), prefix=False)
    require(
        re.fullmatch(VERSION_CORE + r"-rc\.[1-9]\d*", data.get("candidate_version", "")),
        "Invalid candidate version",
    )
    require(re.fullmatch(VERSION_CORE, data.get("release_version", "")), "Invalid stable version")
    require(
        data.get("chart_version")
        == data["candidate_version" if stage == "candidate" else "release_version"],
        "Chart version does not match release stage",
    )

    require(
        isinstance(data.get("base_digests"), dict) and data["base_digests"],
        "Missing base identities",
    )

    for value in data["base_digests"].values():
        digest(value)

    require(
        isinstance(data.get("toolchain_versions"), dict)
        and data["toolchain_versions"].get("go") == "1.27.3",
        "Missing release toolchain identity",
    )

    require(
        data.get("upstream_revision") == "6ac14546046ffb17d144a08bcad65337e535cf5",
        "Native source differs from approved release input",
    )

    images = data.get("image_manifest_digests")
    require(
        isinstance(images, dict) and set(images) == {"api", "backend"},
        "Both image identities are required",
    )

    images = cast(dict[str, str], images)
    for value in images.values():
        digest(value)

    ci = data.get("ci_run_refs")
    require(isinstance(ci, list) and len(ci) == 1, "One exact required CI run is required")

    ci = cast(list[dict[str, Any]], ci)
    run = ci[0]
    require(
        isinstance(run.get("run_id"), int)
        and run["run_id"] > 0
        and run.get("conclusion") == "success",
        "Required CI run did not succeed",
    )

    require(
        run.get("source_sha") == data["release_source_sha"]
        and run.get("build_input_identity") == data["build_input_identity"],
        "CI input drift",
    )

    require(
        isinstance(run.get("jobs"), dict)
        and all(run["jobs"].get(name) == "success" for name in REQUIRED_JOBS),
        "Missing or unsuccessful required CI job",
    )

    reports = data.get("security_report_refs")
    require(
        isinstance(reports, list) and len(reports) == 1,
        "Complete aggregate security evidence is required",
    )

    reports = cast(list[dict[str, Any]], reports)
    report = reports[0]
    require(
        report.get("conclusion") == "success"
        and report.get("source_sha") == data["release_source_sha"]
        and report.get("image_source_sha", report.get("source_sha")) == data["candidate_source_sha"]
        and report.get("build_input_identity") == data["build_input_identity"]
        and report.get("image_manifest_digests") == images
        and report.get("chart_archive_sha256") == data["chart_archive_sha256"],
        "Security evidence drift or failure",
    )
    fresh(report.get("scan_time"), now)
    # Coverage limitations must be explicit; identification gaps cannot silently become clean scans.
    require(report.get("coverage_gaps") == [], "Unresolved security coverage gap")

    channels = data.get("channel_status")
    require(
        isinstance(channels, dict) and set(channels) == set(CHANNELS), "Missing publication channel"
    )

    channels = cast(dict[str, dict[str, Any]], channels)
    for name in CHANNELS:
        observed = channels[name]
        expected = (
            data["chart_archive_sha256"]
            if name.startswith("chart-")
            else images[name.split("-")[0]]
        )
        if require_channels:
            require(
                observed.get("status") == "verified" and observed.get("identity") == expected,
                "Partial publication or wrong read-back identity",
            )
        else:
            require(
                observed.get("status") in ("pending", "failed", "verified"),
                "Invalid publication state",
            )

    if stage == "candidate":
        return "candidate_published_unqualified" if require_channels else "candidate_building"

    require(
        data.get("accepted_image_manifest_digests") == images,
        "Stable images differ from qualified candidate",
    )

    gpu = data.get("gpu_report_refs")
    require(
        isinstance(gpu, list)
        and len(gpu) == 2
        and {item.get("target") for item in gpu} == GPU_TARGETS,
        "Both GPU target reports are required",
    )

    gpu = cast(list[dict[str, Any]], gpu)
    for report in gpu:
        require(
            report.get("conclusion") == "success"
            and report.get("runtime_input_identity") == data["runtime_input_identity"]
            and report.get("image_manifest_digests") == images,
            "GPU evidence drift or failure",
        )

    documentation = data.get("documentation", {})
    require(
        documentation.get("complete") is True
        and documentation.get("source_sha") == data["release_source_sha"],
        "Final documentation is incomplete or stale",
    )

    rendering = data.get("rendered_equivalence", {})
    require(
        rendering.get("verified") is True
        and rendering.get("runtime_input_identity") == data["runtime_input_identity"],
        "Stable chart changes runtime behavior",
    )

    if require_channels and require_aliases:
        aliases = data.get("alias_status", {})
        require(
            set(aliases) == {name for name in CHANNELS if not name.startswith("chart-")},
            "Stable aliases have not been finalized",
        )

        names = stable_aliases(data["release_version"])
        for channel, observed in aliases.items():
            require(
                observed.get("status") == "verified"
                and observed.get("identities")
                == {name: images[channel.split("-")[0]] for name in names},
                "Stable alias read-back is incomplete",
            )

    return "stable_complete" if require_channels and require_aliases else "stable_prepared"


def stable_aliases(version: str) -> tuple[str, str, str]:
    require(re.fullmatch(VERSION_CORE, version), "Stable aliases require a stable version")

    major, minor, _ = version.split(".")

    return (major, major + "." + minor, "latest")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inventory", type=Path, required=True)
    parser.add_argument("--stage", choices=("candidate", "stable"), required=True)
    parser.add_argument(
        "--prepare", action="store_true", help="Validate prerequisites before channel writes"
    )

    args = parser.parse_args()

    try:
        state = validate(
            json.loads(args.inventory.read_bytes()), args.stage, require_channels=not args.prepare
        )
    except OSError, ValueError, KeyError, TypeError, AttributeError:
        print(
            "FAIL: release evidence is incomplete, invalid or does not match selected inputs",
            file=sys.stderr,
        )
        return 1

    print(json.dumps({"state": state, "stage": args.stage}))
    return 0


if __name__ == "__main__":
    sys.exit(main())
