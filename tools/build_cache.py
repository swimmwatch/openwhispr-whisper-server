#!/usr/bin/env python3
"""Select immutable BuildKit cache inputs; promote only after caller OCI verification."""

import argparse
import json
import os
import re
import subprocess
import sys
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

_COMPONENT = r"[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*"
_PREFIX = rf"ghcr\.io/{_COMPONENT}/{_COMPONENT}-buildcache"
_DIGEST = r"sha256:[0-9a-f]{64}"
_STAGING = r"run-[1-9][0-9]*-[1-9][0-9]*-linux-amd64-v1"
_MAIN_TAG = "main-linux-amd64-v1"
_PLATFORM = "linux/amd64"
_NATIVE_TIMEOUT = 120


def _require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def _repository(value: str) -> str:
    _require(re.fullmatch(_PREFIX, value) is not None, "Invalid cache repository prefix")
    return value


def validate_record(
    data: object,
    *,
    cold: bool = False,
    role: str | None = None,
    repository: str | None = None,
) -> dict[str, Any]:
    """Validate artifact structure, not provenance; supplied repository binds its namespace.

    This function does not consult the environment or resolve remote references.
    Without repository, reference grammar and same-package coherence are checked only.
    """
    if not isinstance(data, dict):
        raise ValueError("Cache record must be an object")
    required = {"schema_version", "policy", "role", "platform", "status", "sources"}
    _require(required <= data.keys() <= required | {"export_ref"}, "Invalid cache record keys")
    _require(type(data["schema_version"]) is int and data["schema_version"] == 1, "Invalid schema")
    actual_role = data["role"]
    _require(type(actual_role) is str and actual_role in ("api", "backend"), "Invalid cache role")
    if role is not None:
        _require(role in ("api", "backend") and actual_role == role, "Cache role mismatch")
    _require(type(data["platform"]) is str and data["platform"] == _PLATFORM, "Invalid platform")
    policy, status, sources = data["policy"], data["status"], data["sources"]
    _require(type(policy) is str and policy in ("disabled", "trusted-main-v1"), "Invalid policy")
    _require(
        type(status) is str and status in ("disabled", "available", "unavailable"),
        "Invalid cache status",
    )
    _require(type(sources) is list and len(sources) <= 1, "Invalid cache sources")
    prefix = _repository(repository) if repository is not None else None
    package: str | None = None
    for source in sources:
        _require(type(source) is str, "Cache source must be a string")
        match = re.fullmatch(rf"({_PREFIX}-{actual_role})@{_DIGEST}", source)
        _require(match is not None, "Cache source must be an immutable same-role GHCR reference")
        if match is not None:
            package = match[1]
    if "export_ref" in data:
        exporter = data["export_ref"]
        _require(type(exporter) is str, "Cache exporter must be a string")
        match = re.fullmatch(rf"({_PREFIX}-{actual_role}):{_STAGING}", exporter)
        _require(match is not None, "Invalid staging cache exporter")
        if match is not None:
            _require(package is None or package == match[1], "Cache package mismatch")
            package = match[1]
    if prefix is not None and package is not None:
        _require(package == f"{prefix}-{actual_role}", "Cache repository mismatch")
    if policy == "disabled":
        _require(
            status == "disabled" and not sources and "export_ref" not in data, "Cache is disabled"
        )
    else:
        _require(
            (status == "available" and len(sources) == 1)
            or (status == "unavailable" and not sources),
            "Cache status and sources disagree",
        )
    _require(not cold or policy == "disabled", "Cold builds cannot use caches")
    return {**data, "sources": list(sources)}


def _expected_repository(environ: Mapping[str, str]) -> str:
    repository = environ.get("GITHUB_REPOSITORY", "").lower()
    return _repository(f"ghcr.io/{repository}-buildcache")


