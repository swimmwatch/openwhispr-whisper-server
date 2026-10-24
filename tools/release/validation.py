"""Validate immutable release identifiers at external input boundaries."""

import re


def require(condition: object, message: str) -> None:
    if not condition:
        raise ValueError(message)


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
