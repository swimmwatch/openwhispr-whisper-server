"""Persist release state and immutable chart files without version replacement."""

import copy
import hashlib
import json
import os
import shutil
import tempfile
from pathlib import Path
from typing import Any

from tools.release.validation import digest, require


def merge_index(previous: dict[str, Any], incoming: dict[str, Any]) -> dict[str, Any]:
    """Preserve existing Helm entries; reject a different digest for an existing version."""
    for index in (previous, incoming):
        require(
            isinstance(index, dict)
            and index.get("apiVersion") == "v1"
            and isinstance(index.get("entries"), dict),
            "Invalid Helm index",
        )

    merged = copy.deepcopy(previous)
    for name, entries in incoming["entries"].items():
        require(
            name == "whisper-server" and isinstance(entries, list), "Unexpected chart index entry"
        )

        retained = merged["entries"].setdefault(name, [])
        for entry in entries:
            require(
                isinstance(entry, dict) and isinstance(entry.get("version"), str),
                "Invalid chart version entry",
            )
            digest(entry.get("digest"), prefix=False)

            existing = [item for item in retained if item.get("version") == entry["version"]]
            require(len(existing) <= 1, "Duplicate existing chart version")

            if existing:
                require(
                    existing[0].get("digest") == entry["digest"],
                    "Published chart version has different bytes",
                )
            else:
                retained.insert(0, copy.deepcopy(entry))

    return merged


def immutable_copy(source_path: Path | str, target_path: Path | str, expected: str) -> None:
    """Atomically install local publisher bytes without replacing an existing version."""
    digest(expected, prefix=False)

    content = Path(source_path).read_bytes()
    require(hashlib.sha256(content).hexdigest() == expected, "Source archive identity mismatch")

    target = Path(target_path)
    target.parent.mkdir(parents=True, exist_ok=True)

    with tempfile.NamedTemporaryFile(dir=target.parent, delete=False) as temporary:
        pending = Path(temporary.name)

        try:
            temporary.write(content)
            os.fchmod(temporary.fileno(), 0o644)
            temporary.flush()
            os.fsync(temporary.fileno())

            try:
                os.link(pending, target)
            except FileExistsError:
                require(
                    hashlib.sha256(target.read_bytes()).hexdigest() == expected,
                    "Published artifact version has different bytes",
                )
        finally:
            pending.unlink(missing_ok=True)


def save(path: Path, data: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)

    with tempfile.NamedTemporaryFile(mode="w", dir=path.parent, delete=False) as stream:
        temporary = Path(stream.name)
        json.dump(data, stream, sort_keys=True, indent=2)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())

    temporary.replace(path)


def link_or_copy(source: str, target: str) -> None:
    """Transfer immutable archives by hardlink, or copy across filesystems."""
    try:
        os.link(source, target)
    except OSError:
        shutil.copyfile(source, target)
