import copy
import io
import json
import os
import subprocess
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any
from unittest.mock import patch

from tools import build_cache

REPOSITORY = "ghcr.io/owner/project-buildcache"
DIGEST = "sha256:" + "a" * 64
OTHER_DIGEST = "sha256:" + "b" * 64
ENVIRONMENT = {
    "GITHUB_REPOSITORY": "Owner/Project",
    "GITHUB_DEFAULT_BRANCH": "trunk",
    "GITHUB_EVENT_NAME": "push",
    "GITHUB_REF": "refs/heads/trunk",
    "GITHUB_RUN_ID": "123",
    "GITHUB_RUN_ATTEMPT": "2",
}


def available_record(role: str = "api") -> dict[str, Any]:
    return {
        "schema_version": 1,
        "policy": "trusted-main-v1",
        "role": role,
        "platform": "linux/amd64",
        "status": "available",
        "sources": [f"{REPOSITORY}-{role}@{DIGEST}"],
    }


def staging_ref(role: str = "api") -> str:
    return f"{REPOSITORY}-{role}:run-123-2-linux-amd64-v1"


def native_result(stdout: str = "") -> subprocess.CompletedProcess[str]:
    return subprocess.CompletedProcess([], 0, stdout=stdout, stderr="private-native-diagnostics")


def prepare_arguments(root: Path, access: str, role: str = "api") -> list[str]:
    return [
        "prepare",
        "--role",
        role,
        "--repository",
        REPOSITORY,
        "--platform",
        "linux/amd64",
        "--access",
        access,
        "--output",
        str(root / "record.json"),
        "--github-output",
        str(root / "github-output"),
    ]


class RecordValidationTests(unittest.TestCase):
    def test_structural_validation_is_pure_and_does_not_claim_namespace_trust(self) -> None:
        record = available_record()
        record["sources"] = [f"ghcr.io/another/project-buildcache-api@{DIGEST}"]
        with (
            patch.dict(os.environ, {}, clear=True),
            patch("tools.build_cache.subprocess.run") as native,
        ):
            validated = build_cache.validate_record(record, role="api")
            self.assertEqual(validated["sources"], record["sources"])
            with self.assertRaises(ValueError):
                build_cache.validate_record(record, repository=REPOSITORY)
            native.assert_not_called()
        validated["sources"].clear()
        self.assertEqual(len(record["sources"]), 1)

    def test_cold_record_and_optional_exporter(self) -> None:
        disabled = available_record()
        disabled.update(policy="disabled", status="disabled", sources=[])
        self.assertEqual(build_cache.validate_record(disabled, cold=True), disabled)
        with self.assertRaises(ValueError):
            build_cache.validate_record(available_record(), cold=True)
        for role in ("api", "backend"):
            record = available_record(role)
            record["export_ref"] = staging_ref(role)
            self.assertEqual(
                build_cache.validate_record(record, role=role, repository=REPOSITORY), record
            )
            record.update(status="unavailable", sources=[])
            self.assertEqual(build_cache.validate_record(record), record)

    def test_rejects_unusable_or_injected_artifact_fields(self) -> None:
        invalid_fields: list[tuple[str, object]] = [
            ("schema_version", True),
            ("schema_version", 1.0),
            ("schema_version", 2),
            ("role", "worker"),
            ("role", ["api"]),
            ("platform", "linux/arm64"),
            ("policy", "gha"),
            ("policy", []),
            ("status", "hit"),
            ("status", []),
            ("status", "unavailable"),
            ("status", "disabled"),
            ("sources", []),
            ("sources", "not-a-list"),
            ("sources", [None]),
            ("sources", [1]),
            ("sources", [f"{REPOSITORY}-api@{DIGEST}"] * 2),
            ("sources", [f"{REPOSITORY}-api:main-linux-amd64-v1"]),
            ("sources", [f"{REPOSITORY}-backend@{DIGEST}"]),
            ("sources", [f"{REPOSITORY}-api@sha256:abc"]),
            ("sources", [f"{REPOSITORY}-api@{DIGEST.upper()}"]),
            ("sources", [f"docker.io/owner/project-buildcache-api@{DIGEST}"]),
            ("sources", [f"{REPOSITORY}-api@{DIGEST}\ncache-to=attacker"]),
            ("export_ref", None),
            ("export_ref", 1),
            ("export_ref", f"{REPOSITORY}-api:main-linux-amd64-v1"),
            ("export_ref", f"{REPOSITORY}-api:run-0-2-linux-amd64-v1"),
            ("export_ref", staging_ref("backend")),
            ("export_ref", "ghcr.io/another/project-buildcache-api:run-1-1-linux-amd64-v1"),
            ("export_ref", staging_ref() + ",mode=min"),
            ("export-ref", staging_ref()),
            ("secret", "private"),
        ]
        for key, value in invalid_fields:
            with self.subTest(key=key, value=value):
                record = available_record()
                record[key] = value
                with self.assertRaises(ValueError):
                    build_cache.validate_record(record)
        for value in (None, [], "record", 1):
            with self.subTest(value=value), self.assertRaises(ValueError):
                build_cache.validate_record(value)
        for key in available_record():
            record = available_record()
            del record[key]
            with self.subTest(missing=key), self.assertRaises(ValueError):
                build_cache.validate_record(record)

    def test_disabled_and_unavailable_records_cannot_hide_sources_or_exporters(self) -> None:
        record = available_record()
        record.update(policy="disabled", status="disabled", sources=[])
        for key, value in (
            ("export_ref", staging_ref()),
            ("status", "unavailable"),
            ("sources", available_record()["sources"]),
        ):
            invalid = {**record, key: value}
            with self.subTest(key=key), self.assertRaises(ValueError):
                build_cache.validate_record(invalid)
        record.update(policy="trusted-main-v1")
        with self.assertRaises(ValueError):
            build_cache.validate_record(record)

    def test_supplied_role_and_repository_bind_the_artifact(self) -> None:
        for role, repository in (
            ("backend", None),
            ("worker", None),
            (None, "ghcr.io/another/project-buildcache"),
            (None, "ghcr.io/Owner/project-buildcache"),
            (None, REPOSITORY + "\n"),
        ):
            with self.subTest(role=role, repository=repository), self.assertRaises(ValueError):
                build_cache.validate_record(available_record(), role=role, repository=repository)


