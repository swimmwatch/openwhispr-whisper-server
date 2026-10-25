#!/usr/bin/env python3
"""Install checksum-pinned tools concurrently, revalidating cached downloads."""

import argparse
import hashlib
import io
import json
import os
import subprocess
import tarfile
import tempfile
import urllib.request
from collections.abc import Mapping, Sequence
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]

DOWNLOAD_TIMEOUT_SECONDS = 120


@dataclass(frozen=True)
class DownloadInput:
    """An immutable, checksum-pinned input from the tool manifests."""

    url: str
    sha256: str

    @classmethod
    def from_metadata(cls, metadata: Mapping[str, str]) -> DownloadInput:
        url = metadata.get("download", metadata.get("url"))
        if not url:
            raise ValueError("CI input download URL is missing")

        return cls(url=url, sha256=metadata["sha256"])


def archive_binary(data: bytes, name: str) -> bytes:
    with tarfile.open(fileobj=io.BytesIO(data)) as archive:
        members = [
            item
            for item in archive.getmembers()
            if item.name.lstrip("./") in (name, "linux-amd64/" + name) and item.isfile()
        ]
        if len(members) != 1:
            raise ValueError("CI archive has no unique regular binary")

        stream = archive.extractfile(members[0])
        if stream is None:
            raise ValueError("CI archive binary is unreadable")

        with stream:
            return stream.read()


def download_input(metadata: Mapping[str, str], cache: Path | None = None) -> bytes:
    source = DownloadInput.from_metadata(metadata)
    identity = source.sha256
    cached = cache / identity if cache is not None else None
    if cached is not None and cached.is_file():
        data = cached.read_bytes()
    else:
        with urllib.request.urlopen(source.url, timeout=DOWNLOAD_TIMEOUT_SECONDS) as response:
            data = response.read()

    if hashlib.sha256(data).hexdigest() != identity:
        raise ValueError("CI input checksum mismatch, including restored cache")

    if cached is not None and not cached.exists():
        cached.parent.mkdir(parents=True, exist_ok=True, mode=0o700)

        with tempfile.NamedTemporaryFile(dir=cache, delete=False) as stream:
            temporary = Path(stream.name)
            stream.write(data)

        try:
            temporary.replace(cached)
        finally:
            temporary.unlink(missing_ok=True)

    return data


def install(
    name: str,
    metadata: Mapping[str, str],
    destination: Path,
    cache: Path | None = None,
    archive_name: str | None = None,
) -> None:
    data = download_input(metadata, cache)
    url = DownloadInput.from_metadata(metadata).url
    if url.endswith(".tar.gz"):
        data = archive_binary(data, archive_name or name)

    if (
        "binary_sha256" in metadata
        and hashlib.sha256(data).hexdigest() != metadata["binary_sha256"]
    ):
        raise ValueError("CI executable identity mismatch")

    binary = destination / name
    binary.write_bytes(data)
    binary.chmod(0o755)


def install_tool(
    name: str,
    versions: Mapping[str, Any],
    destination: Path,
    cache: Path | None,
) -> None:
    if name == "govulncheck":
        metadata = versions["scanners"][name]
        module = metadata["module"] + "@" + metadata["version"]

        with tempfile.TemporaryDirectory(prefix="whisper-go-tool-") as workspace:
            env = {**os.environ, "GOBIN": str(destination), "GOTOOLCHAIN": "local", "GOWORK": "off"}
            result = subprocess.run(
                ["go", "mod", "download", "-json", module],
                cwd=workspace,
                env=env,
                capture_output=True,
                check=True,
            )
            identity = json.loads(result.stdout)
            if (
                identity.get("Sum") != metadata["sum"]
                or identity.get("GoModSum") != metadata["go_mod_sum"]
            ):
                raise ValueError("Go scanner module identity mismatch")

            subprocess.run(
                ["go", "install", metadata["module"] + "/cmd/govulncheck@" + metadata["version"]],
                cwd=workspace,
                env=env,
                check=True,
            )
    elif name in ("helm3", "helm4"):
        inputs = json.loads((ROOT / "tests/integration/testdata/chart-inputs.json").read_bytes())
        metadata = next(
            item
            for item in inputs
            if item["kind"] == "helm" and item["version"].startswith(name[-1] + ".")
        )
        install(name, metadata, destination, cache, archive_name="helm")
    else:
        metadata = (
            versions["ci"][name] if name in ("actionlint", "buildx") else versions["scanners"][name]
        )
        install(name, metadata, destination, cache)

    print("Installed pinned tool:", name, flush=True)


def install_tools(
    names: Sequence[str],
    versions: Mapping[str, Any],
    destination: Path,
    cache: Path | None,
    jobs: int,
) -> None:
    if jobs < 1 or not names or len(names) != len(set(names)):
        raise ValueError("Positive parallelism and unique tool names are required")

    with ThreadPoolExecutor(max_workers=min(jobs, len(names))) as executor:
        futures = [
            executor.submit(install_tool, name, versions, destination, cache) for name in names
        ]
        for future in futures:
            future.result()


def verify_existing(names: Sequence[str], versions: Mapping[str, Any], destination: Path) -> None:
    """Verify Action-installed or cached scanner bytes against reviewed identities."""
    for name in names:
        expected = versions["scanners"][name]["binary_sha256"]
        if hashlib.sha256((destination / name).read_bytes()).hexdigest() != expected:
            raise ValueError("Action-installed tool identity mismatch")

        print("Verified pinned tool:", name, flush=True)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--destination", type=Path, required=True)
    parser.add_argument(
        "--tools", nargs="+", default=["actionlint", "trivy", "gosec", "hadolint", "govulncheck"]
    )
    parser.add_argument("--download-cache", type=Path)
    parser.add_argument("--jobs", type=int, default=os.cpu_count() or 1)
    parser.add_argument("--verify-existing", action="store_true")

    args = parser.parse_args()
    if args.jobs < 1:
        parser.error("--jobs must be a positive integer")

    destination = args.destination.resolve()
    destination.mkdir(parents=True, exist_ok=True)

    versions = json.loads((ROOT / "tools/versions.yaml").read_bytes())
    cache = args.download_cache.resolve() if args.download_cache is not None else None
    if args.verify_existing:
        verify_existing(args.tools, versions, destination)
    else:
        install_tools(args.tools, versions, destination, cache, args.jobs)


if __name__ == "__main__":
    main()
