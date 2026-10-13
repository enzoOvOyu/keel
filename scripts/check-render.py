#!/usr/bin/env python3
"""Network role bindings and the four batch-config renderers (FR-5.2-5.5, FR-5.9)."""
import http.client, http.cookies, json, os, secrets, urllib.parse
from pathlib import Path

file = Path(os.environ.get('KEEL_AUTH_BROWSER_CREDENTIALS_FILE', '/tmp/keel-auth-browser-credentials.json'))
assert file.stat().st_mode & 0o077 == 0, 'Private isolated credentials required'
credentials = json.loads(file.read_text())
base = urllib.parse.urlsplit(credentials['base_url'])
assert base.scheme == 'http' and base.hostname in ('127.0.0.1', 'localhost') and base.port == 3102, 'Isolated API only'
prefix = 'rn-' + secrets.token_hex(5)
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
admin.call('POST', '/admin/users', {'username': approver_name, 'display_name': 'RN approver', 'password': approver_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': approver_name, 'role': 'Admin'}, code=201)
approver = Client(ip='127.0.0.62')
approver.call('GET', '/auth/csrf')
approver.call('POST', '/auth/login', {'method': 'local', 'username': approver_name, 'password': approver_password})

viewer_name = prefix + '-viewer'
viewer_password = secrets.token_urlsafe(18)
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'RN viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.61')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})

# The two-phase b300 scenario: cx7 for management, x710 for storage, cx7 for
# the two compute rails. The model declares the default role per slot and the
# capability matrix the partition renderer generates from.
SLOTS = [
    {'slot_no': 1, 'pcie_slot': '01', 'expect_nic_model': 'cx7', 'panel_port': 'port1', 'default_role': 'management'},
    {'slot_no': 2, 'pcie_slot': '02', 'expect_nic_model': 'x710', 'panel_port': 'port2', 'default_role': 'storage'},
    {'slot_no': 3, 'pcie_slot': '03', 'expect_nic_model': 'cx7', 'panel_port': 'port3', 'default_role': 'compute'},
    {'slot_no': 4, 'pcie_slot': '04', 'expect_nic_model': 'cx7', 'panel_port': 'port4', 'default_role': 'compute'},
]
CAPABILITY = {'nic_partition': {'cx7': {'pf': 2, 'vf': 64, 'rate_gbps': 200}, 'x710': {'pf': 2, 'vf': 32, 'rate_gbps': 10}}}

model = admin.call('POST', path + '/models', {'name': 'B300-8U', 'kind': 'server'}, code=201)
admin.call('POST', f'{path}/models/{model["id"]}/versions', {'hw_spec': {'gpus': 8}, 'capability_matrix': CAPABILITY, 'slots': SLOTS}, code=201)
bom = {'file_version': 'v1', 'machines': [
    {'serial_number': 'SN-RN1', 'rack': '', 'unit_position': '', 'bmc_address': '', 'hostname': 'rn-node-1', 'ip_reference': '', 'model_name': 'B300-8U',
     'ports': [{'mac_address': f'aa:bb:cc:e1:00:0{i}', 'port_name': f'port{i}', 'rail': 0 if i < 3 else 1} for i in range(1, 5)]},
]}
admin.call('POST', path + '/bom/commit', bom, code=201)
mid = admin.call('GET', path + '/assets?page=1&page_size=20')['items'][0]['machine_id']

# The RoCE profile is a ConfigSet: versioned keys approved by a second admin.
KEYS = {'dcqcn': {'ai_rate': 5, 'rp_hai_rate': 50}, 'pfc': {'priority': 3}, 'ets': {'tc': 4}, 'mtu': 9000, 'congestion': {'ecn': 1}}
draft = admin.call('POST', path + '/config-sets', {'name': 'roce-b300', 'scope': 'model', 'model_name': 'B300-8U',
                                                  'template': 'mtu={{param.mtu}}', 'keys': json.dumps(KEYS)}, code=201)
