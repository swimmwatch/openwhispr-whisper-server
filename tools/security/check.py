#!/usr/bin/env python3
"""Normalize scanner results and fail closed on incomplete release scan coverage."""

import argparse
import json
import re
import sys
from collections.abc import Iterable
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from tools.release.check import digest, fresh, require, source
from tools.security.check_secrets import evaluate as evaluate_secrets
from tools.security.exceptions import exception_ref as exception_ref
from tools.security.exceptions import validate_exceptions as validate_exceptions

ROOT = Path(__file__).resolve().parents[2]
VALUES_VARIANTS = ["defaults", "two-pairs", "ingress-cache", "prepared", "digest-scheduling"]
TARGETS = {
    "go-vuln": "govulncheck",
    "go-source": "gosec",
    "source-secret": "trivy",
    "dockerfile-api": "hadolint",
    "dockerfile-backend": "hadolint",
    "docker-config": "trivy",
    "api-vuln": "trivy",
    "backend-vuln": "trivy",
    "api-secret": "trivy",
    "backend-secret": "trivy",
    "package-secret": "trivy",
    "package-config": "trivy",
}


def required_targets(variants: Any) -> list[str]:
    require(
        isinstance(variants, list)
        and len(variants) == len(VALUES_VARIANTS)
        and set(variants) == set(VALUES_VARIANTS),
        "Incomplete supported Helm values matrix",
    )

    return list(TARGETS) + [
        prefix + value for value in variants for prefix in ("chart-", "rendered-")
    ]


def expected_version(target: str) -> str:
    scanner = TARGETS.get(target, "trivy")

    return json.loads((ROOT / "tools/versions.yaml").read_bytes())["scanners"][scanner][
        "version"
    ].lstrip("v")


def safe_findings(scanner: str, findings: list[dict[str, Any]]) -> list[dict[str, str]]:
    """Retain advisory identifiers/remediation without snippets or free-form text."""
    fields = {
        "govulncheck": "osv",
        "gosec": "rule_id",
        "hadolint": "code",
        "trivy-vuln": "VulnerabilityID",
        "trivy-config": "ID",
    }
    result = []
    for finding in findings:
        identity = finding.get(fields[scanner], "")
        if not isinstance(identity, str) or not re.fullmatch(
            r"(?:GO-\d{4}-\d+|G\d{3}|(?:DL|SC)\d{4}|CVE-\d{4}-\d+|GHSA-[a-z0-9-]+|AVD-[A-Z0-9-]+)",
            identity,
        ):
            continue

        item = {"id": identity}
        for original, key in (
            ("PkgName", "package"),
            ("InstalledVersion", "installed_version"),
            ("FixedVersion", "fixed_version"),
        ):
            value = finding.get(original)
            if (
                isinstance(value, str)
                and len(value) <= 200
                and re.fullmatch(r"[A-Za-z0-9_./+:@~ -]*", value)
            ):
                item[key] = value

        if finding.get("Severity") in ("UNKNOWN", "LOW", "MEDIUM", "HIGH", "CRITICAL"):
            item["severity"] = finding["Severity"]

        result.append(item)

    return result


def evaluate(
    scanner: str,
    raw: Any,
    scanner_exit: int,
    exceptions: Iterable[dict[str, Any]] = (),
    now: datetime | None = None,
    root: Path = ROOT,
) -> dict[str, Any]:
    exceptions = list(exceptions)
    validate_exceptions(exceptions, now, root)
    require(scanner_exit in (0, 1), "Scanner execution failed")

    findings = []
    exempted = []
    if scanner == "govulncheck":
        require(isinstance(raw, list) and raw, "Unsupported Go vulnerability report")
        require(all(isinstance(item, dict) for item in raw), "Malformed Go vulnerability stream")
        require(
            any(
                item.get("config", {}).get("scanner_name") == "govulncheck"
                and item.get("config", {}).get("protocol_version") == "v1.0.0"
                for item in raw
            ),
            "Missing Go vulnerability protocol identity",
        )

        findings = [item["finding"] for item in raw if "finding" in item]
        # Only call-level reachability blocks; module/package findings remain visible.
        blocking = sum(
            any(frame.get("function") for frame in item.get("trace", [])) for item in findings
        )
    elif scanner == "gosec":
        require(
            isinstance(raw, dict)
            and isinstance(raw.get("Issues"), list)
            and raw.get("Stats", {}).get("files", 0) > 0
            and not raw.get("Golang errors"),
            "Unsupported Go source report",
        )

        findings = raw["Issues"]
        policy = {
            (item["rule"], item["path"]): item for item in exceptions if item["scanner"] == "gosec"
        }
        for finding in findings:
            if policy:
                require(isinstance(finding.get("file"), str), "Missing Go source finding scope")

                try:
                    path = str((root / finding["file"]).resolve().relative_to(root.resolve()))
                except ValueError as error:
                    raise ValueError("Go source finding is outside the selected source") from error

                item = policy.get((finding.get("rule_id"), path))
                if item is not None:
                    exempted.append(exception_ref(item))

        blocking = len(findings) - len(exempted)
    elif scanner == "hadolint":
        require(
            isinstance(raw, list)
            and all(isinstance(item, dict) and "code" in item for item in raw),
            "Unsupported Dockerfile report",
        )

        findings = raw
        blocking = len(findings)
    elif scanner == "trivy-secret":
        report, _status = evaluate_secrets(raw, scanner_exit)

        return {
            "finding_count": report["finding_count"],
            "blocking_count": report["finding_count"],
            "scanner_exit": scanner_exit,
            "exception_count": 0,
            "exception_refs": [],
        }
    elif scanner in ("trivy-vuln", "trivy-config"):
        require(
            isinstance(raw, dict)
            and raw.get("SchemaVersion") == 2
            and isinstance(raw.get("Results"), list)
            and raw["Results"],
            "Unsupported or empty Trivy target",
        )

        field = "Vulnerabilities" if scanner == "trivy-vuln" else "Misconfigurations"
        for result in raw["Results"]:
            require(
                isinstance(result, dict) and isinstance(result.get(field, []), list),
                "Malformed Trivy result",
            )

            findings.extend(
                item
                for item in result.get(field, [])
                if item.get("Status") not in ("PASS", "EXCEPTION")
            )

        blocking = sum(item.get("Severity") in ("HIGH", "CRITICAL") for item in findings)
        if scanner == "trivy-vuln":
            policies = {
                (item["rule"], item["path"]): item
                for item in exceptions
                if item["scanner"] == "trivy"
            }
            for finding in findings:
                matched_policy = policies.get(
                    (finding.get("VulnerabilityID"), finding.get("PkgName"))
                )
                if matched_policy is not None:
                    exempted.append(exception_ref(matched_policy))

                    blocking -= finding.get("Severity") in ("HIGH", "CRITICAL")
    else:
        raise ValueError("Unknown scanner format")

    require(not (scanner_exit == 1 and not findings), "Scanner failed without a supported finding")

    # Do not return arbitrary messages, filenames, matched text, snippets or scanner stderr.
    return {
        "finding_count": len(findings),
        "blocking_count": blocking,
        "scanner_exit": scanner_exit,
        "exception_count": len(exempted),
        "exception_refs": sorted(set(exempted)),
        "findings": safe_findings(scanner, findings),
    }


