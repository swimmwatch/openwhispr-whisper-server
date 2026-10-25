#!/usr/bin/env python3
"""Publish selected immutable artifacts and advance channels only after read-back."""

import argparse
import hashlib
import json
import re
import subprocess
import sys
import tempfile
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from collections.abc import Callable, Sequence
from datetime import datetime
from typing import Any, cast

import yaml

from tools.release.artifacts import inspect_layout
from tools.release.check import (
    digest,
    immutable_copy,
    merge_index,
    require,
    stable_aliases,
    validate,
)
from tools.release.storage import save as save

REPOSITORIES = {
    "api-ghcr": "ghcr.io/swimmwatch/openwhispr-whisper-server-api",
    "backend-ghcr": "ghcr.io/swimmwatch/openwhispr-whisper-server-backend",
    "api-dockerhub": "docker.io/swimmwatch/openwhispr-whisper-server-api",
    "backend-dockerhub": "docker.io/swimmwatch/openwhispr-whisper-server-backend",
}
CHART = "ghcr.io/swimmwatch/charts/whisper-server"
PAGE_URL = "https://swimmwatch.github.io/openwhispr-whisper-server/"
ACCEPT = ",".join(
    [
        "application/vnd.oci.image.index.v1+json",
        "application/vnd.oci.image.manifest.v1+json",
        "application/vnd.docker.distribution.manifest.list.v2+json",
        "application/vnd.docker.distribution.manifest.v2+json",
    ]
)


def command(args: Sequence[str | Path]) -> bytes:
    result = subprocess.run([str(item) for item in args], capture_output=True, timeout=1800)
    require(result.returncode == 0, "Publisher command failed; raw output suppressed")

    return result.stdout


def registry_get(repository: str, category: str, reference: str) -> bytes | None:
    host, path = repository.split("/", 1)
    require(host in ("ghcr.io", "docker.io"), "Unsupported publication registry")

    endpoint = "registry-1.docker.io" if host == "docker.io" else host
    url = (
        "https://"
        + endpoint
        + "/v2/"
        + path
        + "/"
        + category
        + "/"
        + urllib.parse.quote(reference, safe=":")
    )
    request = urllib.request.Request(url, headers={"Accept": ACCEPT})

    try:
        with urllib.request.urlopen(request, timeout=120) as response:
            return response.read()
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return None

        require(error.code == 401, "Registry read failed")

        challenge = error.headers.get("WWW-Authenticate", "")

    fields = dict(re.findall(r'(\w+)="([^"]*)"', challenge))
    realm = fields.get("realm", "")
    require(
        urllib.parse.urlsplit(realm).netloc in ("ghcr.io", "auth.docker.io")
        and realm.startswith("https://"),
        "Unsupported registry authentication realm",
    )

    query = urllib.parse.urlencode(
        {"service": fields.get("service", ""), "scope": "repository:" + path + ":pull"}
    )

    with urllib.request.urlopen(realm + "?" + query, timeout=120) as response:
        credentials = json.load(response)

    token = credentials.get("token", credentials.get("access_token"))
    require(isinstance(token, str) and token, "Anonymous registry token unavailable")
    request.add_header("Authorization", "Bearer " + token)

    try:
        with urllib.request.urlopen(request, timeout=120) as response:
            return response.read()
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return None

        raise ValueError("Anonymous registry read failed") from None


def manifest(repository: str, reference: str) -> bytes | None:
    return registry_get(repository, "manifests", reference)


def publish_image(layout: Path, repository: str, version: str, expected: str) -> None:
    digest(expected)

    existing = manifest(repository, version)
    if existing is not None:
        require(
            "sha256:" + hashlib.sha256(existing).hexdigest() == expected,
            "Immutable image version collision",
        )

        return

    command(
        [
            "buildx",
            "imagetools",
            "create",
            "--prefer-index=false",
            "--tag",
            repository + ":" + version,
            "oci-layout://" + str(layout.resolve()) + "@" + expected,
        ]
    )

    observed = manifest(repository, version)
    require(
        observed is not None and "sha256:" + hashlib.sha256(observed).hexdigest() == expected,
        "Published image read-back mismatch",
    )


def publish_chart(
    archive: Path, version: str, archive_sha: str, manifest_sha: str | None, helm: str | Path
) -> str:
    require(
        hashlib.sha256(archive.read_bytes()).hexdigest() == archive_sha,
        "Chart source identity mismatch",
    )

    existing = manifest(CHART, version)
    if existing is None:
        command([helm, "push", archive, "oci://ghcr.io/swimmwatch/charts"])

        existing = manifest(CHART, version)

    require(existing is not None, "Published chart manifest is unavailable")
    observed = "sha256:" + hashlib.sha256(cast(bytes, existing)).hexdigest()
    require(manifest_sha is None or observed == manifest_sha, "Immutable chart manifest collision")

    layers = json.loads(cast(bytes, existing))["layers"]
    layers = [
        item
        for item in layers
        if item.get("mediaType") == "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
    ]
    require(len(layers) == 1, "Unsupported chart content layers")

    content = registry_get(CHART, "blobs", layers[0]["digest"])
    require(
        content is not None and hashlib.sha256(content).hexdigest() == archive_sha,
        "Chart archive read-back mismatch",
    )
    return observed