set_id = draft['id']
rej = admin.call('POST', path + f'/config-sets/{set_id}/approve', {}, code=409)
assert rej['code'] == 'SELF_APPROVAL', rej
approved = approver.call('POST', path + f'/config-sets/{set_id}/approve', {})
assert approved['status'] == 'approved', approved

ROLES = {'management': {'enabled': True, 'required': True}, 'storage': {'enabled': True, 'required': True},
         'compute': {'enabled': True, 'required': True}, 'oob': {'enabled': True, 'required': False}}


def save(roles=None, overrides=None, client=None, code=200):
    return (client or admin).call('PUT', path + '/roles', {'roles': roles if roles is not None else ROLES, 'slot_overrides': overrides or {}}, code=code)


def render(kind, params=None, apply=False, set_id=None, readback=None, client=None, machine=None, code=200):
    body = {'kind': kind, 'machine_id': machine or mid, 'params': params or {}}
    if set_id:
        body['config_set_id'] = set_id
    if readback is not None:
        body['readback'] = readback
    return (client or admin).call('POST', path + '/config/render' + ('?apply=1' if apply else ''), body, code=code)


def bindings():
    return admin.call('GET', path + f'/roles/bindings?machine_id={mid}')['items']


# 1. Saving the plan binds every port from the model slots and writes the
#    resolved role to PortModel; only enabled+required roles must be covered.
plan = save()
assert plan['coverage'].get('management') == 1 and plan['coverage'].get('storage') == 1, plan
assert plan['coverage'].get('compute') == 2 and plan['missing'] == [], plan
rows = {row['port_name']: row for row in bindings()}
assert rows['port1']['role'] == 'management' and rows['port2']['role'] == 'storage', rows
assert rows['port3']['role'] == 'compute' and rows['port4']['role'] == 'compute', rows
assert rows['port3']['slot_no'] == 3 and rows['port4']['rail'] == 1, rows
passed.append('Slot defaults bind ports to roles and the resolved role lands on PortModel')

# 2. The compute role carries the test tiers: it cannot be disabled or made
#    optional, and unknown roles are refused outright.
rej = save({'compute': {'enabled': False, 'required': True}}, code=409)
assert rej['code'] == 'ROLE_POLICY', rej
rej = save({'compute': {'enabled': True, 'required': False}}, code=409)
assert rej['code'] == 'ROLE_POLICY', rej
rej = save({'gpu-direct': {'enabled': True, 'required': True}}, code=400)
assert 'unknown network role' in rej['message'], rej
passed.append('The compute role cannot be switched off to bypass a test tier')

# 3. Phase two of the b300 scenario: the x710 slot is set unused and storage
#    is disabled — the model and the template stay untouched and no missing
#    binding is reported.
plan = save({'management': {'enabled': True, 'required': True}, 'storage': {'enabled': False, 'required': True},
             'compute': {'enabled': True, 'required': True}, 'oob': {'enabled': True, 'required': False}}, {'2': 'unused'})
assert plan['missing'] == [], plan
rows = {row['port_name']: row for row in bindings()}
assert rows['port2']['role'] == 'unused' and rows['port2']['overridden'] is True, rows
passed.append('A phase-two override drops the storage slot without a missing-binding error')

# 4. Rendering is role-aware: the unused slot is skipped entirely.
NET = {'net': {'management': {'subnet': '10.10.0.0/24', 'gateway': '10.10.0.1', 'mtu': 1500, 'routes': ['172.16.0.0/16']},
               'storage': {'subnet': '10.20.0.0/24'}, 'compute': {'subnet': '10.30.0.0/24', 'mtu': 9000, 'policy': 'tc qdisc add dev nic-r0-p1 root fq'}}}
out = render('gpu_network', NET)
assert 'port2' not in out['rendered'] and '10.20.0.' not in out['rendered'], out
assert 'iface <unmapped:port1>' in out['rendered'], out
assert 'address 10.30.0.13/24' in out['rendered'] and 'address 10.30.0.14/24' in out['rendered'], out
assert 'mtu 9000' in out['rendered'] and 'post-up ip route add 172.16.0.0/16 dev' in out['rendered'], out
rej = render('gpu_network', {'net': {'management': {'subnet': '10.10.0.0/24'}}}, code=409)
assert 'net.compute' in rej['message'], rej
passed.append('GPU network config renders per role from the mapping and skips unused slots')

