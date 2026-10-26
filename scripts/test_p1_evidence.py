import unittest
import json
from pathlib import Path
import tempfile
from unittest.mock import patch
from copy import deepcopy

from p1_evidence import go_test_evidence, repository_source, source_provenance, ztp_logical_batch_evidence


class EvidenceBoundaries(unittest.TestCase):
    def test_skip_partial_and_failure_never_verify_transport(self):
        required = {'transport': ['TestIdentity', 'TestTasks']}
        cases = [
            ('--- PASS: TestIdentity (0.1s)\n--- SKIP: TestTasks (0.0s)\n', 0),
            ('--- PASS: TestIdentity (0.1s)\n', 0),
            ('--- PASS: TestIdentity (0.1s)\n--- PASS: TestTasks (0.1s)\n', 1),
            ('    --- PASS: TestIdentity (0.1s)\n--- PASS: TestTasks (0.1s)\n', 0),
            ('--- PASS: TestIdentity (0.1s)\n--- PASS: TestTasks (0.1s)\n--- FAIL: TestOther (0.1s)\n', 0),
        ]
        for transcript, status in cases:
            with self.subTest(transcript=transcript, status=status):
                self.assertFalse(go_test_evidence(transcript, status, required)['verified_groups']['transport'])

    def test_complete_named_group_is_still_not_full_p1(self):
        record = go_test_evidence('--- PASS: TestIdentity (0.1s)\n--- PASS: TestTasks (0.1s)\n', 0, {'transport': ['TestIdentity', 'TestTasks']})
        self.assertTrue(record['verified_groups']['transport'])
        self.assertFalse(record['full_p1_acceptance'])

    def test_skipped_subtest_does_not_verify_passing_wrapper(self):
        result = go_test_evidence('    --- SKIP: TestTransport/reconnect (0.0s)\n--- PASS: TestTransport (0.1s)\n', 0, {'transport': ['TestTransport']})
        self.assertFalse(result['verified_groups']['transport'])
        self.assertEqual(result['skipped_subtests'], ['TestTransport/reconnect'])

    def test_ztp_counts_require_unique_actual_identity_records(self):
        batch = {'schema_version': 'p1-ztp-logical-batch/v1', 'source': 'sim', 'protocol': 'dhcp6-136',
                 'logical_targets': 200, 'parallelism': 1, 'elapsed_ms': 5000,
                 'protocol_label_violations': 0, 'device_observations': 0,
                 'physical_device_acceptance': False, 'managed_first_pass': 200, 'identity_matches': 200,
                 'cases': [{'switch_id': f'{i+1:032x}', 'session_id': f'{i+201:032x}', 'serial': f'SN-{i}',
                            'managed': True, 'identity_matched': True} for i in range(200)]}
        self.assertTrue(ztp_logical_batch_evidence(batch))
        for mutate in [lambda b: b.pop('cases'), lambda b: b['cases'].pop(),
                       lambda b: b['cases'].__setitem__(1, deepcopy(b['cases'][0])),
                       lambda b: b.__setitem__('identity_matches', True),
                       lambda b: b['cases'][0].__setitem__('managed', False),
                       lambda b: b.__setitem__('physical_device_acceptance', True)]:
            changed = deepcopy(batch); mutate(changed)
            self.assertFalse(ztp_logical_batch_evidence(changed))
        for case in batch['cases'][:2]:
            case['managed'] = False
            case['failure'] = 'nine_state_protocol_incomplete'
        batch['managed_first_pass'] = 198
        self.assertTrue(ztp_logical_batch_evidence(batch))
        batch['cases'][2]['managed'] = False
        batch['managed_first_pass'] = 197
        self.assertFalse(ztp_logical_batch_evidence(batch))

    def test_dirty_or_changed_source_is_not_a_clean_snapshot(self):
        original = {'commit': 'a' * 40, 'tracked_changes': False, 'unexpected_untracked': False}
        self.assertTrue(source_provenance(original, dict(original))['unchanged_clean_snapshot'])
        for key, value in [('commit', 'b' * 40), ('tracked_changes', True), ('unexpected_untracked', True)]:
            changed = {**original, key: value}
            self.assertFalse(source_provenance(original, changed)['unchanged_clean_snapshot'])
            self.assertFalse(source_provenance(changed, changed)['unchanged_clean_snapshot'])

    def test_preserved_drafts_require_the_exact_exclusion_overlay(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            ignored = root / 'ignored.go'
            ignored.write_text('//go:build ignore\n\npackage platform\n')
            overlay = root / 'overlay.json'
            replacements = {
                str(root / 'internal/platform/fabric_precheck.go'): str(ignored),
                str(root / 'internal/platform/migrations/026_fabric_precheck.sql'): '',
            }
            overlay.write_text(json.dumps({'Replace': replacements}))

            def git(command, **_):
                if command[1] == 'ls-files':
                    return b'internal/platform/fabric_precheck.go\0internal/platform/migrations/026_fabric_precheck.sql\0'
                if command[1] == 'diff':
                    return b''
                return b'a' * 40 + b'\n'

            with patch('p1_evidence.subprocess.check_output', side_effect=git), patch.dict('os.environ', {'GOFLAGS': '-overlay=' + str(overlay)}):
                safe = repository_source(root)
                self.assertTrue(safe['expected_draft_overlay'])
                self.assertTrue(source_provenance(safe, safe)['unchanged_clean_snapshot'])
                replacements[str(root / 'internal/platform/other.go')] = str(ignored)
                overlay.write_text(json.dumps({'Replace': replacements}))
                changed = repository_source(root)
                self.assertFalse(changed['expected_draft_overlay'])
                self.assertFalse(source_provenance(changed, changed)['unchanged_clean_snapshot'])


if __name__ == '__main__':
    unittest.main()
