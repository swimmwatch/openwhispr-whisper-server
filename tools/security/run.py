#!/usr/bin/env python3
"""Execute pinned scanners privately and emit only normalized, bound evidence."""

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile
from dataclasses import dataclass, field
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from tools.release.check import fresh, require
from tools.security import check
from tools.security.advisories import component_coverage as component_coverage
from tools.security.advisories import component_findings as component_findings
from tools.security.check_secrets import scan as source_secrets

ROOT = Path(__file__).resolve().parents[2]
HELM_SCAN_FLAGS = ["--helm-kube-version", "1.37.0", "--file-patterns", r"helm:.*\.json"]
GOSEC_SCAN_FLAGS = ["-nosec", "-fmt=json"]
HADOLINT_SCAN_FLAGS = ["--disable-ignore-pragma", "--format=json"]


def validate_database_metadata(metadata: dict[str, Any], now: datetime | None = None) -> None:
    """Reject expired cached databases even when the scanner exits successfully."""
    now = now or datetime.now(UTC)
    database = metadata.get("VulnerabilityDB", {})
    require(database.get("Version") == 2, "Missing or unsupported vulnerability database")
    fresh(database.get("UpdatedAt"), now)

    try:
        next_update = datetime.fromisoformat(database["NextUpdate"].replace("Z", "+00:00"))
        require(
            next_update.tzinfo is not None and next_update > now, "Expired vulnerability database"
        )
    except (AttributeError, KeyError, TypeError, ValueError) as error:
        raise ValueError("Expired or invalid vulnerability database metadata") from error


def json_stream(text: str) -> list[dict[str, Any]]:
    decoder = json.JSONDecoder()
    result: list[dict[str, Any]] = []
    while text.strip():
        item, end = decoder.raw_decode(text.lstrip())
        result.append(item)

        text = text.lstrip()[end:]

    require(bool(result), "Empty scanner stream")

    return result


def execute(command: list[str], cwd: Path = ROOT, timeout: int = 1200) -> tuple[int, str]:
    # Raw stdout/stderr can contain credentials. Neither is inherited or logged.
    env = {
        key: value
        for key, value in os.environ.items()
        if not key.startswith(("TRIVY_", "GOSEC_", "GOVULNCHECK_"))
    }
    env.update(
        GOTOOLCHAIN="local", GOWORK="off", GOOS="linux", GOARCH="amd64", CGO_ENABLED="0", GOFLAGS=""
    )

    with tempfile.TemporaryFile() as stdout, tempfile.TemporaryFile() as stderr:
        process = subprocess.run(
            command, cwd=cwd, env=env, stdout=stdout, stderr=stderr, timeout=timeout
        )
        require(process.returncode in (0, 1), "Scanner execution failed; raw output suppressed")
        stdout.seek(0)

        return process.returncode, stdout.read().decode("utf-8")


def input_binding(
    chart: dict[str, Any], builds: dict[str, Any], image_source_sha: str | None = None
) -> dict[str, Any]:
    binding = {
        key: chart[key] for key in ("source_sha", "chart_archive_sha256", "values_matrix_identity")
    }
    binding["image_manifest_digests"] = {
        role: value["manifest_digest"] for role, value in builds.items()
    }
    binding["image_source_sha"] = image_source_sha or binding["source_sha"]
    require(
        isinstance(binding["image_source_sha"], str)
        and re.fullmatch("[a-f0-9]{40}", binding["image_source_sha"]),
        "Invalid selected image source",
    )

    require(
        set(builds) == {"api", "backend"}
        and all(item["source_sha"] == binding["image_source_sha"] for item in builds.values()),
        "Build source drift",
    )

    return binding


