#!/usr/bin/env python3
"""Resolve a release tag and the successful PR checks for its merged source."""

import argparse
import json
import os
import re
import subprocess
import sys
from pathlib import Path
from typing import Any

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from tools.release.check import REQUIRED_JOBS, VERSION_CORE, require
from tools.release.storage import save

ROOT = Path(__file__).resolve().parents[2]


def release_version(tag: str) -> tuple[str, str]:
    require(re.fullmatch("v" + VERSION_CORE + r"(?:-rc\.[1-9]\d*)?", tag), "Invalid release tag")
    version = tag.removeprefix("v")
    return version, "candidate" if "-rc." in version else "stable"


def git(*args: str, root: Path = ROOT) -> str:
    result = subprocess.run(["git", *args], cwd=root, capture_output=True, timeout=90)
    require(result.returncode == 0, "Release Git source is unavailable or outside main")
    return result.stdout.decode().strip()


def resolve_tag(tag: str, root: Path = ROOT) -> str:
    release_version(tag)
    source_sha = git("rev-parse", "refs/tags/" + tag + "^{commit}", root=root)
    git("merge-base", "--is-ancestor", source_sha, "origin/main", root=root)
    return source_sha


def api(path: str) -> Any:
    result = subprocess.run(["gh", "api", path], capture_output=True, timeout=90)
    require(result.returncode == 0, "GitHub release source evidence is unavailable")
    return json.loads(result.stdout)


def verify_pr_run(
    pull_request: dict[str, Any],
    run: dict[str, Any],
    jobs: list[dict[str, Any]],
    source_sha: str,
) -> dict[str, Any]:
    require(
        pull_request.get("merged_at")
        and pull_request.get("merge_commit_sha") == source_sha
        and pull_request["base"]["ref"] == "main"
        and run.get("event") == "pull_request"
        and run.get("head_sha") == pull_request["head"]["sha"]
        and run.get("path") == ".github/workflows/ci.yml"
        and run.get("conclusion") == "success",
        "Release source requires a merged main PR with successful CI",
    )
    actual = {job["name"]: job["conclusion"] for job in jobs}
    require(
        all(actual.get(name) == "success" for name in REQUIRED_JOBS), "Required PR check failed"
    )
    return {
        "run_id": run["id"],
        "source_sha": source_sha,
        "tested_head_sha": run["head_sha"],
        "pull_request": pull_request["number"],
        "merge_commit_sha": pull_request["merge_commit_sha"],
        "conclusion": run["conclusion"],
        "jobs": actual,
    }


def qualify_source(repository: str, source_sha: str) -> dict[str, Any]:
    # This relies on main's required up-to-date PR checks. Bypass merges need
    # independent validation of the selected release source before publication.
    base = "repos/" + repository
    pulls = api(base + "/commits/" + source_sha + "/pulls?per_page=100")
    matches = [
        item
        for item in pulls
        if item.get("merged_at")
        and item.get("merge_commit_sha") == source_sha
        and item["base"]["ref"] == "main"
    ]
    require(bool(matches), "Release tag must identify a merged main PR")
    pull = matches[0]
    runs = api(
        base
        + "/actions/workflows/ci.yml/runs?event=pull_request&head_sha="
        + pull["head"]["sha"]
        + "&per_page=100"
    )["workflow_runs"]
    require(bool(runs), "Successful PR CI is required before a release")
    run = runs[0]
    jobs: list[dict[str, Any]] = []
    page = 1
    while True:
        batch = api(base + f"/actions/runs/{run['id']}/jobs?per_page=100&page={page}")["jobs"]
        jobs.extend(batch)
        if len(batch) < 100:
            break
        page += 1
    return verify_pr_run(pull, run, jobs, source_sha)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--github-output", type=Path, required=True)
    args = parser.parse_args()
    try:
        version, stage = release_version(args.tag)
        source_sha = resolve_tag(args.tag)
        if os.environ.get("GITHUB_EVENT_NAME") == "release":
            event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_bytes())
            require(
                event["release"]["prerelease"] == (stage == "candidate"), "Release mode mismatch"
            )
        qualification = qualify_source(args.repository, source_sha)
        args.output.mkdir(parents=True, exist_ok=True)
        image_run = int(os.environ["GITHUB_RUN_ID"])
        image_source = source_sha
        if stage == "stable":
            approved = json.loads(git("show", f"{source_sha}:docs/releases/{version}.json"))
            image_run = approved["candidate_run_id"]
            require(type(image_run) is int and image_run > 0, "Accepted RC run is required")
            image_source = approved["candidate_source_sha"]
            save(args.output / "approved.json", approved)
        metadata = {
            "tag": args.tag,
            "version": version,
            "stage": stage,
            "source_sha": source_sha,
            "image_source_sha": image_source,
            "candidate_run_id": image_run,
            "ci_run_ref": qualification,
        }
        save(args.output / "source.json", metadata)
        outputs = {
            "tag": args.tag,
            "version": version,
            "stage": stage,
            "source-sha": source_sha,
            "image-source": image_source,
            "images-run": image_run,
        }
        with args.github_output.open("a", encoding="utf-8") as stream:
            for key, value in outputs.items():
                stream.write(f"{key}={value}\n")
    except OSError, ValueError, KeyError, TypeError, IndexError, subprocess.SubprocessError:
        print("FAIL: release tag or PR qualification is unavailable", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
