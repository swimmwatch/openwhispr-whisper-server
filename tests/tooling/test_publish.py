import hashlib
import unittest
from pathlib import Path
from unittest.mock import patch

from tests.tooling.test_release import NOW, inventory
from tools.release import check


class PublisherTests(unittest.TestCase):
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
