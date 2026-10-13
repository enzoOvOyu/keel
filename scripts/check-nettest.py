#!/usr/bin/env python3
"""RoCE connectivity module (WP-3.3, FR-11): precheck, five-layer plan, per-pair results, T1-T6 tickets, G5 and admission."""
import http.client, http.cookies, json, os, secrets, urllib.parse
from pathlib import Path

file = Path(os.environ.get('KEEL_AUTH_BROWSER_CREDENTIALS_FILE', '/tmp/keel-auth-browser-credentials.json'))
assert file.stat().st_mode & 0o077 == 0, 'Private isolated credentials required'
credentials = json.loads(file.read_text())
base = urllib.parse.urlsplit(credentials['base_url'])
assert base.scheme == 'http' and base.hostname in ('127.0.0.1', 'localhost') and base.port == 3102, 'Isolated API only'
prefix = 'nt-' + secrets.token_hex(5)
passed = []


class Client:
    def __init__(self, ip='127.0.0.60'):
        self.cookies, self.csrf, self.ip = {}, None, ip

    def call(self, method, path, body=None, key=None, code=200):
        headers = {'Origin': credentials['base_url']}
        if self.cookies:
            headers['Cookie'] = '; '.join(k + '=' + v for k, v in self.cookies.items())
        if self.csrf:
            headers['X-CSRF-Token'] = self.csrf
        if method != 'GET':
            headers['Idempotency-Key'] = key or secrets.token_hex(16)
        if body is not None:
            headers['Content-Type'] = 'application/json'
        conn = http.client.HTTPConnection(base.hostname, base.port, timeout=60, source_address=(self.ip, 0))
        try:
            conn.request(method, '/api/v1' + path, body=None if body is None else json.dumps(body), headers=headers)
            response = conn.getresponse()
            status, raw = response.status, response.read()
            for name, value in response.getheaders():
                if name.lower() == 'set-cookie':
                    for name, m in http.cookies.SimpleCookie(value).items():
                        if not m.value:
                            self.cookies.clear()
                        else:
                            self.cookies[name] = m.value
            payload = json.loads(raw) if raw else None
        finally:
            conn.close()
        assert status == code, (method, path, status, code, payload)
        if isinstance(payload, dict):
            self.csrf = payload.get('csrf_token', self.csrf)
        return payload


admin = Client()
admin.call('GET', '/auth/csrf')
admin.call('POST', '/auth/login', {'method': 'local', 'username': credentials['username'], 'password': credentials['password']})
project = admin.call('POST', '/projects', {'name': prefix, 'slug': prefix}, code=201)
path = '/projects/' + project['id']

approver_name = prefix + '-approver'
approver_password = secrets.token_urlsafe(18)
admin.call('POST', '/admin/users', {'username': approver_name, 'display_name': 'NT approver', 'password': approver_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': approver_name, 'role': 'Admin'}, code=201)
approver = Client(ip='127.0.0.62')
approver.call('GET', '/auth/csrf')
approver.call('POST', '/auth/login', {'method': 'local', 'username': approver_name, 'password': approver_password})

viewer_name = prefix + '-viewer'
viewer_password = secrets.token_urlsafe(18)
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'NT viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.61')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})

# Fixture: two B300 machines, four slots each (management / storage / two
# compute rails), the default role plan, a published mapping (G3), a PCIe
# snapshot and an approved ConfigSet, then verified applies for every
# required renderer so the G4 precondition holds on both targets.
SLOTS = [
    {'slot_no': 1, 'pcie_slot': '01', 'expect_nic_model': 'cx7', 'panel_port': 'port1', 'default_role': 'management'},
    {'slot_no': 2, 'pcie_slot': '02', 'expect_nic_model': 'x710', 'panel_port': 'port2', 'default_role': 'storage'},
    {'slot_no': 3, 'pcie_slot': '03', 'expect_nic_model': 'cx7', 'panel_port': 'port3', 'default_role': 'compute'},
    {'slot_no': 4, 'pcie_slot': '04', 'expect_nic_model': 'cx7', 'panel_port': 'port4', 'default_role': 'compute'},
]
CAPABILITY = {'nic_partition': {'cx7': {'pf': 2, 'vf': 64, 'rate_gbps': 200}, 'x710': {'pf': 2, 'vf': 32, 'rate_gbps': 10}}}
model = admin.call('POST', path + '/models', {'name': 'B300-8U', 'kind': 'server'}, code=201)
admin.call('POST', f'{path}/models/{model["id"]}/versions', {'hw_spec': {'gpus': 8}, 'capability_matrix': CAPABILITY, 'slots': SLOTS}, code=201)