def _staging_ref(repository: str, role: str, environ: Mapping[str, str]) -> str:
    _require(
        environ.get("GITHUB_EVENT_NAME") in ("push", "workflow_dispatch"),
        "Cache writes require a trusted branch event",
    )
    branch = environ.get("GITHUB_DEFAULT_BRANCH", "")
    _require(
        bool(branch) and environ.get("GITHUB_REF") == f"refs/heads/{branch}",
        "Cache writes require the default branch",
    )
    run, attempt = environ.get("GITHUB_RUN_ID", ""), environ.get("GITHUB_RUN_ATTEMPT", "")
    _require(
        re.fullmatch(r"[1-9][0-9]*", run) is not None
        and re.fullmatch(r"[1-9][0-9]*", attempt) is not None,
        "Cache writes require positive run identifiers",
    )
    return f"{repository}-{role}:run-{run}-{attempt}-linux-amd64-v1"


def _native(arguments: Sequence[str]) -> str:
    """Keep native diagnostics private: registry errors may contain credentials."""
    try:
        result = subprocess.run(
            ["docker", "buildx", "imagetools", *arguments],
            check=True,
            capture_output=True,
            text=True,
            timeout=_NATIVE_TIMEOUT,
        )
    except OSError, UnicodeError, subprocess.SubprocessError:
        raise RuntimeError("Native cache operation failed") from None
    return result.stdout


def _inspect_digest(reference: str) -> str:
    digest = _native(["inspect", reference, "--format", "{{.Manifest.Digest}}"]).strip()
    _require(re.fullmatch(_DIGEST, digest) is not None, "Native cache digest is invalid")
    return digest


def prepare(
    *,
    role: str,
    repository: str,
    platform: str,
    access: str,
    environ: Mapping[str, str],
) -> dict[str, Any]:
    """Resolve the shared main tag without treating availability as a layer hit."""
    _require(repository == _expected_repository(environ), "Cache repository mismatch")
    _require(access in ("none", "read", "write"), "Invalid cache access")
    record: dict[str, Any] = {
        "schema_version": 1,
        "policy": "disabled" if access == "none" else "trusted-main-v1",
        "role": role,
        "platform": platform,
        "status": "disabled" if access == "none" else "unavailable",
        "sources": [],
    }
    validate_record(record, role=role, repository=repository)
    if access == "none":
        return record
    if access == "write":
        record["export_ref"] = _staging_ref(repository, role, environ)
    try:
        digest = _inspect_digest(f"{repository}-{role}:{_MAIN_TAG}")
    except RuntimeError:
        print(
            "::warning::Build cache is unavailable; continuing without imported layers.",
            file=sys.stderr,
        )
    else:
        record["status"] = "available"
        record["sources"] = [f"{repository}-{role}@{digest}"]
    return validate_record(record, role=role, repository=repository)


def promote(data: object, *, environ: Mapping[str, str]) -> None:
    """Advance main to this run's staging digest after the caller verified its OCI export."""
    repository = _expected_repository(environ)
    record = validate_record(data, repository=repository)
    staging = _staging_ref(repository, record["role"], environ)
    _require(record.get("export_ref") == staging, "Promotion requires this run's cache exporter")
    digest = _inspect_digest(staging)
    target = f"{repository}-{record['role']}:{_MAIN_TAG}"
    _native(["create", "--prefer-index=false", "--tag", target, f"{staging}@{digest}"])
    _require(_inspect_digest(target) == digest, "Promoted cache digest differs from staging")


@dataclass(frozen=True)
class CacheVersion:
    identifier: int
    digest: str
    created: datetime
    updated: datetime
    tags: tuple[str, ...]


def _retention_context(environ: Mapping[str, str]) -> tuple[str, str]:
    """Bind REST paths to the caller's own default-branch repository, never CLI input."""
    prefix = _expected_repository(environ)
    repository = environ.get("GITHUB_REPOSITORY", "")
    _require(
        environ.get("GITHUB_EVENT_REPOSITORY") == repository,
        "Retention requires the event's own repository",
    )
    _require(
        environ.get("GITHUB_EVENT_NAME") in ("push", "workflow_dispatch", "schedule"),
        "Retention requires a trusted branch event",
    )
    branch = environ.get("GITHUB_DEFAULT_BRANCH", "")
    _require(
        bool(branch) and environ.get("GITHUB_REF") == f"refs/heads/{branch}",
        "Retention requires the default branch",
    )
    owner_type = environ.get("GITHUB_REPOSITORY_OWNER_TYPE", "")
    _require(owner_type in ("User", "Organization"), "Invalid repository owner type")
    owner, package = prefix.removeprefix("ghcr.io/").split("/")
    endpoint = "users" if owner_type == "User" else "orgs"
    return f"/{endpoint}/{owner}/packages/container", package