# 5. Back to phase one for the remaining checks, then publish the mapping
#    (G3) so applies are allowed and interfaces resolve.
save()
admin.call('POST', path + '/collections', {'level': 'L1', 'machine_ids': [mid]})
admin.call('POST', path + '/collections', {'level': 'L2', 'machine_ids': [mid]})
admin.call('POST', path + '/gates/evaluate', {'gate': 'G1', 'machine_ids': [mid]})
admin.call('POST', path + '/lldp/snapshots', {'snapshots': [{'machine_id': mid, 'ports': {
    f'port{i}': {'peer': 'sw1', 'peer_port': f'Eth1/{i}', 'link_up': True, 'dom_warning': False, 'flapping': False} for i in range(1, 5)}}]})
ports = {p['port_name']: p for p in admin.call('GET', path + f'/machines/{mid}/ports')['items']}
entries = {'port1': ('mgmt0', '', ''), 'port2': ('stor0', '', ''), 'port3': ('nic-r0-p1', '18:00.0', 'gpu-0'), 'port4': ('nic-r1-p1', '5a:00.0', 'gpu-1')}
admin.call('POST', path + '/mappings/publish', {'machine_id': mid, 'entries': [
    {'port_id': ports[name]['id'], 'os_interface': iface, 'mac_address': ports[name]['mac_address'],
     'bdf': bdf, 'switch_port': 'sw1', 'gpu_affinity': gpu} for name, (iface, bdf, gpu) in entries.items()], 'note': 'render fixture'}, code=201)
out = render('gpu_network', NET)
assert 'iface nic-r0-p1' in out['rendered'] and 'iface mgmt0' in out['rendered'], out
passed.append('A published mapping (G3) resolves interfaces in the rendered plan')

# 6. NIC partitioning is generated from the model capability matrix and
#    previews the before/after diff; exceeding the capability refuses.
out = render('nic_partition')
assert 'pf=2 vf=64 rate_gbps=200' in out['rendered'], out
assert 'pf=2 vf=32 rate_gbps=10' in out['rendered'], out
assert 'total_gbps=610' in out['rendered'], out
assert '- # role=management slot=1 nic=cx7 iface=mgmt0 pf=1 vf=0 rate_gbps=0' in out['diff'], out
assert '+ # role=management slot=1 nic=cx7 iface=mgmt0 pf=2 vf=64 rate_gbps=200' in out['diff'], out
rej = render('nic_partition', {'partition': {'compute': {'pf': 8, 'vf': 512}}}, code=409)
assert rej['code'] == 'CAPABILITY_EXCEEDED', rej
passed.append('Partitioning is generated from the model capability and previews a before/after diff')

# 7. GPU-NIC binding needs the FR-4.8 topology snapshot: without it the
#    NUMA layout is unknown and an apply refuses instead of guessing.
out = render('gpu_nic_binding')
assert 'numa_unknown' in out['warnings'], out
rej = render('gpu_nic_binding', apply=True, set_id=None, code=400)
assert 'requires a config set' in rej['message'], rej


def device(role, index, bdf, numa, slot):
    return {'role': role, 'index': index, 'bdf': bdf, 'upstream': [slot], 'numa_node': numa,
            'lnk_cap_width': 16, 'lnk_sta_width': 16, 'slot': slot}


def snapshot(devices):
    return admin.call('POST', path + '/pcie/snapshots', {'machine_id': mid, 'devices': devices,
                                                        'expected': {'slot_by_role': {}, 'gpu_numa_group': [0, 1], 'nvlink_pairs': []}, 'nvlink_raw': ''})