def machine(serial, hostname, base_mac):
    return {'serial_number': serial, 'rack': '', 'unit_position': '', 'bmc_address': '', 'hostname': hostname,
            'ip_reference': '', 'model_name': 'B300-8U',
            'ports': [{'mac_address': f'{base_mac}:00:0{i}', 'port_name': f'port{i}', 'rail': 0 if i < 4 else 1} for i in range(1, 5)]}


admin.call('POST', path + '/bom/commit', {'file_version': 'v1', 'machines': [
    machine('SN-NT1', 'nt-node-1', 'aa:bb:cc:f3'), machine('SN-NT2', 'nt-node-2', 'aa:bb:cc:f4'),
]}, code=201)
assets = {a['hostname']: a['machine_id'] for a in admin.call('GET', path + '/assets?page=1&page_size=20')['items']}
m1, m2 = assets['nt-node-1'], assets['nt-node-2']

admin.call('PUT', path + '/roles', {'roles': {'management': {'enabled': True, 'required': True},
    'storage': {'enabled': True, 'required': True}, 'compute': {'enabled': True, 'required': True},
    'oob': {'enabled': True, 'required': False}}, 'slot_overrides': {}})

for mid in (m1, m2):
    admin.call('POST', path + '/collections', {'level': 'L1', 'machine_ids': [mid]})
    admin.call('POST', path + '/collections', {'level': 'L2', 'machine_ids': [mid]})
    admin.call('POST', path + '/gates/evaluate', {'gate': 'G1', 'machine_ids': [mid]})

admin.call('POST', path + '/lldp/snapshots', {'snapshots': [
    {'machine_id': m1, 'ports': {f'port{i}': {'peer': 'sw1', 'peer_port': f'Eth1/{i}', 'link_up': True, 'dom_warning': False, 'flapping': False} for i in range(1, 5)}},
    {'machine_id': m2, 'ports': {f'port{i}': {'peer': 'sw1', 'peer_port': f'Eth2/{i}', 'link_up': True, 'dom_warning': False, 'flapping': False} for i in range(1, 5)}},
]})
for mid in (m1, m2):
    ports = {p['port_name']: p for p in admin.call('GET', path + f'/machines/{mid}/ports')['items']}
    entries = {'port1': ('mgmt0', '', ''), 'port2': ('stor0', '', ''),
               'port3': ('nic-r0-p1', '18:00.0', 'gpu-0'), 'port4': ('nic-r1-p1', '5a:00.0', 'gpu-1')}
    admin.call('POST', path + '/mappings/publish', {'machine_id': mid, 'entries': [
        {'port_id': ports[name]['id'], 'os_interface': iface, 'mac_address': ports[name]['mac_address'],
         'bdf': bdf, 'switch_port': 'sw1', 'gpu_affinity': gpu} for name, (iface, bdf, gpu) in entries.items()],
        'note': 'nettest fixture'}, code=201)
    admin.call('POST', path + '/pcie/snapshots', {'machine_id': mid, 'devices': [
        {'role': 'gpu', 'index': 0, 'bdf': '01:00.0', 'upstream': ['slot-1'], 'numa_node': 0, 'lnk_cap_width': 16, 'lnk_sta_width': 16, 'slot': 'slot-1'},
        {'role': 'gpu', 'index': 1, 'bdf': '02:00.0', 'upstream': ['slot-2'], 'numa_node': 1, 'lnk_cap_width': 16, 'lnk_sta_width': 16, 'slot': 'slot-2'},
        {'role': 'nic', 'index': 0, 'bdf': '18:00.0', 'upstream': ['slot-3'], 'numa_node': 0, 'lnk_cap_width': 16, 'lnk_sta_width': 16, 'slot': 'slot-3'},
        {'role': 'nic', 'index': 1, 'bdf': '5a:00.0', 'upstream': ['slot-4'], 'numa_node': 1, 'lnk_cap_width': 16, 'lnk_sta_width': 16, 'slot': 'slot-4'},
    ], 'expected': {'slot_by_role': {}, 'gpu_numa_group': [0, 1], 'nvlink_pairs': []}, 'nvlink_raw': ''})

