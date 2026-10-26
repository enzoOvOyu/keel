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
    # A root test can PASS while a required environment-dependent t.Run skips.
    # Preserve that distinction instead of promoting the wrapper to acceptance.
    skipped_subtests = sorted(set(re.findall(r'^\s+--- SKIP: (\S+/\S+)', transcript, re.MULTILINE)))
    incomplete_roots = {name.split('/', 1)[0] for name in skipped_subtests}
    successful = exit_code == 0 and not outcomes['FAIL']
    return {
        'passed_tests': sorted(outcomes['PASS']),
        'skipped_tests': sorted(outcomes['SKIP']),
        'failed_tests': sorted(outcomes['FAIL']),
        'skipped_subtests': skipped_subtests,
        'verified_groups': {
            group: successful and bool(names) and all(
                name in outcomes['PASS'] and name not in outcomes['SKIP'] and name not in incomplete_roots
                for name in names
            )
            for group, names in required_groups.items()
        },
        # These are component batches; no single batch closes the whole phase.
        'full_p1_acceptance': False,
    }


def ztp_logical_batch_evidence(batch, targets=200):
    """Check actual per-device records, not only self-reported summary counts."""
    if not isinstance(batch, dict) or type(targets) is not int or targets < 1:
        return False
    fixed = {'schema_version': 'p1-ztp-logical-batch/v1', 'source': 'sim',
             'protocol': 'dhcp6-136', 'logical_targets': targets,
             'protocol_label_violations': 0, 'device_observations': 0,
             'physical_device_acceptance': False}
    if any(type(batch.get(key)) is not type(value) or batch.get(key) != value for key, value in fixed.items()):
        return False
    if type(batch.get('parallelism')) is not int or not 1 <= batch['parallelism'] <= targets:
        return False
    if type(batch.get('elapsed_ms')) is not int or batch['elapsed_ms'] < 0:
        return False
    cases = batch.get('cases')
    if not isinstance(cases, list) or len(cases) != targets:
        return False
    switches, sessions, serials = set(), set(), set()
    managed = identities = 0
    for case in cases:
        if not isinstance(case, dict):
            return False
        switch, session, serial = case.get('switch_id'), case.get('session_id'), case.get('serial')
        if not isinstance(switch, str) or not re.fullmatch('[a-f0-9]{32}', switch):
            return False
        if not isinstance(session, str) or not re.fullmatch('[a-f0-9]{32}', session):
            return False
        if not isinstance(serial, str) or not serial or len(serial) > 128:
            return False
        if switch in switches or session in sessions or serial in serials:
            return False
        switches.add(switch); sessions.add(session); serials.add(serial)
        if type(case.get('managed')) is not bool or type(case.get('identity_matched')) is not bool:
            return False
        if case['managed'] and (not case['identity_matched'] or 'failure' in case):
            return False
        managed += int(case['managed'])
        identities += int(case['identity_matched'])
    return (type(batch.get('managed_first_pass')) is int and batch['managed_first_pass'] == managed
            and type(batch.get('identity_matches')) is int and batch['identity_matches'] == identities
            and managed * 100 >= targets * 99 and identities == targets)
