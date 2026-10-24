#!/usr/bin/env python3
"""Reconcile selected release evidence with the actual trusted GitHub CI run."""

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path
from typing import Any

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from tools.release.check import REQUIRED_JOBS, require, validate
from tools.security.check import validate as security_check

ROOT = Path(__file__).resolve().parents[2]


def verify_inventory_revision(
    data: dict[str, Any], inventory: Path, approval_sha: str, root: Path = ROOT
) -> None:
    """Allow only the reviewed inventory to differ from the artifact source."""
    root = Path(root)
    require(not (root / inventory).is_symlink(), "Inventory must be a committed regular file")

    def git(*args: str) -> str:
        result = subprocess.run(["git", "-C", str(root), *args], capture_output=True, timeout=90)
        require(result.returncode == 0, "Approval Git evidence unavailable")

        return result.stdout.decode()

    require(git("rev-parse", "HEAD").strip() == approval_sha, "Approval checkout mismatch")
    require(
        git("status", "--porcelain", "--untracked-files=all").strip() == "",
        "Approval checkout is modified",
    )

    artifact_sha = data["release_source_sha"]
    exists = subprocess.run(
        ["git", "-C", str(root), "cat-file", "-e", artifact_sha + "^{commit}"],
        capture_output=True,
        timeout=90,
    )
    if exists.returncode != 0:
        git("fetch", "--no-tags", "--depth=1", "origin", artifact_sha)

    changed = set(
        git("diff", "--name-only", "--no-renames", "-z", artifact_sha, approval_sha).split("\0")
    ) - {""}
    require(
        changed <= {Path(inventory).as_posix()}, "Approval commit changes unverified release inputs"
    )
    git("ls-files", "--error-unmatch", "--", str(inventory))


def api(path: str) -> dict[str, Any]:
    result = subprocess.run(["gh", "api", path], capture_output=True, timeout=90)
    require(result.returncode == 0, "GitHub CI evidence unavailable")

    return json.loads(result.stdout)


def verify_run(
    run: dict[str, Any], jobs: list[dict[str, Any]], expected_sha: str, expected_run: int
) -> None:
    require(
        run.get("event") == "push"
        and run.get("head_branch") == "main"
        and run.get("conclusion") == "success"
        and run.get("path") == ".github/workflows/ci.yml"
        and run.get("head_sha") == expected_sha
        and run.get("id") == expected_run,
        "Selected CI run is not a successful trusted source run",
    )

    actual = {job["name"]: job["conclusion"] for job in jobs}
    require(
        all(actual.get(name) == "success" for name in REQUIRED_JOBS),
        "Missing or unsuccessful actual CI job",
    )


def verify(
    data: dict[str, Any],
    run: dict[str, Any],
    jobs: list[dict[str, Any]],
    security: dict[str, Any],
    checkout_sha: str,
    stage: str = "candidate",
) -> None:
    require(checkout_sha == data["release_source_sha"], "Artifact source mismatch")
    verify_run(run, jobs, checkout_sha, data["ci_run_refs"][0]["run_id"])

    checked = security_check(security)
    require(
        checked["source_sha"] == checkout_sha
        and checked["image_source_sha"] == checkout_sha
        and checked["chart_archive_sha256"] == data["chart_archive_sha256"],
        "Downloaded security evidence mismatch",
    )

    if stage == "candidate":
        require(
            checked["image_manifest_digests"] == data["image_manifest_digests"],
            "Candidate image evidence mismatch",
        )


def jobs_for(base: str) -> list[dict[str, Any]]:
    jobs, page = [], 1
    while True:
        batch = api(base + "/jobs?per_page=100&page=" + str(page))["jobs"]
        jobs.extend(batch)

        if len(batch) < 100:
            return jobs

        page += 1


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inventory", type=Path, required=True)
    parser.add_argument("--security", type=Path, required=True)
    parser.add_argument("--stage", choices=("candidate", "stable"), required=True)
    parser.add_argument("--selected-security", type=Path)

    args = parser.parse_args()

    try:
        require(
            os.environ.get("GITHUB_REPOSITORY") == "swimmwatch/openwhispr-whisper-server"
            and os.environ.get("GITHUB_REF") == "refs/heads/main",
            "Trusted main publisher required",
        )

        data = json.loads(args.inventory.read_bytes())
        validate(data, args.stage, require_channels=False)
        verify_inventory_revision(data, args.inventory, os.environ["GITHUB_SHA"])

        run_id = data["ci_run_refs"][0]["run_id"]
        base = "repos/swimmwatch/openwhispr-whisper-server/actions/runs/" + str(run_id)
        run = api(base)
        verify(
            data,
            run,
            jobs_for(base),
            json.loads(args.security.read_bytes()),
            data["release_source_sha"],
            args.stage,
        )

        if args.stage == "stable":
            candidate_run = data.get("candidate_ci_run_id")
            require(
                type(candidate_run) is int and candidate_run > 0,
                "Accepted candidate CI run is required",
            )

            candidate_base = "repos/swimmwatch/openwhispr-whisper-server/actions/runs/" + str(
                candidate_run
            )
            verify_run(
                api(candidate_base),
                jobs_for(candidate_base),
                data["candidate_source_sha"],
                candidate_run,
            )
            require(
                args.selected_security is not None, "Stable requires fresh accepted-image scans"
            )

        if args.selected_security is not None:
            selected = security_check(json.loads(args.selected_security.read_bytes()))
            require(
                selected["source_sha"] == data["release_source_sha"]
                and selected["image_source_sha"] == data["candidate_source_sha"]
                and selected["image_manifest_digests"] == data["image_manifest_digests"]
                and selected["chart_archive_sha256"] == data["chart_archive_sha256"],
                "Selected release scan drift",
            )
    except OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError:
        print(
            "FAIL: actual trusted CI/security evidence did not match the selected release",
            file=sys.stderr,
        )
        return 1

    print("PASS: selected release matches actual trusted CI and downloaded security evidence")
    return 0


if __name__ == "__main__":
    sys.exit(main())
