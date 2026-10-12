#!/usr/bin/env python3
"""Check the approved design baseline and requirement references without dependencies."""
import json
import re
from pathlib import Path

root = Path(__file__).resolve().parents[1]
core = ['PRD.md', 'ARCHITECTURE.md', 'DESIGN-SYSTEM.md', 'IMPLEMENTATION-PLAN.md', 'ACCEPTANCE.md']
texts = {name: (root / name).read_text() for name in core}
errors = []
for name, value in texts.items():
    if 'v2.5' not in value[:2200]: errors.append(f'{name}: missing v2.5 header')
    if 'keel' not in value[:2200]: errors.append(f'{name}: missing keel branding')
    if sum(line.lstrip().startswith('```') for line in value.splitlines()) % 2:
        errors.append(f'{name}: unbalanced fenced blocks')
    if re.search(r'bdp-|BDP_', value): errors.append(f'{name}: old runtime naming remains')

fr = set(map(int, re.findall(r'^### 3\.\d+ FR-(\d+)\b', texts['PRD.md'], re.M)))
if fr != set(range(1,18)): errors.append(f'PRD functional domains: {sorted(fr)}')
for word in ['GateEvidence','GroupRun','PhysicalPort','FR-10.1']:
    if word not in texts['PRD.md']: errors.append(f'PRD: missing {word}')
for word in ['GateEvidence','GroupRun','PhysicalPort','outbox']:
    if word not in texts['ARCHITECTURE.md']: errors.append(f'ARCH: missing {word}')
ui=texts['DESIGN-SYSTEM.md']
for word in ['/login','5.17','CSRF','HttpOnly','allowed_transitions','selection_token']:
    if word not in ui: errors.append(f'DESIGN: missing {word}')
for old in ['有选中集时作用于选中集','782→371 GB/s','达标 ≥95%']:
    if old in '\n'.join(texts.values()): errors.append(f'obsolete rule: {old}')
part=ui.split('## 8. 组件清单',1)[1].split('## 9.',1)[0]
component_rows=[line for line in part.splitlines() if line.startswith('|') and len(line.split('|'))>=5 and not re.match(r'^\|\s*[-:]|^\|\s*组件',line)]
if len(component_rows)!=34: errors.append(f'component count: {len(component_rows)}, expected 34')

# A Markdown table can render without warning even when a role delimiter is missing.
permissions=ui.split('## 6. 交互与权限矩阵',1)[1].split('## 7.',1)[0]
for row in permissions.splitlines():
    if row.startswith('|') and len(row.split('|')) != 9:
        errors.append('DESIGN: permissions table must have page plus six role columns')
if 'returnTo' in texts['PRD.md']: errors.append('PRD: login return parameter must be next')
for old in ['partition-coverage','full-communicator','lldp (G2 进行)']:
    if old in '\n'.join(texts.values()): errors.append(f'inconsistent contract: {old}')

trace_path=root/'docs/requirements-traceability.json' 
if not trace_path.exists():
    errors.append('missing docs/requirements-traceability.json')
else:
    trace=json.loads(trace_path.read_text())
    raw=json.dumps(trace,ensure_ascii=False)
    work=trace.get('work_package_index',[])
    by_id={entry['id']:entry for entry in work}
    if len(work)!=56 or len(by_id)!=len(work): errors.append('expected 56 unique work packages')
    active, finished = set(), set()
    def visit(node):
        if node in active:
            errors.append(f'work package dependency cycle at {node}')
            return
        if node in finished: return
        if node not in by_id:
            errors.append(f'undefined dependency {node}')
            return
        active.add(node)
        for parent in by_id[node].get('depends_on',[]): visit(parent)
        active.remove(node)
        finished.add(node)
    for node in by_id: visit(node)
    for entry in trace.get('requirements', []) + trace.get('review_items', []):
        for ref in entry.get('design_refs', []):
            file=root/ref['file']
            if not file.is_file() or ref['marker'] not in file.read_text(): errors.append(f"trace marker missing: {entry['id']} {ref}")
    for n in range(1,18):
        if not re.search(r'FR-0?'+str(n)+r'(?!\d)', raw): errors.append(f'trace missing FR-{n}')
        if not re.search(r'R-?0?'+str(n)+r'(?!\d)',raw): errors.append(f'trace missing R{n:02}')
    for wp in set(re.findall(r'WP-\d+\.\d+[a-z]?',raw)):
        if not re.search(re.escape(wp)+r'(?![\da-z])',texts['IMPLEMENTATION-PLAN.md']): errors.append(f'trace undefined {wp}')
    for acc in set(re.findall(r'\b(?:F-\d+(?:\.\d+)+|[DUPSHK]-\d+)\b',raw)):
        if not re.search(re.escape(acc)+r'(?![\d.])',texts['ACCEPTANCE.md']): errors.append(f'trace undefined acceptance {acc}')
    for doc in set(re.findall(r'[A-Z][A-Z-]+\.md',raw)):
        if not (root/doc).is_file(): errors.append(f'trace missing document {doc}')

if errors:
    raise SystemExit('\n'.join('FAIL '+e for e in errors))
print('Design v2.5 OK: 17 FR domains, 34 UI components, review/requirement references and core contracts.')
print('Document checks do not prove implementation or hardware acceptance.')
