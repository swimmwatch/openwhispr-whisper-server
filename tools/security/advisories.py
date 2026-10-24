"""Review native component advisories independently of scanner report execution."""

from __future__ import annotations

import hashlib
import json
import re
import urllib.request
from dataclasses import dataclass
from datetime import UTC, date, datetime
from pathlib import Path
from typing import Any

from tools.release.validation import require


@dataclass(frozen=True)
class NativeAdvisoryReview:
    """One selected inventory and its evaluation time; no process or report state."""

    data: dict[str, Any]
    now: datetime

    @classmethod
    def load(cls, path: Path, now: datetime | None = None) -> NativeAdvisoryReview:
        return cls(json.loads(path.read_bytes()), now or datetime.now(UTC))

    def coverage(self, role: str, build_identity: str) -> list[str]:
        require(
            self.data.get("schema_version") == 1
            and self.data.get("build_input_identity") == build_identity,
            "Native component inventory drift",
        )

        expected = {"api": {"ffmpeg"}, "backend": {"whisper.cpp", "cuda"}}[role]
        records = [item for item in self.data["components"] if item.get("role") == role]
        require(
            len(records) == len(expected) and {item.get("name") for item in records} == expected,
            "Incomplete native component inventory",
        )

        gaps = []
        for item in records:
            require(
                date.fromisoformat(item.get("expires", "")) > self.now.date(),
                "Expired component advisory review",
            )

            require(
                isinstance(item.get("owner"), str) and item["owner"] and item.get("sources"),
                "Missing advisory review ownership",
            )

            for evidence in item["sources"]:
                require(
                    evidence.get("url", "").startswith("https://")
                    and re.fullmatch("[a-f0-9]{64}", evidence.get("sha256", "")),
                    "Missing advisory source identity",
                )

                with urllib.request.urlopen(evidence["url"], timeout=90) as response:
                    require(
                        hashlib.sha256(response.read()).hexdigest() == evidence["sha256"],
                        "Advisory source changed; review required",
                    )

            require(
                item.get("conclusion")
                in ("no_applicable_blocking_advisories", "unresolved", "blocking"),
                "Unsupported advisory disposition",
            )

            require(
                isinstance(item.get("rationale"), str) and item["rationale"].strip(),
                "Missing component advisory rationale",
            )

            advisories = item.get("advisories")
            require(
                isinstance(advisories, list)
                and len({entry.get("id") for entry in advisories}) == len(advisories),
                "Missing native advisory accounting",
            )

            for entry in advisories:
                require(
                    re.fullmatch(r"CVE-\d{4}-\d{4,}", entry.get("id", ""))
                    and entry.get("severity") in ("LOW", "MEDIUM", "HIGH", "CRITICAL", "UNKNOWN")
                    and entry.get("disposition") in ("applicable", "not_applicable", "unresolved")
                    and isinstance(entry.get("rationale"), str)
                    and entry["rationale"].strip(),
                    "Invalid native advisory entry",
                )

                if entry["disposition"] == "unresolved" or (
                    entry["disposition"] == "applicable"
                    and entry["severity"] in ("HIGH", "CRITICAL", "UNKNOWN")
                ):
                    gaps.append(item["name"] + ":" + entry["id"] + ":blocking-or-unresolved")

            if item["conclusion"] != "no_applicable_blocking_advisories":
                gaps.append(item["name"] + "-advisory-" + item["conclusion"])

        return gaps

    def findings(self, role: str) -> list[dict[str, str]]:
        return [
            {
                "advisory": entry["id"],
                "package": component["name"],
                "severity": entry["severity"],
                "status": "applicable",
                "source": "native-advisory-review",
            }
            for component in self.data["components"]
            if component["role"] == role
            for entry in component["advisories"]
            if entry["disposition"] == "applicable"
        ]


def component_coverage(
    path: Path | None, role: str, build_identity: str, now: datetime | None = None
) -> list[str]:
    if path is None or not path.is_file():
        return ["custom-native-advisory-reconciliation-required"]

    return NativeAdvisoryReview.load(path, now).coverage(role, build_identity)


def component_findings(path: Path | None, role: str) -> list[dict[str, str]]:
    """Preserve applicable findings that an image scanner cannot identify."""
    if path is None or not path.is_file():
        return []

    return NativeAdvisoryReview.load(path).findings(role)
