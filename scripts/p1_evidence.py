"""Evidence metadata only; importing this module never starts verification."""
import json
import os
from pathlib import Path
import shlex
import re
import subprocess

PRESERVED_DRAFTS = frozenset({
    'internal/platform/fabric_precheck.go',
    'internal/platform/migrations/026_fabric_precheck.sql',
})


def repository_source(root):
    def git(*args):
        return subprocess.check_output(['git', *args], cwd=root)
    untracked = git('ls-files', '--others', '--exclude-standard', '-z').decode().split('\0')
    flags = shlex.split(os.environ.get('GOFLAGS', ''))
    overlay_paths = [flag.split('=', 1)[1] for flag in flags if flag.startswith('-overlay=')]
    expected_overlay = False
    if len(overlay_paths) == 1:
        try:
            replacements = json.loads(Path(overlay_paths[0]).read_text())['Replace']
            go_draft = str(root / 'internal/platform/fabric_precheck.go')
            sql_draft = str(root / 'internal/platform/migrations/026_fabric_precheck.sql')
            expected_overlay = (set(replacements) == {go_draft, sql_draft}
                                and replacements[sql_draft] == ''
                                and Path(replacements[go_draft]).read_text() == '//go:build ignore\n\npackage platform\n')
        except (OSError, KeyError, TypeError, ValueError):
            pass
    return {
        'commit': git('rev-parse', 'HEAD').decode().strip(),
        'tracked_changes': bool(git('diff', 'HEAD', '--name-only', '-z')),
        'unexpected_untracked': any(path and path not in PRESERVED_DRAFTS for path in untracked),
        'preserved_drafts_present': bool(PRESERVED_DRAFTS.intersection(untracked)),
        'expected_draft_overlay': expected_overlay,
    }


def source_provenance(before, after):
    clean = not before['tracked_changes'] and not before['unexpected_untracked']
    clean = clean and (not before.get('preserved_drafts_present') or before.get('expected_draft_overlay', False))
    return {
        'before': before,
        'after': after,
        'unchanged_clean_snapshot': clean and before == after,
    }


def go_test_evidence(transcript, exit_code, required_groups):
    # Top-level Go -v records, not indented subtests or arbitrary t.Log output.
    outcomes = {'PASS': set(), 'SKIP': set(), 'FAIL': set()}
    for result, name in re.findall(r'^--- (PASS|SKIP|FAIL): (\S+)', transcript, re.MULTILINE):
        outcomes[result].add(name)
    successful = exit_code == 0 and not outcomes['FAIL']
    return {
        'passed_tests': sorted(outcomes['PASS']),
        'skipped_tests': sorted(outcomes['SKIP']),
        'failed_tests': sorted(outcomes['FAIL']),
        'verified_groups': {
            group: successful and bool(names) and all(
                name in outcomes['PASS'] and name not in outcomes['SKIP']
                for name in names
            )
            for group, names in required_groups.items()
        },
        # These are component batches; no single batch closes the whole phase.
        'full_p1_acceptance': False,
    }
