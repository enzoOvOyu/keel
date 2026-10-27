"""Synthetic parser checks only: these fixtures never represent hardware runs."""
import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
import validate


class P2EvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='keel-p2-evidence-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.index = 0
        self.project = '1' * 32
        self.start, self.at, self.finish, self.until = ('2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', '2026-01-01T00:02:00Z', '2026-01-01T01:00:00Z')
        self.data = {'schema': 'keel.p2.hardware/v1', 'status': 'COLLECTED', 'project_id': self.project, 'platform_commit': 'a' * 40, 'environment': 'synthetic-test-only', 'approval_ref': 'synthetic', 'operator': 'test-owner', 'reviewer': 'test-reviewer', 'started_at': self.start, 'finished_at': self.finish, 'platform_health': self.put({'status': 'ok', 'version': 'a' * 40}), 'servers': [], 'switches': [{'switch_id': f'{n:032x}', 'serial': f'sw{n}', 'model': 'synthetic', 'nos': 'synthetic', 'nos_version': '1'} for n in (90, 91)], 'cases': [], 'sim_difference_register': self.put({'status': 'synthetic-only'})}
        for n in range(8):
            asset = f'{n+10:032x}'
            host = {'asset_id': asset, 'serial': f'sn{n}', 'model': 'synthetic', 'family': 'HGX_B300', 'arch': 'amd64', 'comparisons': {'mapping_identity': 'MATCH', 'dom_readback': 'UNSUPPORTED_MARKED'}}
            host['bmc_manifest'] = self.put({'project_id': self.project, 'asset_id': asset, 'origin': 'real', 'level': 'L1', 'state': 'completed', 'identity_matched': True, 'asset_current': True, 'credential_batch_ref': 'synthetic', 'credential_version': '1', 'content_hash': 'b' * 64, 'collected_at': self.at})
            host['bmc_system_raw'] = self.put({'SerialNumber': host['serial']})
            for key in ('lldp_host_raw', 'lldp_switch_raw', 'manual_mapping', 'dom_direct', 'dom_panel', 'comparison_record'):
                host[key] = self.put({'fixture': key, 'synthetic_only': True})
            for number in (2, 3):
                host[f'step{number}'] = self.put({'asset_id': asset, 'step': number, 'effective_verdict': 'PASS', 'created_at': self.at, 'valid_until': self.until, 'detail': {'host_origin': 'real', 'switch_origin': 'device', 'command_id': 'synthetic'}})
            host['mapping'] = self.put({'effective_verdict': 'PASS', 'candidate': {'project_id': self.project, 'asset_id': asset, 'ready': True, 'bindings': [{'synthetic': True}], 'coverage': {'required_ports': 1, 'mapped_ports': 1}, 'valid_until': self.until}})
            self.data['servers'].append(host)
        for name in ('credential-version-reverify', 'wrong-patch-T2', 'counter-growth-T4', 'dom-unsupported'):
            case = {'name': name, 'observed_at': self.at, 'operator': 'test-owner', 'restoration_ref': 'synthetic', **{key: self.put({'synthetic_only': True}) for key in ('before', 'after', 'operation_log')}}
            if name in ('wrong-patch-T2', 'counter-growth-T4'):
                category = 'T2' if name == 'wrong-patch-T2' else 'T4'
                case.update(issue=self.put({'issue': {'project_id': self.project, 'category': category, 'source': 'g2', 'id': 'c' * 32, 'state': 'closed', 'version': 4}}), verifications=self.put({'page': 1, 'total': 1, 'items': [{'issue_id': 'c' * 32, 'verdict': 'PASS', 'applied_issue_version': 3, 'repair_version': 2, 'command_id': 'd' * 32}]}), repair_report=self.put({'synthetic_only': True}), closed_version=4)
            self.data['cases'].append(case)

    def put(self, value):
        self.index += 1
        raw = json.dumps(value).encode()
        path = f'{self.index}.json'
        (self.root / path).write_bytes(raw)
        return {'path': path, 'sha256': hashlib.sha256(raw).hexdigest()}

    def run_check(self):
        path = self.root / 'manifest.json'
        path.write_text(json.dumps(self.data))
        return validate.check(path)

    def test_internal_consistency_never_signs_hardware(self):
        result = self.run_check()
        self.assertEqual(result['status'], 'READY_FOR_REVIEW')
        self.assertEqual(result['hardware_acceptance'], 'UNVERIFIED')

    def test_rejects_sim_source_and_partial_scope(self):
        original = copy.deepcopy(self.data)
        self.data['servers'][0]['bmc_manifest'] = self.put({'origin': 'sim'})
        with self.assertRaises(ValueError): self.run_check()
        self.data = original
        self.data['servers'].pop()
        with self.assertRaises(ValueError): self.run_check()

    def test_rejects_tampered_bytes(self):
        ref = self.data['servers'][0]['dom_direct']
        (self.root / ref['path']).write_text('changed')
        with self.assertRaises(ValueError): self.run_check()

    def test_template_is_not_execution(self):
        self.data['status'] = 'NOT_RUN'
        with self.assertRaises(ValueError): self.run_check()


if __name__ == '__main__':
    unittest.main()
