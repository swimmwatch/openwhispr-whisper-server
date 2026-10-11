#!/usr/bin/env python3
"""Run the pinned source secret scanner without publishing matches or snippets."""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile


SKIP_DIRS = ('.git', '.local', '.venv', '**/.workflow', '**/__pycache__')
SKIP_FILES = ('docs/specs/**/manifest.yaml', 'docs/specs/**/decisions.yaml')


def evaluate(raw, scanner_exit):
    if not isinstance(raw, dict) or raw.get('SchemaVersion') != 2 or scanner_exit not in (0, 1):
        raise ValueError('Missing, unsupported or failed scanner report')
    results = raw.get('Results')
    if 'Results' not in raw:
        # Trivy omits Results for an empty clean filesystem (verified with 0.75.0).
        if (raw.get('ArtifactType') != 'filesystem' or
                not isinstance(raw.get('ArtifactName'), str) or not raw['ArtifactName'] or
                not isinstance(raw.get('CreatedAt'), str) or
                not isinstance(raw.get('Trivy'), dict)):
            raise ValueError('Missing scanner result identity')
        results = []
    if not isinstance(results, list):
        raise ValueError('Malformed scanner results')
    count = 0
    for result in results:
        if not isinstance(result, dict):
            raise ValueError('Malformed scanner result')
        findings = result.get('Secrets', [])
        if not isinstance(findings, list):
            raise ValueError('Malformed secret findings')
        for finding in findings:
            if not isinstance(finding, dict) or not isinstance(finding.get('RuleID'), str):
                raise ValueError('Malformed secret finding')
            count += 1
    if scanner_exit == 1 and not count:
        raise ValueError('Scanner failed without a supported finding')
    # Retain counts only: titles, paths, matches and code can all contain secret data.
    return {'finding_count': count, 'scanner_exit': scanner_exit}, int(count > 0)


def scan(root, binary, secret_config, project_root):
    binary = shutil.which(str(binary))
    if not binary:
        raise ValueError('Pinned Trivy executable is required')
    versions = json.loads((project_root / 'tools/versions.yaml').read_text())
    expected = versions['scanners']['trivy']['version'].lstrip('v')
    env = {key: value for key, value in os.environ.items() if not key.startswith('TRIVY_')}
    version = subprocess.run([str(binary), '--version'], capture_output=True, text=True,
                             env=env, timeout=15, check=True)
    if not version.stdout.startswith(f'Version: {expected}\n'):
        raise ValueError('Unexpected Trivy version')
    with tempfile.TemporaryDirectory(prefix='whisper-secret-check-') as temporary:
        private = Path(temporary)
        report_path = private / 'raw.json'
        config_path = private / 'config.yaml'
        ignore_path = private / 'ignore'
        config_path.write_text('{}\n')
        ignore_path.write_text('')
        command = [str(binary), 'fs', '--config', str(config_path), '--scanners', 'secret',
                   '--secret-config', str(secret_config.resolve()), '--ignorefile', str(ignore_path),
                   '--format', 'json', '--output', str(report_path), '--exit-code', '1',
                   '--quiet', '--cache-dir', str(private / 'cache')]
        for path in SKIP_DIRS:
            command.extend(['--skip-dirs', path])
        for path in SKIP_FILES:
            command.extend(['--skip-files', path])
        command.append('.')
        process = subprocess.run(command, cwd=root, env=env, capture_output=True, timeout=180)
        try:
            raw = json.loads(report_path.read_bytes())
        except (OSError, ValueError) as error:
            raise ValueError('Scanner did not produce a valid report') from error
        report, status = evaluate(raw, process.returncode)
    source_sha = os.environ.get('GITHUB_SHA', '')
    if not re.fullmatch(r'[0-9a-f]{40}', source_sha):
        source_sha = 'local-uncommitted'
    report.update({
        'scanner': 'Trivy', 'scanner_version': expected,
        'secret_rule_identity': {'binary_sha256': hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
                                 'config_sha256': hashlib.sha256(secret_config.read_bytes()).hexdigest()},
        'scan_time': datetime.now(timezone.utc).isoformat(),
        'source_sha': source_sha,
        'coverage': 'Current source secrets only; no Go vulnerabilities, images, Helm or GPU acceptance',
    })
    return report, status


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument('--trivy', default=shutil.which('trivy'))
    args = parser.parse_args()
    if not args.trivy:
        print('FAIL: pinned Trivy executable is required', file=sys.stderr)
        return 2
    project_root = Path(__file__).resolve().parents[2]
    try:
        report, status = scan(args.root.resolve(), args.trivy,
                              project_root / 'tools/security/trivy-secret.yaml', project_root)
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f'FAIL: source secret scanner ({type(error).__name__}); raw output suppressed', file=sys.stderr)
        return 2
    print(json.dumps(report, sort_keys=True))
    return status


if __name__ == '__main__':
    sys.exit(main())