def prepare_pages(
    archive: Path, version: str, expected: str, directory: Path, helm: str | Path
) -> None:
    directory.mkdir(parents=True, exist_ok=True)

    target = directory / ("whisper-server-" + version + ".tgz")
    immutable_copy(archive, target, expected)

    previous = (
        yaml.safe_load((directory / "index.yaml").read_text())
        if (directory / "index.yaml").exists()
        else {"apiVersion": "v1", "entries": {}}
    )

    with tempfile.TemporaryDirectory(prefix="whisper-index-") as td:
        isolated = Path(td)
        immutable_copy(archive, isolated / target.name, expected)
        command([helm, "repo", "index", isolated, "--url", PAGE_URL])

        incoming = yaml.safe_load((isolated / "index.yaml").read_text())

    merged = merge_index(previous, incoming)
    (directory / "index.yaml").write_text(yaml.safe_dump(merged, sort_keys=False))


def prepare_aliases(
    data: dict[str, Any],
    artifacts: Path,
    now: datetime | None = None,
    conservative_rollback: bool = False,
) -> tuple[str, str, str]:
    """Snapshot qualified aliases before writes; retain the original retry baseline."""
    validate(data, "stable", now, require_aliases=False)

    aliases = stable_aliases(data["release_version"])
    previous = {}
    retained = data.get("alias_status", {})
    for channel, repository in REPOSITORIES.items():
        role = channel.split("-")[0]
        actual = inspect_layout(artifacts / role / "layout", data["candidate_source_sha"])
        require(
            actual["manifest_digest"] == data["image_manifest_digests"][role],
            "Alias input identity drift",
        )

        for alias in aliases:
            saved = retained.get(channel, {}).get("previous_identities", {})
            if alias in saved:
                previous[channel, alias] = saved[alias]
                if saved[alias] is not None:
                    digest(saved[alias])
            else:
                observed = manifest(repository, alias)
                previous[channel, alias] = (
                    "sha256:" + hashlib.sha256(observed).hexdigest()
                    if observed is not None
                    else None
                )

    data["alias_status"] = {
        channel: {
            "status": "pending",
            "identities": {},
            "rollback": {},
            "previous_identities": {alias: previous[channel, alias] for alias in aliases},
            "touched": list(aliases)
            if conservative_rollback
            else retained.get(channel, {}).get("touched", []),
        }
        for channel in REPOSITORIES
    }

    return aliases


def finalize_aliases(
    data: dict[str, Any],
    artifacts: Path,
    now: datetime | None = None,
    checkpoint: Callable[[], None] = lambda: None,
) -> None:
    """Advance only qualified stable images; retain explicit partial rollback state."""
    aliases = prepare_aliases(data, artifacts, now)
    previous = {
        (channel, alias): state["previous_identities"][alias]
        for channel, state in data["alias_status"].items()
        for alias in aliases
    }
    touched = [
        (channel, alias)
        for channel in REPOSITORIES
        for alias in data["alias_status"][channel]["touched"]
    ]
    require(all(alias in aliases for _, alias in touched), "Invalid retained alias state")
    checkpoint()

    try:
        for channel, repository in REPOSITORIES.items():
            role = channel.split("-")[0]
            expected = data["image_manifest_digests"][role]
            layout = artifacts / role / "layout"
            for alias in aliases:
                # Record before dispatch: even a failed request can have reached the registry.
                if (channel, alias) not in touched:
                    touched.append((channel, alias))
                    data["alias_status"][channel]["touched"].append(alias)

                checkpoint()
                command(
                    [
                        "docker",
                        "buildx",
                        "imagetools",
                        "create",
                        "--prefer-index=false",
                        "--tag",
                        repository + ":" + alias,
                        "oci-layout://" + str(layout.resolve()) + "@" + expected,
                    ]
                )

                observed = manifest(repository, alias)
                require(
                    observed is not None
                    and "sha256:" + hashlib.sha256(observed).hexdigest() == expected,
                    "Stable alias read-back mismatch",
                )

                data["alias_status"][channel]["identities"][alias] = expected
                checkpoint()

            data["alias_status"][channel]["status"] = "verified"
            checkpoint()
    except OSError, ValueError, subprocess.SubprocessError:
        for channel, alias in reversed(touched):
            saved = previous[channel, alias]
            rollback = data["alias_status"][channel]["rollback"]
            data["alias_status"][channel]["status"] = "failed"

            if saved is None:
                # Deleting a manifest could also delete immutable version tags sharing it.
                rollback[alias] = "no_previous_alias; partial_state_requires_reconciliation"
                continue

            expected = saved
            repository = REPOSITORIES[channel]

            try:
                command(
                    [
                        "docker",
                        "buildx",
                        "imagetools",
                        "create",
                        "--prefer-index=false",
                        "--tag",
                        repository + ":" + alias,
                        repository + "@" + expected,
                    ]
                )

                restored = manifest(repository, alias)
                require(
                    restored is not None
                    and "sha256:" + hashlib.sha256(restored).hexdigest() == expected,
                    "Alias rollback read-back mismatch",
                )

                rollback[alias] = "restored"
            except OSError, ValueError, subprocess.SubprocessError:
                rollback[alias] = "failed"

            checkpoint()

        raise ValueError("Stable aliases incomplete; rollback state retained") from None


