"""Real pinned scanner fixtures; enable explicitly after installing all scanners."""

import json
import os
import tempfile
import unittest
from pathlib import Path

from tools.security.check import evaluate
from tools.security.run import HELM_SCAN_FLAGS, execute, json_stream


@unittest.skipUnless(
    os.environ.get("C02_SCANNER_TESTS") == "1", "Requires pinned C02 scanner executables"
)
class ActualScannerTests(unittest.TestCase):
    def setUp(self) -> None:
        self.binaries = Path(os.environ["C02_SCANNER_BIN"]).resolve()
        self.workspace = tempfile.TemporaryDirectory(prefix="whisper-scanner-fixture-")
        self.addCleanup(self.workspace.cleanup)
        self.root = Path(self.workspace.name)

    def test_reachable_fixed_database_advisory_blocks_even_when_scanner_exits_zero(self) -> None:
        database = self.root / "database"
        (database / "index").mkdir(parents=True)
        (database / "ID").mkdir()
        instant, identifier = "2026-10-01T00:00:00Z", "GO-2099-9999"
        (database / "index/db.json").write_text(json.dumps({"modified": instant}))
        (database / "index/modules.json").write_text(
            json.dumps(
                [
                    {
                        "path": "stdlib",
                        "vulns": [{"id": identifier, "modified": instant, "fixed": "99.0.0"}],
                    }
                ]
            )
        )
        (database / "index/vulns.json").write_text(
            json.dumps([{"id": identifier, "modified": instant}])
        )
        (database / "ID" / (identifier + ".json")).write_text(
            json.dumps(
                {
                    "schema_version": "1.3.1",
                    "id": identifier,
                    "modified": instant,
                    "published": instant,
                    "details": "Isolated synthetic scanner fixture; not an actual advisory",
                    "affected": [
                        {
                            "package": {"ecosystem": "Go", "name": "stdlib"},
                            "ranges": [
                                {
                                    "type": "SEMVER",
                                    "events": [{"introduced": "0"}, {"fixed": "99.0.0"}],
                                }
                            ],
                            "ecosystem_specific": {
                                "imports": [{"path": "strings", "symbols": ["ToUpper"]}]
                            },
                        }
                    ],
                }
            )
        )
        (self.root / "go.mod").write_text("module example.com/scanner-fixture\n\ngo 1.27.3\n")
        (self.root / "main.go").write_text(
            'package main\nimport ("fmt"; "strings")\nfunc main(){fmt.Println(strings.ToUpper("fixture"))}\n'
        )

        code, stdout = execute(
            [str(self.binaries / "govulncheck"), "-json", "-db", database.as_uri(), "./..."],
            self.root,
        )

        self.assertEqual(code, 0)

        raw = json_stream(stdout)

        self.assertTrue(any(item.get("finding", {}).get("osv") == identifier for item in raw))
        self.assertGreater(evaluate("govulncheck", raw, code)["blocking_count"], 0)

        (self.root / "main.go").write_text("package main\nfunc main(){}\n")

        code, stdout = execute(
            [str(self.binaries / "govulncheck"), "-json", "-db", database.as_uri(), "./..."],
            self.root,
        )

        self.assertEqual(evaluate("govulncheck", json_stream(stdout), code)["blocking_count"], 0)

    def test_unsafe_go_source_has_the_expected_rule(self) -> None:
        (self.root / "go.mod").write_text("module example.com/scanner-fixture\n\ngo 1.27.3\n")
        (self.root / "main.go").write_text(
            'package main\nimport ("os"; "os/exec")\nfunc main(){_ = exec.Command(os.Args[1]).Run()}\n'
        )

        code, stdout = execute([str(self.binaries / "gosec"), "-fmt=json", "./..."], self.root)
        raw = json.loads(stdout)

        self.assertIn("G204", {item["rule_id"] for item in raw["Issues"]})
        self.assertGreater(evaluate("gosec", raw, code)["blocking_count"], 0)

    def test_unsafe_dockerfile_and_privileged_manifest_block(self) -> None:
        dockerfile = self.root / "Dockerfile"
        dockerfile.write_text("FROM ubuntu:24.04\nUSER root\nRUN apt-get update\n")

        code, stdout = execute([str(self.binaries / "hadolint"), "--format=json", str(dockerfile)])

        self.assertGreater(evaluate("hadolint", json.loads(stdout), code)["blocking_count"], 0)

        dockerfile.unlink()
        manifest = self.root / "pod.yaml"
        manifest.write_text(
            "apiVersion: v1\nkind: Pod\nmetadata:\n  name: unsafe\nspec:\n  automountServiceAccountToken: true\n  containers:\n    - name: unsafe\n      image: ubuntu:24.04\n      securityContext:\n        privileged: true\n"
        )

        code, stdout = execute(
            [
                str(self.binaries / "trivy"),
                "config",
                "--config",
                "",
                "--ignorefile",
                "",
                "--cache-dir",
                str(self.root / "cache"),
                "--quiet",
                "--format",
                "json",
                str(self.root),
            ]
        )

        self.assertGreater(evaluate("trivy-config", json.loads(stdout), code)["blocking_count"], 0)

    def test_helm_data_files_are_rendered_and_unsafe_chart_is_not_silently_skipped(self) -> None:
        (self.root / "files").mkdir()
        (self.root / "templates").mkdir()
        (self.root / "Chart.yaml").write_text("apiVersion: v2\nname: fixture\nversion: 0.1.0\n")
        (self.root / "values.yaml").write_text("{}\n")
        (self.root / "files/defaults.json").write_text('{"name":"unsafe"}\n')
        (self.root / "templates/pod.yaml").write_text(
            '{{- $data := mustFromJson (.Files.Get "files/defaults.json") -}}\n'
            "apiVersion: v1\nkind: Pod\nmetadata:\n  name: {{ $data.name }}\nspec:\n  containers:\n"
            "    - name: unsafe\n      image: ubuntu:24.04\n      securityContext:\n        privileged: true\n"
        )

        code, stdout = execute(
            [
                str(self.binaries / "trivy"),
                "config",
                "--config",
                "",
                "--ignorefile",
                "",
                "--cache-dir",
                str(self.root / "cache"),
                "--quiet",
                "--format",
                "json",
                *HELM_SCAN_FLAGS,
                str(self.root),
            ]
        )

        self.assertTrue(
            json.loads(stdout).get("Results"), "Helm scanner silently skipped chart data files"
        )
        self.assertGreater(evaluate("trivy-config", json.loads(stdout), code)["blocking_count"], 0)


if __name__ == "__main__":
    unittest.main()
