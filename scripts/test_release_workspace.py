import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('release_workspace', Path(__file__).with_name('check-release-workspace.py'))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseWorkspaceTests(unittest.TestCase):
    def test_only_explicit_untracked_drafts_with_build_exclusions_are_accepted(self):
        ignored = '\n'.join(sorted(release.RETAINED)) + '\n!.env.example\n'
        status = b''.join(b'?? ' + path.encode() + b'\0' for path in sorted(release.RETAINED))
        self.assertEqual(release.violations(status, ignored), [])
        self.assertTrue(release.violations(status, ''))
        self.assertTrue(release.violations(status, ignored + '!internal/**/*.go\n'))
        self.assertTrue(release.violations(status, ignored, b'internal/platform/fabric_precheck.go\0'))

    def test_tracked_changes_new_sources_and_renames_are_never_exempted(self):
        ignored = '\n'.join(sorted(release.RETAINED))
        for status in [b' M internal/platform/fabric_precheck.go\0', b'?? unexpected.go\0', b'M  api/openapi.yaml\0', b'R  netbom.xlsx\0old.xlsx\0']:
            with self.subTest(status=status):
                self.assertTrue(release.violations(status, ignored))
