#!/usr/bin/env python3
"""Suite guard: no two isolated checks may share a MAC segment.

The disposable PostgreSQL is preserved between runs for inspection, and a
Machine/PhysicalPort MAC is globally unique, so two check scripts committing
the same literal MAC segment collide the moment both run against one
database. This lint keeps the segments disjoint; it costs nothing and runs
before any database work.
"""
import re
import sys
from collections import defaultdict
from pathlib import Path

scripts = sorted(Path('scripts').glob('check-*.py')) + sorted(Path('scripts').glob('check-*.mjs'))
segments = defaultdict(list)
pattern = re.compile(r'aa:bb:cc:([0-9a-fA-F]{2})')
for script in scripts:
    if script.suffix == '.pyc':
        continue
    for lineno, line in enumerate(script.read_text().splitlines(), 1):
        for match in pattern.finditer(line):
            segments[match.group(1).lower()].append(f'{script}:{lineno}')
duplicates = {segment: where for segment, where in segments.items() if len({w.split(':')[0] for w in where}) > 1}
if duplicates:
    for segment, where in sorted(duplicates.items()):
        print(f'MAC segment aa:bb:cc:{segment} is shared: {", ".join(where)}', file=sys.stderr)
    sys.exit(1)
print(f'MAC segments disjoint across {len(scripts)} check scripts ({len(segments)} segments)')
