#!/usr/bin/env python3
"""Require committed release sources while preserving the known local drafts."""
from pathlib import Path
import subprocess

RETAINED = {
    'internal/platform/fabric_precheck.go',
    'internal/platform/migrations/026_fabric_precheck.sql',
    'netbom.xlsx',
}


def violations(status: bytes, dockerignore: str, tracked: bytes = b'') -> list[str]:
    errors = []
    patterns = [line.strip() for line in dockerignore.splitlines() if line.strip() and not line.lstrip().startswith('#')]
    if tracked:
        errors.append('Retained local drafts must remain untracked.')
    retained = []
    for item in status.split(b'\0'):
        if not item:
            continue
        code, path = item[:2], item[3:].decode('utf-8', 'surrogateescape')
        if code == b'??' and path in RETAINED:
            retained.append(path)
        else:
            errors.append(f'Uncommitted release source: {item.decode("utf-8", "surrogateescape")!r}')
    for path in retained:
        if path not in patterns and '/' + path not in patterns:
            errors.append(f'Retained draft lacks an exact Docker exclusion: {path}')
    if retained and any(pattern.startswith('!') and pattern != '!.env.example' for pattern in patterns):
        errors.append('Review Docker reinclude patterns before releasing with retained local drafts.')
    return errors


def main() -> None:
    root = Path(__file__).resolve().parents[1]
    status = subprocess.check_output(['git', 'status', '--porcelain=v1', '-z', '--untracked-files=all'], cwd=root)
    tracked = subprocess.check_output(['git', 'ls-files', '-z', '--', *sorted(RETAINED)], cwd=root)
    errors = violations(status, (root / '.dockerignore').read_text(), tracked)
    if errors:
        raise SystemExit('\n'.join(errors))
    print('Release sources match committed HEAD; retained untracked drafts are excluded from the image.')


if __name__ == '__main__':
    main()