def _gh_api(endpoint: str, *, method: str = "GET", paginate: bool = False) -> str:
    """Use official GitHub CLI REST transport without publishing native diagnostics."""
    arguments = [
        "gh",
        "api",
        "--hostname",
        "github.com",
        "--method",
        method,
        "--header",
        "Accept: application/vnd.github+json",
        "--header",
        "X-GitHub-Api-Version: 2022-11-28",
    ]
    if paginate:
        arguments.extend(("--paginate", "--slurp"))
    arguments.append(endpoint)
    try:
        result = subprocess.run(
            arguments,
            check=True,
            capture_output=True,
            text=True,
            timeout=_NATIVE_TIMEOUT,
        )
    except OSError, UnicodeError, subprocess.SubprocessError:
        raise RuntimeError("GitHub cache retention operation failed") from None
    return result.stdout


def _retention_timestamp(value: object, now: datetime) -> datetime:
    _require(
        isinstance(value, str)
        and re.fullmatch(
            r"\d{4}-\d{2}-\d{2}T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d"
            r"(?:\.\d+)?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)",
            value,
        )
        is not None,
        "Invalid cache version timestamp",
    )
    timestamp = datetime.fromisoformat(str(value))
    _require(timestamp.utcoffset() is not None and timestamp <= now, "Unsafe cache timestamp")
    return timestamp


def _cache_version(data: object, now: datetime) -> CacheVersion:
    if not isinstance(data, dict):
        raise ValueError("Cache version must be an object")
    identifier, digest = data.get("id"), data.get("name")
    if type(identifier) is not int or identifier <= 0:
        raise ValueError("Invalid cache version ID")
    if not isinstance(digest, str) or re.fullmatch(_DIGEST, digest) is None:
        raise ValueError("Invalid cache version digest")
    created = _retention_timestamp(data.get("created_at"), now)
    updated = _retention_timestamp(data.get("updated_at"), now)
    _require(updated >= created, "Cache timestamps are inconsistent")
    metadata = data.get("metadata")
    if not isinstance(metadata, dict):
        raise ValueError("Missing cache version metadata")
    _require(metadata.get("package_type") == "container", "Not a container cache version")
    container = metadata.get("container")
    _require(isinstance(container, dict), "Missing cache container metadata")
    tags = container.get("tags") if isinstance(container, dict) else None
    if not isinstance(tags, list) or not all(isinstance(tag, str) and bool(tag) for tag in tags):
        raise ValueError("Invalid cache version tags")
    return CacheVersion(identifier, digest, created, updated, tuple(tags))


def _retention_versions(endpoint: str, now: datetime) -> list[CacheVersion]:
    pages = json.loads(_gh_api(f"{endpoint}?per_page=100&state=active", paginate=True))
    _require(
        isinstance(pages, list) and all(isinstance(page, list) for page in pages),
        "Invalid paginated cache versions",
    )
    versions = [_cache_version(item, now) for page in pages for item in page]
    _require(
        len({version.identifier for version in versions}) == len(versions),
        "Duplicate cache version IDs",
    )
    _require(
        len({version.digest for version in versions}) == len(versions),
        "Duplicate cache version digests",
    )
    return versions


def _retention_candidates(versions: list[CacheVersion], now: datetime) -> list[CacheVersion]:
    main = [version for version in versions if _MAIN_TAG in version.tags]
    _require(len(main) == 1, "Retention requires exactly one current main cache digest")
    newest = sorted(
        versions, key=lambda version: (version.created, version.identifier), reverse=True
    )
    protected = {version.digest for version in newest[:3]} | {main[0].digest}
    candidates = []
    for version in versions:
        if version.digest in protected:
            continue
        if not version.tags:
            age = timedelta(days=14)
        elif all(re.fullmatch(_STAGING, tag) is not None for tag in version.tags):
            age = timedelta(days=7)
        else:
            # Release, main, foreign epoch/platform and unknown tags always retain the digest.
            continue
        if now - version.updated > age:
            candidates.append(version)
    return sorted(candidates, key=lambda version: (version.updated, version.identifier))


