import base64
import contextlib
import copy
import hashlib
import io
import json
import os
import tempfile
import unittest
import urllib.error
import urllib.request
from datetime import UTC, datetime
from email.message import Message
from http import HTTPStatus
from http.client import HTTPMessage
from pathlib import Path
from unittest.mock import patch

from tests.tooling.test_release import NOW, inventory
from tools.release import check


class PublisherTests(unittest.TestCase):
    def test_chart_retry_requires_the_same_anonymous_manifest(self) -> None:
        from tools.release import publish

        content = b"chart fixture"
        archive_sha = hashlib.sha256(content).hexdigest()
        private_manifest = json.dumps(
            {
                "layers": [
                    {
                        "mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
                        "digest": "sha256:" + archive_sha,
                    }
                ]
            }
        ).encode()
        manifest_sha = "sha256:" + hashlib.sha256(private_manifest).hexdigest()
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "chart.tgz"
            archive.write_bytes(content)
            for public_manifest in (None, private_manifest + b"\n", private_manifest):
                with (
                    self.subTest(public=public_manifest),
                    patch.object(
                        publish, "manifest", side_effect=[private_manifest, public_manifest]
                    ),
                    patch.object(publish, "registry_get", return_value=content),
                    patch.object(publish, "command") as writer,
                ):
                    if public_manifest == private_manifest:
                        self.assertEqual(
                            publish.publish_chart(
                                archive, "0.1.0", archive_sha, manifest_sha, "helm"
                            ),
                            manifest_sha,
                        )
                    else:
                        with self.assertRaises(ValueError):
                            publish.publish_chart(
                                archive, "0.1.0", archive_sha, manifest_sha, "helm"
                            )
                    writer.assert_not_called()

    def test_registry_credentials_are_not_forwarded_to_redirect_targets(self) -> None:
        from tools.release import publish

        initial_headers: list[str | None] = []
        redirected_headers: list[str | None] = []
        with tempfile.TemporaryDirectory() as directory:
            (Path(directory) / "config.json").write_text(
                json.dumps({"auths": {"ghcr.io": {"auth": "private-login"}}})
            )

            def exchange(request: urllib.request.Request, timeout: int) -> io.BytesIO:
                authorization = request.get_header("Authorization")
                if authorization is None:
                    headers = Message()
                    headers["WWW-Authenticate"] = (
                        'Bearer realm="https://ghcr.io/token",service="ghcr.io"'
                    )
                    raise urllib.error.HTTPError(
                        request.full_url, HTTPStatus.UNAUTHORIZED, "Challenge", headers, None
                    )
                initial_headers.append(authorization)
                redirected = urllib.request.HTTPRedirectHandler().redirect_request(
                    request,
                    io.BytesIO(),
                    HTTPStatus.TEMPORARY_REDIRECT,
                    "Redirect",
                    HTTPMessage(),
                    "https://cdn.example.invalid/content",
                )
                self.assertIsNotNone(redirected)
                assert redirected is not None
                redirected_headers.append(redirected.get_header("Authorization"))
                return io.BytesIO(
                    b'{"token":"private-token"}'
                    if "/token?" in request.full_url
                    else b"verified content"
                )

            with (
                patch.dict(os.environ, {"DOCKER_CONFIG": directory}),
                patch.object(publish.urllib.request, "urlopen", side_effect=exchange),
            ):
                content = publish.registry_get(
                    "ghcr.io/fixture/image", "blobs", "sha256:" + "a" * 64, authenticated=True
                )

        self.assertEqual(content, b"verified content")
        self.assertEqual(initial_headers, ["Basic private-login", "Bearer private-token"])
        self.assertEqual(redirected_headers, [None, None])

    def test_recorded_chart_identity_allows_retry_with_original_selection(self) -> None:
        from tools.release import publish

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            selected = inventory()
            selected["security_report_refs"][0]["scan_time"] = datetime.now(UTC).isoformat()
            selected["chart_oci_manifest_digest"] = None
            selected["channel_status"]["chart-oci"] = {"status": "pending"}
            inventory_path, state_path = root / "inventory.json", root / "state.json"
            inventory_path.write_text(json.dumps(selected))
            arguments = [
                "publish.py",
                "--inventory",
                str(inventory_path),
                "--artifacts",
                str(root),
                "--state",
                str(state_path),
                "--stage",
                "candidate",
                "--channel",
                "chart",
            ]
            expected = "sha256:" + "a" * 64
            with (
                patch.object(publish, "publish_chart", return_value=expected) as write,
                patch("sys.argv", arguments),
                contextlib.redirect_stderr(io.StringIO()),
            ):
                self.assertEqual(publish.main(), 0)
                self.assertEqual(publish.main(), 0)
                self.assertEqual(write.call_args.args[3], expected)
            self.assertEqual(
                json.loads(state_path.read_text())["chart_oci_manifest_digest"], expected
            )

            drifted = copy.deepcopy(selected)
            drifted["chart_oci_manifest_digest"] = "sha256:" + "b" * 64
            inventory_path.write_text(json.dumps(drifted))
            with (
                patch.object(publish, "publish_chart") as write,
                patch("sys.argv", arguments),
                contextlib.redirect_stderr(io.StringIO()),
            ):
                self.assertEqual(publish.main(), 1)
                write.assert_not_called()

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