snapshot([device('gpu', 0, '01:00.0', 0, 'slot-1'), device('gpu', 1, '02:00.0', 1, 'slot-2'),
          device('nic', 0, '18:00.0', 0, 'slot-3'), device('nic', 1, '5a:00.0', 1, 'slot-4')])
out = render('gpu_nic_binding')
assert 'NCCL_NETDEVS_POLICY=GPU:0:nic-r0-p1,GPU:1:nic-r1-p1' in out['rendered'], out
assert 'numactl --cpunodebind=0 --membind=0   # iface=nic-r0-p1 gpu=0' in out['rendered'], out
assert 'numactl --cpunodebind=1 --membind=1   # iface=nic-r1-p1 gpu=1' in out['rendered'], out
assert 'cross_numa' not in out['warnings'], out
passed.append('GPU-NIC binding pins each rail inside one NUMA node')

# 8. A NIC moved to the far NUMA node is a warning on preview and a hard
#    refusal on apply (RoCE traffic must not cross NUMA).
snapshot([device('gpu', 0, '01:00.0', 0, 'slot-1'), device('gpu', 1, '02:00.0', 1, 'slot-2'),
          device('nic', 0, '18:00.0', 1, 'slot-3'), device('nic', 1, '5a:00.0', 1, 'slot-4')])
out = render('gpu_nic_binding')
assert 'cross_numa:nic-r0-p1(gpu=0 gpu_numa=0 nic_numa=1)' in out['warnings'], out
rej = render('gpu_nic_binding', apply=True, set_id=set_id, code=409)
assert rej['code'] == 'CROSS_NUMA', rej
passed.append('A cross-NUMA rail warns on preview and blocks the apply')

# 9. RoCE parameters travel as ConfigSet keys: the import previews the diff
#    and only a matching read-back marks the receipt verified.
snapshot([device('gpu', 0, '01:00.0', 0, 'slot-1'), device('gpu', 1, '02:00.0', 1, 'slot-2'),
          device('nic', 0, '18:00.0', 0, 'slot-3'), device('nic', 1, '5a:00.0', 1, 'slot-4')])
out = render('roce_import', set_id=set_id)
assert 'dcqcn.ai_rate=5' in out['rendered'] and 'mtu=9000' in out['rendered'] and 'pfc.priority=3' in out['rendered'], out
assert out['diff'].count('+ ') == 6 and out['verified'] is False and 'readback_missing' in out['warnings'], out
readback = {'dcqcn': {'ai_rate': 5, 'rp_hai_rate': 50}, 'pfc': {'priority': 3}, 'ets': {'tc': 4}, 'mtu': 9000, 'congestion': {'ecn': 1}}
out = render('roce_import', set_id=set_id, readback=readback)
assert out['verified'] is True and 'verified=true' in out['receipt'], out
bad = dict(readback, mtu=1500)
out = render('roce_import', set_id=set_id, readback=bad)
assert out['verified'] is False and 'mismatches=mtu' in out['receipt'], out
passed.append('RoCE imports preview the diff and verify only against a matching read-back')

# 10. Applies record against the ConfigSet version: the first apply carries a
#     diff, the second is a no-op, and the G3 gate still guards the target.
out = render('gpu_network', NET, apply=True, set_id=set_id)
assert out['applied'] is True and out['diff'] == 'new', out
out = render('gpu_network', NET, apply=True, set_id=set_id)
assert out['applied'] is True and out['diff'] == '', out
out = render('roce_import', apply=True, set_id=set_id, readback=readback)
assert out['applied'] is True and out['verified'] is True, out
# A target without a published mapping keeps the G3 gate closed for applies.
admin.call('POST', path + '/bom/commit', {'file_version': 'v2', 'machines': [
    {'serial_number': 'SN-RN2', 'rack': '', 'unit_position': '', 'bmc_address': '', 'hostname': 'rn-node-2', 'ip_reference': '',
     'model_name': 'B300-8U', 'ports': [{'mac_address': f'aa:bb:cc:e2:00:0{i}', 'port_name': f'port{i}'} for i in range(1, 5)]}]}, code=201)
