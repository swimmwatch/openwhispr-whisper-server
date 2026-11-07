import copy
import subprocess
import tempfile
import unittest
from pathlib import Path
from typing import Any
from unittest.mock import patch

from tests.tooling.test_release import SHA
from tools.release.check import REQUIRED_JOBS
from tools.release.source import qualify_source, release_version, resolve_tag, verify_pr_run


class ReleaseSourceTests(unittest.TestCase):
    def source_run(self) -> tuple[dict[str, Any], dict[str, Any], list[dict[str, Any]]]:
        pull: dict[str, Any] = {
            "number": 7,
            "merged_at": "2026-10-25T10:00:00Z",
            "merge_commit_sha": SHA,
            "base": {"ref": "main"},
            "head": {"sha": "4" * 40},
        }
        run = {
            "id": 12,
            "event": "pull_request",
            "head_sha": pull["head"]["sha"],
            "path": ".github/workflows/ci.yml",
            "conclusion": "success",
        }
        jobs = [{"name": name, "conclusion": "success"} for name in REQUIRED_JOBS]
        return pull, run, jobs

    def test_successful_pr_run_binds_the_head_and_merged_release_source(self) -> None:
        pull, run, jobs = self.source_run()
        selected = verify_pr_run(pull, run, jobs, SHA)
        self.assertEqual(selected["source_sha"], SHA)
        self.assertEqual(selected["tested_head_sha"], pull["head"]["sha"])
        self.assertEqual(selected["run_id"], run["id"])

    def test_unmerged_wrong_base_and_unrelated_source_cannot_qualify(self) -> None:
        pull, run, jobs = self.source_run()
        for change in (
            {"merged_at": None},
            {"base": {"ref": "other"}},
            {"merge_commit_sha": "5" * 40},
        ):
            with self.subTest(change=change), self.assertRaises(ValueError):
                verify_pr_run(dict(pull, **change), run, jobs, SHA)
        for change in (
            {"event": "push"},
            {"event": "pull_request_target"},
            {"head_sha": "5" * 40},
            {"conclusion": "failure"},
            {"path": ".github/workflows/other.yml"},
        ):
            with self.subTest(change=change), self.assertRaises(ValueError):
                verify_pr_run(pull, dict(run, **change), jobs, SHA)

    def test_missing_cancelled_skipped_and_failed_jobs_deny_release(self) -> None:
        pull, run, jobs = self.source_run()
        with self.assertRaises(ValueError):
            verify_pr_run(pull, run, jobs[:-1], SHA)
        for status in ("failure", "cancelled", "skipped"):
            changed = copy.deepcopy(jobs)
            changed[-1]["conclusion"] = status
            with self.subTest(status=status), self.assertRaises(ValueError):
                verify_pr_run(pull, run, changed, SHA)

    def test_only_supported_release_tags_determine_the_mode(self) -> None:
        self.assertEqual(release_version("v0.1.0-rc.2"), ("0.1.0-rc.2", "candidate"))
        self.assertEqual(release_version("v0.1.0"), ("0.1.0", "stable"))
        for tag in ("main", "v0.01.0", "v0.1.0-rc.0", "v0.1.0\n", "--upload-pack=bad"):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                release_version(tag)

    def test_an_older_green_run_does_not_hide_a_newer_failed_run(self) -> None:
        pull, run, jobs = self.source_run()
        responses = [
            [pull],
            {"workflow_runs": [dict(run, conclusion="failure"), run]},
            {"jobs": jobs},
        ]
        with (
            patch("tools.release.source.api", side_effect=responses),
            self.assertRaises(ValueError),
        ):
            qualify_source("fixture/repository", SHA)

    def test_tag_resolution_rejects_a_commit_outside_main(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)

            def git(*args: str) -> str:
                result = subprocess.run(
                    ["git", *args], cwd=root, capture_output=True, text=True, check=True
                )
                return result.stdout.strip()

            git("init", "--quiet", "--initial-branch=main")
            git("config", "user.name", "Release fixture")
            git("config", "user.email", "fixture@example.invalid")
            git("commit", "--quiet", "--allow-empty", "-m", "fixture: main")
            expected = git("rev-parse", "HEAD")
            git("update-ref", "refs/remotes/origin/main", expected)
            git("tag", "-a", "v0.1.0-rc.1", "-m", "fixture")
            self.assertEqual(resolve_tag("v0.1.0-rc.1", root), expected)
            git("checkout", "--quiet", "-b", "other")
            git("commit", "--quiet", "--allow-empty", "-m", "fixture: unrelated")
            git("tag", "v0.1.0-rc.2")
            with self.assertRaises(ValueError):
                resolve_tag("v0.1.0-rc.2", root)