draft = admin.call('POST', path + '/config-sets', {'name': 'roce-b300', 'scope': 'model', 'model_name': 'B300-8U',
                                                  'template': 'mtu={{param.mtu}}', 'keys': json.dumps({'mtu': 9000})}, code=201)
set_id = draft['id']
approver.call('POST', path + f'/config-sets/{set_id}/approve', {})

NET = {'net': {'management': {'subnet': '10.10.0.0/24', 'gateway': '10.10.0.1', 'mtu': 1500},
               'storage': {'subnet': '10.20.0.0/24'}, 'compute': {'subnet': '10.30.0.0/24', 'mtu': 9000}}}


def render(kind, mid, params=None, apply=False, readback=None, code=200):
    body = {'kind': kind, 'machine_id': mid, 'params': params or {}}
    if set_id:
        body['config_set_id'] = set_id
    if readback is not None:
        body['readback'] = readback
    return admin.call('POST', path + '/config/render' + ('?apply=1' if apply else ''), body, code=code)


for mid in (m1, m2):
    assert render('gpu_network', mid, NET, apply=True,
                  readback={'role': 'compute', 'address': '10.30.0.13/24', 'mtu': '9000'})['verified'] is True
    assert render('nic_partition', mid, apply=True, readback={'pf': '2', 'total_gbps': '610'})['verified'] is True
    assert render('roce_import', mid, apply=True, readback={'mtu': 9000})['verified'] is True
    assert render('gpu_nic_binding', mid, apply=True,
                  readback={'NCCL_NETDEVS_POLICY': 'GPU:0:nic-r0-p1,GPU:1:nic-r1-p1', 'gpu': '0', 'nic': 'nic-r0-p1'})['verified'] is True

board = admin.call('GET', path + '/dashboard')['cards']
assert board['G4']['state'] == 'pass' and board['G4']['pass_rate'] == 100, board['G4']
passed.append('The fixture carries verified G4 evidence on both targets')

ports = {mid: {p['port_name']: p for p in admin.call('GET', path + f'/machines/{mid}/ports')['items']} for mid in (m1, m2)}


def mesh(layer, state='pass', failure=None, only=None):
    out = []
    for name_a, pa in ports[m1].items():
        for name_b, pb in ports[m2].items():
            if pa['rail'] != pb['rail'] or only is not None and (name_a, name_b) != only:
                continue
            out.append({'layer': layer, 'machine_id': m1, 'port_id': pa['id'], 'peer_machine_id': m2,
                        'peer_port_id': pb['id'], 'state': state, 'bandwidth_gbps': 196.5,
                        'loss_ppm': 0, 'pfc_pause': 12, 'hotspot': layer == 'L4',
                        **({'failure': failure} if failure else {})})
    return out


def full_plan(run_id):
    results = []
    for mid in (m1, m2):
        for name, p in ports[mid].items():
            results.append({'layer': 'L1', 'machine_id': mid, 'port_id': p['id'], 'peer_machine_id': mid,
                            'peer_port_id': p['id'], 'state': 'pass',
                            'detail': {'link_up': True, 'rate': '200G'}})
    for layer in ('L2', 'L3', 'L4'):
        results += mesh(layer)
    for mid in (m1, m2):
        for rail in sorted({p['rail'] for p in ports[mid].values()}):
            first = [p for p in ports[mid].values() if p['rail'] == rail][0]
            results.append({'layer': 'L5', 'machine_id': mid, 'port_id': first['id'], 'peer_machine_id': mid,
                            'peer_port_id': '', 'state': 'pass', 'pfc_pause': 3,
                            'detail': {'rail': rail, 'pfc_watchdog': 'ok', 'ecn_rate': 0.001}})
    return results


# 1. The precheck refuses to start before the network platform handshake.
rej = admin.call('POST', path + '/nettest/runs', {'machine_ids': [m1], 'profile': 'quick'}, code=409)
assert rej['code'] == 'PRECHECK_FAILED' and 'platform' in rej['message'], rej
passed.append('A run without the network-platform handshake is refused')

