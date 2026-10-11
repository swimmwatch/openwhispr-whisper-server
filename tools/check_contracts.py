#!/usr/bin/env python3
"""Check public contracts and bootstrap CI; workflow state belongs to its MCP."""

import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

import jsonschema
import yaml


class StrictLoader(yaml.SafeLoader):
    pass


# GitHub's `on` key must retain its YAML 1.2 spelling rather than YAML 1.1 bools.
StrictLoader.yaml_implicit_resolvers = {
    key: [(tag, pattern) for tag, pattern in values
          if tag != 'tag:yaml.org,2002:bool']
    for key, values in yaml.SafeLoader.yaml_implicit_resolvers.items()
}
StrictLoader.add_implicit_resolver('tag:yaml.org,2002:bool',
                                 re.compile(r'^(true|false)$', re.IGNORECASE),
                                 list('tTfF'))


def mapping(loader, node):
    result = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node)
        if not isinstance(key, (str, int, float, bool, type(None))) or key in result:
            raise ValueError('Duplicate or unsupported YAML mapping key')
        result[key] = loader.construct_object(value_node)
    return result


StrictLoader.add_constructor('tag:yaml.org,2002:map', mapping)


def load_yaml(text):
    try:
        for event in yaml.parse(text):
            if isinstance(event, yaml.AliasEvent) or getattr(event, 'anchor', None):
                raise ValueError('YAML aliases and anchors are not supported')
            if getattr(event, 'tag', None):
                raise ValueError('Explicit YAML tags are not supported')
        value = yaml.load(text, Loader=StrictLoader)
    except yaml.YAMLError as error:
        raise ValueError('Invalid YAML syntax') from error
    if not isinstance(value, dict):
        raise ValueError('Expected one YAML mapping')
    return value


def walk(value):
    if isinstance(value, dict):
        yield value
        for child in value.values():
            yield from walk(child)
    elif isinstance(value, list):
        for child in value:
            yield from walk(child)


def check_requirement_refs(document, requirements):
    for value in walk(document):
        for key in ('requirement_ids', 'x-requirement-ids'):
            if key not in value:
                continue
            refs = value[key]
            if (not isinstance(refs, list) or not refs or
                    any(not isinstance(x, str) or x not in requirements for x in refs) or
                    len(set(refs)) != len(refs)):
                raise ValueError('Invalid or unknown requirement reference')


def check_local_refs(document):
    for value in walk(document):
        if '$ref' not in value:
            continue
        ref = value['$ref']
        if not isinstance(ref, str) or not ref.startswith('#/'):
            raise ValueError('Only local OpenAPI references are supported')
        target = document
        try:
            for key in ref[2:].split('/'):
                key = key.replace('~1', '/').replace('~0', '~')
                target = target[int(key)] if isinstance(target, list) else target[key]
        except (KeyError, IndexError, TypeError, ValueError) as error:
            raise ValueError('Broken local OpenAPI reference') from error


def check_openapi(document, schema):
    validator = jsonschema.validators.validator_for(schema)
    validator.check_schema(schema)
    for error in validator(schema).iter_errors(document):
        # Validation messages may contain user-controlled values; print only paths/rules.
        path = '/'.join(str(x) for x in error.absolute_path)
        raise ValueError(f'Invalid OpenAPI at /{path} (rule {error.validator})')
    check_local_refs(document)