mid2 = [a['machine_id'] for a in admin.call('GET', path + '/assets?page=1&page_size=20')['items'] if a['hostname'] == 'rn-node-2'][0]
rej = render('gpu_network', NET, apply=True, set_id=set_id, machine=mid2, code=409)
assert rej['code'] == 'GATE_EVIDENCE', rej
passed.append('Applies record against the config set version and the G3 gate stays closed for unmapped targets')

# 11. Read-only roles read the plan and bindings but cannot change them.
viewer.call('GET', path + '/roles')
viewer.call('GET', path + f'/roles/bindings?machine_id={mid}')
viewer.call('PUT', path + '/roles', {'roles': ROLES, 'slot_overrides': {}}, code=403)
viewer.call('POST', path + '/config/render', {'kind': 'gpu_network', 'machine_id': mid, 'params': NET}, code=403)
passed.append('Read-only roles read the role scheme but cannot bind or render')

# 12. FR-5.2: the three shared renderers verify a sampled read-back against
#     the rendered plan with the same contract as the RoCE import — a matching
#     sample verifies, a disagreeing sample is recorded as a mismatch, and a
#     missing sample never counts as a pass.
out = render('gpu_network', NET, apply=True, set_id=set_id,
             readback={'role': 'compute', 'iface': 'nic-r0-p1', 'address': '10.30.0.13/24', 'mtu': '9000'})
assert out['applied'] is True and out['verified'] is True, out
assert 'readback_ok' in out['receipt'] and 'readback_incomplete' not in str(out['warnings']), out
out = render('gpu_network', NET, apply=True, set_id=set_id, readback={'address': '10.30.0.99/24'})
assert out['applied'] is True and out['verified'] is False, out
assert 'readback_mismatch' in out['receipt'] and 'address' in out['receipt'], out
out = render('gpu_network', NET, apply=True, set_id=set_id, readback={'lease': 'dhcp'})
assert out['verified'] is False and 'readback_incomplete' in out['receipt'], out
out = render('nic_partition', apply=True, set_id=set_id, readback={'pf': '2', 'total_gbps': '610'})
assert out['applied'] is True and out['verified'] is True, out
out = render('nic_partition', apply=True, set_id=set_id)
assert out['applied'] is True and out['verified'] is False and 'readback_missing' in out['receipt'], out
snapshot([device('gpu', 0, '01:00.0', 0, 'slot-1'), device('gpu', 1, '02:00.0', 1, 'slot-2'),
          device('nic', 0, '18:00.0', 0, 'slot-3'), device('nic', 1, '5a:00.0', 1, 'slot-4')])
out = render('gpu_nic_binding', apply=True, set_id=set_id,
             readback={'NCCL_NETDEVS_POLICY': 'GPU:0:nic-r0-p1,GPU:1:nic-r1-p1', 'gpu': '0', 'nic': 'nic-r0-p1'})
assert out['applied'] is True and out['verified'] is True, out
passed.append('All four renderers verify a sampled read-back before the receipt counts')

# 13. The G4 gate card (PRD 3.4: config aligned, then sampled read-back
#     verified) reads those receipts: the newest unverified applies fail the
#     card at their true rate, and a fully verified target passes it.
board = admin.call('GET', path + '/dashboard')['cards']
assert board['G4']['state'] == 'fail' and board['G4']['pass_rate'] == 50, board['G4']
out = render('gpu_network', NET, apply=True, set_id=set_id,
             readback={'role': 'compute', 'address': '10.30.0.13/24', 'mtu': '9000'})
assert out['applied'] is True and out['verified'] is True, out
out = render('nic_partition', apply=True, set_id=set_id, readback={'pf': '2', 'total_gbps': '610'})
assert out['applied'] is True and out['verified'] is True, out
board = admin.call('GET', path + '/dashboard')['cards']
assert board['G4']['state'] == 'pass' and board['G4']['pass_rate'] == 100, board['G4']
passed.append('The G4 gate card reads the read-back receipts and never fabricates a pass')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
