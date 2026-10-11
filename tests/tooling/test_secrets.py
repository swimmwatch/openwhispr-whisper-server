import contextlib
import io
import json
import os
from pathlib import Path
import tempfile
import unittest

from tools.security import check_secrets


ROOT = Path(__file__).resolve().parents[2]


class ReportTests(unittest.TestCase):
    def test_exit_zero_with_findings_is_still_a_failure_and_redacted(self):
        marker = 'C01_' + 'NONCREDENTIAL_' + 'Z' * 20
        raw = {'SchemaVersion': 2, 'Results': [{
            'Target': 'fixture.txt', 'Class': 'secret',
            'Secrets': [{'RuleID': 'fixture-rule', 'Severity': 'LOW', 'StartLine': 1,
                         'Match': marker, 'Code': {'Lines': [{'Content': marker}]}}],
        }]}
        report, exit_code = check_secrets.evaluate(raw, 0)
        self.assertEqual(exit_code, 1)
        self.assertEqual(report['finding_count'], 1)
        self.assertNotIn(marker, json.dumps(report))

    def test_missing_malformed_and_error_results_fail_closed(self):
        for raw, status in (({}, 0), ({'SchemaVersion': 2, 'Results': 'wrong'}, 0),
                            ({'SchemaVersion': 2, 'Results': []}, 1),
                            ({'SchemaVersion': 2, 'Results': []}, 2)):
            with self.subTest(raw=raw, status=status), self.assertRaises(ValueError):
                check_secrets.evaluate(raw, status)

    def test_clean_supported_report_passes(self):
        report, status = check_secrets.evaluate({'SchemaVersion': 2, 'Results': []}, 0)
        self.assertEqual((report['finding_count'], status), (0, 0))

    def test_real_empty_filesystem_report_omits_results(self):
        raw = {'SchemaVersion': 2, 'ArtifactType': 'filesystem', 'ArtifactName': '.',
               'CreatedAt': '2026-10-11T00:00:00Z', 'Trivy': {'Version': '0.75.0'}}
        report, status = check_secrets.evaluate(raw, 0)
        self.assertEqual((report['finding_count'], status), (0, 0))


class RealScannerTests(unittest.TestCase):
    def test_real_scanner_detects_marker_and_excludes_private_workflow_state(self):
        binary = os.environ.get('C01_TRIVY_BINARY', 'trivy')
        marker = 'C01_' + 'NONCREDENTIAL_' + 'Z' * 20
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = root.parent / (root.name + '-rules.yaml')
            config.write_text(
                'disable-allow-rules: [markdown]\nskip-patterns: []\nrules:\n'
                '  - id: fixture-rule\n    category: fixture\n'
                '    title: Non-credential fixture\n    severity: LOW\n'
                '    regex: "C01_NONCREDENTIAL_[A-Z]{20}"\n')
            try:
                source = root / 'public.md'
                source.write_text(marker)
                output = io.StringIO()
                with contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
                    report, status = check_secrets.scan(root, binary, config, ROOT)
                self.assertEqual((report['finding_count'], status), (1, 1))
                self.assertNotIn(marker, output.getvalue() + json.dumps(report))
                source.unlink()
                for relative in ('.local/private.md', '.git/private.md', '.venv/private.md',
                                 'docs/specs/fixture/manifest.yaml',
                                 'docs/specs/fixture/decisions.yaml',
                                 'docs/specs/fixture/.workflow/review-inputs/private.json'):
                    path = root / relative
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_text(marker)
                report, status = check_secrets.scan(root, binary, config, ROOT)
                self.assertEqual((report['finding_count'], status), (0, 0))
            finally:
                config.unlink()


if __name__ == '__main__':
    unittest.main()
