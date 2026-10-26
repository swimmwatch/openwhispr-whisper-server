#!/usr/bin/env python3
"""Observe one explicitly selected GitHub run attempt; never publish or mutate GitHub.

Selection schema 1 is either {schema_version: 1, armed: false}, or armed: true
with Target fields and absolute armed_at/deadline UTC timestamps. Probe never
writes state, acknowledges a result, follows API URLs, or resets the deadline.
A successful pipeline still requires the parent's independent artifact readback.
This POSIX CLI owns and kills its gh process group; it is not a daemon.
"""

import argparse
import contextlib
import fcntl
import json
import os
import re
import selectors
import signal
import stat
import subprocess
import sys
import tempfile
import time
from dataclasses import asdict, dataclass
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any, Never

MAX_SELECTION_BYTES = 8192
MAX_GH_BYTES = 512 * 1024
MAX_TIMEOUT_SECONDS = 86400
GH_TIMEOUT_SECONDS = 4.0
GH_ATTEMPTS = 2
RETRY_DELAY_SECONDS = 0.25
PROCESS_REAP_SECONDS = 0.5
MAX_ID = 2**63 - 1
WORKFLOW_PATHS = frozenset({".github/workflows/release.yml", ".github/workflows/ci.yml"})
EVENTS = frozenset({"workflow_dispatch", "release", "pull_request"})
PENDING_STATUSES = frozenset({"waiting", "queued", "requested", "pending", "in_progress"})
CONCLUSIONS = frozenset(
    {
        "success",
        "failure",
        "cancelled",
        "timed_out",
        "action_required",
        "stale",
        "skipped",
        "startup_failure",
        "neutral",
    }
)
TARGET_FIELDS = frozenset(
    {"repository", "run_id", "attempt", "head_sha", "workflow_id", "workflow_path", "event"}
)
UNARMED = {"schema_version": 1, "armed": False}


class WatchError(Exception):
    """A fixed diagnostic code, never an external error body or credential."""

    def __init__(self, code: str) -> None:
        super().__init__(code)
        self.code = code


def require(condition: object, code: str = "invalid_selection") -> None:
    if not condition:
        raise WatchError(code)


def utc_now() -> datetime:
    return datetime.now(UTC)


def timestamp(instant: datetime) -> str:
    return instant.astimezone(UTC).isoformat(timespec="microseconds").replace("+00:00", "Z")


def parse_timestamp(value: object) -> datetime:
    require(
        isinstance(value, str)
        and re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z", value)
    )
    try:
        return datetime.fromisoformat(str(value))
    except ValueError:
        raise WatchError("invalid_selection") from None


def positive_id(value: object) -> bool:
    return type(value) is int and 1 <= value <= MAX_ID