class CacheCliTests(unittest.TestCase):
    def test_disabled_cli_writes_empty_outputs_without_native_access(self) -> None:
        with (
            tempfile.TemporaryDirectory() as directory,
            patch.dict(os.environ, ENVIRONMENT, clear=True),
            patch("tools.build_cache.subprocess.run") as native,
        ):
            root = Path(directory)
            self.assertEqual(build_cache.main(prepare_arguments(root, "none")), 0)
            record = json.loads((root / "record.json").read_text())
            self.assertEqual(build_cache.validate_record(record, cold=True)["status"], "disabled")
            self.assertEqual((root / "github-output").read_text(), "cache-from=\ncache-to=\n")
            native.assert_not_called()

    def test_untrusted_read_resolves_immutable_main_without_export(self) -> None:
        environment = {"GITHUB_REPOSITORY": "Owner/Project", "GITHUB_EVENT_NAME": "pull_request"}
        for role in ("api", "backend"):
            with (
                self.subTest(role=role),
                tempfile.TemporaryDirectory() as directory,
                patch.dict(os.environ, environment, clear=True),
                patch(
                    "tools.build_cache.subprocess.run", return_value=native_result(DIGEST + "\n")
                ) as native,
            ):
                root = Path(directory)
                self.assertEqual(build_cache.main(prepare_arguments(root, "read", role)), 0)
                record = json.loads((root / "record.json").read_text())
                self.assertEqual(record["sources"], [f"{REPOSITORY}-{role}@{DIGEST}"])
                self.assertNotIn("export_ref", record)
                self.assertEqual(
                    (root / "github-output").read_text(),
                    f"cache-from=type=registry,ref={REPOSITORY}-{role}@{DIGEST}\ncache-to=\n",
                )
                self.assertEqual(
                    native.call_args.args[0],
                    [
                        "docker",
                        "buildx",
                        "imagetools",
                        "inspect",
                        f"{REPOSITORY}-{role}:main-linux-amd64-v1",
                        "--format",
                        "{{.Manifest.Digest}}",
                    ],
                )
                self.assertTrue(native.call_args.kwargs["capture_output"])
                self.assertGreater(native.call_args.kwargs["timeout"], 0)

    def test_write_cli_emits_staging_max_export_and_preserves_existing_outputs(self) -> None:
        for event in ("push", "workflow_dispatch"):
            with (
                self.subTest(event=event),
                tempfile.TemporaryDirectory() as directory,
                patch.dict(os.environ, {**ENVIRONMENT, "GITHUB_EVENT_NAME": event}, clear=True),
                patch("tools.build_cache.subprocess.run", return_value=native_result(DIGEST)),
            ):
                root = Path(directory)
                (root / "github-output").write_text("existing=value\n")
                self.assertEqual(build_cache.main(prepare_arguments(root, "write")), 0)
                record = json.loads((root / "record.json").read_text())
                self.assertEqual(record["export_ref"], staging_ref())
                self.assertEqual(record["status"], "available")
                lines = (root / "github-output").read_text().splitlines()
                self.assertEqual(lines[0], "existing=value")
                self.assertEqual(len(lines), 3)
                self.assertEqual(
                    lines[2],
                    f"cache-to=type=registry,ref={staging_ref()},mode=max,oci-mediatypes=true,"
                    "image-manifest=true,compression=zstd,compression-level=3",
                )

    def test_unavailable_cache_warns_safely_and_can_seed_staging(self) -> None:
        failures = (
            subprocess.CalledProcessError(1, ["docker"], output="secret", stderr="secret"),
            subprocess.TimeoutExpired(["docker"], 120, output="secret", stderr="secret"),
            FileNotFoundError("secret"),
        )
        for failure in failures:
            for access in ("read", "write"):
                with (
                    self.subTest(failure=type(failure), access=access),
                    tempfile.TemporaryDirectory() as directory,
                    patch.dict(os.environ, ENVIRONMENT, clear=True),
                    patch(
                        "tools.build_cache.subprocess.run",
                        side_effect=failure,
                        return_value=native_result("secret\ncache-from=attacker"),
                    ),
                    redirect_stdout(io.StringIO()) as stdout,
                    redirect_stderr(io.StringIO()) as stderr,
                ):
                    root = Path(directory)
                    self.assertEqual(build_cache.main(prepare_arguments(root, access)), 0)
                    record = json.loads((root / "record.json").read_text())
                    self.assertEqual(record["status"], "unavailable")
                    self.assertEqual(record["sources"], [])
                    self.assertEqual("export_ref" in record, access == "write")
                    self.assertIn("::warning::Build cache is unavailable", stderr.getvalue())
                    public = stdout.getvalue() + stderr.getvalue() + json.dumps(record)
                    public += (root / "github-output").read_text()
                    self.assertNotIn("secret", public)
                    self.assertNotIn("attacker", public)
                    self.assertTrue(
                        (root / "github-output").read_text().startswith("cache-from=\n")
                    )

    def test_invalid_native_digest_fails_instead_of_claiming_cache_unavailability(self) -> None:
        with (
            tempfile.TemporaryDirectory() as directory,
            patch.dict(os.environ, ENVIRONMENT, clear=True),
            patch(
                "tools.build_cache.subprocess.run",
                return_value=native_result("secret\ncache-from=attacker"),
            ),
            redirect_stderr(io.StringIO()) as stderr,
        ):
            root = Path(directory)
            self.assertEqual(build_cache.main(prepare_arguments(root, "write")), 1)
            self.assertFalse((root / "record.json").exists())
            self.assertFalse((root / "github-output").exists())
            self.assertNotIn("secret", stderr.getvalue())
            self.assertNotIn("attacker", stderr.getvalue())
            self.assertIn("::error::", stderr.getvalue())

    def test_write_guard_rejects_untrusted_context_before_any_native_call(self) -> None:
        invalid_environment = [
            {"GITHUB_EVENT_NAME": event}
            for event in ("pull_request", "pull_request_target", "release", "schedule", "")
        ] + [
            {"GITHUB_REF": "refs/heads/feature"},
            {"GITHUB_REF": "refs/tags/trunk"},
            {"GITHUB_DEFAULT_BRANCH": ""},
            {"GITHUB_DEFAULT_BRANCH": "main"},
            {"GITHUB_RUN_ID": "0"},
            {"GITHUB_RUN_ID": "01"},
            {"GITHUB_RUN_ID": "-1"},
            {"GITHUB_RUN_ID": "123\n"},
            {"GITHUB_RUN_ATTEMPT": ""},
            {"GITHUB_RUN_ATTEMPT": "2,mode=max"},
            {"GITHUB_REPOSITORY": "attacker/project"},
            {"GITHUB_REPOSITORY": ""},
        ]
        for changes in invalid_environment:
            environment = {**ENVIRONMENT, **changes}
            with (
                self.subTest(changes=changes),
                patch("tools.build_cache.subprocess.run") as native,
            ):
                with self.assertRaises(ValueError):
                    build_cache.prepare(
                        role="api",
                        repository=REPOSITORY,
                        platform="linux/amd64",
                        access="write",
                        environ=environment,
                    )
                record = available_record()
                record["export_ref"] = staging_ref()
                with self.assertRaises(ValueError):
                    build_cache.promote(record, environ=environment)
                native.assert_not_called()

    def test_prepare_rejects_bad_namespace_role_platform_or_access_without_network(self) -> None:
        options = {
            "role": "api",
            "repository": REPOSITORY,
            "platform": "linux/amd64",
            "access": "read",
        }
        for changes in (
            {"repository": "ghcr.io/attacker/project-buildcache"},
            {"repository": REPOSITORY + "\n"},
            {"role": "api,mode=max"},
            {"platform": "linux/arm64"},
            {"access": "admin"},
        ):
            with self.subTest(changes=changes), patch("tools.build_cache.subprocess.run") as native:
                with self.assertRaises(ValueError):
                    build_cache.prepare(**(options | changes), environ=ENVIRONMENT)
                native.assert_not_called()