def retain(*, environ: Mapping[str, str], apply: bool = False) -> dict[str, Any]:
    """Plan both cache packages before mutation; default to read-only, capped retention.

    The maintenance caller must share the cache writer's concurrency group. REST has no
    conditional DELETE: candidate readback detects drift but cannot make deletion atomic.
    """
    root, package = _retention_context(environ)
    now = datetime.now(UTC)
    selected: list[tuple[str, str, CacheVersion]] = []
    sources = []
    for role in ("api", "backend"):
        endpoint = f"{root}/{package}-{role}/versions"
        versions = _retention_versions(endpoint, now)
        candidates = _retention_candidates(versions, now)
        sources.append(
            {
                "role": role,
                "versions": len(versions),
                "eligible": len(candidates),
                "retained": len(versions) - len(candidates),
            }
        )
        selected.extend((role, endpoint, version) for version in candidates)
    selected.sort(key=lambda item: (item[2].updated, item[0], item[2].identifier))
    eligible = len(selected)
    selected = selected[:100]
    if apply:
        # Re-read every selected ID before the first DELETE; never consume IDs from a plan file.
        for _, endpoint, version in selected:
            current = _cache_version(json.loads(_gh_api(f"{endpoint}/{version.identifier}")), now)
            _require(current == version, "Cache candidate changed since listing")
        for _, endpoint, version in selected:
            _require(_retention_context(environ) == (root, package), "Retention context changed")
            _gh_api(f"{endpoint}/{version.identifier}", method="DELETE")
    return {
        "schema_version": 1,
        "policy": "trusted-main-v1",
        "mode": "apply" if apply else "dry-run",
        "sources": sources,
        "eligible": eligible,
        "selected": len(selected),
        "deferred": eligible - len(selected),
        "deleted": len(selected) if apply else 0,
        "candidates": [
            {"role": role, "id": version.identifier, "digest": version.digest}
            for role, _, version in selected
        ],
    }


def _write_outputs(record: dict[str, Any], path: Path) -> None:
    sources = record["sources"]
    cache_from = f"type=registry,ref={sources[0]}" if sources else ""
    exporter = record.get("export_ref")
    cache_to = (
        f"type=registry,ref={exporter},mode=max,oci-mediatypes=true,image-manifest=true,"
        "compression=zstd,compression-level=3"
        if exporter is not None
        else ""
    )
    with path.open("a", encoding="utf-8") as output:
        output.write(f"cache-from={cache_from}\ncache-to={cache_to}\n")


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    preparation = commands.add_parser("prepare")
    preparation.add_argument("--role", choices=("api", "backend"), required=True)
    preparation.add_argument("--repository", required=True)
    preparation.add_argument("--platform", choices=(_PLATFORM,), required=True)
    preparation.add_argument("--access", choices=("none", "read", "write"), required=True)
    preparation.add_argument("--output", type=Path, required=True)
    preparation.add_argument("--github-output", type=Path, required=True)
    promotion = commands.add_parser("promote", help="Run only after successful OCI verification")
    promotion.add_argument("--record", type=Path, required=True)
    retention = commands.add_parser("retain", help="Plan cache retention; delete only with --apply")
    retention.add_argument("--apply", action="store_true")
    retention.add_argument("--output", type=Path, required=True)
    arguments = parser.parse_args(argv)
    try:
        if arguments.command == "prepare":
            record = prepare(
                role=arguments.role,
                repository=arguments.repository,
                platform=arguments.platform,
                access=arguments.access,
                environ=os.environ,
            )
            arguments.output.write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8")
            _write_outputs(record, arguments.github_output)
        elif arguments.command == "promote":
            promote(json.loads(arguments.record.read_text(encoding="utf-8")), environ=os.environ)
        else:
            report = retain(environ=os.environ, apply=arguments.apply)
            arguments.output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
            print(
                f"Cache retention {report['mode']}: {report['eligible']} eligible, "
                f"{report['selected']} selected, {report['deleted']} deleted, "
                f"{report['deferred']} deferred."
            )
    except OSError, UnicodeError, ValueError, RuntimeError:
        print(
            "::error::Build cache operation failed; check policy, record and native tooling.",
            file=sys.stderr,
        )
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
