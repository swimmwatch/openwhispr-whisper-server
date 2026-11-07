import contextlib
import copy
import io
import json
import os
import subprocess
import sys
import tempfile
import time
import unittest
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any
from unittest.mock import patch

from tools.release import watch_pipeline as watch


class PipelineWatchTests(unittest.TestCase):
    def setUp(self) -> None:
        temporary = tempfile.TemporaryDirectory(prefix="pipeline-watch-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.selection = self.root / "selection.json"
        self.now = datetime(2026, 10, 26, 10, 0, tzinfo=UTC)
        clock = patch.object(watch, "utc_now", return_value=self.now)
        self.clock = clock.start()
        self.addCleanup(clock.stop)
        self.target = watch.Target.from_data(
            {
                "repository": "example/project",
                "run_id": 40212576458,
                "attempt": 1,
                "head_sha": "a" * 40,
                "workflow_id": 395360911,
                "workflow_path": ".github/workflows/release.yml",
                "event": "workflow_dispatch",
            }
        )
        self.observation: dict[str, Any] = {
            "repository": {"full_name": self.target.repository},
            "id": self.target.run_id,
            "run_attempt": self.target.attempt,
            "head_sha": self.target.head_sha,
            "workflow_id": self.target.workflow_id,
            "path": self.target.workflow_path,
            "event": self.target.event,
            "status": "queued",
            "conclusion": None,
            "html_url": "https://evil.invalid/ghp_SECRET",
            "logs_url": "ghp_SECRET",
        }

    def cli(self, *args: str) -> tuple[int, dict[str, Any], str]:
        stdout, stderr = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            code = watch.main(list(args))
        return code, json.loads(stdout.getvalue()), stderr.getvalue()

    def arm(self, *, timeout: int = 3600, replace: bool = False) -> None:
        if not self.selection.exists():
            watch.initialize(self.selection)
        with patch.object(watch, "fetch_run", return_value=self.observation):
            watch.arm(self.selection, self.target, timeout, replace=replace)

    def test_init_and_unarmed_probe_are_fixed_and_do_not_use_github(self) -> None:
        with patch.object(watch, "fetch_run") as fetch:
            code, result, stderr = self.cli("init", "--selection", str(self.selection))
            self.assertEqual((code, result, stderr), (0, {"wake": False}, ""))
            before = self.selection.read_bytes()
            for _ in range(2):
                self.assertEqual(watch.probe(self.selection), {"wake": False})
            self.assertEqual(self.selection.read_bytes(), before)
            fetch.assert_not_called()
        code, result, _ = self.cli("init", "--selection", str(self.selection))
        self.assertEqual(code, 2)
        self.assertFalse(result["wake"])
        self.assertEqual(self.selection.read_bytes(), before)

    def test_help_is_structured_success_without_selection_or_network(self) -> None:
        with patch.object(watch, "fetch_run") as fetch:
            for arguments in (
                ("--help",),
                ("init", "--help"),
                ("arm", "--help"),
                ("probe", "--help"),
            ):
                with self.subTest(arguments=arguments):
                    code, result, stderr = self.cli(*arguments)
                    self.assertEqual((code, stderr), (0, ""))
                    self.assertIn("--head-sha", result["usage"])
                    self.assertIn("--selection", result["usage"])
                    self.assertFalse(result["wake"])
            fetch.assert_not_called()
        self.assertFalse(self.selection.exists())

    def test_init_never_replaces_an_existing_selection_entry(self) -> None:
        self.selection.symlink_to(self.root / "missing-target.json")
        with self.assertRaises(watch.WatchError):
            watch.initialize(self.selection)
        self.assertTrue(self.selection.is_symlink())
        self.assertEqual(os.readlink(self.selection), str(self.root / "missing-target.json"))

    def test_arm_validates_then_writes_private_atomic_selection(self) -> None:
        self.arm()
        selected = json.loads(self.selection.read_text())
        self.assertTrue(selected["armed"])
        self.assertEqual(selected["run_id"], self.target.run_id)
        self.assertEqual(selected["deadline"], "2026-10-26T11:00:00.000000Z")
        self.assertEqual(self.selection.stat().st_mode & 0o777, 0o600)
        self.assertNotIn("ghp_SECRET", self.selection.read_text())

    def test_each_identity_mismatch_wakes_and_cannot_arm(self) -> None:
        self.arm()
        original = self.selection.read_bytes()
        changes: dict[str, object] = {
            "repository": {"full_name": "foreign/project"},
            "id": self.target.run_id + 1,
            "run_attempt": 2,
            "head_sha": "b" * 40,
            "workflow_id": self.target.workflow_id + 1,
            "path": ".github/workflows/foreign.yml",
            "event": "release",
        }
        for key, value in changes.items():
            with self.subTest(key=key):
                changed = copy.deepcopy(self.observation)
                changed[key] = value
                with patch.object(watch, "fetch_run", return_value=changed):
                    self.assertEqual(watch.probe(self.selection)["reason"], "identity_mismatch")
                    other = self.root / (key + ".json")
                    watch.initialize(other)
                    with self.assertRaises(watch.WatchError):
                        watch.arm(other, self.target, 3600)
                    self.assertFalse(json.loads(other.read_text())["armed"])
                self.assertEqual(self.selection.read_bytes(), original)

    def test_pending_states_are_identical_read_only_results(self) -> None:
        self.arm()
        before = self.selection.stat()
        contents = self.selection.read_bytes()
        files = sorted(self.root.iterdir())
        for status in ("queued", "requested", "pending", "in_progress"):
            with self.subTest(status=status):
                self.observation["status"] = status
                with patch.object(watch, "fetch_run", return_value=self.observation):
                    self.assertEqual(watch.probe(self.selection), {"wake": False})
        self.assertEqual(self.selection.read_bytes(), contents)
        self.assertEqual(self.selection.stat().st_mtime_ns, before.st_mtime_ns)
        self.assertEqual(sorted(self.root.iterdir()), files)

    def test_waiting_for_protected_environment_wakes_before_completion(self) -> None:
        self.arm()
        self.observation["status"] = "waiting"
        with patch.object(watch, "fetch_run", return_value=self.observation):
            code, result, stderr = self.cli("probe", "--selection", str(self.selection))
        self.assertEqual((code, stderr), (0, ""))
        self.assertTrue(result["wake"])
        self.assertEqual(result["reason"], "pipeline_waiting_requires_attention")
        self.assertEqual(result["status"], "waiting")
        self.assertIsNone(result["conclusion"])

    def test_terminal_conclusions_wake_and_never_claim_qualification(self) -> None:
        self.arm()
        self.observation["status"] = "completed"
        for conclusion in (
            "success",
            "failure",
            "cancelled",
            "timed_out",
            "action_required",
            "stale",
            "skipped",
            "startup_failure",
            "neutral",
        ):
            with self.subTest(conclusion=conclusion):
                self.observation["conclusion"] = conclusion
                with patch.object(watch, "fetch_run", return_value=self.observation):
                    result = watch.probe(self.selection)
                self.assertTrue(result["wake"])
                self.assertEqual(result["conclusion"], conclusion)
                self.assertEqual(
                    result["reason"],
                    "pipeline_success_requires_artifact_readback"
                    if conclusion == "success"
                    else "pipeline_unsuccessful",
                )
                self.assertEqual(
                    result["run_url"],
                    "https://github.com/example/project/actions/runs/40212576458/attempts/1",
                )
                self.assertNotIn("ghp_SECRET", json.dumps(result))

    def test_unknown_or_contradictory_states_fail_closed(self) -> None:
        self.arm()
        for status, conclusion in (
            ("alien", None),
            (None, None),
            ("completed", None),
            ("completed", "alien"),
            ("queued", "success"),
            ("success", "success"),
            ([], None),
        ):
            with self.subTest(status=status, conclusion=conclusion):
                self.observation.update(status=status, conclusion=conclusion)
                with patch.object(watch, "fetch_run", return_value=self.observation):
                    result = watch.probe(self.selection)
                self.assertTrue(result["wake"])
                self.assertEqual(result["reason"], "observation_error")

    def test_missing_conclusion_is_not_treated_as_pending(self) -> None:
        self.arm()
        del self.observation["conclusion"]
        with patch.object(watch, "fetch_run", return_value=self.observation):
            self.assertEqual(watch.probe(self.selection)["reason"], "observation_error")

    def test_deadline_is_absolute_including_restarted_cli(self) -> None:
        self.arm(timeout=30)
        contents = self.selection.read_bytes()
        with (
            patch.object(watch, "utc_now", return_value=self.now + timedelta(seconds=30)),
            patch.object(watch, "fetch_run") as fetch,
        ):
            for _ in range(2):
                code, result, stderr = self.cli("probe", "--selection", str(self.selection))
                self.assertEqual((code, stderr), (0, ""))
                self.assertEqual(result["reason"], "expired")
            fetch.assert_not_called()
        self.assertEqual(contents, self.selection.read_bytes())

    def test_deadline_crossed_during_observation_overrides_every_result(self) -> None:
        self.arm(timeout=30)
        for conclusion in (None, "success", "failure"):
            with self.subTest(conclusion=conclusion):
                self.observation.update(
                    status="queued" if conclusion is None else "completed", conclusion=conclusion
                )

                def late_observation(*args: object, **kwargs: object) -> dict[str, Any]:
                    self.clock.return_value = self.now + timedelta(seconds=31)
                    return self.observation

                self.clock.return_value = self.now
                with patch.object(watch, "fetch_run", side_effect=late_observation):
                    self.assertEqual(watch.probe(self.selection)["reason"], "expired")

        def late_error(*args: object, **kwargs: object) -> dict[str, Any]:
            self.clock.return_value = self.now + timedelta(seconds=31)
            raise watch.WatchError("github_timeout")

        self.clock.return_value = self.now
        with patch.object(watch, "fetch_run", side_effect=late_error):
            self.assertEqual(watch.probe(self.selection)["reason"], "expired")

    def test_no_implicit_overwrite_and_replace_requires_new_valid_attempt(self) -> None:
        self.arm()
        contents = self.selection.read_bytes()
        with patch.object(watch, "fetch_run", return_value=self.observation) as fetch:
            with self.assertRaises(watch.WatchError):
                watch.arm(self.selection, self.target, 3600)
            with self.assertRaises(watch.WatchError):
                watch.arm(self.selection, self.target, 7200, replace=True)
            fetch.assert_not_called()
        self.assertEqual(contents, self.selection.read_bytes())
        target = watch.Target.from_data({**self.target.to_data(), "attempt": 2})
        with (
            patch.object(watch, "fetch_run", return_value=self.observation),
            self.assertRaises(watch.WatchError),
        ):
            watch.arm(self.selection, target, 3600, replace=True)
        self.assertEqual(contents, self.selection.read_bytes())
        self.observation["run_attempt"] = 2
        with patch.object(watch, "fetch_run", return_value=self.observation):
            watch.arm(self.selection, target, 3600, replace=True)
        self.assertEqual(json.loads(self.selection.read_text())["attempt"], 2)

    def test_uninitialized_or_invalid_selection_fails_closed_without_network(self) -> None:
        corrupt = [
            b"",
            b"{",
            b"[]",
            b"null",
            b"\xff",
            b'{"schema_version":1,"armed":false,"secret":"ghp_SECRET"}',
            b'{"schema_version":1,"armed":false,"armed":true}',
            b"x" * (watch.MAX_SELECTION_BYTES + 1),
            b"[" * 2000 + b"]" * 2000,
        ]
        with patch.object(watch, "fetch_run") as fetch:
            self.assertEqual(watch.probe(self.selection)["reason"], "observation_error")
            for content in corrupt:
                with self.subTest(content=content[:40]):
                    self.selection.write_bytes(content)
                    result = watch.probe(self.selection)
                    self.assertTrue(result["wake"])
                    self.assertEqual(result["reason"], "observation_error")
                    self.assertNotIn("ghp_SECRET", json.dumps(result))
            fetch.assert_not_called()

    def test_selection_fields_and_lifetime_are_validated_before_network(self) -> None:
        self.arm()
        selected = json.loads(self.selection.read_text())
        invalid: dict[str, object] = {
            "schema_version": True,
            "armed": 1,
            "run_id": True,
            "attempt": 0,
            "workflow_id": -1,
            "head_sha": "bad",
            "repository": "../foreign",
            "workflow_path": ".github/workflows/other.yml",
            "event": "push",
            "deadline": "2026-10-28T10:00:00.000000Z",
            "armed_at": "no date",
        }
        with patch.object(watch, "fetch_run") as fetch:
            for field, value in invalid.items():
                with self.subTest(field=field):
                    self.selection.write_text(json.dumps({**selected, field: value}))
                    self.assertEqual(watch.probe(self.selection)["reason"], "observation_error")
            fetch.assert_not_called()

    def test_invalid_targets_and_timeout_cannot_reach_github_or_change_state(self) -> None:
        watch.initialize(self.selection)
        before = self.selection.read_bytes()
        invalid: dict[str, list[object]] = {
            "repository": [
                "owner/repo/extra",
                "../repo",
                "owner/repo?x",
                "owner/%2f",
                "-owner/repo",
                "owner/..",
            ],
            "run_id": [0, -1, True, "1", 2**63],
            "attempt": [0, False],
            "workflow_id": [-1, None],
            "head_sha": ["a" * 39, "g" * 40],
            "workflow_path": ["https://evil.invalid", ".github/workflows/foreign.yml"],
            "event": ["push", "ghp_SECRET"],
        }
        with patch.object(watch, "fetch_run") as fetch:
            for key, values in invalid.items():
                for value in values:
                    with self.subTest(key=key, value=value), self.assertRaises(watch.WatchError):
                        target = watch.Target.from_data({**self.target.to_data(), key: value})
                        watch.arm(self.selection, target, 60)
            for timeout in (0, -1, watch.MAX_TIMEOUT_SECONDS + 1):
                with self.subTest(timeout=timeout), self.assertRaises(watch.WatchError):
                    watch.arm(self.selection, self.target, timeout)
            fetch.assert_not_called()
        self.assertEqual(self.selection.read_bytes(), before)

    def test_failed_atomic_arm_preserves_previous_state(self) -> None:
        watch.initialize(self.selection)
        before = self.selection.read_bytes()
        with (
            patch.object(watch, "fetch_run", return_value=self.observation),
            patch.object(watch.os, "replace", side_effect=OSError("ghp_SECRET")),
            self.assertRaises(OSError),
        ):
            watch.arm(self.selection, self.target, 60)
        self.assertEqual(self.selection.read_bytes(), before)
        self.assertEqual(
            sorted(path.name for path in self.root.iterdir()),
            ["selection.json", "selection.json.lock"],
        )

    def test_uninitialized_arm_and_concurrent_writer_do_not_overwrite(self) -> None:
        import fcntl

        with patch.object(watch, "fetch_run") as fetch:
            with self.assertRaises(OSError):
                watch.arm(self.selection, self.target, 60)
            watch.initialize(self.selection)
            before = self.selection.read_bytes()
            with self.selection.with_name("selection.json.lock").open("rb") as lock:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                with self.assertRaises(watch.WatchError):
                    watch.arm(self.selection, self.target, 60)
            self.assertEqual(self.selection.read_bytes(), before)
            fetch.assert_not_called()

    def test_api_path_ref_does_not_allow_different_workflow(self) -> None:
        self.arm()
        for path, valid in (
            (self.target.workflow_path + "@refs/heads/main", True),
            (self.target.workflow_path + "@", False),
            (self.target.workflow_path + "@ref\nsecret", False),
            ("other/repository/" + self.target.workflow_path + "@main", False),
        ):
            with self.subTest(path=path):
                self.observation["path"] = path
                with patch.object(watch, "fetch_run", return_value=self.observation):
                    self.assertEqual(watch.probe(self.selection)["wake"], not valid)

    def test_cli_arm_and_invalid_arguments_only_emit_safe_json(self) -> None:
        watch.initialize(self.selection)
        args = ["arm", "--selection", str(self.selection)]
        for key, value in self.target.to_data().items():
            args.extend(["--" + key.replace("_", "-"), str(value)])
        args.extend(["--timeout-seconds", "60"])
        with patch.object(watch, "fetch_run", return_value=self.observation):
            code, result, stderr = self.cli(*args)
        self.assertEqual((code, stderr), (0, ""))
        self.assertEqual(result["reason"], "armed")
        for invalid in (
            ["probe", "--selection", str(self.selection), "--token=ghp_SECRET"],
            ["arm", "--repository", "ghp_SECRET"],
            ["ghp_SECRET"],
        ):
            code, result, stderr = self.cli(*invalid)
            self.assertEqual(code, 2)
            self.assertEqual(stderr, "")
            self.assertNotIn("ghp_SECRET", json.dumps(result))

    def fake_gh(self, body: str) -> Path:
        executable = self.root / "gh"
        executable.write_text(f"#!{sys.executable}\n" + body)
        executable.chmod(0o700)
        return executable

    def test_actual_cli_readback_and_exact_get_only_boundary(self) -> None:
        self.observation.update(status="completed", conclusion="success")
        arguments = self.root / "arguments.json"
        self.fake_gh(
            "import json, pathlib, sys\n"
            f"path = pathlib.Path({str(arguments)!r})\n"
            "calls = json.loads(path.read_text()) if path.exists() else []\n"
            "path.write_text(json.dumps(calls + [sys.argv[1:]]))\n"
            f"print({json.dumps(self.observation)!r})\n"
        )
        env = {**os.environ, "PATH": str(self.root) + os.pathsep + os.environ["PATH"]}
        arm_arguments = ["arm", "--selection", str(self.selection), "--timeout-seconds", "60"]
        for key, value in self.target.to_data().items():
            arm_arguments.extend(["--" + key.replace("_", "-"), str(value)])
        commands = (
            ["init", "--selection", str(self.selection)],
            arm_arguments,
            ["probe", "--selection", str(self.selection)],
        )
        for index, command in enumerate(commands):
            result = subprocess.run(
                [sys.executable, "-m", "tools.release.watch_pipeline", *command],
                env=env,
                capture_output=True,
                text=True,
                timeout=5,
                check=False,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stderr, "")
            self.assertNotIn("ghp_SECRET", result.stdout)
            observed = json.loads(result.stdout)
            if index == 0:
                self.assertEqual(observed, {"wake": False})
                self.assertFalse(arguments.exists())
            elif index == 1:
                self.assertEqual(observed["reason"], "armed")
                selected = json.loads(self.selection.read_text())
                self.assertEqual(selected["head_sha"], self.target.head_sha)
                self.assertEqual(selected["attempt"], self.target.attempt)
            else:
                self.assertEqual(observed["reason"], "pipeline_success_requires_artifact_readback")
        expected = [
            "api",
            "--hostname",
            "github.com",
            "--method",
            "GET",
            "repos/example/project/actions/runs/40212576458/attempts/1",
        ]
        self.assertEqual(json.loads(arguments.read_text()), [expected, expected])

    def test_transport_retries_only_bounded_process_errors(self) -> None:
        with (
            patch.object(
                watch,
                "gh_output",
                side_effect=[
                    watch.WatchError("github_unavailable"),
                    json.dumps(self.observation).encode(),
                ],
            ) as output,
            patch.object(watch.time, "sleep") as sleep,
        ):
            self.assertEqual(watch.fetch_run(self.target)["id"], self.target.run_id)
            self.assertEqual(output.call_count, 2)
            sleep.assert_called_once()
        for raw in (b"{", b"null", b"[]", b"\xff", b'{"id":1,"id":2}'):
            with patch.object(watch, "gh_output", return_value=raw) as output:
                with self.assertRaises(watch.WatchError):
                    watch.fetch_run(self.target)
                self.assertEqual(output.call_count, 1)
        with (
            patch.object(
                watch, "gh_output", side_effect=watch.WatchError("github_unavailable")
            ) as output,
            patch.object(watch.time, "sleep"),
        ):
            with self.assertRaises(watch.WatchError):
                watch.fetch_run(self.target)
            self.assertEqual(output.call_count, 2)

    def test_actual_process_timeout_and_output_limit_kill_owned_group(self) -> None:
        child_pid = self.root / "child.pid"
        for mode in ("timeout", "oversize"):
            with self.subTest(mode=mode):
                self.fake_gh(
                    "import os, pathlib, signal, sys, time\n"
                    "pid = os.fork()\n"
                    "if pid == 0:\n"
                    "    while True: time.sleep(10)\n"
                    f"pathlib.Path({str(child_pid)!r}).write_text(str(pid))\n"
                    + (
                        "time.sleep(10)\n"
                        if mode == "timeout"
                        else f"sys.stdout.write('x' * {watch.MAX_GH_BYTES + 1}); sys.stdout.flush(); time.sleep(10)\n"
                    )
                )
                start = time.monotonic()
                with (
                    patch.dict(
                        os.environ, {"PATH": str(self.root) + os.pathsep + os.environ["PATH"]}
                    ),
                    self.assertRaises(watch.WatchError) as raised,
                ):
                    watch.gh_output(self.target, timeout_seconds=0.5)
                self.assertEqual(
                    raised.exception.code,
                    "github_timeout" if mode == "timeout" else "github_output_limit",
                )
                self.assertLess(time.monotonic() - start, 3)
                pid = int(child_pid.read_text())
                # A dead descendant can briefly remain a zombie under the host's PID 1.
                proc_status = Path(f"/proc/{pid}/stat")
                if proc_status.exists():
                    self.assertEqual(proc_status.read_text().split()[2], "Z")

    def test_transport_errors_never_print_github_credentials(self) -> None:
        self.arm()
        self.fake_gh(
            "import sys\nprint('ghp_SECRET')\nprint('ghp_SECRET', file=sys.stderr)\nsys.exit(1)\n"
        )
        with (
            patch.dict(os.environ, {"PATH": str(self.root) + os.pathsep + os.environ["PATH"]}),
            patch.object(watch.time, "sleep"),
        ):
            code, result, stderr = self.cli("probe", "--selection", str(self.selection))
        self.assertEqual((code, result["reason"], stderr), (0, "observation_error", ""))
        self.assertNotIn("ghp_SECRET", json.dumps(result))


if __name__ == "__main__":
    unittest.main()