# 2. With the platform ready the plan materialises the five layers.
admin.call('PUT', path + '/nettest/platform', {'status': 'ready', 'detail': {'switch': 'sw1', 'counters': 'pfc,ecn'}})
run = admin.call('POST', path + '/nettest/runs', {'machine_ids': [m1, m2], 'profile': 'quick'}, code=201)
layers = {l['layer']: l for l in run['layers']}
assert layers['L1']['kind'] == 'port_baseline' and layers['L1']['pairs'] == 8, layers
assert layers['L2']['kind'] == 'mesh' and layers['L2']['pairs'] == 10, layers
assert layers['L5']['kind'] == 'rail' and layers['L5']['pairs'] == 4, layers
assert run['batch_size'] == 8 and run['params']['nettest.l2_pair_sample'] == 32, run
assert {c['check']: c['state'] for c in run['precheck']} == {
    'g4': 'pass', 'mapping': 'pass', 'alerts': 'pass', 'platform': 'pass'}, run['precheck']
passed.append('A ready platform plans the five layers from the frozen mapping')

# 3. The parameter surface is closed; the full tier demands full coverage.
rej = admin.call('POST', path + '/nettest/runs', {'machine_ids': [m1], 'profile': 'quick',
                                                  'params': {'nettest.unknown_key': 1}}, code=400)
assert 'unknown nettest parameter' in rej['message'], rej
full = admin.call('POST', path + '/nettest/runs', {'machine_ids': [m1, m2], 'profile': 'full'}, code=201)
assert full['params']['nettest.l3_pair_mode'] == 'full' and full['params']['nettest.required_path_coverage'] == 1.0, full
passed.append('The parameter surface is closed and the full tier demands full coverage')

# 4. Ingest the whole plan: L1 baselines, the L2-L4 mesh and the L5 rails.
plan = full_plan(run['id'])
assert len(plan) == 8 + 30 + 4, len(plan)
ingested = admin.call('POST', path + f'/nettest/runs/{run["id"]}/results', {'results': plan})
assert ingested['state'] == 'running', ingested
passed.append('Every layer ingests its per-NIC-pair results through the mock channel')

# 5. A finished all-pass run records G5 PASS and opens NCCL admission.
finished = admin.call('POST', path + f'/nettest/runs/{run["id"]}/finish')
assert finished['state'] == 'finished' and finished['conclusion'] == 'PASS', finished
coverage = finished['coverage']
assert coverage['L1']['coverage'] == 1.0 and coverage['L4']['failed'] == 0, coverage
assert coverage['complete'] is True and coverage['g5_evidence'] and coverage['sampled'] is False, coverage
admission = admin.call('GET', path + f'/nettest/admission?machine_ids={m1},{m2}')['items']
assert all(a['net_verified'] and not a['waived'] for a in admission), admission
board = admin.call('GET', path + '/dashboard')['cards']
assert board['G5']['state'] == 'pass' and board['G5']['pass_rate'] == 100, board['G5']
bar = {layer['layer']: layer for layer in board['G5'].get('layers', [])}
assert set(bar) == {'L1', 'L2', 'L3', 'L4', 'L5'}, bar
assert bar['L1']['planned'] == 8 and bar['L1']['rate'] == 100, bar['L1']
assert bar['L2']['planned'] == 10 and bar['L2']['failed'] == 0, bar['L2']
assert bar['L5']['planned'] == 4 and bar['L5']['rate'] == 100, bar['L5']
passed.append('A complete all-pass run records G5 PASS and opens the NCCL admission')

# 6. The heat matrix aggregates one cell per machine pair and layer.
cells = admin.call('GET', path + f'/nettest/runs/{run["id"]}/matrix?layer=L3')['items']
assert len(cells) == 1 and cells[0]['pairs'] == 10 and cells[0]['state'] == 'pass', cells
assert cells[0]['hostname'] == 'nt-node-1' and cells[0]['peer_hostname'] == 'nt-node-2', cells
passed.append('The rail heat matrix aggregates one cell per machine pair and layer')