class PromotionTests(unittest.TestCase):
    def test_promotion_uses_immutable_staging_and_verifies_main_without_rewriting_record(
        self,
    ) -> None:
        for role in ("api", "backend"):
            with (
                self.subTest(role=role),
                tempfile.TemporaryDirectory() as directory,
                patch.dict(os.environ, ENVIRONMENT, clear=True),
                patch(
                    "tools.build_cache.subprocess.run",
                    side_effect=[native_result(DIGEST), native_result(), native_result(DIGEST)],
                ) as native,
            ):
                record = available_record(role)
                record.update(status="unavailable", sources=[], export_ref=staging_ref(role))
                path = Path(directory) / "record.json"
                content = json.dumps(record, indent=4) + "\n"
                path.write_text(content)
                self.assertEqual(build_cache.main(["promote", "--record", str(path)]), 0)
                self.assertEqual(path.read_text(), content)
                self.assertEqual(
                    native.call_args_list[1].args[0],
                    [
                        "docker",
                        "buildx",
                        "imagetools",
                        "create",
                        "--prefer-index=false",
                        "--tag",
                        f"{REPOSITORY}-{role}:main-linux-amd64-v1",
                        f"{staging_ref(role)}@{DIGEST}",
                    ],
                )
                self.assertIn(
                    f"{REPOSITORY}-{role}:main-linux-amd64-v1", native.call_args_list[2].args[0]
                )

    def test_promotion_rejects_missing_foreign_or_previous_run_exporter(self) -> None:
        for exporter in (
            None,
            staging_ref().replace("run-123-2", "run-122-2"),
            staging_ref().replace("run-123-2", "run-123-1"),
            staging_ref().replace("owner/", "attacker/"),
        ):
            record = available_record()
            if exporter is not None:
                record["export_ref"] = exporter
            original = copy.deepcopy(record)
            with (
                self.subTest(exporter=exporter),
                patch("tools.build_cache.subprocess.run") as native,
            ):
                with self.assertRaises(ValueError):
                    build_cache.promote(record, environ=ENVIRONMENT)
                self.assertEqual(record, original)
                native.assert_not_called()

    def test_no_success_on_failed_resolve_copy_or_mismatched_readback(self) -> None:
        failure = subprocess.CalledProcessError(1, ["docker"], stderr="private-secret")
        scenarios = (
            [failure],
            [native_result("private-secret")],
            [native_result(DIGEST), failure],
            [native_result(DIGEST), native_result(), failure],
            [native_result(DIGEST), native_result(), native_result(OTHER_DIGEST)],
        )
        for responses in scenarios:
            with (
                self.subTest(responses=len(responses)),
                tempfile.TemporaryDirectory() as directory,
                patch.dict(os.environ, ENVIRONMENT, clear=True),
                patch("tools.build_cache.subprocess.run", side_effect=responses) as native,
                redirect_stderr(io.StringIO()) as stderr,
            ):
                record = available_record()
                record["export_ref"] = staging_ref()
                path = Path(directory) / "record.json"
                content = json.dumps(record)
                path.write_text(content)
                self.assertEqual(build_cache.main(["promote", "--record", str(path)]), 1)
                self.assertEqual(path.read_text(), content)
                self.assertEqual(native.call_count, len(responses))
                self.assertNotIn("private-secret", stderr.getvalue())
                self.assertIn("::error::", stderr.getvalue())

    def test_bad_json_fails_before_remote_access_and_keeps_original_bytes(self) -> None:
        with (
            tempfile.TemporaryDirectory() as directory,
            patch("tools.build_cache.subprocess.run") as native,
            redirect_stderr(io.StringIO()) as stderr,
        ):
            path = Path(directory) / "record.json"
            path.write_text("not-json-private-secret")
            self.assertEqual(build_cache.main(["promote", "--record", str(path)]), 1)
            self.assertEqual(path.read_text(), "not-json-private-secret")
            native.assert_not_called()
            self.assertNotIn("private-secret", stderr.getvalue())


