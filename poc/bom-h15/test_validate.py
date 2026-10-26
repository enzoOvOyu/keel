"""Synthetic completeness fixtures; no real BOM or H-15 acceptance evidence."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('h15_validator', Path(__file__).with_name('validate.py'))
validator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(validator)


class H15PackageTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.manifest = self.root / 'evidence.json'
        self.data = json.loads(Path(__file__).with_name('evidence.template.json').read_text())
        self.data.update(project_id='1' * 32, platform_commit='2' * 40, expected_identity_count=2, matched_identity_count=2)
        for field in ('environment', 'original_bom_source_ref', 'mapping_profile_ref', 'design_team_feedback_ref'):
            self.data[field] = 'synthetic-test-only'
        for artifact in self.data['artifacts']:
            file = self.root / artifact['path']
            file.parent.mkdir(parents=True, exist_ok=True)
            file.write_bytes(b'synthetic completeness fixture, not physical evidence')
            artifact['sha256'] = hashlib.sha256(file.read_bytes()).hexdigest()

    def check(self):
        self.manifest.write_text(json.dumps(self.data))
        return validator.check(self.manifest)

    def test_complete_files_remain_review_only(self):
        self.assertEqual(self.check(), [])

    def test_identity_mismatch_and_missing_cases_block(self):
        self.data['matched_identity_count'] = 1
        self.assertTrue(self.check())
        self.data['matched_identity_count'] = 2
        self.data['cases'].pop()
        self.assertTrue(self.check())

    def test_corruption_and_outside_paths_block(self):
        artifact = self.data['artifacts'][0]
        (self.root / artifact['path']).write_bytes(b'changed')
        self.assertTrue(self.check())
        artifact['path'] = '../outside.zip'
        self.assertTrue(self.check())


if __name__ == '__main__':
    unittest.main()