@dataclass
class ScanSession:
    """Own report accumulation and failure state for one artifact-bound scan."""

    binaries: Path
    binding: dict[str, Any]
    builds: dict[str, Any]
    exceptions: list[dict[str, Any]]
    advisories: Path | None
    trivy: list[str]
    cache: Path
    reports: list[dict[str, Any]] = field(default_factory=list)
    failed: bool = False

    def add(self, target: str, scanner: str, command: list[str]) -> None:
        now = datetime.now(UTC).isoformat()

        try:
            code, stdout = execute(command)
            require(not scanner.startswith("trivy") or code == 0, "Trivy execution failed")

            raw = json_stream(stdout) if scanner == "govulncheck" else json.loads(stdout)
            result = check.evaluate(scanner, raw, code, self.exceptions)
            rule_bytes = (self.binaries / check.TARGETS.get(target, "trivy")).read_bytes()
            if scanner.startswith("trivy"):
                _, metadata = execute(
                    self.trivy + ["--cache-dir", str(self.cache), "--version", "--format", "json"]
                )
                if scanner == "trivy-vuln":
                    validate_database_metadata(json.loads(metadata))

                rule_bytes += metadata.encode()
            elif scanner == "govulncheck":
                rule_bytes += json.dumps(
                    [x["config"] for x in raw if "config" in x], sort_keys=True
                ).encode()

            result.update(
                target=target,
                scanner_version=check.expected_version(target),
                rule_identity="sha256:" + hashlib.sha256(rule_bytes).hexdigest(),
                scan_time=now,
                database_checked_at=now,
                coverage_gaps=[],
                **self.binding,
            )

            if target == "backend-vuln" or target == "api-vuln":
                role = target.split("-")[0]
                result["coverage_gaps"] = component_coverage(
                    self.advisories, role, self.builds[role]["build_input_identity"]
                )
                native = component_findings(self.advisories, role)
                result["native_findings"] = native
                result["finding_count"] += len(native)
                result["blocking_count"] += sum(
                    item["severity"] in ("HIGH", "CRITICAL", "UNKNOWN") for item in native
                )

            self.reports.append(result)

            self.failed |= result["blocking_count"] > 0 or bool(result["coverage_gaps"])
            print(
                json.dumps(
                    {
                        "target": target,
                        **{
                            k: result[k]
                            for k in ("finding_count", "blocking_count", "coverage_gaps")
                        },
                    }
                )
            )
        except OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError:
            self.failed = True
            print(
                json.dumps(
                    {
                        "target": target,
                        "status": "failed",
                        "detail": "Raw scanner output suppressed",
                    }
                )
            )