@dataclass(frozen=True)
class Target:
    repository: str
    run_id: int
    attempt: int
    head_sha: str
    workflow_id: int
    workflow_path: str
    event: str

    @classmethod
    def from_data(cls, data: dict[str, Any]) -> Target:
        require(set(data) == TARGET_FIELDS)
        repository = data["repository"]
        require(isinstance(repository, str) and len(repository) <= 140)
        owner, separator, name = repository.partition("/")
        require(
            separator
            and re.fullmatch(r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?", owner)
            and "--" not in owner
            and re.fullmatch(r"[A-Za-z0-9_.-]{1,100}", name)
            and name not in {".", ".."}
        )
        require(all(positive_id(data[key]) for key in ("run_id", "attempt", "workflow_id")))
        require(
            isinstance(data["head_sha"], str) and re.fullmatch(r"[0-9a-f]{40}", data["head_sha"])
        )
        require(isinstance(data["workflow_path"], str) and data["workflow_path"] in WORKFLOW_PATHS)
        require(isinstance(data["event"], str) and data["event"] in EVENTS)
        return cls(**data)

    def to_data(self) -> dict[str, Any]:
        return asdict(self)

    def run_url(self) -> str:
        return (
            f"https://github.com/{self.repository}/actions/runs/{self.run_id}"
            f"/attempts/{self.attempt}"
        )


def unique_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        require(key not in result, "invalid_json")
        result[key] = value
    return result


def decode_object(raw: bytes) -> dict[str, Any]:
    try:
        data: Any = json.loads(raw, object_pairs_hook=unique_object)
    except ValueError, UnicodeError, RecursionError:
        raise WatchError("invalid_json") from None
    require(isinstance(data, dict), "invalid_json")
    return data


def load_selection(path: Path) -> dict[str, Any]:
    # Nonblocking open also bounds malformed input such as a FIFO, not just its bytes.
    descriptor = os.open(path, os.O_RDONLY | os.O_NONBLOCK)
    with os.fdopen(descriptor, "rb") as stream:
        require(stat.S_ISREG(os.fstat(stream.fileno()).st_mode))
        raw = stream.read(MAX_SELECTION_BYTES + 1)
    require(len(raw) <= MAX_SELECTION_BYTES)
    data = decode_object(raw)
    require(type(data.get("schema_version")) is int and data["schema_version"] == 1)
    require(type(data.get("armed")) is bool)
    if not data["armed"]:
        require(set(data) == set(UNARMED))
        return data
    require(set(data) == TARGET_FIELDS | {"schema_version", "armed", "armed_at", "deadline"})
    Target.from_data({key: data[key] for key in TARGET_FIELDS})
    armed_at, deadline = parse_timestamp(data["armed_at"]), parse_timestamp(data["deadline"])
    require(timedelta(seconds=1) <= deadline - armed_at <= timedelta(seconds=MAX_TIMEOUT_SECONDS))
    require(armed_at <= utc_now())
    return data


def atomic_write(path: Path, data: dict[str, Any]) -> None:
    """The writer lock covers validation and replacement; readers see old or new JSON."""
    descriptor, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    temporary_path = Path(temporary)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(data, stream, sort_keys=True, separators=(",", ":"))
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary_path, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        temporary_path.unlink(missing_ok=True)


def writer_lock(path: Path) -> int:
    """Keep a stable sibling lock inode; probe never opens or creates this file."""
    descriptor = os.open(path.with_name(path.name + ".lock"), os.O_CREAT | os.O_RDWR, 0o600)
    try:
        fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        os.close(descriptor)
        raise WatchError("selection_busy") from None
    return descriptor


def initialize(path: Path) -> None:
    descriptor = writer_lock(path)
    try:
        require(not os.path.lexists(path), "selection_exists")
        atomic_write(path, dict(UNARMED))
    finally:
        os.close(descriptor)


def matches_identity(target: Target, run: dict[str, Any]) -> bool:
    repository = run.get("repository")
    path = run.get("path")
    # GitHub may append @ref to path. It is not a different workflow identity:
    # the base path, workflow ID and run head SHA must still match independently.
    path_matches = isinstance(path, str) and (
        path == target.workflow_path
        or re.fullmatch(re.escape(target.workflow_path) + r"@[A-Za-z0-9_./-]{1,255}", path)
        is not None
    )
    return bool(
        isinstance(repository, dict)
        and repository.get("full_name") == target.repository
        and positive_id(run.get("id"))
        and run["id"] == target.run_id
        and positive_id(run.get("run_attempt"))
        and run["run_attempt"] == target.attempt
        and run.get("head_sha") == target.head_sha
        and positive_id(run.get("workflow_id"))
        and run["workflow_id"] == target.workflow_id
        and path_matches
        and run.get("event") == target.event
    )


def observed_state(run: dict[str, Any]) -> tuple[str, str | None]:
    status: object = run.get("status")
    conclusion: object = run.get("conclusion")
    if not isinstance(status, str) or "conclusion" not in run:
        raise WatchError("invalid_state")
    if status in PENDING_STATUSES:
        require(conclusion is None, "invalid_state")
        return status, None
    if status != "completed" or not isinstance(conclusion, str) or conclusion not in CONCLUSIONS:
        raise WatchError("invalid_state")
    return status, conclusion


def gh_output(target: Target, *, timeout_seconds: float = GH_TIMEOUT_SECONDS) -> bytes:
    """Bound stdout while streaming, and kill/reap the owned group on every exit."""
    endpoint = f"repos/{target.repository}/actions/runs/{target.run_id}/attempts/{target.attempt}"
    environment = {**os.environ, "GH_PROMPT_DISABLED": "1", "GH_NO_UPDATE_NOTIFIER": "1"}
    try:
        process = subprocess.Popen(
            ["gh", "api", "--hostname", "github.com", "--method", "GET", endpoint],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            start_new_session=True,
            env=environment,
        )
    except OSError:
        raise WatchError("github_unavailable") from None
    deadline = time.monotonic() + min(timeout_seconds, GH_TIMEOUT_SECONDS)
    output = bytearray()
    assert process.stdout is not None
    try:
        os.set_blocking(process.stdout.fileno(), False)
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout, selectors.EVENT_READ)
            while selector.get_map():
                remaining = deadline - time.monotonic()
                require(remaining > 0, "github_timeout")
                for key, _ in selector.select(remaining):
                    chunk = os.read(key.fd, min(65536, MAX_GH_BYTES + 1 - len(output)))
                    if not chunk:
                        selector.unregister(key.fd)
                        break
                    output.extend(chunk)
                    require(len(output) <= MAX_GH_BYTES, "github_output_limit")
        try:
            code = process.wait(timeout=max(0, deadline - time.monotonic()))
        except subprocess.TimeoutExpired:
            raise WatchError("github_timeout") from None
        require(code == 0, "github_unavailable")
        return bytes(output)
    finally:
        with contextlib.suppress(ProcessLookupError):
            os.killpg(process.pid, signal.SIGKILL)
        process.stdout.close()
        try:
            process.wait(timeout=PROCESS_REAP_SECONDS)
        except subprocess.TimeoutExpired:
            raise WatchError("github_cleanup_failed") from None


