import copy
import json
from pathlib import Path
import unittest

from tools import check_contracts


ROOT = Path(__file__).resolve().parents[2]


class YamlTests(unittest.TestCase):
    def test_duplicate_mapping_key_is_rejected(self):
        with self.assertRaises(ValueError):
            check_contracts.load_yaml("job:\n  permissions: read\n  permissions: write\n")

    def test_alias_and_explicit_tag_are_rejected(self):
        for text in ("a: &value [1]\nb: *value\n", "a: !unsafe value\n"):
            with self.subTest(text=text), self.assertRaises(ValueError):
                check_contracts.load_yaml(text)

    def test_workflow_on_key_is_not_a_boolean(self):
        self.assertEqual(check_contracts.load_yaml("on: pull_request\n")['on'], 'pull_request')


class ReferenceTests(unittest.TestCase):
    def test_unknown_nested_requirement_is_rejected(self):
        good = {'scenarios': [{'requirement_ids': ['REQ-024']}]}
        check_contracts.check_requirement_refs(good, {'REQ-024'})
        good['scenarios'][0]['requirement_ids'] = ['REQ-999']
        with self.assertRaises(ValueError):
            check_contracts.check_requirement_refs(good, {'REQ-024'})

    def test_broken_local_openapi_pointer_is_rejected(self):
        with self.assertRaises(ValueError):
            check_contracts.check_local_refs({'paths': {}, '$ref': '#/missing'})

    def test_external_openapi_pointer_is_rejected(self):
        with self.assertRaises(ValueError):
            check_contracts.check_local_refs({'$ref': 'https://example.invalid/schema'})

    def test_invalid_openapi_operation_is_rejected(self):
        spec = {'openapi': '3.0.3', 'info': {'title': 'Fixture', 'version': '0.1.0'},
                'paths': {'/fixture': {'get': {'responses': {'200': {}}}}}}
        schema = json.loads((ROOT / 'tools/schemas/openapi-3.0.json').read_text())
        with self.assertRaises(ValueError):
            check_contracts.check_openapi(spec, schema)


class WorkflowTests(unittest.TestCase):
    def setUp(self):
        self.workflow = {
            'on': {'pull_request': {}, 'push': {'branches': ['main']}},
            'permissions': {},
            'jobs': {'bootstrap': {
                'name': 'Bootstrap checks', 'runs-on': 'ubuntu-24.04',
                'permissions': {'contents': 'read'}, 'timeout-minutes': 20,
                'steps': [{'uses': 'actions/checkout@' + 'a' * 40,
                           'with': {'persist-credentials': False}},
                          {'run': 'python3 tools/check_contracts.py'}],
            }},
        }
        self.pins = {'actions/checkout': 'a' * 40}

    def test_job_override_cannot_escalate_top_level_permissions(self):
        check_contracts.check_workflow(self.workflow, self.pins)
        self.workflow['jobs']['bootstrap']['permissions']['packages'] = 'write'
        with self.assertRaises(ValueError):
            check_contracts.check_workflow(self.workflow, self.pins)

    def test_unpinned_action_is_rejected(self):
        self.workflow['jobs']['bootstrap']['steps'][0]['uses'] = 'actions/checkout@v7'
        with self.assertRaises(ValueError):
            check_contracts.check_workflow(self.workflow, self.pins)

    def test_secret_access_and_failure_bypass_are_rejected(self):
        for override in ({'env': {'TOKEN': '${{ secrets.FIXTURE }}'}},
                         {'continue-on-error': True}, {'if': 'always()'}):
            workflow = copy.deepcopy(self.workflow)
            workflow['jobs']['bootstrap']['steps'][-1].update(override)
            with self.subTest(override=override), self.assertRaises(ValueError):
                check_contracts.check_workflow(workflow, self.pins)

    def test_privileged_trigger_runner_and_reusable_job_are_rejected(self):
        for override in ({'runs-on': ['self-hosted', 'linux']},
                         {'uses': 'owner/repo/workflow@' + 'a' * 40}):
            workflow = copy.deepcopy(self.workflow)
            workflow['jobs']['bootstrap'].update(override)
            with self.subTest(override=override), self.assertRaises(ValueError):
                check_contracts.check_workflow(workflow, self.pins)
        self.workflow['on']['pull_request_target'] = {}
        with self.assertRaises(ValueError):
            check_contracts.check_workflow(self.workflow, self.pins)


if __name__ == '__main__':
    unittest.main()