NOW = datetime.now(UTC)
RETENTION_ENVIRONMENT = {
    **ENVIRONMENT,
    "GITHUB_EVENT_REPOSITORY": "Owner/Project",
    "GITHUB_REPOSITORY_OWNER_TYPE": "User",
}
STAGING_TAG = "run-123-2-linux-amd64-v1"
MAIN_TAG = "main-linux-amd64-v1"


def version_data(
    identifier: int,
    *,
    days: float = 30,
    tags: tuple[str, ...] = (),
    updated_days: float | None = None,
) -> dict[str, Any]:
    return {
        "id": identifier,
        "name": f"sha256:{identifier:064x}",
        "created_at": (NOW - timedelta(days=days)).isoformat(),
        "updated_at": (
            NOW - timedelta(days=days if updated_days is None else updated_days)
        ).isoformat(),
        "metadata": {"package_type": "container", "container": {"tags": list(tags)}},
    }


def retained_versions() -> list[dict[str, Any]]:
    # Old fixtures deliberately make newest-three protection independent of age protection.
    return [version_data(i, days=20 + i, tags=(STAGING_TAG,)) for i in (1, 2, 3)] + [
        version_data(4, days=40, tags=(MAIN_TAG,)),
    ]


def pages_result(*pages: list[dict[str, Any]]) -> subprocess.CompletedProcess[str]:
    return native_result(json.dumps(pages))