def validate(data: Any, now: datetime | None = None) -> dict[str, Any]:
    now = now or datetime.now(UTC)
    require(
        isinstance(data, dict) and data.get("schema_version") == 1, "Unsupported security inventory"
    )

    exceptions = data.get("exceptions", [])
    validate_exceptions(exceptions, now)

    approved_refs = {exception_ref(item) for item in exceptions}
    source(data.get("source_sha"))

    image_source = data.get("image_source_sha", data["source_sha"])
    source(image_source)
    digest(data.get("chart_archive_sha256"), prefix=False)
    digest(data.get("values_matrix_identity"))

    images = data.get("image_manifest_digests")
    require(
        isinstance(images, dict) and set(images) == {"api", "backend"},
        "Both scanned images are required",
    )

    for identity in images.values():
        digest(identity)

    expected = set(required_targets(data.get("values_variants")))
    reports = data.get("reports")
    require(
        isinstance(reports, list) and len(reports) == len(expected),
        "Missing or duplicate scanner result",
    )
    require(
        {item.get("target") for item in reports} == expected, "Incomplete scanner target coverage"
    )

    for report in reports:
        require(
            report.get("scanner_version") == expected_version(report["target"]),
            "Unexpected scanner version",
        )
        digest(report.get("rule_identity"))

        for field in (
            "source_sha",
            "image_manifest_digests",
            "chart_archive_sha256",
            "values_matrix_identity",
        ):
            require(report.get(field) == data[field], "Scanner input identity mismatch")

        require(
            report.get("image_source_sha", report.get("source_sha")) == image_source,
            "Scanner image source identity mismatch",
        )
        fresh(report.get("scan_time"), now)
        # Refresh time is distinct from the publisher's last content modification.
        fresh(report.get("database_checked_at"), now)

        count = report.get("finding_count")
        exempt = report.get("exception_count", 0)
        refs = report.get("exception_refs", [])
        require(
            type(count) is int
            and type(exempt) is int
            and 0 <= exempt <= count
            and isinstance(refs, list)
            and len(refs) == len(set(refs))
            and set(refs) <= approved_refs
            and (bool(refs) == (exempt > 0)),
            "Invalid security exception accounting",
        )

        allowed = {
            exception_ref(item)
            for item in exceptions
            if item["scanner"] == TARGETS.get(report["target"], "trivy")
        }
        require(
            set(refs) <= allowed
            and type(report.get("blocking_count")) is int
            and 0 <= report["blocking_count"] <= count
            and type(report.get("scanner_exit")) is int,
            "Invalid scanner finding accounting",
        )

        successful_exit = report.get("scanner_exit") == 0 or (
            report.get("scanner_exit") == 1 and count > 0 and exempt == count and bool(refs)
        )
        require(
            successful_exit
            and report.get("blocking_count") == 0
            and report.get("coverage_gaps") == [],
            "Scanner failure, blocking finding or unresolved coverage gap",
        )

    return {
        "conclusion": "success",
        "source_sha": data["source_sha"],
        "scan_time": now.isoformat(),
        "image_source_sha": image_source,
        "image_manifest_digests": images,
        "chart_archive_sha256": data["chart_archive_sha256"],
        "values_matrix_identity": data["values_matrix_identity"],
        "coverage_gaps": [],
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--stage", choices=["complete"], required=True)
    parser.add_argument("--inventory", type=Path, required=True)

    args = parser.parse_args()

    try:
        report = validate(json.loads(args.inventory.read_bytes()))
    except OSError, ValueError, KeyError, TypeError, AttributeError:
        print(
            "FAIL: Security Check has incomplete, stale, failed or mismatched evidence",
            file=sys.stderr,
        )
        return 1

    print(json.dumps(report, sort_keys=True))
    return 0


if __name__ == "__main__":
    sys.exit(main())
