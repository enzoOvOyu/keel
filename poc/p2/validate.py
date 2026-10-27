#!/usr/bin/env python3
"""Check retained P2 H-2..H-5 evidence offline; never sign hardware acceptance."""
import argparse
import importlib.util
import json
from pathlib import Path

spec = importlib.util.spec_from_file_location('keel_p2_reader', Path(__file__).parents[1] / 'h1' / 'validate.py')
reader = importlib.util.module_from_spec(spec)
spec.loader.exec_module(reader)
require, artifact, instant = reader.require, reader.artifact, reader.instant


def document(root, ref):
    value = reader.decode(artifact(root, ref))
    require(isinstance(value, dict), 'JSON object evidence required')
    return value


def check(path):
    path = Path(path)
    root = path.parent.resolve()
    data = reader.decode(reader.read_file(root, path.name))
    require(isinstance(data, dict), 'manifest must be a JSON object')
    require(data.get('schema') == 'keel.p2.hardware/v1' and data.get('status') == 'COLLECTED', 'P2 hardware evidence has not been collected')
    require(reader.ID.fullmatch(data.get('project_id', '')) and reader.REVISION.fullmatch(data.get('platform_commit', '')), 'exact project and platform commit required')
    require(data.get('environment') and data.get('approval_ref') and data.get('operator') and data.get('reviewer') and data['operator'] != data['reviewer'], 'approved environment and independent reviewer required')
    start, finish = instant(data.get('started_at')), instant(data.get('finished_at'))
    require(start < finish, 'approved UTC evidence window required')
    health = document(root, data.get('platform_health'))
    require(health.get('status') == 'ok' and health.get('version') == data['platform_commit'], 'running platform commit differs')
    hosts, switches = data.get('servers', []), data.get('switches', [])
    require(isinstance(hosts, list) and len(hosts) == 8, 'eight physical servers required')
    require(isinstance(switches, list) and len(switches) >= 2, 'at least two physical switches required')
    for group, key in ((hosts, 'asset_id'), (switches, 'switch_id')):
        require(all(reader.ID.fullmatch(item.get(key, '')) and item.get('serial') and item.get('model') for item in group), 'stable device identity/model required')
        require(len({item[key] for item in group}) == len(group) and len({item['serial'] for item in group}) == len(group), 'duplicate physical device identity')
    require(all(host.get('family') in ('HGX_B300', 'GB300') and host.get('arch') == ('arm64' if host['family'] == 'GB300' else 'amd64') for host in hosts), 'explicit B300/GB300 architecture required')
    require(all(switch.get('nos') and switch.get('nos_version') for switch in switches), 'NOS family/version required')
    project = data['project_id']
    for host in hosts:
        asset = host['asset_id']
        inventory = document(root, host.get('bmc_manifest'))
        require(inventory.get('project_id') == project and inventory.get('asset_id') == asset and inventory.get('origin') == 'real', 'H-2 BMC source/asset mismatch')
        require(inventory.get('level') == 'L1' and inventory.get('state') == 'completed' and inventory.get('identity_matched') is True and inventory.get('asset_current') is True, 'H-2 complete current L1 identity required')
        require(inventory.get('credential_batch_ref') and inventory.get('credential_version') and reader.SHA.fullmatch(inventory.get('content_hash', '')), 'H-2 frozen credential and content references required')
        require(start <= instant(inventory.get('collected_at')) <= finish, 'BMC capture outside evidence window')
        raw = document(root, host.get('bmc_system_raw'))
        require(raw.get('SerialNumber') == host['serial'], 'BMC raw serial differs from physical identity')
        for name in ('lldp_host_raw', 'lldp_switch_raw', 'manual_mapping', 'dom_direct', 'dom_panel'):
            require(len(artifact(root, host.get(name))) > 0, f'{name}: original evidence required')
        steps = [document(root, host.get('step2')), document(root, host.get('step3'))]
        for step, number in zip(steps, (2, 3)):
            detail = step.get('detail', {})
            require(step.get('asset_id') == asset and step.get('step') == number and step.get('effective_verdict') == 'PASS', 'H-3 current Step2/Step3 PASS required')
            require(detail.get('host_origin') == 'real' and detail.get('switch_origin') == 'device', 'H-3 simulated/missing host or switch evidence rejected')
            require(detail.get('command_id') or detail.get('port_sources'), 'H-3 original command/port sources required')
            require(start <= instant(step.get('created_at')) <= finish < instant(step.get('valid_until')), 'H-3 expired or out-of-window report')
        mapping = document(root, host.get('mapping'))
        candidate = mapping.get('candidate', {})
        require(mapping.get('effective_verdict') == 'PASS' and candidate.get('project_id') == project and candidate.get('asset_id') == asset and candidate.get('ready') is True, 'H-4 current full-scope G3 mapping required')
        require(candidate.get('bindings') and candidate.get('coverage', {}).get('required_ports') == candidate.get('coverage', {}).get('mapped_ports'), 'H-4 incomplete physical mapping denominator')
        require(finish < instant(candidate.get('valid_until')), 'H-4 mapping expired during evidence window')
        comparisons = host.get('comparisons', {})
        require(comparisons.get('mapping_identity') == 'MATCH' and comparisons.get('dom_readback') in ('MATCH', 'UNSUPPORTED_MARKED'), 'H-4/H-5 owner comparison records incomplete')
        artifact(root, host.get('comparison_record'))
    cases = data.get('cases', [])
    required = {'credential-version-reverify', 'wrong-patch-T2', 'counter-growth-T4', 'dom-unsupported'}
    require(len(cases) == len(required) and {case.get('name') for case in cases} == required, 'four explicit negative/recovery cases required')
    for case in cases:
        require(start <= instant(case.get('observed_at')) <= finish and case.get('operator') and case.get('restoration_ref'), 'case approved observation/restoration record required')
        artifact(root, case.get('before')); artifact(root, case.get('after')); artifact(root, case.get('operation_log'))
        if case['name'] in ('wrong-patch-T2', 'counter-growth-T4'):
            issue = document(root, case.get('issue')).get('issue', {})
            category = 'T2' if case['name'] == 'wrong-patch-T2' else 'T4'
            require(issue.get('project_id') == project and issue.get('category') == category and issue.get('source') == 'g2' and issue.get('state') == 'closed', 'H-4 same-domain issue closure required')
            verifications = reader.page(artifact(root, case.get('verifications')))
            require(verifications and case.get('closed_version') == issue.get('version'), 'complete wiring-retests history and exact close version required')
            require(any(row.get('issue_id') == issue.get('id') and row.get('verdict') == 'PASS' and row.get('applied_issue_version', -1) + 1 == issue.get('version') and row.get('repair_version', 0) > 0 and reader.ID.fullmatch(row.get('command_id', '')) for row in verifications), 'H-4 close must follow the same issue original command PASS retest')
            artifact(root, case.get('repair_report'))
    artifact(root, data.get('sim_difference_register'))
    return {'item': '#P2/H-2..H-5', 'status': 'READY_FOR_REVIEW', 'hardware_acceptance': 'UNVERIFIED', 'H-13': 'SEPARATE_PACKAGE_REQUIRED', 'note': 'Hashes and selected exported invariants only; owner must inspect raw TLV/DOM, manual physical matches, repair proofs, authorization and sim differences. No gate or acceptance state is changed.'}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('manifest', type=Path)
    args = parser.parse_args()
    try:
        print(json.dumps(check(args.manifest), ensure_ascii=False))
    except (ValueError, OSError, TypeError, KeyError, OverflowError) as error:
        print(json.dumps({'item': '#P2/H-2..H-5', 'status': 'BLOCKED', 'hardware_acceptance': 'UNVERIFIED', 'error': str(error)}, ensure_ascii=False))
        raise SystemExit(1)


if __name__ == '__main__':
    main()
