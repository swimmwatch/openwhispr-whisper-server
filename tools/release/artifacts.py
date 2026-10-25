#!/usr/bin/env python3
"""Prepare GPU-free CI artifacts and verify the bytes selected for release."""

import argparse
import json
import os
import subprocess
import sys
import tarfile
import tempfile
import uuid
from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Any

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from tools.release.oci import blob as blob
from tools.release.oci import extract as extract
from tools.release.oci import inspect_layout as inspect_layout
from tools.release.oci import sha as sha
from tools.release.validation import require, source

ROOT = Path(__file__).resolve().parents[2]
BUILD_TIMEOUT_SECONDS = 7200


@dataclass(frozen=True, slots=True)
class BuildOptions:
    """Validated cache policy and worker budget for one isolated builder."""

    cold: bool
    cache_from: Path | None = None
    cache_to: Path | None = None
    parallelism: int = 1

    def __post_init__(self) -> None:
        require(
            not self.cold or (self.cache_from is None and self.cache_to is None),
            "Cold builds cannot import or export caches",
        )

        require(
            type(self.parallelism) is int and self.parallelism > 0,
            "Build parallelism must be positive",
        )

        require(
            all(
                "," not in str(path)
                for path in (self.cache_from, self.cache_to)
                if path is not None
            ),
            "Cache paths cannot contain commas",
        )


def runtime_identity(root: Path = ROOT) -> str:
    """Fingerprint executable/packaging inputs; chart release metadata is separate."""
    files = {
        root / name
        for name in (
            "go.mod",
            "go.sum",
            "LICENSE",
            "THIRD_PARTY_NOTICES.md",
            "containers/api.Dockerfile",
            "containers/backend.Dockerfile",
            "containers/build-inputs.json",
            "containers/scripts/build-jobs.sh",
            "charts/whisper-server/values.yaml",
            "charts/whisper-server/values.schema.json",
        )
    }
    require(all(path.is_file() for path in files), "Missing runtime fingerprint input")

    for folder in (
        "cmd",
        "internal",
        "charts/whisper-server/templates",
        "charts/whisper-server/files",
    ):
        files.update(path for path in (root / folder).rglob("*") if path.is_file())

    manifest = {path.relative_to(root).as_posix(): sha(path.read_bytes()) for path in sorted(files)}

    return "sha256:" + sha(json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode())


def command(args: Sequence[str | Path], **kwargs: Any) -> None:
    subprocess.run(
        [str(x) for x in args], cwd=ROOT, check=True, timeout=BUILD_TIMEOUT_SECONDS, **kwargs
    )


def build(
    role: str,
    output: Path,
    source_sha: str,
    cold: bool,
    cache_from: Path | None = None,
    cache_to: Path | None = None,
    parallelism: int | None = None,
) -> None:
    source(source_sha)

    options = BuildOptions(
        cold,
        cache_from,
        cache_to,
        parallelism if parallelism is not None else (os.cpu_count() or 1),
    )
    parallelism = options.parallelism
    output.mkdir(parents=True, exist_ok=True)

    builder = "whisper-ci-" + uuid.uuid4().hex[:12]
    buildkit = json.loads((ROOT / "tools/versions.yaml").read_bytes())["ci"]["buildkit"]
    # An exclusive builder makes --no-cache a real cold build, including cache mounts.
    configuration = output / "buildkitd.toml"
    configuration.write_text("[worker.oci]\n  max-parallelism = " + str(parallelism) + "\n")
    command(
        [
            "buildx",
            "create",
            "--name",
            builder,
            "--driver",
            "docker-container",
            "--driver-opt",
            "image=" + buildkit["image"],
            "--buildkitd-config",
            str(configuration),
        ]
    )

    try:
        args = [
            "buildx",
            "build",
            "--builder",
            builder,
            "--platform",
            "linux/amd64",
            "--provenance=mode=max",
            "--attest",
            "type=sbom,generator=" + buildkit["sbom_generator"],
            "--file",
            "containers/" + role + ".Dockerfile",
            "--build-arg",
            "SOURCE_REVISION=" + source_sha,
            "--output",
            "type=oci,dest=" + str(output / "image.tar"),
            ".",
        ]
        if cold:
            args.insert(2, "--no-cache")

        if cache_from is not None and (cache_from / "index.json").is_file():
            args[2:2] = ["--cache-from", "type=local,src=" + str(cache_from)]

        if cache_to is not None:
            args[2:2] = [
                "--cache-to",
                "type=local,dest="
                + str(cache_to)
                + ",mode=max,compression=zstd,compression-level=3",
            ]

        command(args)
        record_image(output, source_sha, cold)
    finally:
        command(["buildx", "rm", builder])