class RetentionPolicyTests(unittest.TestCase):
    def select(self, *versions: dict[str, Any]) -> list[int]:
        data = [version_data(i, days=i, tags=(STAGING_TAG,)) for i in (1, 2, 3)]
        data += [version_data(4, days=40, tags=(MAIN_TAG,)), *versions]
        return [
            version.identifier
            for version in build_cache._retention_candidates(
                [build_cache._cache_version(item, NOW) for item in data], NOW
            )
        ]

    def test_main_and_newest_three_digests_survive_even_when_old(self) -> None:
        versions = [
            build_cache._cache_version(item, NOW)
            for item in retained_versions() + [version_data(5)]
        ]
        self.assertEqual(
            [item.identifier for item in build_cache._retention_candidates(versions, NOW)], [5]
        )

    def test_only_old_staging_or_untagged_versions_are_eligible(self) -> None:
        self.assertEqual(
            self.select(
                version_data(5, days=8, tags=(STAGING_TAG,)),
                version_data(6, days=15),
                version_data(7, days=7, tags=(STAGING_TAG,)),
                version_data(8, days=14),
                version_data(9, days=6, tags=(STAGING_TAG,)),
                version_data(10, days=13),
                version_data(11, days=31, updated_days=1),
            ),
            [6, 5],
        )

    def test_release_unknown_other_platform_or_mixed_tags_forbid_deletion(self) -> None:
        for tags in (
            ("v1.0.0",),
            (STAGING_TAG, "v1.0.0"),
            (STAGING_TAG, "latest"),
            ("run-1-1-linux-arm64-v1",),
            ("run-1-1-linux-amd64-v2",),
            ("run-01-1-linux-amd64-v1",),
            ("MAIN-linux-amd64-v1",),
            ("run-1-1-linux-amd64-v1\n",),
        ):
            with self.subTest(tags=tags):
                self.assertEqual(self.select(version_data(5, tags=tags)), [])
        self.assertEqual(
            self.select(version_data(5, tags=(STAGING_TAG, "run-2-1-linux-amd64-v1"))), [5]
        )

    def test_recently_updated_old_digest_is_retained(self) -> None:
        self.assertEqual(
            self.select(version_data(5, days=100, updated_days=2, tags=(STAGING_TAG,))), []
        )

    def test_missing_or_multiple_main_digests_fail_closed(self) -> None:
        for data in (
            [],
            retained_versions()[:3],
            retained_versions() + [version_data(5, tags=(MAIN_TAG,))],
        ):
            with self.subTest(versions=len(data)), self.assertRaises(ValueError):
                build_cache._retention_candidates(
                    [build_cache._cache_version(item, NOW) for item in data], NOW
                )

    def test_invalid_ids_digests_timestamps_and_metadata_fail_closed(self) -> None:
        metadata_values: tuple[object, ...] = (
            None,
            [],
            {},
            {"package_type": "docker", "container": {"tags": []}},
            {"package_type": "container"},
            {"package_type": "container", "container": {"tags": None}},
            {"package_type": "container", "container": {"tags": "latest"}},
            {"package_type": "container", "container": {"tags": [False]}},
            {"package_type": "container", "container": {"tags": [""]}},
        )
        invalid: list[dict[str, Any]] = (
            [{"id": value} for value in (True, 0, -1, "5", 5.0, "5/../../other")]
            + [{"name": value} for value in (None, "sha256:abc", DIGEST.upper(), DIGEST + "\n")]
            + [
                {field: value}
                for field in ("created_at", "updated_at")
                for value in (
                    None,
                    123,
                    "2026-01-01",
                    "2026-01-01T00:00:00",
                    "2026-02-31T00:00:00Z",
                    "2026-01-01T00:00:00+01:99",
                    "2026-01-01T00:00:00+24:00",
                    "2026-01-01T00:60:00Z",
                    "2026-01-01T24:00:00Z",
                    "2026-01-01T00:00:00Z\n",
                    (NOW + timedelta(days=1)).isoformat(),
                )
            ]
            + [{"metadata": value} for value in metadata_values]
        )
        for changes in invalid:
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                build_cache._cache_version(version_data(5) | changes, NOW)
        invalid_objects: tuple[object, ...] = (None, [], "private-secret")
        for value in invalid_objects:
            with self.subTest(value=value), self.assertRaises(ValueError):
                build_cache._cache_version(value, NOW)
        with self.assertRaises(ValueError):
            build_cache._cache_version(version_data(5, days=1, updated_days=2), NOW)

    def test_utc_offset_timestamp_is_valid_but_never_naive(self) -> None:
        data = version_data(5)
        data["created_at"] = "2020-01-01T03:00:00+03:00"
        data["updated_at"] = "2020-01-01T00:00:00Z"
        version = build_cache._cache_version(data, NOW)
        self.assertEqual(version.created, version.updated)


