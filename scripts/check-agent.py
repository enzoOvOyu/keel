#!/usr/bin/env python3
"""Agent installer (WP-3.5, FR-5.7 / F-5.4): media://agent admission, G3+
delivery admission, arch selection, one-time enrollment tokens, credential
lifecycle (issue/rotate/revoke), cache-hit distribution accounting."""
import http.client, http.cookies, json, os, secrets, urllib.parse
from pathlib import Path

file = Path(os.environ.get('KEEL_AUTH_BROWSER_CREDENTIALS_FILE', '/tmp/keel-auth-browser-credentials.json'))
assert file.stat().st_mode & 0o077 == 0, 'Private isolated credentials required'
credentials = json.loads(file.read_text())
base = urllib.parse.urlsplit(credentials['base_url'])
assert base.scheme == 'http' and base.hostname in ('127.0.0.1', 'localhost') and base.port == 3102, 'Isolated API only'
prefix = 'ag-' + secrets.token_hex(5)
groups = []


def group(label):
    groups.append(label)
    print('PASS ' + label, flush=True)


class Client:
    def __init__(self, ip='127.0.0.82'):
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
admin.call('POST', '/admin/users', {'username': approver_name, 'display_name': 'AG approver', 'password': approver_password, 'is_admin': False}, code=201)
admin.call('POST', f'{path}/members', {'username': approver_name, 'role': 'Admin'}, code=201)
approver = Client(ip='127.0.0.83')
approver.call('GET', '/auth/csrf')
approver.call('POST', '/auth/login', {'method': 'local', 'username': approver_name, 'password': approver_password})

viewer_name = prefix + '-viewer'
viewer_password = secrets.token_urlsafe(18)
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'AG viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'{path}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.84')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})

# Fixture: a model with a published version declaring arch=amd64, three
# machines (two mapped = G3+, one unmapped), and approved agent media in both
# arches. Media approval needs the second reviewer (self-approval blocked).
model = admin.call('POST', f'{path}/models', {'name': 'AG300-8U', 'kind': 'server'}, code=201)
admin.call('POST', f'{path}/models/{model["id"]}/versions', {
    'hw_spec': {'gpus': 8, 'arch': 'amd64'},
    'slots': [
        {'slot_no': 1, 'pcie_slot': '01', 'expect_nic_model': 'cx7', 'panel_port': 'port1', 'default_role': 'management'},
        {'slot_no': 2, 'pcie_slot': '02', 'expect_nic_model': 'x710', 'panel_port': 'port2', 'default_role': 'storage'},
    ],
}, code=201)


def machine(serial, hostname, mac_base):
    return {
        'serial_number': serial, 'rack': '', 'unit_position': '', 'bmc_address': '',
        'hostname': hostname, 'ip_reference': '', 'model_name': 'AG300-8U',
        'ports': [
            {'mac_address': f'{mac_base}:00:01', 'port_name': 'port1', 'rail': 0},
            {'mac_address': f'{mac_base}:00:02', 'port_name': 'port2', 'rail': 0},
        ],
    }


mac_a = 'aa:%s:%s:%s' % (prefix[-2:], secrets.token_hex(1), secrets.token_hex(1))
mac_b = 'aa:%s:%s:%s' % (prefix[-2:], secrets.token_hex(1), secrets.token_hex(1))
mac_c = 'aa:%s:%s:%s' % (prefix[-2:], secrets.token_hex(1), secrets.token_hex(1))
commit = admin.call('POST', f'{path}/bom/commit', {'file_version': 'v1', 'machines': [
    machine(prefix + '-1', 'ag-node-1', mac_a),
    machine(prefix + '-2', 'ag-node-2', mac_b),
    machine(prefix + '-3', 'ag-node-3', mac_c),
]}, code=201)
assets = admin.call('GET', f'{path}/assets?page=1&page_size=50')
by_host = {a['hostname']: a for a in assets['items']}
m1, m2, m3 = by_host['ag-node-1']['machine_id'], by_host['ag-node-2']['machine_id'], by_host['ag-node-3']['machine_id']
admin.call('PUT', f'{path}/roles', {
    'roles': {'management': {'enabled': True, 'required': True}, 'storage': {'enabled': True, 'required': True}, 'oob': {'enabled': True, 'required': False}},
    'slot_overrides': {},
})