def record_image(
    output: Path, source_sha: str, cold: bool, expected_digest: str | None = None
) -> None:
    """Verify an OCI export before recording the identities used by later jobs."""
    source(source_sha)
    extract(output / "image.tar", output / "layout")

    result = inspect_layout(output / "layout", source_sha)
    if expected_digest is not None:
        require(result["manifest_digest"] == expected_digest, "Build action digest mismatch")

    result.update(
        cold=cold,
        build_input_identity="sha256:" + sha((ROOT / "containers/build-inputs.json").read_bytes()),
        runtime_input_identity=runtime_identity(),
    )
    (output / "image.json").write_text(json.dumps(result, sort_keys=True, indent=2) + "\n")


def chart(output: Path, helm: str | Path, source_sha: str, version: str | None = None) -> None:
    source(source_sha)
    output.mkdir(parents=True, exist_ok=True)

    variants = {
        "defaults": {},
        "two-pairs": {"replicaCount": 2},
        "ingress-cache": {
            "replicaCount": 2,
            "cache": {"existingClaim": "operator-cache", "accessModes": ["ReadWriteMany"]},
            "ingress": {
                "enabled": True,
                "host": "whisper.example",
                "className": "nginx",
                "tlsSecretName": "operator-tls",
            },
        },
        "prepared": {
            "prepared": {"existingClaim": "operator-models"},
            "config": {
                "models": {
                    "catalog": [
                        {
                            "id": "custom",
                            "source": {"kind": "prepared", "path": "/prepared/base.bin"},
                        }
                    ],
                    "served": "custom",
                    "downloadSet": [],
                }
            },
        },
        "digest-scheduling": {
            "images": {
                "api": {"digest": "sha256:" + "a" * 64},
                "backend": {"digest": "sha256:" + "b" * 64},
            },
            "api": {"nodeSelector": {"node-role": "api"}},
            "backend": {"runtimeClassName": "nvidia"},
        },
    }
    package: list[str | Path] = [helm, "package", "charts/whisper-server", "--destination", output]
    if version is not None:
        package.extend(["--version", version, "--app-version", version])
    command(package)
    archives = list(output.glob("*.tgz"))
    require(len(archives) == 1, "Expected one chart archive")
    extract(archives[0], output / "extracted")

    for name, values in variants.items():
        values_path = output / "values" / (name + ".json")
        values_path.parent.mkdir(exist_ok=True)
        values_path.write_text(json.dumps(values, sort_keys=True) + "\n")

        rendered = output / "rendered" / name
        rendered.mkdir(parents=True, exist_ok=True)

        with (rendered / "manifest.yaml").open("wb") as stream:
            command(
                [
                    helm,
                    "template",
                    "whisper",
                    archives[0],
                    "--namespace",
                    "whisper-test",
                    "--kube-version",
                    "1.37.0",
                    "--values",
                    values_path,
                ],
                stdout=stream,
            )

    result = {
        "source_sha": source_sha,
        "archive": archives[0].name,
        "chart_archive_sha256": sha(archives[0].read_bytes()),
        "values_matrix_identity": "sha256:" + sha(json.dumps(variants, sort_keys=True).encode()),
        "rendered_sha256": {
            name: sha((output / "rendered" / name / "manifest.yaml").read_bytes())
            for name in variants
        },
    }
    (output / "chart.json").write_text(json.dumps(result, sort_keys=True, indent=2) + "\n")


