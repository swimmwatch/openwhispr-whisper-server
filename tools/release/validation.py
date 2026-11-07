"""Validate immutable release identifiers at external input boundaries."""

import re
from datetime import datetime, timedelta
from typing import cast


def require(condition: object, message: str) -> None:
    if not condition:
        raise ValueError(message)


def fresh(value: object, now: datetime) -> None:
    try:
        instant = datetime.fromisoformat(cast(str, value).replace("Z", "+00:00"))
        require(
            instant.tzinfo is not None and timedelta(0) <= now - instant <= timedelta(hours=24),
            "Stale or future security evidence",
        )
    except (AttributeError, TypeError, ValueError) as error:
        raise ValueError("Stale or invalid security timestamp") from error


def digest(value: object, prefix: bool = True) -> None:
    require(
        isinstance(value, str)
        and re.fullmatch(("sha256:" if prefix else "") + "[a-f0-9]{64}", value),
        "Invalid artifact digest",
    )


def source(value: object) -> None:
    require(
        isinstance(value, str) and re.fullmatch("[a-f0-9]{40}", value), "Invalid source revision"
    )