def verify_pages(archive: Path, version: str, expected: str) -> None:
    """Require both the public archive bytes and index entry to match selection."""
    with urllib.request.urlopen(PAGE_URL + archive.name, timeout=120) as response:
        require(
            hashlib.sha256(response.read()).hexdigest() == expected,
            "Pages read-back mismatch",
        )

    with urllib.request.urlopen(PAGE_URL + "index.yaml", timeout=120) as response:
        index = yaml.safe_load(response.read())

    require(
        any(
            item.get("version") == version and item.get("digest") == expected
            for item in index["entries"]["whisper-server"]
        ),
        "Pages index read-back mismatch",
    )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inventory", type=Path, required=True)
    parser.add_argument("--artifacts", type=Path, required=True)
    parser.add_argument("--state", type=Path, required=True)
    parser.add_argument("--stage", choices=("candidate", "stable"), required=True)
    parser.add_argument(
        "--channel",
        choices=("images", "chart", "pages-prepare", "pages-verify", "aliases-prepare", "aliases"),
        required=True,
    )
    parser.add_argument("--pages-dir", type=Path)
    parser.add_argument("--helm", default="helm3")

    args = parser.parse_args()

    try:
        data = json.loads((args.state if args.state.exists() else args.inventory).read_bytes())
        selected = json.loads(args.inventory.read_bytes())
        require(
            all(
                data.get(key) == value
                for key, value in selected.items()
                if key not in ("channel_status", "alias_status")
            ),
            "Retry selection drift",
        )
        validate(data, args.stage, require_channels=False)

        version = data["candidate_version" if args.stage == "candidate" else "release_version"]
        if args.channel in ("aliases-prepare", "aliases"):
            require(args.stage == "stable", "Candidate cannot advance stable aliases")

            try:
                if args.channel == "aliases-prepare":
                    prepare_aliases(data, args.artifacts, conservative_rollback=True)
                else:
                    finalize_aliases(
                        data, args.artifacts, checkpoint=lambda: save(args.state, data)
                    )
            finally:
                save(args.state, data)
        elif args.channel == "images":
            for channel, repository in REPOSITORIES.items():
                role = channel.split("-")[0]
                layout = args.artifacts / role / "layout"
                actual = inspect_layout(layout, data["candidate_source_sha"])
                expected = data["image_manifest_digests"][role]
                require(actual["manifest_digest"] == expected, "Selected image drift")

                data["channel_status"][channel] = {"status": "failed"}
                save(args.state, data)
                publish_image(layout, repository, version, expected)
                publish_image(layout, repository, "sha-" + data["candidate_source_sha"], expected)

                data["channel_status"][channel] = {"status": "verified", "identity": expected}
                save(args.state, data)
        else:
            archive = (
                args.artifacts / "chart" / ("whisper-server-" + data["chart_version"] + ".tgz")
            )
            if args.channel == "chart":
                data["channel_status"]["chart-oci"] = {"status": "failed"}
                save(args.state, data)
                data["chart_oci_manifest_digest"] = publish_chart(
                    archive,
                    version,
                    data["chart_archive_sha256"],
                    data["chart_oci_manifest_digest"],
                    args.helm,
                )

                data["channel_status"]["chart-oci"] = {
                    "status": "verified",
                    "identity": data["chart_archive_sha256"],
                }
            elif args.channel == "pages-prepare":
                require(args.pages_dir is not None, "Pages directory required")

                data["channel_status"]["chart-pages"] = {"status": "pending"}
                prepare_pages(
                    archive, version, data["chart_archive_sha256"], args.pages_dir, args.helm
                )
            else:
                verify_pages(archive, version, data["chart_archive_sha256"])

                data["channel_status"]["chart-pages"] = {
                    "status": "verified",
                    "identity": data["chart_archive_sha256"],
                }

            save(args.state, data)
    except OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError, yaml.YAMLError:
        print(
            "FAIL: publication incomplete; preserved channel state is authoritative",
            file=sys.stderr,
        )
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
