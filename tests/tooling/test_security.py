import contextlib
import copy
import hashlib
import io
import json
import tempfile
import unittest
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any
from unittest.mock import patch

from tools.security import check, run

NOW = datetime(2026, 10, 23, 12, tzinfo=UTC)
SHA = "1" * 40
DIGEST = "sha256:" + "2" * 64


class ScannerReportTests(unittest.TestCase):
    def test_trivy_requires_coverage_for_the_requested_scanner(self) -> None:
        for scanner in ("trivy-config", "trivy-vuln"):
            for result in ({}, {"Class": "secret", "Target": "source", "Secrets": []}):
                with self.subTest(scanner=scanner, result=result), self.assertRaises(ValueError):
                    check.evaluate(scanner, {"SchemaVersion": 2, "Results": [result]}, 0)

    def test_trivy_rejects_malformed_finding_severity(self) -> None:
        for scanner, field, kind in (
            ("trivy-config", "Misconfigurations", "config"),
            ("trivy-vuln", "Vulnerabilities", "os-pkgs"),
        ):
            for severity in (None, "", "high", "UNSUPPORTED"):
                result = {
                    "Class": kind,
                    "Target": "fixture",
                    "Type": "dockerfile" if kind == "config" else "ubuntu",
                    field: [{"ID": "AVD-DS-0002", "Status": "FAIL", "Severity": severity}],
                }
                with (
                    self.subTest(scanner=scanner, severity=severity),
                    self.assertRaises(ValueError),
                ):
                    check.evaluate(scanner, {"SchemaVersion": 2, "Results": [result]}, 0)

    def test_vulnerability_status_cannot_bypass_severity_policy(self) -> None:
        for status in ("PASS", "EXCEPTION"):
            raw = {
                "SchemaVersion": 2,
                "Results": [
                    {
                        "Class": "os-pkgs",
                        "Type": "ubuntu",
                        "Target": "fixture",
                        "Vulnerabilities": [
                            {
                                "VulnerabilityID": "CVE-2026-12345",
                                "PkgName": "fixture",
                                "Severity": "HIGH",
                                "Status": status,
                            }
                        ],
                    }
                ],
            }
            with self.subTest(status=status):
                self.assertEqual(check.evaluate("trivy-vuln", raw, 0)["blocking_count"], 1)

    def test_config_report_without_evaluated_rules_is_not_clean(self) -> None:
        raw = {
            "SchemaVersion": 2,
            "Results": [
                {
                    "Class": "config",
                    "Type": "helm",
                    "Target": "fixture",
                }
            ],
        }
        with self.assertRaises(ValueError):
            check.evaluate("trivy-config", raw, 0)

    def test_reported_suppressions_require_project_review_not_scanner_waivers(self) -> None:
        for scanner, kind, field in (
            ("trivy-config", "config", "Misconfigurations"),
            ("trivy-vuln", "os-pkgs", "Vulnerabilities"),
            ("trivy-secret", "secret", "Secrets"),
        ):
            result = {
                "Class": kind,
                "Target": "fixture",
                "Type": "fixture",
                field: [],
                "ExperimentalModifiedFindings": [
                    {"Status": "ignored", "Finding": {"Severity": "HIGH"}}
                ],
            }
            with self.subTest(scanner=scanner), self.assertRaisesRegex(ValueError, "suppressed"):
                check.evaluate(scanner, {"SchemaVersion": 2, "Results": [result]}, 0)

    def test_govulncheck_missing_reachability_is_not_a_clean_finding(self) -> None:
        config = {"config": {"scanner_name": "govulncheck", "protocol_version": "v1.0.0"}}
        for finding in ({"osv": "GO-2026-1234"}, {"osv": "GO-2026-1234", "trace": []}):
            with self.subTest(finding=finding), self.assertRaises(ValueError):
                check.evaluate("govulncheck", [config, {"finding": finding}], 0)

    def test_only_evaluated_successful_config_rules_are_clean(self) -> None:
        result: dict[str, Any] = {
            "Class": "config",
            "Type": "kubernetes",
            "Target": "fixture",
            "Misconfigurations": [{"ID": "AVD-KSV-0001", "Severity": "HIGH", "Status": "PASS"}],
        }
        raw = {"SchemaVersion": 2, "Results": [result]}
        self.assertEqual(check.evaluate("trivy-config", raw, 0)["blocking_count"], 0)
        for status in ("EXCEPTION", "", None):
            result["Misconfigurations"][0]["Status"] = status
            with self.subTest(status=status), self.assertRaises(ValueError):
                check.evaluate("trivy-config", raw, 0)

    def test_covered_clean_packages_and_uncalled_go_findings_remain_nonblocking(self) -> None:
        raw = {
            "SchemaVersion": 2,
            "Results": [
                {
                    "Class": "os-pkgs",
                    "Type": "ubuntu",
                    "Target": "fixture",
                }
            ],
        }
        self.assertEqual(check.evaluate("trivy-vuln", raw, 0)["blocking_count"], 0)
        stream = [
            {"config": {"scanner_name": "govulncheck", "protocol_version": "v1.0.0"}},
            {"finding": {"osv": "GO-2026-1234", "trace": [{"module": "fixture"}]}},
        ]
        result = check.evaluate("govulncheck", stream, 0)
        self.assertEqual((result["finding_count"], result["blocking_count"]), (1, 0))

    def test_trivy_exception_matches_only_one_advisory_and_module(self) -> None:
        exception = {
            "scanner": "trivy",
            "rule": "GHSA-cxf3-9h37-7rr8",
            "path": "github.com/labstack/echo/v5",
            "disposition": "not_applicable",
            "owner": "fixture",
            "rationale": "Proxy code is not linked; independently compiled scope test is mandatory",
            "evidence": "tests/tooling/test_echo_security_scope.py",
            "expires": "2026-10-31",
        }
        raw = {
            "SchemaVersion": 2,
            "Results": [
                {
                    "Class": "lang-pkgs",
                    "Type": "gobinary",
                    "Target": "fixture",
                    "Vulnerabilities": [
                        {
                            "VulnerabilityID": exception["rule"],
                            "PkgName": exception["path"],
                            "Severity": "CRITICAL",
                        },
                        {
                            "VulnerabilityID": "GHSA-unrelated-rule-test",
                            "PkgName": exception["path"],
                            "Severity": "HIGH",
                        },
                    ],
                }
            ],
        }
        result = check.evaluate("trivy-vuln", raw, 0, [exception], NOW)

        self.assertEqual(result["blocking_count"], 1)
        self.assertEqual(result["exception_count"], 1)

        changed = copy.deepcopy(exception)
        changed["path"] = "github.com/labstack/echo/v4"

        self.assertEqual(check.evaluate("trivy-vuln", raw, 0, [changed], NOW)["blocking_count"], 2)

    def test_exit_zero_govulncheck_called_finding_blocks(self) -> None:
        raw = {
            "finding": {"osv": "GO-FIXTURE", "trace": [{"module": "fixture", "function": "Unsafe"}]}
        }
        result = check.evaluate(
            "govulncheck",
            [{"config": {"scanner_name": "govulncheck", "protocol_version": "v1.0.0"}}, raw],
            0,
        )

        self.assertEqual(result["blocking_count"], 1)

    def test_all_secret_severities_block_and_content_is_redacted(self) -> None:
        marker = "C02_" + "NONCREDENTIAL_" + "Z" * 20
        for severity in ("LOW", "HIGH", "UNKNOWN"):
            raw = {
                "SchemaVersion": 2,
                "Results": [
                    {
                        "Secrets": [
                            {
                                "RuleID": "fixture",
                                "Severity": severity,
                                "Match": marker,
                                "Code": marker,
                            }
                        ]
                    }
                ],
            }
            result = check.evaluate("trivy-secret", raw, 0)

            self.assertEqual(result["blocking_count"], 1)
            self.assertNotIn(marker, json.dumps(result))

    def test_high_unfixed_vulnerability_and_misconfiguration_block(self) -> None:
        for field in ("Vulnerabilities", "Misconfigurations"):
            raw = {
                "SchemaVersion": 2,
                "Results": [
                    {
                        "Class": "os-pkgs" if field == "Vulnerabilities" else "config",
                        "Target": "fixture",
                        "Type": "ubuntu" if field == "Vulnerabilities" else "dockerfile",
                        field: [{"Severity": "HIGH", "Status": "FAIL", "FixedVersion": ""}],
                    }
                ],
            }

            self.assertEqual(
                check.evaluate(
                    "trivy-vuln" if field == "Vulnerabilities" else "trivy-config", raw, 0
                )["blocking_count"],
                1,
            )

    def test_gosec_and_hadolint_findings_or_scanner_errors_are_not_clean(self) -> None:
        self.assertEqual(
            check.evaluate("gosec", {"Issues": [{"rule_id": "G304"}], "Stats": {"files": 1}}, 0)[
                "blocking_count"
            ],
            1,
        )
        self.assertEqual(
            check.evaluate("hadolint", [{"code": "DL3002", "level": "warning"}], 0)[
                "blocking_count"
            ],
            1,
        )

        for scanner in (
            "govulncheck",
            "gosec",
            "hadolint",
            "trivy-secret",
            "trivy-config",
            "trivy-vuln",
        ):
            with self.subTest(scanner=scanner), self.assertRaises(ValueError):
                check.evaluate(scanner, {}, 2)

    def test_exceptions_require_exact_scope_evidence_owner_and_expiry(self) -> None:
        valid = {
            "scanner": "gosec",
            "rule": "G304",
            "path": "internal/adapter/models/cache_linux.go",
            "disposition": "false_positive",
            "owner": "maintainer",
            "rationale": "Validated os.Root confinement",
            "evidence": "internal/adapter/models/containment_test.go",
            "expires": "2026-11-01",
        }
        check.validate_exceptions([valid], NOW)

        for mutate in (
            lambda x: x.update(path="internal/**"),
            lambda x: x.update(expires="2026-10-22"),
            lambda x: x.pop("owner"),
            lambda x: x.update(disposition="accepted_risk"),
            lambda x: x.update(rule="*"),
            lambda x: x.pop("evidence"),
        ):
            invalid = copy.deepcopy(valid)
            mutate(invalid)

            with self.assertRaises(ValueError):
                check.validate_exceptions([invalid], NOW)

    def test_directory_and_combined_rule_exceptions_are_rejected(self) -> None:
        valid = {
            "scanner": "gosec",
            "rule": "G304",
            "path": "internal/adapter/models/cache_linux.go",
            "disposition": "not_applicable",
            "owner": "swimmwatch",
            "rationale": "Operator-selected prepared file; never a request filename",
            "evidence": "internal/adapter/models/provision_test.go",
            "expires": "2026-11-22",
        }
        for change in ({"path": "internal/adapter/models"}, {"rule": "G304,G204"}):
            invalid = dict(valid, **change)

            with self.subTest(change=change), self.assertRaises(ValueError):
                check.validate_exceptions([invalid], NOW)

    def test_gosec_exception_matches_only_its_exact_rule_and_file(self) -> None:
        exception = {
            "scanner": "gosec",
            "rule": "G304",
            "path": "internal/adapter/models/cache_linux.go",
            "disposition": "not_applicable",
            "owner": "swimmwatch",
            "rationale": "Operator-selected prepared file; never a request filename",
            "evidence": "internal/adapter/models/provision_test.go",
            "expires": "2026-11-22",
        }
        raw = {
            "Issues": [
                {"rule_id": "G304", "file": str(check.ROOT / exception["path"])},
                {"rule_id": "G304", "file": str(check.ROOT / "internal/adapter/audio/decoder.go")},
                {"rule_id": "G204", "file": str(check.ROOT / exception["path"])},
            ],
            "Stats": {"files": 2},
        }
        result = check.evaluate("gosec", raw, 1, exceptions=[exception], now=NOW)

        self.assertEqual(result["finding_count"], 3)
        self.assertEqual(result["exception_count"], 1)
        self.assertEqual(result["blocking_count"], 2)
        self.assertEqual(len(result["exception_refs"]), 1)

    def test_expired_exception_does_not_allow_a_clean_report(self) -> None:
        exception = {
            "scanner": "gosec",
            "rule": "G304",
            "path": "internal/adapter/models/cache_linux.go",
            "disposition": "not_applicable",
            "owner": "swimmwatch",
            "rationale": "Operator-selected prepared file",
            "evidence": "internal/adapter/models/provision_test.go",
            "expires": "2026-10-22",
        }

        with self.assertRaises(ValueError):
            check.evaluate(
                "gosec", {"Issues": [], "Stats": {"files": 1}}, 0, exceptions=[exception], now=NOW
            )