def check_workflow(document, pins):
    events = document.get('on')
    if not isinstance(events, dict) or set(events) != {'pull_request', 'push'}:
        raise ValueError('CI must use only pull_request and push')
    if events['pull_request'] not in ({}, None) or events['push'] != {'branches': ['main']}:
        raise ValueError('CI must check every PR and every main push without path filters')
    permissions = document.get('permissions')
    if permissions not in ({}, {'contents': 'read'}):
        raise ValueError('Unreviewed top-level token permissions')
    if re.search(r'\bsecrets\s*[.\[]', json.dumps(document).lower()):
        raise ValueError('Bootstrap CI cannot access secrets')
    jobs = document.get('jobs')
    if not isinstance(jobs, dict) or not jobs:
        raise ValueError('Missing CI jobs')
    for job in jobs.values():
        if not isinstance(job, dict):
            raise ValueError('Invalid CI job')
        effective = job.get('permissions', permissions)
        if effective != {'contents': 'read'}:
            raise ValueError('Job permissions must be contents read only')
        if job.get('runs-on') != 'ubuntu-24.04' or any(
                key in job for key in ('uses', 'container', 'services', 'if', 'continue-on-error')):
            raise ValueError('Unsupported CI runner, reusable job or failure bypass')
        timeout = job.get('timeout-minutes')
        if not isinstance(timeout, int) or isinstance(timeout, bool) or not 1 <= timeout <= 30:
            raise ValueError('CI job needs a bounded timeout')
        steps = job.get('steps')
        if not isinstance(steps, list) or not steps:
            raise ValueError('Missing CI steps')
        for step in steps:
            if not isinstance(step, dict) or any(
                    key in step for key in ('if', 'continue-on-error')):
                raise ValueError('CI step cannot bypass failures')
            if 'uses' in step:
                action, separator, revision = step['uses'].partition('@')
                if not separator or not re.fullmatch(r'[a-f0-9]{40}', revision) or pins.get(action) != revision:
                    raise ValueError('Unpinned or unreviewed CI action')
                if action == 'actions/checkout' and step.get('with', {}).get('persist-credentials') is not False:
                    raise ValueError('Checkout must not persist credentials')
            elif not isinstance(step.get('run'), str) or not step['run'].strip():
                raise ValueError('Missing executable CI step')


def check_document_links(path, root):
    text = path.read_text()
    if not text.endswith('\n') or text.count('```') % 2:
        raise ValueError('Invalid Markdown newline or fenced code block')
    for link in re.findall(r'\]\(([^)]+)\)', text):
        if link.startswith(('https:', 'http:', 'mailto:', '#', 'codex:')):
            continue
        target = (path.parent / link.split('#', 1)[0]).resolve()
        if not target.is_relative_to(root) or not target.is_file():
            raise ValueError('Broken or non-portable local documentation link')