def fetch_run(target: Target, *, timeout_seconds: float | None = None) -> dict[str, Any]:
    budget = GH_TIMEOUT_SECONDS * GH_ATTEMPTS + RETRY_DELAY_SECONDS
    deadline = time.monotonic() + (
        budget if timeout_seconds is None else min(budget, timeout_seconds)
    )
    for attempt in range(GH_ATTEMPTS):
        remaining = deadline - time.monotonic()
        require(remaining > 0, "github_timeout")
        try:
            return decode_object(
                gh_output(target, timeout_seconds=min(GH_TIMEOUT_SECONDS, remaining))
            )
        except WatchError as error:
            if (
                error.code not in {"github_unavailable", "github_timeout"}
                or attempt + 1 == GH_ATTEMPTS
            ):
                raise
            remaining = deadline - time.monotonic()
            require(remaining > RETRY_DELAY_SECONDS, "github_timeout")
            time.sleep(RETRY_DELAY_SECONDS)
    raise WatchError("github_unavailable")


def arm(path: Path, target: Target, timeout: int, *, replace: bool = False) -> dict[str, Any]:
    require(type(timeout) is int and 1 <= timeout <= MAX_TIMEOUT_SECONDS, "invalid_timeout")
    # Validate even for callers constructing Target directly instead of via CLI.
    Target.from_data(target.to_data())
    descriptor = writer_lock(path)
    try:
        previous = load_selection(path)
        if previous["armed"]:
            require(replace, "selection_already_armed")
            require(
                (previous["repository"], previous["run_id"], previous["attempt"])
                != (target.repository, target.run_id, target.attempt),
                "same_attempt_deadline_is_immutable",
            )
        run = fetch_run(target)
        require(matches_identity(target, run), "identity_mismatch")
        observed_state(run)
        now = utc_now()
        selected = {
            "schema_version": 1,
            "armed": True,
            **target.to_data(),
            "armed_at": timestamp(now),
            "deadline": timestamp(now + timedelta(seconds=timeout)),
        }
        atomic_write(path, selected)
        return selected
    finally:
        os.close(descriptor)