class SecurityAggregateTests(unittest.TestCase):
    def inventory(self) -> dict[str, Any]:
        targets = check.required_targets(check.VALUES_VARIANTS)
        return {
            "schema_version": 1,
            "source_sha": SHA,
            "image_manifest_digests": {"api": DIGEST, "backend": DIGEST},
            "chart_archive_sha256": "3" * 64,
            "values_matrix_identity": DIGEST,
            "values_variants": check.VALUES_VARIANTS,
            "reports": [
                {
                    "target": target,
                    "scanner_version": check.expected_version(target),
                    "rule_identity": DIGEST,
                    "source_sha": SHA,
                    "image_manifest_digests": {"api": DIGEST, "backend": DIGEST},
                    "chart_archive_sha256": "3" * 64,
                    "values_matrix_identity": DIGEST,
                    "scan_time": NOW.isoformat(),
                    "database_checked_at": NOW.isoformat(),
                    "finding_count": 0,
                    "blocking_count": 0,
                    "scanner_exit": 0,
                    "coverage_gaps": [],
                }
                for target in targets
            ],
        }

    def test_evidence_freshness_keeps_its_timezone_and_24_hour_boundary(self) -> None:
        for timestamp, valid in (
            ((NOW - timedelta(hours=24)).isoformat(), True),
            (NOW.isoformat().replace("+00:00", "Z"), True),
            ((NOW - timedelta(hours=24, microseconds=1)).isoformat(), False),
            ((NOW + timedelta(microseconds=1)).isoformat(), False),
            (NOW.replace(tzinfo=None).isoformat(), False),
            (None, False),
        ):
            with self.subTest(timestamp=timestamp):
                data = self.inventory()
                data["reports"][0].update(scan_time=timestamp, database_checked_at=timestamp)
                if valid:
                    self.assertEqual(check.validate(data, NOW)["conclusion"], "success")
                else:
                    with self.assertRaisesRegex(ValueError, "Stale or invalid security timestamp"):
                        check.validate(data, NOW)

    def test_complete_matching_coverage_passes(self) -> None:
        self.assertEqual(check.validate(self.inventory(), NOW)["conclusion"], "success")

    def test_missing_duplicate_stale_wrong_digest_and_blocking_reports_deny(self) -> None:
        for mutate in (
            lambda x: x["reports"].pop(),
            lambda x: x["reports"].append(copy.deepcopy(x["reports"][0])),
            lambda x: x["reports"][0].update(blocking_count=1),
            lambda x: x["reports"][0].update(scanner_exit=2),
            lambda x: x["reports"][0].update(source_sha="4" * 40),
            lambda x: x["reports"][0].update(chart_archive_sha256="5" * 64),
            lambda x: x["reports"][0].update(database_checked_at="2026-10-20T00:00:00+00:00"),
            lambda x: x["reports"][0].update(coverage_gaps=["unidentified-native-source"]),
        ):
            data = self.inventory()
            mutate(data)

            with self.assertRaises(ValueError):
                check.validate(data, NOW)

    def test_only_valid_gosec_exceptions_allow_its_finding_exit(self) -> None:
        data = self.inventory()
        exception = json.loads((check.ROOT / "tools/security/exceptions.json").read_bytes())[0]
        data["exceptions"] = [exception]
        report = next(item for item in data["reports"] if item["target"] == "go-source")
        report.update(
            finding_count=2,
            exception_count=2,
            exception_refs=[check.exception_ref(exception)],
            scanner_exit=1,
        )
        check.validate(data, NOW)

        for change in (
            {"exception_count": 1},
            {"exception_refs": ["sha256:" + "9" * 64]},
            {"target": "api-vuln"},
            {"finding_count": True},
            {"scanner_exit": True},
        ):
            invalid = copy.deepcopy(data)
            selected = next(item for item in invalid["reports"] if item["target"] == "go-source")
            selected.update(change)

            with self.subTest(change=change), self.assertRaises(ValueError):
                check.validate(invalid, NOW)


