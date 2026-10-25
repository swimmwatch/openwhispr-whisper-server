#!/usr/bin/env python3
"""Assemble release evidence from the selected tag and the actual built artifacts."""

import argparse
import json
import sys
from datetime import datetime
from pathlib import Path
from typing import Any

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from tools.release.artifacts import ROOT, verify_chart
from tools.release.check import CHANNELS, require, validate
from tools.release.prepare import select
from tools.release.storage import save
from tools.security.check import validate as validate_security


def assemble(
    metadata: dict[str, Any],
    builds: dict[str, Any],
    chart: dict[str, Any],
    pins: dict[str, Any],
    approved: dict[str, Any] | None = None,
) -> dict[str, Any]:
    data = dict(approved or {})
    images = {role: builds[role]["manifest_digest"] for role in ("api", "backend")}
    require(
        all(builds[role]["cold"] is True for role in images), "Release requires cold image builds"
    )
    runtime = builds["api"]["runtime_input_identity"]
    require(runtime == builds["backend"]["runtime_input_identity"], "Image runtime inputs differ")
    require(chart["source_sha"] == metadata["source_sha"], "Chart source differs from release tag")
    if metadata["stage"] == "stable":
        require(approved is not None, "Stable release requires reviewed RC acceptance")
        require(data.get("accepted_image_manifest_digests") == images, "Accepted RC images changed")
        require(data.get("runtime_input_identity") == runtime, "Accepted RC runtime changed")
    else:
        data.update(
            candidate_version=metadata["version"], candidate_source_sha=metadata["source_sha"]
        )
    ci = dict(metadata["ci_run_ref"], build_input_identity=builds["api"]["build_input_identity"])
    data.update(
        schema_version=1,
        release_version=metadata["version"].split("-rc.")[0],
        release_source_sha=metadata["source_sha"],
        candidate_run_id=metadata["candidate_run_id"],
        build_input_identity=builds["api"]["build_input_identity"],
        runtime_input_identity=runtime,
        upstream_revision=pins["backend"]["upstream_requested_ref"],
        upstream_commit=pins["backend"]["upstream_commit"],
        base_digests={
            "go": pins["go"]["builder"].split("@", 1)[1],
            "api": pins["api"]["builder_runtime"].split("@", 1)[1],
            "backend": pins["backend"]["cuda_runtime"].split("@", 1)[1],
        },
        toolchain_versions={"go": pins["go"]["version"], "cuda": pins["backend"]["nvcc"]},
        image_manifest_digests=images,
        chart_version=metadata["version"],
        chart_archive_sha256=chart["chart_archive_sha256"],
        chart_oci_manifest_digest=None,
        ci_run_refs=[ci],
        security_report_refs=[],
        gpu_report_refs=data.get("gpu_report_refs", []),
        channel_status={name: {"status": "pending"} for name in CHANNELS},
        alias_status={},
        rendered_equivalence={},
    )
    if metadata["stage"] == "stable":
        documentation = dict(data.get("documentation", {}))
        require(documentation.get("complete") is True, "Reviewed final documentation is required")
        data["documentation"] = dict(documentation, source_sha=metadata["source_sha"])
    return data


def qualify(
    data: dict[str, Any], security: dict[str, Any], stage: str, now: datetime | None = None
) -> None:
    summary = validate_security(security, now)
    summary["build_input_identity"] = security["build_input_identity"]
    data["security_report_refs"] = [summary]
    validate(data, stage, now, require_channels=False)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("assemble", "qualify"))
    parser.add_argument("--inventory", type=Path, required=True)
    parser.add_argument("--source", type=Path)
    parser.add_argument("--current", type=Path)
    parser.add_argument("--accepted", type=Path)
    parser.add_argument("--approved", type=Path)
    parser.add_argument("--security", type=Path)
    parser.add_argument("--stage", choices=("candidate", "stable"))
    parser.add_argument("--output", type=Path)
    parser.add_argument("--helm")
    args = parser.parse_args()
    try:
        if args.action == "assemble":
            if (
                args.source is None
                or args.current is None
                or args.accepted is None
                or args.output is None
                or args.helm is None
            ):
                raise ValueError("Artifact inputs are required")
            data = assemble(
                json.loads(args.source.read_bytes()),
                {
                    role: json.loads((args.accepted / role / "image.json").read_bytes())
                    for role in ("api", "backend")
                },
                verify_chart(args.current / "chart"),
                json.loads((ROOT / "containers/build-inputs.json").read_bytes()),
                json.loads(args.approved.read_bytes()) if args.approved else None,
            )
            metadata = json.loads(args.source.read_bytes())
            select(data, metadata["stage"], args.current, args.accepted, args.output, args.helm)
            if metadata["stage"] == "stable":
                data["rendered_equivalence"] = {
                    "verified": True,
                    "runtime_input_identity": data["runtime_input_identity"],
                }
        else:
            if args.security is None or args.stage is None:
                raise ValueError("Security evidence and stage are required")
            data = json.loads(args.inventory.read_bytes())
            qualify(data, json.loads(args.security.read_bytes()), args.stage)
        args.inventory.parent.mkdir(parents=True, exist_ok=True)
        save(args.inventory, data)
    except OSError, ValueError, KeyError, TypeError:
        print("FAIL: release artifacts or qualification do not match", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
