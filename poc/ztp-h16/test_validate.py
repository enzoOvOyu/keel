"""Synthetic file completeness only; never a DHCP or hardware execution."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('h16_validator', Path(__file__).with_name('validate.py'))
validator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(validator)


class H16PackageTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.manifest = self.root / 'evidence.json'
        self.data = json.loads(Path(__file__).with_name('evidence.template.json').read_text())
        self.data['kea_active_hash_before'] = self.data['kea_active_hash_after_restart'] = 'a' * 64
        for case in self.data['cases']:
            case['artifacts'] = [{'kind': kind, 'path': 'evidence/' + case['name'] + '-' + kind, 'sha256': ''} for kind in validator.KINDS]
            if case['name'] == 'matched-60-43':
                case['option43_sha256'] = 'b' * 64
        artifacts = [artifact for case in self.data['cases'] for artifact in case['artifacts']]
        artifacts += [self.data[name] for name in ('first_boot_minimal_config_artifact', 'pushjob_approval_artifact', 'golden_readback_artifact')]
        for artifact in artifacts:
            file = self.root / artifact['path']
            file.parent.mkdir(parents=True, exist_ok=True)
            file.write_bytes(b'synthetic artifact, not real protocol evidence')
            artifact['sha256'] = hashlib.sha256(file.read_bytes()).hexdigest()

    def check(self):
        self.manifest.write_text(json.dumps(self.data))
        return validator.check(self.manifest)

    def test_two_switches_can_cover_three_channels(self):
        self.assertEqual(self.check(), [])

    def test_missing_vendor_profile_and_channel_block(self):
        vendor = next(case for case in self.data['cases'] if case['name'] == 'matched-60-43')
        vendor['vendor_profile_ref'] = ''
        self.assertTrue(self.check())
        self.data['cases'].remove(vendor)
        self.assertTrue(self.check())

    def test_one_physical_switch_and_changed_artifact_block(self):
        for case in self.data['cases']:
            if case['name'].startswith('matched-'):
                case['serial'] = 'SAME-SWITCH'
        self.assertTrue(self.check())
        (self.root / self.data['cases'][0]['artifacts'][0]['path']).write_bytes(b'changed')
        self.assertTrue(self.check())


if __name__ == '__main__':
    unittest.main()