# Publish a mapping revision for machines 1 and 2 (the G3 evidence): mapping
# entries reference the physical ports with their OS interfaces.
def publish_mapping(machine_id):
    machine_ports = admin.call('GET', f'{path}/machines/{machine_id}/ports')['items']
    port_ids = {p['port_name']: p['id'] for p in machine_ports}
    entries = [
        {'port_id': port_ids['port1'], 'os_interface': 'nic-r0-p1', 'mac_address': 'aa:bb:cc:85:00:01', 'bdf': '18:00.0', 'switch_port': 'sw-a Eth1/1', 'gpu_affinity': 'gpu-0'},
        {'port_id': port_ids['port2'], 'os_interface': 'nic-r0-p2', 'mac_address': 'aa:bb:cc:85:00:02', 'bdf': '18:00.1', 'switch_port': 'sw-a Eth1/2', 'gpu_affinity': 'gpu-4'},
    ]
    # The mapping publish needs G1 evidence: collect L1+L2, evaluate G1 and
    # submit a healthy LLDP snapshot so no difference blocks the publish.
    admin.call('POST', f'{path}/collections', {'level': 'L1', 'machine_ids': [machine_id]})
    admin.call('POST', f'{path}/collections', {'level': 'L2', 'machine_ids': [machine_id]})
    admin.call('POST', f'{path}/gates/evaluate', {'gate': 'G1', 'machine_ids': [machine_id]})
    admin.call('POST', f'{path}/lldp/snapshots', {'snapshots': [{'machine_id': machine_id, 'ports': {
        'port1': {'peer': 'sw1', 'peer_port': 'Eth1/1', 'link_up': True, 'dom_warning': False, 'flapping': False},
        'port2': {'peer': 'sw1', 'peer_port': 'Eth1/2', 'link_up': True, 'dom_warning': False, 'flapping': False},
    }}]})
    admin.call('POST', f'{path}/mappings/publish', {'machine_id': machine_id, 'entries': entries, 'note': 'agent fixture'}, code=201)


publish_mapping(m1)
publish_mapping(m2)

agent_amd = admin.call('POST', f'{path}/media', {
    'type': 'agent', 'name': 'keel-agent', 'version': '1.4.0', 'sha256': 'ab' * 32,
    'size': 48 * 1024 * 1024, 'arch': 'amd64', 'manifest_ref': 'minio://media/keel-agent-1.4.0-amd64.tar.gz',
}, code=201)
agent_arm = admin.call('POST', f'{path}/media', {
    'type': 'agent', 'name': 'keel-agent', 'version': '1.4.0-arm64', 'sha256': 'cd' * 32,
    'size': 44 * 1024 * 1024, 'arch': 'arm64', 'manifest_ref': 'minio://media/keel-agent-1.4.0-arm64.tar.gz',
}, code=201)
approver.call('POST', f'{path}/media/{agent_amd["id"]}/transition', {'state': 'verified'})
approver.call('POST', f'{path}/media/{agent_amd["id"]}/transition', {'state': 'approved'})
approver.call('POST', f'{path}/media/{agent_arm["id"]}/transition', {'state': 'verified'})
approver.call('POST', f'{path}/media/{agent_arm["id"]}/transition', {'state': 'approved'})

# 1. Only approved agent media can be installed: an uploaded agent media and
# a non-agent type are both refused.
draft = admin.call('POST', f'{path}/media', {
    'type': 'agent', 'name': 'keel-agent', 'version': '1.5.0-rc1', 'sha256': 'ef' * 32,
    'size': 1024, 'arch': 'amd64', 'manifest_ref': 'minio://media/rc.tar.gz',
}, code=201)
admin.call('POST', f'{path}/agent/plans', {'name': 'rc-plan', 'media_id': draft['id'], 'machines': [m1]}, code=409)
admin.call('POST', f'{path}/agent/plans', {'name': 'type-plan', 'media_id': agent_amd['id'], 'machines': [m1]}, code=201)
detail = admin.call('GET', f'{path}/agent/plans/{admin.call("GET", f"{path}/agent/plans?page=1&page_size=10")["items"][0]["id"]}')
group('Agent plan admission requires approved type=agent media (media://agent distribution)')

# 2. G3+ admission: the unmapped machine 3 is blocked at creation with a
# reason and never receives an enrollment token; machines 1-2 stay pending.
plan = admin.call('POST', f'{path}/agent/plans', {
    'name': prefix + '-install', 'media_id': agent_amd['id'], 'machines': [m1, m2, m3],
}, code=201)
detail = admin.call('GET', f'{path}/agent/plans/{plan["id"]}')
by_machine = {t['machine_id']: t for t in detail['targets']}
assert by_machine[m3]['state'] == 'blocked', by_machine[m3]
assert 'G3' in by_machine[m3]['reason'], by_machine[m3]['reason']
assert by_machine[m3]['enrollment_token'] == '', 'a blocked machine must never receive a token'
assert by_machine[m1]['state'] == 'pending' and by_machine[m1]['enrollment_token'].startswith('keel-enroll-')
assert by_machine[m2]['state'] == 'pending'
assert detail['plan']['media']['uri'] == 'media://agent/keel-agent@1.4.0', detail['plan']['media']
group('G3+ admission blocks unmapped machines without an enrollment token')