class ScanSessionTests(unittest.TestCase):
    def test_rule_identity_retains_binary_then_exact_scanner_context(self) -> None:
        config = {"scanner_name": "govulncheck", "protocol_version": "v1.0.0"}
        metadata = '{"Version":"fixture-version","RuleBundle":"fixture-rules"}\n'
        for target, scanner, raw, context in (
            ("dockerfile-api", "hadolint", "[]", b""),
            (
                "package-secret",
                "trivy-secret",
                '{"SchemaVersion":2,"Results":[]}',
                metadata.encode(),
            ),
            (
                "go-vuln",
                "govulncheck",
                json.dumps({"config": config}),
                json.dumps([config], sort_keys=True).encode(),
            ),
        ):
            with self.subTest(scanner=scanner), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                binary = root / check.TARGETS[target]
                observed = []
                for content in (b"pinned executable A", b"pinned executable B"):
                    binary.write_bytes(content)
                    session = run.ScanSession(root, {}, {}, [], None, [], root)
                    with (
                        patch.object(run, "execute", side_effect=[(0, raw), (0, metadata)]),
                        contextlib.redirect_stdout(io.StringIO()),
                    ):
                        session.add(target, scanner, ["scanner"])
                    self.assertFalse(session.failed)
                    expected = "sha256:" + hashlib.sha256(content + context).hexdigest()
                    self.assertEqual(session.reports[0]["rule_identity"], expected)
                    observed.append(expected)
                self.assertNotEqual(observed[0], observed[1])

    def test_failed_target_remains_failed_after_a_later_clean_target(self) -> None:
        marker = "PRIVATE_SCANNER_OUTPUT"
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "hadolint").write_bytes(b"pinned fixture")
            session = run.ScanSession(root, {}, {}, [], None, [], root)
            output = io.StringIO()
            with (
                patch.object(run, "execute", side_effect=[ValueError(marker), (0, "[]")]),
                contextlib.redirect_stdout(output),
            ):
                session.add("dockerfile-api", "hadolint", ["scanner"])
                session.add("dockerfile-backend", "hadolint", ["scanner"])

            self.assertTrue(session.failed)
            self.assertEqual(len(session.reports), 1)
            self.assertEqual(session.reports[0]["target"], "dockerfile-backend")
            self.assertNotIn(marker, output.getvalue())
            self.assertNotIn("findings", output.getvalue())
            self.assertEqual(
                json.loads(output.getvalue().splitlines()[0])["stage"], "scanner_execution"
            )

    def test_native_review_drift_is_distinct_from_successful_vulnerability_scanning(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "trivy").write_bytes(b"pinned fixture")
            advisory_path = root / "advisories.json"
            advisory_path.write_text(
                json.dumps({"schema_version": 1, "build_input_identity": "sha256:" + "a" * 64})
            )
            session = run.ScanSession(
                root,
                {},
                {"api": {"build_input_identity": "sha256:" + "b" * 64}},
                [],
                advisory_path,
                [],
                root,
            )
            raw = {
                "SchemaVersion": 2,
                "Results": [{"Class": "os-pkgs", "Type": "ubuntu", "Target": "fixture"}],
            }
            now = datetime.now(UTC)
            metadata = {
                "VulnerabilityDB": {
                    "Version": 2,
                    "UpdatedAt": now.isoformat(),
                    "NextUpdate": (now + timedelta(hours=1)).isoformat(),
                }
            }
            output = io.StringIO()
            with (
                patch.object(
                    run, "execute", side_effect=[(0, json.dumps(raw)), (0, json.dumps(metadata))]
                ),
                contextlib.redirect_stdout(output),
            ):
                session.add("api-vuln", "trivy-vuln", ["scanner"])

            self.assertTrue(session.failed)
            self.assertEqual(session.reports, [])
            diagnostic = json.loads(output.getvalue())
            self.assertEqual(diagnostic["status"], "failed")
            self.assertEqual(diagnostic["stage"], "native_component_review")

    def test_successful_scanner_with_stale_database_has_no_accepted_report(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "trivy").write_bytes(b"pinned fixture")
            session = run.ScanSession(root, {}, {}, [], None, [], root)
            raw = {
                "SchemaVersion": 2,
                "Results": [{"Class": "os-pkgs", "Type": "ubuntu", "Target": "fixture"}],
            }
            metadata = {
                "VulnerabilityDB": {
                    "Version": 2,
                    "UpdatedAt": "2000-01-01T00:00:00Z",
                    "NextUpdate": "2000-01-02T00:00:00Z",
                }
            }
            output = io.StringIO()
            with (
                patch.object(
                    run, "execute", side_effect=[(0, json.dumps(raw)), (0, json.dumps(metadata))]
                ),
                contextlib.redirect_stdout(output),
            ):
                session.add("api-vuln", "trivy-vuln", ["scanner"])

            self.assertTrue(session.failed)
            self.assertEqual(session.reports, [])
            diagnostic = json.loads(output.getvalue())
            self.assertEqual(diagnostic["status"], "failed")
            self.assertEqual(diagnostic["stage"], "database_freshness")

    def test_public_findings_retain_only_safe_advisory_fields(self) -> None:
        result = check.safe_findings(
            "trivy-vuln",
            [
                {
                    "VulnerabilityID": "CVE-2026-12345",
                    "PkgName": "safe/package",
                    "InstalledVersion": "1.0",
                    "FixedVersion": "2.0",
                    "Severity": "HIGH",
                    "Description": "PRIVATE_SCANNER_OUTPUT",
                    "Code": {"Lines": ["secret"]},
                },
                {"VulnerabilityID": "unsafe free-form identity"},
            ],
        )

        self.assertEqual(
            result,
            [
                {
                    "id": "CVE-2026-12345",
                    "package": "safe/package",
                    "installed_version": "1.0",
                    "fixed_version": "2.0",
                    "severity": "HIGH",
                }
            ],
        )


if __name__ == "__main__":
    unittest.main()
