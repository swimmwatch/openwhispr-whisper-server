"""Exact, expiring security exception policies and stable evidence identities."""

from __future__ import annotations

import hashlib
import json
import re
from datetime import UTC, date, datetime
from pathlib import Path
from typing import Any

from tools.release.validation import require

ROOT = Path(__file__).resolve().parents[2]


def exception_ref(item: dict[str, Any]) -> str:
    return (
        "sha256:"
        + hashlib.sha256(
            json.dumps(item, sort_keys=True, separators=(",", ":")).encode()
        ).hexdigest()
    )


def validate_exceptions(exceptions: Any, now: datetime | None = None, root: Path = ROOT) -> None:
    today = (now or datetime.now(UTC)).date()
    require(isinstance(exceptions, list), "Invalid security exception inventory")

    seen = set()
    for item in exceptions:
        require(
            isinstance(item, dict)
            and set(item)
            == {
                "scanner",
                "rule",
                "path",
                "disposition",
                "owner",
                "rationale",
                "evidence",
                "expires",
            },
            "Incomplete security exception",
        )

        require(
            item["scanner"] in ("gosec", "hadolint", "trivy", "govulncheck")
            and item["disposition"] in ("false_positive", "not_applicable"),
            "Unapproved exception disposition",
        )

        for key in ("rule", "path", "owner", "rationale", "evidence"):
            require(
                isinstance(item[key], str) and bool(item[key].strip()) and len(item[key]) <= 1000,
                "Missing security exception justification",
            )

        require(
            not re.search(r"[*?\[\]]", item["rule"] + item["path"])
            and not item["path"].startswith("/")
            and ".." not in Path(item["path"]).parts,
            "Security exception must have exact repository scope",
        )

        require(
            re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.:-]*", item["rule"]) is not None,
            "Security exception must identify one rule",
        )

        if item["scanner"] in ("gosec", "hadolint"):
            require(
                (root / item["path"]).is_file(), "Security exception must identify one source file"
            )

        require(
            not Path(item["evidence"]).is_absolute()
            and ".." not in Path(item["evidence"]).parts
            and (root / item["evidence"]).is_file(),
            "Missing security exception evidence file",
        )

        try:
            require(date.fromisoformat(item["expires"]) > today, "Expired security exception")
        except (TypeError, ValueError) as error:
            raise ValueError("Invalid security exception expiry") from error

        identity = (item["scanner"], item["rule"], item["path"])
        require(identity not in seen, "Duplicate security exception")
        seen.add(identity)