def verify_chart(output: Path) -> dict[str, Any]:
    from tools.security.check import VALUES_VARIANTS

    saved = json.loads((output / "chart.json").read_bytes())
    source(saved["source_sha"])
    require(
        Path(saved["archive"]).name == saved["archive"] and saved["archive"].endswith(".tgz"),
        "Invalid chart archive path",
    )

    archive = output / saved["archive"]
    require(
        sha(archive.read_bytes()) == saved["chart_archive_sha256"],
        "Transferred chart archive mismatch",
    )

    variants = {
        name: json.loads((output / "values" / (name + ".json")).read_bytes())
        for name in VALUES_VARIANTS
    }
    require(
        "sha256:" + sha(json.dumps(variants, sort_keys=True).encode())
        == saved["values_matrix_identity"],
        "Transferred Helm values mismatch",
    )

    require(
        set(saved["rendered_sha256"]) == set(variants)
        and all(
            sha((output / "rendered" / name / "manifest.yaml").read_bytes())
            == saved["rendered_sha256"][name]
            for name in variants
        ),
        "Transferred rendered manifest mismatch",
    )

    with tempfile.TemporaryDirectory(prefix="whisper-chart-verify-") as td:
        clean = Path(td)
        extract(archive, clean)

        expected = {
            path.relative_to(clean): sha(path.read_bytes())
            for path in clean.rglob("*")
            if path.is_file()
        }
        actual = {
            path.relative_to(output / "extracted"): sha(path.read_bytes())
            for path in (output / "extracted").rglob("*")
            if path.is_file()
        }
        require(actual == expected, "Extracted chart differs from the selected archive")

    return saved


def required_jobs(needs: dict[str, Any], required: set[str]) -> None:
    require(
        set(needs) == set(required)
        and all(value.get("result") == "success" for value in needs.values()),
        "Missing, skipped, cancelled or failed required job",
    )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="action", required=True)
    b = sub.add_parser("build")
    b.add_argument("--role", choices=("api", "backend"), required=True)
    b.add_argument("--output", type=Path, required=True)
    b.add_argument("--source-sha", required=True)
    b.add_argument("--cold", action="store_true")
    b.add_argument("--cache-from", type=Path)
    b.add_argument("--cache-to", type=Path)
    b.add_argument("--parallelism", type=int)

    image = sub.add_parser("record-image")
    image.add_argument("--output", type=Path, required=True)
    image.add_argument("--source-sha", required=True)
    image.add_argument("--expected-digest", required=True)
    image.add_argument("--cold", action="store_true")

    c = sub.add_parser("chart")
    c.add_argument("--output", type=Path, required=True)
    c.add_argument("--helm", required=True)
    c.add_argument("--source-sha", required=True)
    c.add_argument("--version")

    g = sub.add_parser("jobs")
    g.add_argument("--needs", required=True)
    g.add_argument("--required", nargs="+", required=True)

    u = sub.add_parser("unpack")
    u.add_argument("--artifacts", type=Path, required=True)
    u.add_argument("--images-only", action="store_true")

    args = parser.parse_args()

    try:
        if args.action == "build":
            build(
                args.role,
                args.output.resolve(),
                args.source_sha,
                args.cold,
                args.cache_from.resolve() if args.cache_from is not None else None,
                args.cache_to.resolve() if args.cache_to is not None else None,
                args.parallelism,
            )
        elif args.action == "record-image":
            record_image(args.output.resolve(), args.source_sha, args.cold, args.expected_digest)
        elif args.action == "chart":
            chart(args.output.resolve(), args.helm, args.source_sha, args.version)
        elif args.action == "jobs":
            required_jobs(json.loads(args.needs), set(args.required))
        else:
            if not args.images_only:
                verify_chart(args.artifacts / "chart")

            for role in ("api", "backend"):
                base = args.artifacts / role
                extract(base / "image.tar", base / "layout")

                saved = json.loads((base / "image.json").read_bytes())
                require(
                    all(
                        saved[key] == value
                        for key, value in inspect_layout(
                            base / "layout", saved["source_sha"]
                        ).items()
                    ),
                    "Transferred OCI artifact mismatch",
                )
    except OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError, tarfile.TarError:
        print("FAIL: artifact preparation or identity verification", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