def run(
    artifacts: Path,
    binaries: Path,
    output: Path,
    advisories: Path | None = None,
    image_source_sha: str | None = None,
    db_cache: Path | None = None,
) -> int:
    chart = json.loads((artifacts / "chart/chart.json").read_bytes())
    builds = {
        role: json.loads((artifacts / role / "image.json").read_bytes())
        for role in ("api", "backend")
    }
    binding = input_binding(chart, builds, image_source_sha)
    exceptions = json.loads((ROOT / "tools/security/exceptions.json").read_bytes())
    check.validate_exceptions(exceptions)

    versions = json.loads((ROOT / "tools/versions.yaml").read_bytes())["scanners"]
    for name, metadata in versions.items():
        flag = "-version" if name in ("gosec", "govulncheck") else "--version"
        _, actual = execute([str(binaries / name), flag])
        require(metadata["version"].lstrip("v") in actual, "Unexpected scanner executable version")

    with tempfile.TemporaryDirectory(prefix="whisper-security-") as td:
        private = Path(td)
        (private / "config.yaml").write_text("{}\n")
        (private / "ignore").write_text("")

        trivy = [str(binaries / "trivy")]
        cache = db_cache if db_cache is not None else private / "cache"
        cache.mkdir(parents=True, exist_ok=True, mode=0o700)

        common = [
            "--config",
            str(private / "config.yaml"),
            "--ignorefile",
            str(private / "ignore"),
            "--cache-dir",
            str(cache),
            "--quiet",
            "--format",
            "json",
            "--exit-code",
            "0",
        ]

        session = ScanSession(binaries, binding, builds, exceptions, advisories, trivy, cache)

        session.add(
            "go-vuln",
            "govulncheck",
            [str(binaries / "govulncheck"), "-json", "./cmd/...", "./internal/..."],
        )

        session.add(
            "go-source",
            "gosec",
            [
                str(binaries / "gosec"),
                *GOSEC_SCAN_FLAGS,
                "./cmd/...",
                "./internal/...",
            ],
        )

        for role in ("api", "backend"):
            session.add(
                "dockerfile-" + role,
                "hadolint",
                [
                    str(binaries / "hadolint"),
                    *HADOLINT_SCAN_FLAGS,
                    "containers/" + role + ".Dockerfile",
                ],
            )

        session.add(
            "docker-config",
            "trivy-config",
            trivy + ["config", *common, "--include-non-failures", "containers"],
        )

        for variant in check.VALUES_VARIANTS:
            session.add(
                "chart-" + variant,
                "trivy-config",
                trivy
                + [
                    "config",
                    *common,
                    *HELM_SCAN_FLAGS,
                    "--include-non-failures",
                    "--helm-values",
                    str(artifacts / "chart/values" / (variant + ".json")),
                    "charts/whisper-server",
                ],
            )

            session.add(
                "rendered-" + variant,
                "trivy-config",
                trivy
                + [
                    "config",
                    *common,
                    "--include-non-failures",
                    str(artifacts / "chart/rendered" / variant),
                ],
            )

        session.add(
            "package-config",
            "trivy-config",
            trivy
            + [
                "config",
                *common,
                *HELM_SCAN_FLAGS,
                "--include-non-failures",
                str(artifacts / "chart/extracted"),
            ],
        )

        session.add(
            "package-secret",
            "trivy-secret",
            trivy
            + [
                "fs",
                *common,
                "--scanners",
                "secret",
                "--secret-config",
                str(ROOT / "tools/security/trivy-secret.yaml"),
                str(artifacts / "chart/extracted"),
            ],
        )

        try:
            result, status = source_secrets(
                ROOT, binaries / "trivy", ROOT / "tools/security/trivy-secret.yaml", ROOT
            )
            now = datetime.now(UTC).isoformat()
            session.reports.append(
                dict(
                    binding,
                    target="source-secret",
                    scanner_version=check.expected_version("source-secret"),
                    rule_identity="sha256:"
                    + hashlib.sha256(
                        json.dumps(result["secret_rule_identity"], sort_keys=True).encode()
                    ).hexdigest(),
                    scan_time=now,
                    database_checked_at=now,
                    scanner_exit=result["scanner_exit"],
                    finding_count=result["finding_count"],
                    blocking_count=result["finding_count"],
                    coverage_gaps=[],
                )
            )

            session.failed |= status != 0
        except OSError, ValueError, subprocess.SubprocessError:
            session.failed = True

        for role in ("api", "backend"):
            target = str(artifacts / role / "layout")
            session.add(
                role + "-vuln",
                "trivy-vuln",
                trivy
                + [
                    "image",
                    *common,
                    "--platform",
                    "linux/amd64",
                    "--scanners",
                    "vuln",
                    "--input",
                    target,
                ],
            )

            session.add(
                role + "-secret",
                "trivy-secret",
                trivy
                + [
                    "image",
                    *common,
                    "--platform",
                    "linux/amd64",
                    "--scanners",
                    "secret",
                    "--image-config-scanners",
                    "secret",
                    "--secret-config",
                    str(ROOT / "tools/security/trivy-secret.yaml"),
                    "--input",
                    target,
                ],
            )

    inventory = dict(
        binding,
        schema_version=1,
        values_variants=check.VALUES_VARIANTS,
        exceptions=exceptions,
        reports=session.reports,
    )
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(inventory, sort_keys=True, indent=2) + "\n")
    check.validate(inventory)

    return int(session.failed)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--artifacts", type=Path, required=True)
    parser.add_argument("--bin", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument(
        "--component-advisories",
        type=Path,
        default=ROOT / "tools/security/component-advisories.json",
    )
    parser.add_argument("--image-source-sha")
    parser.add_argument("--db-cache", type=Path)

    args = parser.parse_args()

    try:
        return run(
            args.artifacts.resolve(),
            args.bin.resolve(),
            args.output,
            args.component_advisories,
            args.image_source_sha,
            args.db_cache.resolve() if args.db_cache else None,
        )
    except OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError:
        print("FAIL: incomplete Security Check; raw scanner output suppressed", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
