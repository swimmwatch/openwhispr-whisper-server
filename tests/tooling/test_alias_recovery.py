import copy
import hashlib
import unittest
from pathlib import Path
from typing import Any
from unittest.mock import patch

from tests.tooling.test_release import NOW
from tests.tooling.test_release_promotion import stable_inventory
from tools.release import check, publish


class AliasRecoveryTests(unittest.TestCase):
    def setUp(self) -> None:
        self.old = b"previous-index"
        self.new = b"qualified-index"
        self.identity = "sha256:" + hashlib.sha256(self.new).hexdigest()
        self.data = stable_inventory()
        self.data["image_manifest_digests"] = dict.fromkeys(("api", "backend"), self.identity)
        self.data["accepted_image_manifest_digests"] = dict(self.data["image_manifest_digests"])
        self.data["security_report_refs"][0]["image_manifest_digests"] = self.data[
            "image_manifest_digests"
        ]
        for report in self.data["gpu_report_refs"]:
            report["image_manifest_digests"] = self.data["image_manifest_digests"]
        for channel in publish.REPOSITORIES:
            self.data["channel_status"][channel]["identity"] = self.identity

        self.stored: dict[tuple[str, str], bytes | None] = {
            (repository, alias): self.old
            for repository in publish.REPOSITORIES.values()
            for alias in check.stable_aliases(self.data["release_version"])
        }

    def test_interrupted_publication_retries_with_original_rollback_snapshot(self) -> None:
        snapshots: list[dict[str, Any]] = []

        def interrupted_write(args: list[str]) -> None:
            repository, alias = args[args.index("--tag") + 1].rsplit(":", 1)
            self.stored[repository, alias] = self.new
            raise KeyboardInterrupt("Injected interruption after remote write")

        def failing_retry(args: list[str]) -> None:
            repository, alias = args[args.index("--tag") + 1].rsplit(":", 1)
            if args[-1].startswith("oci-layout://"):
                raise ValueError("Injected retry failure")
            self.stored[repository, alias] = self.old

        with (
            patch.object(
                publish, "inspect_layout", return_value={"manifest_digest": self.identity}
            ),
            patch.object(
                publish, "manifest", side_effect=lambda repo, alias: self.stored[repo, alias]
            ),
        ):
            with (
                patch.object(publish, "command", side_effect=interrupted_write),
                self.assertRaises(KeyboardInterrupt),
            ):
                publish.finalize_aliases(
                    self.data,
                    Path("/fixture"),
                    NOW,
                    checkpoint=lambda: snapshots.append(copy.deepcopy(self.data)),
                )

            resumed = snapshots[-1]

            with (
                patch.object(publish, "command", side_effect=failing_retry),
                self.assertRaises(ValueError),
            ):
                publish.finalize_aliases(resumed, Path("/fixture"), NOW)

        self.assertTrue(all(value == self.old for value in self.stored.values()))

        touched = [state for state in resumed["alias_status"].values() if state["touched"]]

        self.assertTrue(touched)
        self.assertTrue(
            all(value == "restored" for state in touched for value in state["rollback"].values())
        )

    def test_new_alias_failure_records_partial_state_without_manifest_deletion(self) -> None:
        self.stored = dict.fromkeys(self.stored)

        def failed_write(args: list[str]) -> None:
            self.assertNotIn("delete", args)

            repository, alias = args[args.index("--tag") + 1].rsplit(":", 1)
            self.stored[repository, alias] = self.new
            raise ValueError("Injected failure after creating a new alias")

        with (
            patch.object(
                publish, "inspect_layout", return_value={"manifest_digest": self.identity}
            ),
            patch.object(
                publish, "manifest", side_effect=lambda repo, alias: self.stored[repo, alias]
            ),
            patch.object(publish, "command", side_effect=failed_write) as write,
            self.assertRaises(ValueError),
        ):
            publish.finalize_aliases(self.data, Path("/fixture"), NOW)

        self.assertEqual(write.call_count, 1)

        partial = [
            value
            for state in self.data["alias_status"].values()
            for value in state["rollback"].values()
        ]

        self.assertEqual(partial, ["no_previous_alias; partial_state_requires_reconciliation"])
        with self.assertRaises(ValueError):
            check.validate(self.data, "stable", NOW)

    def test_exported_snapshot_recovers_writes_lost_with_a_runner(self) -> None:
        with (
            patch.object(
                publish, "inspect_layout", return_value={"manifest_digest": self.identity}
            ),
            patch.object(
                publish, "manifest", side_effect=lambda repo, alias: self.stored[repo, alias]
            ),
            patch.object(publish, "command") as write,
        ):
            publish.prepare_aliases(self.data, Path("/fixture"), NOW, conservative_rollback=True)

        write.assert_not_called()

        restored_snapshot = copy.deepcopy(self.data)
        last_key = next(reversed(self.stored))
        self.stored[last_key] = self.new

        def failed_retry(args: list[str]) -> None:
            if args[-1].startswith("oci-layout://"):
                raise ValueError(
                    "Injected retry failure before the earlier runner write is revisited"
                )
            repository, alias = args[args.index("--tag") + 1].rsplit(":", 1)
            self.stored[repository, alias] = self.old

        with (
            patch.object(
                publish, "inspect_layout", return_value={"manifest_digest": self.identity}
            ),
            patch.object(
                publish, "manifest", side_effect=lambda repo, alias: self.stored[repo, alias]
            ),
            patch.object(publish, "command", side_effect=failed_retry),
            self.assertRaises(ValueError),
        ):
            publish.finalize_aliases(restored_snapshot, Path("/fixture"), NOW)

        self.assertTrue(all(value == self.old for value in self.stored.values()))

    def test_failed_durable_snapshot_prevents_remote_writes(self) -> None:
        with (
            patch.object(
                publish, "inspect_layout", return_value={"manifest_digest": self.identity}
            ),
            patch.object(publish, "manifest", return_value=self.old),
            patch.object(publish, "command") as write,
            self.assertRaises(OSError),
        ):
            publish.finalize_aliases(
                self.data, Path("/fixture"), NOW, checkpoint=unavailable_checkpoint
            )

        write.assert_not_called()

    def test_unverified_channel_prevents_even_alias_snapshot_reads(self) -> None:
        self.data["channel_status"]["chart-pages"]["status"] = "failed"

        def write_alias(args: list[str]) -> None:
            repository, alias = args[args.index("--tag") + 1].rsplit(":", 1)
            self.stored[repository, alias] = self.new

        with (
            patch.object(
                publish, "inspect_layout", return_value={"manifest_digest": self.identity}
            ),
            patch.object(
                publish, "manifest", side_effect=lambda repo, alias: self.stored[repo, alias]
            ) as read,
            patch.object(publish, "command", side_effect=write_alias) as write,
            self.assertRaises(ValueError),
        ):
            publish.finalize_aliases(self.data, Path("/fixture"), NOW)

        read.assert_not_called()
        write.assert_not_called()


def unavailable_checkpoint() -> None:
    raise OSError("Injected checkpoint failure")