# 7. A VLAN failure classifies T6, fails the run and opens the ticket.
run_b = admin.call('POST', path + '/nettest/runs', {'machine_ids': [m1, m2], 'profile': 'quick'}, code=201)
plan_b = full_plan(run_b['id'])
plan_b = [r for r in plan_b if not (r['layer'] == 'L3' and r['machine_id'] == m1
                                    and r['port_id'] == ports[m1]['port3']['id']
                                    and r['peer_port_id'] == ports[m2]['port3']['id'])]
plan_b += mesh('L3', state='fail', failure='vlan_unreachable', only=('port3', 'port3'))
admin.call('POST', path + f'/nettest/runs/{run_b["id"]}/results', {'results': plan_b})
failed = admin.call('POST', path + f'/nettest/runs/{run_b["id"]}/finish')
assert failed['conclusion'] == 'FAIL' and failed['coverage']['L3']['failed'] == 1, failed
tickets = failed['coverage']['tickets']
assert len(tickets) == 1, failed
issue = admin.call('GET', path + '/issues?status=open')['items'][0]
assert issue['category'] == 'T6' and issue['source'] == 'nettest', issue
assert 'T6' in issue['title'] and 'port3' in issue['title'], issue
admission = admin.call('GET', path + f'/nettest/admission?machine_ids={m1}')['items']
assert admission[0]['net_verified'] is False and 'FAIL' in admission[0]['reason'], admission
passed.append('A VLAN failure classifies T6, fails G5 and opens the FR-4.7 ticket')

# 8. The port panel lights the failed path's ports (FR-11.4 linkage).
panel = admin.call('GET', path + f'/machines/{m1}/port-panel')['ports']
failed_ports = [p for p in panel if p.get('nettest_failure')]
assert len(failed_ports) == 1 and failed_ports[0]['port_name'] == 'port3', failed_ports
assert failed_ports[0]['nettest_failure'].startswith('L3:T6'), failed_ports
passed.append('The failed path lights its ports in the panel')

# 9. An unclassified failure never guesses a T category.
plan_b2 = mesh('L4', state='fail', failure='mystery_blob', only=('port3', 'port3'))
admin.call('POST', path + f'/nettest/runs/{run_b["id"]}/results', {'results': plan_b2})
again = admin.call('POST', path + f'/nettest/runs/{run_b["id"]}/finish')
assert again['conclusion'] == 'FAIL' and len(again['coverage']['unclassified_failures']) == 1, again
passed.append('An unknown observation stays unclassified instead of guessing a T category')

# 10. The close gate holds while the pair has not re-measured pass.
ticket = admin.call('POST', path + f'/issues/{tickets[0]}/transition?action=assign', {'issue_id': tickets[0], 'version': 1})
ticket = admin.call('POST', path + f'/issues/{tickets[0]}/transition?action=start', {'issue_id': tickets[0], 'version': ticket['version']})
ticket = admin.call('POST', path + '/issues/repair', {'issue_id': tickets[0], 'action': 'config_fix',
                                                      'note': '修正 VLAN 与 MTU 配置，等待定向复测'})
ticket = admin.call('POST', path + f'/issues/{tickets[0]}/transition?action=verify', {'issue_id': tickets[0], 'version': ticket['version']})
rej = admin.call('POST', path + f'/issues/{tickets[0]}/transition?action=close', {'issue_id': tickets[0], 'version': ticket['version']}, code=409)
assert rej['code'] == 'ISSUE_STATE' and 're-measurement' in rej['message'], rej
passed.append('The close gate holds while the pair has not re-measured pass')

# 11. An open flapping alert blocks a plain run; the issue-bound
#     re-verification exempts the fault itself (FR-4.7 / FR-11.2).
admin.call('POST', path + '/mon/ingest', {'machine_id': m1, 'stage': 'M2', 'changed': [
    {'port': 'port1', 'metric': 'link', 'value': {'state': 'down'}},
    {'port': 'port1', 'metric': 'link', 'value': {'state': 'down'}},
    {'port': 'port1', 'metric': 'link', 'value': {'state': 'down'}},
]})
alerts = admin.call('GET', path + '/mon/alerts?page=1&page_size=20')['items']
assert any(a['rule'] == 'flapping' and a['port'] == 'port1' for a in alerts), alerts
rej = admin.call('POST', path + '/nettest/runs', {'machine_ids': [m1], 'profile': 'quick'}, code=409)
assert rej['code'] == 'PRECHECK_FAILED' and 'alerts' in rej['message'], rej
bound = admin.call('POST', path + '/nettest/runs', {'machine_ids': [m1], 'profile': 'quick',
                                                    'issue_id': tickets[0]}, code=201)