def check(root):
    base = root / 'docs/specs/gpu-whisper-server'
    spec = (base / 'spec.md').read_text()
    requirements = set(re.findall(r'^### (REQ-\d+):', spec, re.MULTILINE))
    if not requirements or 'Status: Approved' not in spec:
        raise ValueError('Missing Approved public specification')
    documents = {}
    for relative in ('core/contracts.yaml', 'quality/scenarios.yaml', 'contracts/openapi.yaml'):
        documents[relative] = load_yaml((base / relative).read_text())
        check_requirement_refs(documents[relative], requirements)
    core = documents['core/contracts.yaml']
    quality = documents['quality/scenarios.yaml']
    if core.get('schema_version') != 1 or core.get('kind') != 'resident-service-contract':
        raise ValueError('Unsupported core contract identity')
    if quality.get('schema_version') != 1 or quality.get('kind') != 'resident-quality-scenarios':
        raise ValueError('Unsupported quality contract identity')
    if (core.get('authority') != '../spec.md' or quality.get('authority') != '../spec.md' or
            quality.get('configuration_authority') != '../core/contracts.yaml' or
            quality.get('http_authority') != '../contracts/openapi.yaml'):
        raise ValueError('Invalid contract authority links')
    scenarios = quality.get('scenarios')
    if not isinstance(scenarios, list) or not scenarios:
        raise ValueError('Missing quality scenarios')
    packets = set()
    for path in (base / 'tasks').glob('*.md'):
        match = re.match(r'# ([A-Z]+\d+) ', path.read_text())
        if match:
            packets.add(match[1])
    seen = set()
    for scenario in scenarios:
        required_fields = {'id', 'requirement_ids', 'packets', 'environment',
                           'workload', 'observation', 'threshold', 'planned_check'}
        if not isinstance(scenario, dict) or not required_fields <= set(scenario):
            raise ValueError('Incomplete quality scenario')
        identifier = scenario.get('id')
        if not isinstance(identifier, str) or identifier in seen:
            raise ValueError('Duplicate or invalid scenario identity')
        seen.add(identifier)
        for key, known in (('packets', packets), ('environment', set(quality['environments']))):
            refs = scenario.get(key)
            if not isinstance(refs, list) or not refs or any(x not in known for x in refs):
                raise ValueError('Unknown scenario packet/environment reference')
    versions = load_yaml((root / 'tools/versions.yaml').read_text())
    schema_path = root / 'tools/schemas/openapi-3.0.json'
    if hashlib.sha256(schema_path.read_bytes()).hexdigest() != versions['ci']['openapi_schema']['sha256']:
        raise ValueError('OpenAPI schema checksum mismatch')
    check_openapi(documents['contracts/openapi.yaml'], json.loads(schema_path.read_text()))
    workflow = load_yaml((root / '.github/workflows/ci.yml').read_text())
    pins = {name: value['sha'] for name, value in versions['ci']['actions'].items()}
    check_workflow(workflow, pins)
    commands = {line.strip() for job in workflow['jobs'].values()
                for step in job['steps'] for line in step.get('run', '').splitlines()}
    required_commands = {
        'go mod download', 'go mod verify', 'python tools/check_contracts.py',
        'python tools/check_agents.py', 'python -m unittest discover -s tests/tooling -v',
        'actionlint .github/workflows/ci.yml', 'python tools/security/check_secrets.py',
        'python -m pip install --require-hashes --only-binary=:all: -r tools/requirements-ci.txt',
    }
    if not required_commands <= commands:
        raise ValueError('Missing required bootstrap CI command')
    secret_config = load_yaml((root / 'tools/security/trivy-secret.yaml').read_text())
    if secret_config != {'disable-allow-rules': ['markdown'], 'skip-patterns': []}:
        raise ValueError('Unreviewed secret scanner rule suppression')
    if core['runtime']['api']['toolchain_baseline'] != versions['go']['version']:
        raise ValueError('Core/tool Go versions differ')
    module = (root / 'go.mod').read_text()
    if not re.search(r'^go ' + re.escape(versions['go']['version']) + r'$', module, re.MULTILINE):
        raise ValueError('Module/tool Go versions differ')
    for path in (root / '.github/ISSUE_TEMPLATE').glob('*.yml'):
        load_yaml(path.read_text())
    load_yaml((root / '.github/dependabot.yml').read_text())
    for path in [*root.glob('*.md'), *root.glob('.agents/skills/*/SKILL.md'),
                 root / 'docs/agent-setup.md', *sorted((base / 'tasks').glob('*.md'))]:
        check_document_links(path, root)
    for relative in ('.local/private-probe.txt', '.env', 'model-cache/ggml-fixture.bin',
                     'recordings/fixture.wav', '.codex/history.jsonl'):
        result = subprocess.run(['git', 'check-ignore', '--no-index', '--quiet', relative],
                                cwd=root, capture_output=True, timeout=10)
        if result.returncode != 0:
            raise ValueError('Missing source exclusion')
    dockerignore = (root / '.dockerignore').read_text().splitlines()
    for required in ('.git', '.local', '.venv', 'model-cache', 'recordings'):
        if required not in dockerignore:
            raise ValueError('Missing Docker input exclusion')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args()
    try:
        check(args.root.resolve())
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        message = str(error) if isinstance(error, ValueError) else type(error).__name__
        print(f'FAIL: contract/bootstrap check: {message}', file=sys.stderr)
        return 1
    print('PASS: public contracts, bootstrap CI policy, settings, links and exclusions')
    print('Workflow MCP state and runtime/GPU/full-release behavior are not checked here')
    return 0


if __name__ == '__main__':
    sys.exit(main())
