import hashlib
import unittest
from functools import partial
from pathlib import Path
from unittest.mock import patch

from tests.tooling.test_release import NOW, inventory
from tools.release import check
from tools.security.check import validate as validate_security


class PublisherTests(unittest.TestCase):
    @patch("tools.release.verify_ci.security_check", partial(validate_security, now=NOW))
    def test_external_ci_must_be_the_actual_successful_main_source_run(self) -> None:
        import copy

        from tests.tooling.test_security import SecurityAggregateTests
        from tools.release.verify_ci import verify

        data = inventory()
        security = SecurityAggregateTests().inventory()
        run = {
            "id": 1,
            "event": "push",
            "head_branch": "main",
            "conclusion": "success",
            "head_sha": data["release_source_sha"],
            "path": ".github/workflows/ci.yml",
        }
        jobs = [{"name": name, "conclusion": "success"} for name in check.REQUIRED_JOBS]
        verify(data, run, jobs, security, data["release_source_sha"])

        for change in (
            {"event": "pull_request"},
            {"head_sha": "4" * 40},
            {"conclusion": "failure"},
            {"id": 2},
        ):
            with self.subTest(change=change), self.assertRaises(ValueError):
                verify(data, dict(run, **change), jobs, security, data["release_source_sha"])

        with self.assertRaises(ValueError):
            verify(data, run, jobs[:-1], security, data["release_source_sha"])

        wrong = copy.deepcopy(security)
        wrong["image_manifest_digests"]["api"] = "sha256:" + "9" * 64

        with self.assertRaises(ValueError):
            verify(data, run, jobs, wrong, data["release_source_sha"])

    def test_preparation_does_not_claim_pending_channels_are_published(self) -> None:
        data = inventory()
        for channel in data["channel_status"].values():
            channel["status"] = "pending"

        self.assertEqual(
            check.validate(data, "candidate", NOW, require_channels=False), "candidate_building"
        )
        with self.assertRaises(ValueError):
            check.validate(data, "candidate", NOW)

    def test_manifest_collision_is_checked_before_any_write(self) -> None:
        from tools.release.publish import publish_image

        with (
            patch("tools.release.publish.manifest", return_value=b"existing"),
            patch("tools.release.publish.command") as write,
        ):
            with self.assertRaises(ValueError):
                publish_image(
                    Path("/fixture"), "ghcr.io/fixture/image", "0.1.0-rc.1", "sha256:" + "a" * 64
                )
            write.assert_not_called()

    def test_readback_failure_is_not_a_success_and_retry_is_idempotent(self) -> None:
        from tools.release.publish import publish_image

        content = b'{"fixture":true}'
        expected = "sha256:" + hashlib.sha256(content).hexdigest()
        with (
            patch("tools.release.publish.manifest", side_effect=[None, b"wrong"]),
            patch("tools.release.publish.command") as write,
        ):
            with self.assertRaises(ValueError):
                publish_image(Path("/fixture"), "ghcr.io/fixture/image", "0.1.0-rc.1", expected)
            write.assert_called_once()

        with (
            patch("tools.release.publish.manifest", return_value=content),
            patch("tools.release.publish.command") as write,
        ):
            publish_image(Path("/fixture"), "ghcr.io/fixture/image", "0.1.0-rc.1", expected)

            write.assert_not_called()


if __name__ == "__main__":
    unittest.main()
