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
    if 'v3.0.3' not in value[:2200]: errors.append(f'{name}: missing v3.0.3 header')
    if 'keel' not in value[:2200]: errors.append(f'{name}: missing keel branding')
    if sum(line.lstrip().startswith('```') for line in value.splitlines()) % 2:
        errors.append(f'{name}: unbalanced fenced blocks')
    if re.search(r'bdp-|BDP_', value): errors.append(f'{name}: old runtime naming remains')

fr = set(map(int, re.findall(r'^### 3\.\d+ FR-(\d+)\b', texts['PRD.md'], re.M)))
if fr != set(range(0,18)): errors.append(f'PRD functional domains: {sorted(fr)}')
for word in ['GateEvidence','GroupRun','PhysicalPort','FR-10.1','BOMImportPackage','NP-1','network_readiness','fabric_stage','keel-fabric','LL-01','LL-07','rule_id@version']:
    if word not in texts['PRD.md']: errors.append(f'PRD: missing {word}')
for word in ['GateEvidence','GroupRun','PhysicalPort','outbox','I-11','I-12','FabricReadiness','BOMImportPackage','fabric-svc','fabricmon-svc','netbaseline-svc','boot-svc','ADR-19','ADR-20']:
    if word not in texts['ARCHITECTURE.md']: errors.append(f'ARCH: missing {word}')
plan = texts['IMPLEMENTATION-PLAN.md']
for word in ['WP-1.15','WP-1.16','WP-1.17','WP-2.14','WP-2.15','WP-3.14','WP-3.15','WP-4.10','FR-0','keel-fabric']:
    if word not in plan: errors.append(f'PLAN: missing {word}')
acc = texts['ACCEPTANCE.md']
for word in ['F-0.1','F-0.12','U-16','H-15','H-18','NP-1']:
    if word not in acc: errors.append(f'ACC: missing {word}')
ui=texts['DESIGN-SYSTEM.md']
for word in ['/login','5.17','5.20','5.21','CSRF','HttpOnly','allowed_transitions','selection_token','NetworkDeviceDrawer','ZtpOnboardBoard','PushJobWizard','LosslessMatrix','RuleGovernance','BomImportWizard','fabric:import','fabric:push']:
    if word not in ui: errors.append(f'DESIGN: missing {word}')
for old in ['有选中集时作用于选中集','782→371 GB/s','达标 ≥95%']:
    if old in '\n'.join(texts.values()): errors.append(f'obsolete rule: {old}')
part=ui.split('## 8. 组件清单',1)[1].split('## 9.',1)[0]
component_rows=[line for line in part.splitlines() if line.startswith('|') and len(line.split('|'))>=5 and not re.match(r'^\|\s*[-:]|^\|\s*组件',line)]
if len(component_rows)!=40: errors.append(f'component count: {len(component_rows)}, expected 40')

# A Markdown table can render without warning even when a role delimiter is missing.
permissions=ui.split('## 6. 交互与权限矩阵',1)[1].split('## 7.',1)[0]
main_table=permissions.split('**网络底座域权限专表',1)[0]
for row in main_table.splitlines():
    if row.startswith('|') and len(row.split('|')) != 9:
        errors.append('DESIGN: permissions table must have page plus six role columns')
if '**网络底座域权限专表' in permissions:
    net_table=permissions.split('**网络底座域权限专表',1)[1]
    for row in net_table.splitlines():
        if row.startswith('|') and len(row.split('|')) not in (11,):
            errors.append('DESIGN: network permissions table must have page plus eight role columns')
else:
    errors.append('DESIGN: missing network permission sub-table')
if 'returnTo' in texts['PRD.md']: errors.append('PRD: login return parameter must be next')
for old in ['partition-coverage','full-communicator','lldp (G2 进行)']:
    if old in '\n'.join(texts.values()): errors.append(f'inconsistent contract: {old}')

trace_path=root/'docs/requirements-traceability.json' 
if not trace_path.exists():
    errors.append('missing docs/requirements-traceability.json')
else:
    trace=json.loads(trace_path.read_text())
    raw=json.dumps(trace,ensure_ascii=False)
    if trace.get('design_version') != 'v3.0.3': errors.append('trace baseline must be v3.0.3')
    guide=trace.get('execution_guide')
    if not isinstance(guide,str) or not (root/guide).is_file(): errors.append('trace execution guide missing')
    work=trace.get('work_package_index',[])
    by_id={entry['id']:entry for entry in work}
    if len(work)!=64 or len(by_id)!=len(work): errors.append(f'expected 64 unique work packages, found {len(work)}')
    # The plan and machine index must describe the same work, dependency edges,
    # and acceptance IDs. Merely mentioning an ID elsewhere is insufficient.
    plan_rows={}
    for line in plan.splitlines():
        if not re.match(r'^\| WP-\d+\.\d+[a-z]? \|',line): continue
        cells=re.split(r'(?<!\\)\|',line)
        if len(cells)!=7:
            errors.append('PLAN: malformed work-package row: '+line[:35])
            continue
        key=cells[1].strip()
        if key in plan_rows: errors.append('PLAN: duplicate '+key)
        plan_rows[key]={
            'depends_on':set(re.findall(r'WP-\d+\.\d+[a-z]?',cells[4])),
            'acceptance':set(re.findall(r'\b(?:F-\d+(?:\.\d+)+|[DUEPSHK]-\d+)\b',cells[5])),
        }
    if set(plan_rows)!=set(by_id): errors.append('PLAN/trace work-package IDs differ')
    acceptance_ids=set(re.findall(r'^\| ((?:F-\d+(?:\.\d+)+|[DUEPSHK]-\d+)) \|',acc,re.M))
    for key,row in plan_rows.items():
        indexed=by_id.get(key,{})
        for field in ('depends_on','acceptance'):
            if row[field] != set(indexed.get(field,[])):
                errors.append(f'PLAN/trace {field} differ: {key}')
        if row['acceptance']-acceptance_ids:
            errors.append(f'PLAN: undefined acceptance row: {key}')
    for entry in work:
        for ref in entry.get('evidence_refs',[]):
            if not (root/ref).is_file(): errors.append(f"trace evidence path missing: {entry['id']} {ref}")
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
    if not re.search(r'FR-0(?!\d)', raw): errors.append('trace missing FR-0')
    for wp in set(re.findall(r'WP-\d+\.\d+[a-z]?',raw)):
        if not re.search(re.escape(wp)+r'(?![\da-z])',texts['IMPLEMENTATION-PLAN.md']): errors.append(f'trace undefined {wp}')
    for accid in set(re.findall(r'\b(?:F-\d+(?:\.\d+)+|[DUPSHK]-\d+)\b',raw)):
        if not re.search(re.escape(accid)+r'(?![\d.])',texts['ACCEPTANCE.md']): errors.append(f'trace undefined acceptance {accid}')
    for doc in set(re.findall(r'(?:[A-Za-z0-9_-]+/)*[A-Z][A-Z-]+\.md',raw)):
        if not (root/doc).is_file(): errors.append(f'trace missing document {doc}')

if errors:
    raise SystemExit('\n'.join('FAIL '+e for e in errors))
print('Design v3.0.3 OK: FR-0~FR-17, 40 UI components, 64 WP; references, dependency DAG and plan/trace acceptance mappings agree.')
print('Document checks do not prove implementation or hardware acceptance.')
