"""Exercise build-job selection and invalid resource overrides through the shell helper."""

import os
import subprocess
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class ContainerParameterTests(unittest.TestCase):
    def test_explicit_parallelism_and_invalid_resource_values(self) -> None:
        helper = ROOT / "containers/scripts/build-jobs.sh"

        for jobs, status in (("3", 0), ("0", 2), ("-1", 2), ("01", 2), ("1.5", 2), ("invalid", 2)):
            with self.subTest(jobs=jobs):
                result = subprocess.run(
                    ["bash", str(helper)],
                    env={**os.environ, "BUILD_JOBS": jobs},
                    capture_output=True,
                    text=True,
                    timeout=10,
                )

                self.assertEqual(result.returncode, status)

                if status == 0:
                    self.assertEqual(result.stdout.strip(), jobs)

        for reserve, per_job in (("-1", "1"), ("0", "0"), ("1", "bad")):
            with self.subTest(reserve=reserve, per_job=per_job):
                result = subprocess.run(
                    ["bash", str(helper)],
                    env={
                        **os.environ,
                        "BUILD_JOBS": "",
                        "BUILD_MEMORY_RESERVE_KIB": reserve,
                        "BUILD_MEMORY_PER_JOB_KIB": per_job,
                    },
                    capture_output=True,
                    text=True,
                    timeout=10,
                )

                self.assertEqual(result.returncode, 2)

    def test_automatic_parallelism_keeps_at_least_one_job_when_memory_is_tight(self) -> None:
        result = subprocess.run(
            ["bash", str(ROOT / "containers/scripts/build-jobs.sh")],
            env={
                **os.environ,
                "BUILD_JOBS": "",
                "BUILD_MEMORY_RESERVE_KIB": "0",
                "BUILD_MEMORY_PER_JOB_KIB": str(1024**5),
            },
            capture_output=True,
            text=True,
            timeout=10,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), "1")


if __name__ == "__main__":
    unittest.main()