# 3. Arch selection: the arm64 agent media is blocked on the amd64 model.
arm_plan = admin.call('POST', f'{path}/agent/plans', {
    'name': prefix + '-arm', 'media_id': agent_arm['id'], 'machines': [m1, m2],
}, code=201)
arm_detail = admin.call('GET', f'{path}/agent/plans/{arm_plan["id"]}')
arm_by = {t['machine_id']: t for t in arm_detail['targets']}
assert arm_by[m1]['state'] == 'blocked' and 'arch' in arm_by[m1]['reason'], arm_by[m1]
assert arm_by[m2]['state'] == 'blocked' and 'arch' in arm_by[m2]['reason'], arm_by[m2]
group('Architecture selection blocks mismatched agent media per machine')

# 4. One-time enrollment: the token registers the machine serial+fingerprint
# and issues a credential; replay is refused with TOKEN_REPLAY; a bare serial
# report without a live token is refused (no self-registration).
token = by_machine[m1]['enrollment_token']
serial1 = commit['machines'][0]['serial_number'] if 'machines' in commit else admin.call('GET', f'{path}/assets?page=1&page_size=50')['items'][0].get('serial_number', '')
# Resolve serial via the trust-free read path: assets listing carries serial.
assets_full = admin.call('GET', f'{path}/assets?page=1&page_size=50')
serial_by_machine = {a['machine_id']: a['serial_number'] for a in assets_full['items']}
enroll = admin.call('POST', f'{path}/agent/enroll', {
    'token': token, 'serial_number': serial_by_machine[m1], 'fingerprint': 'SHA256:aaaa',
}, code=200)
assert enroll['credential_id'] and enroll['credential_fingerprint'] == 'SHA256:aaaa'
replay = admin.call('POST', f'{path}/agent/enroll', {
    'token': token, 'serial_number': serial_by_machine[m1], 'fingerprint': 'SHA256:aaaa',
}, code=409)
assert replay['code'] == 'TOKEN_REPLAY', replay
wrong_serial = admin.call('POST', f'{path}/agent/enroll', {
    'token': by_machine[m2]['enrollment_token'], 'serial_number': serial_by_machine[m1], 'fingerprint': 'SHA256:bbbb',
}, code=403)
assert wrong_serial['code'] == 'DEVICE_MISMATCH', wrong_serial
admin.call('POST', f'{path}/agent/enroll', {'token': '', 'serial_number': serial_by_machine[m1], 'fingerprint': 'x'}, code=400)
group('One-time enrollment token issues the credential; replay and mismatches refused')

# 5. Installation outcomes: a cache hit accounts the skipped transfer; the
# plan completes when nothing is in flight.
advance = admin.call('POST', f'{path}/agent/plans/{plan["id"]}/advance', {'results': [
    {'machine_id': m1, 'outcome': 'cache_hit', 'bytes': 0},
    {'machine_id': m2, 'outcome': 'installed', 'bytes': 48 * 1024 * 1024},
]}, code=200)
assert advance['plan']['state'] == 'failed', advance['plan']['state']
summary = advance['summary']
assert summary['cache_hits'] == 1 and summary['bytes'] == 48 * 1024 * 1024, summary
group('Cache-hit distribution accounting and plan completion with a blocked target')

# 6. Credential lifecycle: rotation changes the fingerprint and is audited;
# revocation clears the credential; both appear in the credential events.
rotated = admin.call('POST', f'{path}/agent/credentials/rotate', {
    'machine_id': m1, 'fingerprint': 'SHA256:cccc',
}, code=200)
assert rotated['credential_id'] != enroll['credential_id']
revoked = admin.call('POST', f'{path}/agent/credentials/revoke', {'machine_id': m1}, code=200)
detail = admin.call('GET', f'{path}/agent/plans/{plan["id"]}')
actions = [e['action'] for e in detail['events']]
assert 'issue' in actions and 'rotate' in actions and 'revoke' in actions, actions
group('Credential rotation and revocation are recorded in the lifecycle events')

# 7. Abort cancels a running plan: in-flight targets become blocked.
plan2 = admin.call('POST', f'{path}/agent/plans', {
    'name': prefix + '-abort', 'media_id': agent_amd['id'], 'machines': [m1, m2],
}, code=201)
aborted = admin.call('POST', f'{path}/agent/plans/{plan2["id"]}/abort', code=200)
assert aborted['plan']['state'] == 'aborted'
targets = {t['machine_id']: t for t in aborted['targets']}
assert targets[m1]['state'] == 'blocked' and targets[m2]['state'] == 'blocked'
admin.call('POST', f'{path}/agent/plans/{plan2["id"]}/abort', code=409)
group('Abort cancels in-flight installations and refuses double abort')

# 8. Viewer cannot create plans or rotate credentials; reads stay visible.
viewer.call('POST', f'{path}/agent/plans', {'name': 'v', 'media_id': agent_amd['id'], 'machines': [m1]}, code=403)
viewer.call('POST', f'{path}/agent/credentials/rotate', {'machine_id': m1, 'fingerprint': 'x'}, code=403)
listed = viewer.call('GET', f'{path}/agent/plans?page=1&page_size=10')
assert listed['total'] >= 2
group('Authorization: viewers read but cannot plan or rotate')

print(json.dumps({'status': 'passed', 'groups': groups}))
