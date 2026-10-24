"""Validate release input selection and GitHub output ownership without publication."""

import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from typing import Any

from tools.release.select_run import ROOT, select_run


class RunSelectionTests(unittest.TestCase):
    def setUp(self) -> None:
        temporary = tempfile.TemporaryDirectory(prefix="whisper-run-selection-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.inventory = self.root / "docs/releases/fixture.json"
        self.inventory.parent.mkdir(parents=True)
        self.data: dict[str, Any] = {
            "ci_run_refs": [{"run_id": 11}],
            "candidate_ci_run_id": 29,
            "candidate_source_sha": "a" * 40,
        }
        self.save()

    def save(self) -> None:
        self.inventory.write_text(json.dumps(self.data))

    def select(self, stage: str = "candidate") -> tuple[int, str]:
        return select_run("docs/releases/fixture.json", "11", stage, self.root)

    def test_candidate_uses_selected_run_and_stable_uses_accepted_candidate(self) -> None:
        self.assertEqual(self.select(), (11, "a" * 40))
        self.assertEqual(self.select("stable"), (29, "a" * 40))

    def test_inventory_path_is_restricted_before_reading(self) -> None:
        for path in ("../fixture.json", "/tmp/fixture.json", "docs/releases/../fixture.json"):
            with self.subTest(path=path), self.assertRaisesRegex(ValueError, "inventory path"):
                select_run(path, "11", "candidate", self.root)

    def test_selected_run_requires_a_positive_decimal_identity(self) -> None:
        for run in ("0", "-1", "01", "11.0", "11\n", "11; echo invalid"):
            with self.subTest(run=run), self.assertRaisesRegex(ValueError, "CI run identity"):
                select_run("docs/releases/fixture.json", run, "candidate", self.root)

    def test_selected_run_must_match_inventory(self) -> None:
        self.data["ci_run_refs"][0]["run_id"] = 12
        self.save()

        with self.assertRaisesRegex(ValueError, "identity mismatch"):
            self.select()

    def test_stable_requires_an_integer_candidate_run(self) -> None:
        for value in (None, "29", True, 29.0, 0, -1):
            self.data["candidate_ci_run_id"] = value
            self.save()

            with self.subTest(value=value), self.assertRaisesRegex(ValueError, "candidate CI"):
                self.select("stable")

    def test_unknown_stage_is_rejected(self) -> None:
        with self.assertRaisesRegex(ValueError, "release stage"):
            self.select("unknown")

    def test_source_cannot_inject_github_outputs(self) -> None:
        self.data["candidate_source_sha"] = "a" * 40 + "\nimage-source=unverified"
        self.save()

        with self.assertRaisesRegex(ValueError, "source revision"):
            self.select()

    def cli(self, stage: str = "candidate") -> subprocess.CompletedProcess[str]:
        scripts = self.root / "tools/release"
        scripts.mkdir(parents=True, exist_ok=True)
        for name in ("select_run.py", "validation.py"):
            shutil.copyfile(ROOT / "tools/release" / name, scripts / name)

        return subprocess.run(
            [
                sys.executable,
                str(scripts / "select_run.py"),
                "--inventory",
                "docs/releases/fixture.json",
                "--selected-run",
                "11",
                "--stage",
                stage,
                "--output",
                str(self.root / "github-output"),
            ],
            cwd=self.root,
            capture_output=True,
            text=True,
            timeout=10,
        )

    def test_cli_appends_both_outputs_after_validation(self) -> None:
        for stage, run in (("candidate", 11), ("stable", 29)):
            output = self.root / "github-output"
            output.write_text("existing=value\n")

            result = self.cli(stage)

            with self.subTest(stage=stage):
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(
                    output.read_text(),
                    f"existing=value\nimages-run={run}\nimage-source={'a' * 40}\n",
                )

    def test_cli_rejects_invalid_inventory_without_partial_outputs(self) -> None:
        for document in ("{", "{}", '{"ci_run_refs": []}'):
            self.inventory.write_text(document)
            output = self.root / "github-output"
            output.write_text("existing=value\n")

            result = self.cli()

            with self.subTest(document=document):
                self.assertEqual(result.returncode, 1)
                self.assertEqual(output.read_text(), "existing=value\n")
                self.assertEqual(result.stderr, "FAIL: release CI artifact selection is invalid\n")