def wake_result(
    target: Target, reason: str, *, status: str | None = None, conclusion: str | None = None
) -> dict[str, Any]:
    return {
        "wake": True,
        "reason": reason,
        "repository": target.repository,
        "run_id": target.run_id,
        "attempt": target.attempt,
        "run_url": target.run_url(),
        "status": status,
        "conclusion": conclusion,
    }


def probe(path: Path) -> dict[str, Any]:
    target: Target | None = None
    try:
        selected = load_selection(path)
        if not selected["armed"]:
            return {"wake": False}
        target = Target.from_data({key: selected[key] for key in TARGET_FIELDS})
        deadline = parse_timestamp(selected["deadline"])
        if utc_now() >= deadline:
            return wake_result(target, "expired")
        try:
            run = fetch_run(target, timeout_seconds=(deadline - utc_now()).total_seconds())
        except WatchError, OSError:
            if utc_now() >= deadline:
                return wake_result(target, "expired")
            raise
        if utc_now() >= deadline:
            return wake_result(target, "expired")
        if not matches_identity(target, run):
            return wake_result(target, "identity_mismatch")
        status, conclusion = observed_state(run)
        if status in PENDING_STATUSES:
            return {"wake": False}
        return wake_result(
            target,
            "pipeline_success_requires_artifact_readback"
            if conclusion == "success"
            else "pipeline_unsuccessful",
            status=status,
            conclusion=conclusion,
        )
    except WatchError, OSError:
        return (
            wake_result(target, "observation_error")
            if target
            else {"wake": True, "reason": "observation_error"}
        )


class SafeParser(argparse.ArgumentParser):
    def error(self, message: str) -> Never:
        raise WatchError("invalid_arguments")


def main(argv: list[str] | None = None) -> int:
    parser = SafeParser(prog="watch_pipeline.py", add_help=False, allow_abbrev=False)
    parser.add_argument("mode", choices=("init", "arm", "probe"))
    parser.add_argument("--selection", type=Path, required=True)
    for key in sorted(TARGET_FIELDS):
        parser.add_argument("--" + key.replace("_", "-"))
    parser.add_argument("--timeout-seconds")
    parser.add_argument("--replace", action="store_true")
    parser.add_argument("-h", "--help", action="store_true", help="show structured JSON help")
    arguments = sys.argv[1:] if argv is None else argv
    if arguments in (["--help"], ["-h"]) or (
        len(arguments) == 2
        and arguments[0] in {"init", "arm", "probe"}
        and arguments[1] in {"--help", "-h"}
    ):
        print(
            json.dumps(
                {
                    "wake": False,
                    "reason": "help",
                    "usage": parser.format_help(),
                    "workflow_paths": sorted(WORKFLOW_PATHS),
                    "events": sorted(EVENTS),
                    "timeout_seconds": {"minimum": 1, "maximum": MAX_TIMEOUT_SECONDS},
                },
                sort_keys=True,
                separators=(",", ":"),
            )
        )
        return 0
    try:
        args = parser.parse_args(arguments)
        require(not args.help, "invalid_arguments")
        supplied = {key: getattr(args, key) for key in TARGET_FIELDS}
        if args.mode != "arm":
            require(
                not any(value is not None for value in supplied.values())
                and args.timeout_seconds is None
                and not args.replace,
                "invalid_arguments",
            )
        if args.mode == "init":
            initialize(args.selection)
            result: dict[str, Any] = {"wake": False}
        elif args.mode == "probe":
            result = probe(args.selection)
        else:
            require(all(value is not None for value in supplied.values()), "invalid_arguments")
            for key in ("run_id", "attempt", "workflow_id"):
                supplied[key] = int(supplied[key])
            selected = arm(
                args.selection,
                Target.from_data(supplied),
                int(args.timeout_seconds),
                replace=args.replace,
            )
            result = {"wake": False, "reason": "armed", "deadline": selected["deadline"]}
    except (WatchError, OSError, ValueError, TypeError) as error:
        result = {
            "wake": False,
            "reason": error.code if isinstance(error, WatchError) else "invalid_arguments_or_io",
        }
        print(json.dumps(result, sort_keys=True, separators=(",", ":")))
        return 2
    print(json.dumps(result, sort_keys=True, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    sys.exit(main())