assert bound['targets'] == [m1] and bound['issue_id'] == tickets[0], bound
assert {c['check']: c['state'] for c in bound['precheck']}['alerts'] == 'waived', bound['precheck']
passed.append('An open alert blocks a plain run and is exempted for the issue-bound re-verification')

# 12. The re-measured pass resolves the pair, closes the ticket and
#     re-verifies the run (the newest G5 evidence wins).
admin.call('POST', path + f'/nettest/runs/{run_b["id"]}/results', {'results': mesh('L3', only=('port3', 'port3'))})
admin.call('POST', path + f'/nettest/runs/{run_b["id"]}/results', {'results': mesh('L4', only=('port3', 'port3'))})
closed = admin.call('POST', path + f'/issues/{tickets[0]}/transition?action=close', {'issue_id': tickets[0], 'version': ticket['version']})
assert closed['status'] == 'closed', closed
reverified = admin.call('POST', path + f'/nettest/runs/{run_b["id"]}/finish')
assert reverified['conclusion'] == 'PASS' and reverified['coverage']['g5_evidence'], reverified
panel = admin.call('GET', path + f'/machines/{m1}/port-panel')['ports']
assert not [p for p in panel if p.get('nettest_failure')], panel
passed.append('A re-measured pass resolves the pair, closes the ticket and re-verifies the run')

# 13. A finished run never grows new pairs after its conclusion.
rej = admin.call('POST', path + f'/nettest/runs/{run_b["id"]}/results', {'results': [
    {'layer': 'L3', 'machine_id': m1, 'port_id': ports[m1]['port4']['id'], 'peer_machine_id': m2,
     'peer_port_id': ports[m2]['port1']['id'], 'state': 'pass'}]}, code=409)
assert rej['code'] == 'NETTEST_STATE', rej
passed.append('A finished run only re-measures the pairs it already carries')

# 14. An incomplete run never records G5 evidence. (m2 carries no open
#     alert, so the precheck lets the run start; m1 still holds the
#     flapping alert from the re-verification test.)
partial = admin.call('POST', path + '/nettest/runs', {'machine_ids': [m2], 'profile': 'full'}, code=201)
admin.call('POST', path + f'/nettest/runs/{partial["id"]}/results', {'results': [
    {'layer': 'L1', 'machine_id': m2, 'port_id': ports[m2]['port1']['id'], 'peer_machine_id': m2,
     'peer_port_id': ports[m2]['port1']['id'], 'state': 'pass'}]})
incomplete = admin.call('POST', path + f'/nettest/runs/{partial["id"]}/finish')
assert incomplete['conclusion'] == '' and 'g5_evidence' not in incomplete['coverage'], incomplete
passed.append('An incomplete run concludes nothing and records no G5 evidence')

# 15. Read-only roles read the runs but cannot plan, ingest or refresh.
viewer.call('GET', path + '/nettest/runs')
viewer.call('GET', path + f'/nettest/runs/{run["id"]}')
viewer.call('GET', path + f'/nettest/runs/{run["id"]}/matrix')
viewer.call('GET', path + f'/nettest/admission?machine_ids={m1}')
viewer.call('GET', path + '/nettest/platform')
viewer.call('POST', path + '/nettest/runs', {'machine_ids': [m1], 'profile': 'quick'}, code=403)
viewer.call('PUT', path + '/nettest/platform', {'status': 'ready'}, code=403)
viewer.call('POST', path + f'/nettest/runs/{run["id"]}/results', {'results': [
    {'layer': 'L1', 'machine_id': m1, 'port_id': ports[m1]['port1']['id'], 'peer_machine_id': m1,
     'peer_port_id': ports[m1]['port1']['id'], 'state': 'pass'}]}, code=403)
passed.append('Read-only roles read the runs but cannot plan, ingest or refresh the platform')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
