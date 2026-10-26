#!/usr/bin/env python3
"""Offline H-15 evidence completeness; never contact hardware or grant PASS."""
import importlib.util
import json
import re
import sys
from pathlib import Path

# Reuse the existing PoC artifact boundary and SHA checker.
spec = importlib.util.spec_from_file_location('keel_ztp_evidence', Path(__file__).resolve().parents[1] / 'ztp-h16/validate.py')
evidence = importlib.util.module_from_spec(spec)
spec.loader.exec_module(evidence)
KINDS = {'source-bom', 'mapping-profile', 'preview', 'commit', 'repeat-commit', 'identity-comparison', 'issues-csv', 'project-denial', 'model-proof'}
CASES = {'directory-package', 'wide-table-aliases', 'issue-localization', 'repeated-commit', 'stable-identity', 'cross-project-denial', 'model-constraints'}


def check(manifest: Path) -> list[str]:
    if manifest.stat().st_size > 1024 * 1024:
        return ['manifest exceeds 1MiB']
    data = json.loads(manifest.read_text())
    if not isinstance(data, dict):
        return ['manifest must be an object']
    errors = []
    if data.get('item') != 'H-15' or data.get('source') != 'physical-bom':
        errors.append('H-15 real BOM source declaration required')
    for field in ('environment', 'original_bom_source_ref', 'mapping_profile_ref', 'design_team_feedback_ref'):
        if not isinstance(data.get(field), str) or not data[field].strip():
            errors.append(field + ' required')
    for field, length in (('project_id', 32), ('platform_commit', 40)):
        if not isinstance(data.get(field), str) or not re.fullmatch('[a-f0-9]{' + str(length) + '}', data[field]):
            errors.append(field + ' must be an exact identity')
    expected, matched = data.get('expected_identity_count'), data.get('matched_identity_count')
    if type(expected) is not int or expected <= 0 or type(matched) is not int or expected != matched:
        errors.append('positive, equal expected/matched identity counts required')
    cases = data.get('cases')
    if not isinstance(cases, list) or len(cases) != len(CASES) or any(not isinstance(case, str) for case in cases) or set(cases) != CASES:
        errors.append('all unique H-15 cases required')
    artifacts = data.get('artifacts')
    if not isinstance(artifacts, list) or len(artifacts) != len(KINDS) or any(not isinstance(item, dict) or not isinstance(item.get('kind'), str) or item['kind'] not in KINDS for item in artifacts) or {item.get('kind') for item in artifacts} != KINDS:
        errors.append('one artifact for each H-15 evidence kind required')
    else:
        for item in artifacts:
            evidence.verify_artifact(manifest.parent.resolve(), item, errors)
    return errors


if __name__ == '__main__':
    if len(sys.argv) != 2:
        raise SystemExit('usage: validate.py evidence.json')
    problems = check(Path(sys.argv[1]))
    for problem in problems:
        print('BLOCKED', problem)
    if problems:
        raise SystemExit(1)
    print('READY_FOR_REVIEW: files match manifest; H-15 PASS requires human review')
