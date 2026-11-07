"""Own POSIX scanner processes and keep their output private through cleanup."""

import contextlib
import os
import signal
import subprocess
import tempfile
import threading
from collections.abc import Mapping, Sequence
from pathlib import Path
from types import FrameType
from typing import Never


def _interrupt(_signum: int, _frame: FrameType | None) -> Never:
    raise InterruptedError("Scanner execution interrupted")


def run_private(
    command: Sequence[str], *, cwd: Path, env: Mapping[str, str], timeout: float
) -> subprocess.CompletedProcess[bytes]:
    """Kill the owned group even if the scanner exits before its helper processes."""
    previous = (
        signal.signal(signal.SIGTERM, _interrupt)
        if threading.current_thread() is threading.main_thread()
        else None
    )
    try:
        with tempfile.TemporaryFile() as stdout:
            process = subprocess.Popen(
                command,
                cwd=cwd,
                env=env,
                stdin=subprocess.DEVNULL,
                stdout=stdout,
                stderr=subprocess.DEVNULL,
                start_new_session=True,
            )
            try:
                process.wait(timeout=timeout)
            finally:
                if previous is not None:
                    signal.signal(signal.SIGTERM, signal.SIG_IGN)
                with contextlib.suppress(ProcessLookupError):
                    os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)
            stdout.seek(0)
            return subprocess.CompletedProcess(command, process.returncode, stdout.read())
    finally:
        if previous is not None:
            signal.signal(signal.SIGTERM, previous)
