import base64
import hashlib
import io
import json
import os
import tempfile
import unittest
import urllib.error
import urllib.request
from email.message import Message
from http import HTTPStatus
from pathlib import Path
from unittest.mock import patch

from tests.tooling.test_release import NOW, inventory
from tools.release import check


class PublisherTests(unittest.TestCase):
    def test_first_push_checks_authenticated_existence_then_reads_anonymously(self) -> None:
        from tools.release import publish

        content = b'{"fixture":true}'
        expected = "sha256:" + hashlib.sha256(content).hexdigest()
        login = base64.b64encode(b"fixture:credential").decode()
        token_headers: list[str | None] = []
        with tempfile.TemporaryDirectory() as directory:
            (Path(directory) / "config.json").write_text(
                json.dumps({"auths": {"ghcr.io": {"auth": login}}})
            )
            with (
                patch.dict(os.environ, {"DOCKER_CONFIG": directory}),
                patch.object(publish, "command") as writer,
            ):

                def exchange(request: urllib.request.Request | str, timeout: int) -> io.BytesIO:
                    url = request if isinstance(request, str) else request.full_url
                    authorization = (
                        None if isinstance(request, str) else request.get_header("Authorization")
                    )
                    if "/token?" in url:
                        token_headers.append(authorization)
                        if authorization == "Basic " + login or writer.called:
                            return io.BytesIO(b'{"token":"fixture-session"}')
                        raise urllib.error.HTTPError(
                            url, HTTPStatus.FORBIDDEN, "Denied", Message(), None
                        )
                    if authorization is None:
                        headers = Message()
                        headers["WWW-Authenticate"] = (
                            'Bearer realm="https://ghcr.io/token",service="ghcr.io"'
                        )
                        raise urllib.error.HTTPError(
                            url, HTTPStatus.UNAUTHORIZED, "Challenge", headers, None
                        )
                    if writer.called:
                        return io.BytesIO(content)
                    raise urllib.error.HTTPError(
                        url, HTTPStatus.NOT_FOUND, "Missing", Message(), None
                    )

                with patch.object(publish.urllib.request, "urlopen", side_effect=exchange):
                    publish.publish_image(Path("/fixture"), "ghcr.io/fixture/image", "rc", expected)
                writer.assert_called_once()
                self.assertEqual(token_headers, ["Basic " + login, None])

    def test_retry_cannot_claim_a_private_manifest_is_public(self) -> None:
        from tools.release import publish

        content = b'{"fixture":true}'
        expected = "sha256:" + hashlib.sha256(content).hexdigest()
        with (
            patch.object(publish, "manifest", side_effect=[content, None]),
            patch.object(publish, "command") as writer,
            self.assertRaises(ValueError),
        ):
            publish.publish_image(Path("/fixture"), "ghcr.io/fixture/image", "rc", expected)
        writer.assert_not_called()

    def test_registry_challenge_cannot_send_credentials_to_the_other_registry(self) -> None:
        from tools.release import publish

        headers = Message()
        headers["WWW-Authenticate"] = (
            'Bearer realm="https://auth.docker.io/token",service="ghcr.io"'
        )
        challenge = urllib.error.HTTPError(
            "https://ghcr.io", HTTPStatus.UNAUTHORIZED, "Challenge", headers, None
        )
        with tempfile.TemporaryDirectory() as directory:
            (Path(directory) / "config.json").write_text(
                json.dumps({"auths": {"ghcr.io": {"auth": "fixture"}}})
            )
            with (
                patch.dict(os.environ, {"DOCKER_CONFIG": directory}),
                patch.object(
                    publish.urllib.request,
                    "urlopen",
                    side_effect=[
                        challenge,
                        io.BytesIO(b'{"token":"fixture"}'),
                        io.BytesIO(b"data"),
                    ],
                ) as network,
                self.assertRaises(ValueError),
            ):
                publish.registry_get("ghcr.io/fixture/image", "manifests", "rc", authenticated=True)
            network.assert_called_once()

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
