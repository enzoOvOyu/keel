#!/usr/bin/env python3
"""FR-4.8 PCIe topology: four-dimension judge, resolution, escalation, guards."""
import http.client, http.cookies, json, os, secrets, urllib.parse
from pathlib import Path

file = Path(os.environ.get('KEEL_AUTH_BROWSER_CREDENTIALS_FILE', '/tmp/keel-auth-browser-credentials.json'))
assert file.stat().st_mode & 0o077 == 0, 'Private isolated credentials required'
credentials = json.loads(file.read_text())
base = urllib.parse.urlsplit(credentials['base_url'])
assert base.scheme == 'http' and base.hostname in ('127.0.0.1', 'localhost') and base.port == 3102, 'Isolated API only'
prefix = 'pc-' + secrets.token_hex(5)
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

viewer_name = prefix + '-viewer'
viewer_password = secrets.token_urlsafe(18)
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'PC viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.61')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})

admin.call('POST', path + '/models', {'name': 'B300-8U', 'kind': 'server'}, code=201)
bom = {'file_version': 'v1', 'machines': [
    {'serial_number': 'SN-PC1', 'rack': '', 'unit_position': '', 'bmc_address': '', 'hostname': 'pcie-node-1', 'ip_reference': '', 'model_name': 'B300-8U',
     'ports': [{'mac_address': 'aa:bb:cc:e0:00:01', 'port_name': 'port1', 'rail': 0}]},
]}
admin.call('POST', path + '/bom/commit', bom, code=201)
mid = admin.call('GET', path + '/assets?page=1&page_size=20')['items'][0]['machine_id']

EXPECTED = {'slot_by_role': {'gpu:0': 'slot-3', 'nic:0': 'slot-7'}, 'gpu_numa_group': [0, 1], 'nvlink_pairs': ['0-1', '0-3']}
GOOD_RAW = chr(10).join(['GPU0	NV18	NV18	NV18', 'GPU1	NV18	X	NV18', 'GPU2	NV18	NV18	X', 'GPU3	NV18	NV18	X'])


def device(role, index, bdf, upstream, numa, cap, sta, slot):
    return {'role': role, 'index': index, 'bdf': bdf, 'upstream': upstream, 'numa_node': numa, 'lnk_cap_width': cap, 'lnk_sta_width': sta, 'slot': slot}


def submit(devices, raw=GOOD_RAW, expected=EXPECTED, code=200):
    return admin.call('POST', path + '/pcie/snapshots', {'machine_id': mid, 'devices': devices, 'expected': expected, 'nvlink_raw': raw}, code=code)


def diffs(machine_id=mid):
    return admin.call('GET', path + f'/pcie/diffs?machine_id={machine_id}&page=1&page_size=50')['items']


# 1. A conforming machine records zero diffs.
r = submit([device('gpu', 0, '18:00.0', ['88:00.0', 'slot-3'], 0, 16, 16, 'slot-3'), device('nic', 0, '98:00.0', ['a8:00.0', 'slot-7'], 0, 16, 16, 'slot-7')])
assert r['revision'] == 1 and r['diffs'] == [], r
passed.append('A conforming topology records no diffs')

# 2. All four dimensions classify: wrong slot root, bad NUMA, missing NVLink pair, degraded width.
r = submit([
    device('gpu', 0, '18:00.0', ['88:00.0', 'slot-9'], 3, 16, 16, 'slot-9'),   # slot_path + numa
    device('nic', 0, '98:00.0', ['a8:00.0', 'slot-7'], 0, 16, 8, 'slot-7'),    # link_width
], raw=chr(10).join(['GPU0	NV18	NV18	NV18', 'GPU1	NV18	X	NV18', 'GPU2	NV18	NV18	X', 'GPU3	X	X	X']))  # nvlink 0-3 missing
dims = sorted(d['dimension'] for d in r['diffs'])
assert dims == ['link_width', 'numa', 'nvlink', 'slot_path'], dims
passed.append('The judge classifies slot paths, NUMA, NVLink and link widths')

# 3. Two consecutive failed repairs escalate; a clean pass resolves and resets.
r = submit([device('gpu', 0, '18:00.0', ['88:00.0', 'slot-9'], 3, 16, 16, 'slot-9'), device('nic', 0, '98:00.0', ['a8:00.0', 'slot-7'], 0, 16, 8, 'slot-7')], raw=GOOD_RAW)
assert r['repair_count'] == 2 and r['escalated'] is True, r
history = diffs()
assert history and all(h['escalated'] for h in history if h.get('resolved_at') is None), history[:2]
r = submit([device('gpu', 0, '18:00.0', ['88:00.0', 'slot-3'], 0, 16, 16, 'slot-3'), device('nic', 0, '98:00.0', ['a8:00.0', 'slot-7'], 0, 16, 16, 'slot-7')])
assert r['diffs'] == [], r
history = diffs()
assert history and all(h.get('resolved_at') for h in history), history[:2]
assert all(h['repair_count'] == 0 for h in history), history[:2]
passed.append('Two failed repairs escalate; a clean pass resolves and resets')

# 4. Members read diffs; submission is admin-only; foreign machines refuse.
viewer.call('GET', path + f'/pcie/diffs?machine_id={mid}&page=1&page_size=20')
viewer.call('POST', path + '/pcie/snapshots', {'machine_id': mid, 'devices': [], 'expected': {}}, code=403)
other = admin.call('POST', '/projects', {'name': prefix + '-other', 'slug': prefix + '-other'}, code=201)
admin.call('POST', '/projects/' + other['id'] + '/pcie/snapshots', {'machine_id': mid, 'devices': [device('gpu', 0, '18:00.0', ['slot-3'], 0, 16, 16, 'slot-3')], 'expected': EXPECTED}, code=409)
passed.append('Topology submission is project-scoped and role-guarded')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