class RetentionExecutionTests(unittest.TestCase):
    def test_default_cli_is_dry_run_and_reads_all_pages_of_both_exact_packages(self) -> None:
        for owner_type, scope in (("User", "users"), ("Organization", "orgs")):
            with (
                self.subTest(owner_type=owner_type),
                tempfile.TemporaryDirectory() as directory,
                patch.dict(
                    os.environ,
                    RETENTION_ENVIRONMENT | {"GITHUB_REPOSITORY_OWNER_TYPE": owner_type},
                    clear=True,
                ),
                patch(
                    "tools.build_cache.subprocess.run",
                    side_effect=[
                        pages_result(retained_versions(), [version_data(5)]),
                        pages_result(retained_versions(), [version_data(6, tags=(STAGING_TAG,))]),
                    ],
                ) as native,
                redirect_stdout(io.StringIO()) as stdout,
            ):
                output = Path(directory) / "retention.json"
                self.assertEqual(build_cache.main(["retain", "--output", str(output)]), 0)
                report = json.loads(output.read_text())
                self.assertEqual(
                    (report["mode"], report["selected"], report["deleted"]), ("dry-run", 2, 0)
                )
                self.assertEqual([item["id"] for item in report["candidates"]], [5, 6])
                self.assertIn("2 eligible", stdout.getvalue())
                self.assertEqual(native.call_count, 2)
                for role, call in zip(("api", "backend"), native.call_args_list, strict=True):
                    command = call.args[0]
                    self.assertEqual(command[:3], ["gh", "api", "--hostname"])
                    self.assertIn("github.com", command)
                    self.assertIn("--paginate", command)
                    self.assertIn("--slurp", command)
                    self.assertEqual(
                        command[-1],
                        f"/{scope}/owner/packages/container/project-buildcache-{role}/versions?per_page=100&state=active",
                    )
                    self.assertTrue(call.kwargs["capture_output"])
                    self.assertGreater(call.kwargs["timeout"], 0)
                    self.assertNotIn("private-native-diagnostics", output.read_text())

    def test_apply_preflights_both_lists_and_all_candidates_before_first_delete(self) -> None:
        candidates = [version_data(5), version_data(6)]
        with patch(
            "tools.build_cache.subprocess.run",
            side_effect=[
                pages_result(retained_versions(), [candidates[0]]),
                pages_result(retained_versions(), [candidates[1]]),
                native_result(json.dumps(candidates[0])),
                native_result(json.dumps(candidates[1])),
                native_result(),
                native_result(),
            ],
        ) as native:
            report = build_cache.retain(environ=RETENTION_ENVIRONMENT, apply=True)
        self.assertEqual(report["deleted"], 2)
        calls = [call.args[0] for call in native.call_args_list]
        self.assertEqual(
            [call[call.index("--method") + 1] for call in calls], ["GET"] * 4 + ["DELETE"] * 2
        )
        self.assertEqual(
            calls[4][-1], "/users/owner/packages/container/project-buildcache-api/versions/5"
        )
        self.assertEqual(
            calls[5][-1], "/users/owner/packages/container/project-buildcache-backend/versions/6"
        )

    def test_invalid_later_package_or_page_cannot_trigger_partial_deletion(self) -> None:
        for bad in (
            pages_result(retained_versions(), [{"id": "secret"}]),
            native_result(json.dumps({"message": "private-secret"})),
            native_result(json.dumps([retained_versions(), {"message": "private-secret"}])),
            pages_result(retained_versions(), [retained_versions()[0]]),
            pages_result(
                retained_versions(), [version_data(9) | {"name": retained_versions()[0]["name"]}]
            ),
        ):
            with (
                self.subTest(response=bad.stdout),
                patch(
                    "tools.build_cache.subprocess.run",
                    side_effect=[
                        pages_result(retained_versions(), [version_data(5)]),
                        bad,
                    ],
                ) as native,
            ):
                with self.assertRaises(ValueError):
                    build_cache.retain(environ=RETENTION_ENVIRONMENT, apply=True)
                self.assertEqual(native.call_count, 2)

    def test_changed_candidate_main_tag_or_wrong_id_aborts_before_any_delete(self) -> None:
        for changes in (
            {"id": 99},
            {"name": DIGEST},
            {"updated_at": (NOW - timedelta(days=1)).isoformat()},
            {"metadata": {"package_type": "container", "container": {"tags": [MAIN_TAG]}}},
        ):
            with (
                self.subTest(changes=changes),
                patch(
                    "tools.build_cache.subprocess.run",
                    side_effect=[
                        pages_result(retained_versions(), [version_data(5)]),
                        pages_result(retained_versions(), [version_data(6)]),
                        native_result(json.dumps(version_data(5))),
                        native_result(json.dumps(version_data(6) | changes)),
                    ],
                ) as native,
            ):
                with self.assertRaises(ValueError):
                    build_cache.retain(environ=RETENTION_ENVIRONMENT, apply=True)
                self.assertEqual(native.call_count, 4)

    def test_global_cap_is_one_hundred_and_remaining_candidates_are_deferred(self) -> None:
        with patch(
            "tools.build_cache.subprocess.run",
            side_effect=[
                pages_result(retained_versions(), [version_data(i) for i in range(5, 65)]),
                pages_result(retained_versions(), [version_data(i) for i in range(65, 125)]),
            ]
            + [native_result(json.dumps(version_data(i))) for i in range(5, 105)]
            + [native_result()] * 100,
        ) as native:
            report = build_cache.retain(environ=RETENTION_ENVIRONMENT, apply=True)
        self.assertEqual(
            (report["eligible"], report["selected"], report["deferred"], report["deleted"]),
            (120, 100, 20, 100),
        )
        self.assertEqual(sum("DELETE" in call.args[0] for call in native.call_args_list), 100)

    def test_context_guard_rejects_pr_foreign_repository_branch_and_owner_before_gh(self) -> None:
        changes = [
            {"GITHUB_EVENT_NAME": event}
            for event in ("pull_request", "pull_request_target", "release", "")
        ] + [
            {"GITHUB_EVENT_REPOSITORY": "Attacker/Project"},
            {"GITHUB_REPOSITORY": "Owner/Other"},
            {"GITHUB_REPOSITORY": "Owner/Project/other"},
            {"GITHUB_REPOSITORY": "Owner/Project\n"},
            {"GITHUB_REPOSITORY": ""},
            {"GITHUB_DEFAULT_BRANCH": ""},
            {"GITHUB_REF": "refs/heads/feature"},
            {"GITHUB_REF": "refs/pull/1/merge"},
            {"GITHUB_REPOSITORY_OWNER_TYPE": "orgs"},
            {"GITHUB_REPOSITORY_OWNER_TYPE": ""},
        ]
        for change in changes:
            for apply in (False, True):
                with (
                    self.subTest(change=change, apply=apply),
                    patch("tools.build_cache.subprocess.run") as native,
                ):
                    with self.assertRaises(ValueError):
                        build_cache.retain(environ=RETENTION_ENVIRONMENT | change, apply=apply)
                    native.assert_not_called()

    def test_all_allowed_events_and_context_recheck_before_delete(self) -> None:
        for event in ("push", "workflow_dispatch", "schedule"):
            environment = RETENTION_ENVIRONMENT | {"GITHUB_EVENT_NAME": event}
            with (
                self.subTest(event=event),
                patch(
                    "tools.build_cache.subprocess.run",
                    side_effect=[
                        pages_result(retained_versions()),
                        pages_result(retained_versions()),
                    ],
                ),
            ):
                self.assertEqual(build_cache.retain(environ=environment, apply=True)["deleted"], 0)
        environment = dict(RETENTION_ENVIRONMENT)
        responses = iter(
            [
                pages_result(retained_versions(), [version_data(5)]),
                pages_result(retained_versions()),
                native_result(json.dumps(version_data(5))),
            ]
        )

        def change_context(*args: object, **kwargs: object) -> subprocess.CompletedProcess[str]:
            result = next(responses)
            if not any("--paginate" in arg for arg in args if isinstance(arg, list)):
                environment["GITHUB_EVENT_NAME"] = "pull_request"
            return result

        with patch("tools.build_cache.subprocess.run", side_effect=change_context) as native:
            with self.assertRaises(ValueError):
                build_cache.retain(environ=environment, apply=True)
            self.assertEqual(native.call_count, 3)

    def test_gh_errors_and_private_responses_are_not_exposed_by_cli(self) -> None:
        failures = (
            subprocess.CalledProcessError(
                1, ["gh"], output="private-secret", stderr="private-secret"
            ),
            subprocess.TimeoutExpired(
                ["gh"], 120, output="private-secret", stderr="private-secret"
            ),
            FileNotFoundError("private-secret"),
        )
        for failure in failures:
            with (
                self.subTest(failure=type(failure)),
                tempfile.TemporaryDirectory() as directory,
                patch.dict(os.environ, RETENTION_ENVIRONMENT, clear=True),
                patch("tools.build_cache.subprocess.run", side_effect=failure),
                redirect_stdout(io.StringIO()) as stdout,
                redirect_stderr(io.StringIO()) as stderr,
            ):
                output = Path(directory) / "retention.json"
                self.assertEqual(
                    build_cache.main(["retain", "--apply", "--output", str(output)]), 1
                )
                self.assertFalse(output.exists())
                self.assertNotIn("private-secret", stdout.getvalue() + stderr.getvalue())

    def test_delete_failure_stops_remaining_mutations(self) -> None:
        failure = subprocess.CalledProcessError(1, ["gh"], stderr="private-secret")
        with patch(
            "tools.build_cache.subprocess.run",
            side_effect=[
                pages_result(retained_versions(), [version_data(5), version_data(6)]),
                pages_result(retained_versions()),
                native_result(json.dumps(version_data(5))),
                native_result(json.dumps(version_data(6))),
                failure,
            ],
        ) as native:
            with self.assertRaisesRegex(RuntimeError, "GitHub cache retention operation failed"):
                build_cache.retain(environ=RETENTION_ENVIRONMENT, apply=True)
            self.assertEqual(native.call_count, 5)


if __name__ == "__main__":
    unittest.main()
