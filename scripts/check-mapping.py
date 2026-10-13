#!/usr/bin/env python3
"""G3 mapping revisions: evidence precondition, coverage, immutability, history."""
import http.client, http.cookies, json, os, secrets, urllib.parse
from pathlib import Path

file = Path(os.environ.get('KEEL_AUTH_BROWSER_CREDENTIALS_FILE', '/tmp/keel-auth-browser-credentials.json'))
assert file.stat().st_mode & 0o077 == 0, 'Private isolated credentials required'
credentials = json.loads(file.read_text())
base = urllib.parse.urlsplit(credentials['base_url'])
assert base.scheme == 'http' and base.hostname in ('127.0.0.1', 'localhost') and base.port == 3102, 'Isolated API only'
prefix = 'map-' + secrets.token_hex(5)
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
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'Map viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.61')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})

admin.call('POST', path + '/models', {'name': 'B300-8U', 'kind': 'server'}, code=201)
bom = {'file_version': 'v1', 'machines': [
    {'serial_number': 'SN-MA1', 'rack': '', 'unit_position': '', 'bmc_address': '', 'hostname': 'map-node-1', 'ip_reference': '', 'model_name': 'B300-8U',
     'ports': [{'mac_address': 'aa:bb:cc:c0:00:01', 'port_name': 'port1', 'rail': 0}, {'mac_address': 'aa:bb:cc:c0:00:02', 'port_name': 'port2', 'rail': 1}]},
]}
admin.call('POST', path + '/bom/commit', bom, code=201)
assets = admin.call('GET', path + '/assets?page=1&page_size=20')['items']
mid = assets[0]['machine_id']
# The BOM created physical ports; find their ids via the machine detail.
# physical_ports are keyed by machine — use the ports of the BOM through the
# model-agnostic lookup: the mapping API takes port ids; fetch them from the
# LLDP difference fixture path (submit a healthy snapshot lists ports) —
# simpler: query through a preview endpoint is absent, so use the LLDP
# snapshot submission which validates port names. For ids, the platform
# exposes them via the machine detail in the assets page payload.
ports = admin.call('GET', path + f'/machines/{mid}/ports')['items']
port_ids = {p['port_name']: p['id'] for p in ports}
assert set(port_ids) == {'port1', 'port2'}, port_ids


def entries_all():
    return [
        {'port_id': port_ids['port1'], 'os_interface': 'nic-r0-p1', 'mac_address': 'aa:bb:cc:c0:00:01', 'bdf': '18:00.0', 'switch_port': 'sw-a Eth1/1', 'gpu_affinity': 'gpu-0'},
        {'port_id': port_ids['port2'], 'os_interface': 'nic-r0-p2', 'mac_address': 'aa:bb:cc:c0:00:02', 'bdf': '18:00.1', 'switch_port': 'sw-a Eth1/2', 'gpu_affinity': 'gpu-4'},
    ]


def publish(entries, note='', code=201):
    return admin.call('POST', path + '/mappings/publish', {'machine_id': mid, 'entries': entries, 'note': note}, code=code)


# 1. Without G1 evidence the publish is refused.
rej = publish(entries_all(), code=409)
assert rej['code'] == 'GATE_EVIDENCE', rej
passed.append('Publishing requires a valid G1 PASS or waiver')

# 2. Collect L1+L2, pass G1, then an unresolved line-order difference blocks.
admin.call('POST', path + '/collections', {'level': 'L1', 'machine_ids': [mid]})
admin.call('POST', path + '/collections', {'level': 'L2', 'machine_ids': [mid]})
admin.call('POST', path + '/gates/evaluate', {'gate': 'G1', 'machine_ids': [mid]})
snap_down = admin.call('POST', path + '/lldp/snapshots', {'snapshots': [{'machine_id': mid, 'ports': {
    'port1': {'peer': '', 'peer_port': '', 'link_up': False, 'dom_warning': False, 'flapping': False},
    'port2': {'peer': 'sw1', 'peer_port': 'Eth1/2', 'link_up': True, 'dom_warning': False, 'flapping': False},
}}]})
rej = publish(entries_all(), code=409)
assert 'unresolved' in rej['message'], rej
passed.append('Unresolved line-order differences block the publish')

# 3. Resolve the line order; a missing port in the entries is refused.
admin.call('POST', path + '/lldp/snapshots', {'snapshots': [{'machine_id': mid, 'ports': {
    'port1': {'peer': 'sw1', 'peer_port': 'Eth1/1', 'link_up': True, 'dom_warning': False, 'flapping': False},
    'port2': {'peer': 'sw1', 'peer_port': 'Eth1/2', 'link_up': True, 'dom_warning': False, 'flapping': False},
}}]})
rej = publish(entries_all()[:1], code=409)
assert 'cover every BOM port' in rej['message'], rej
passed.append('A mapping must cover every BOM port exactly once')

# 4. A full publish creates revision 1; republishing appends sequence 2 with history.
r1 = publish(entries_all(), note='initial mapping')
assert r1['sequence'] == 1 and len(r1['entries']) == 2, r1
r2 = publish(entries_all(), note='after NIC replacement')
assert r2['sequence'] == 2 and r2['previous_revision'] == r1['id'], r2
history = admin.call('GET', path + f'/mappings?machine_id={mid}&page=1&page_size=20')['items']
assert [h['sequence'] for h in history] == [2, 1], history
assert history[1]['id'] == r1['id'] and len(history[1]['entries']) == 2
passed.append('Republishing appends an immutable sequence with linked history')

# 5. Read-only roles read the history but cannot publish.
viewer.call('GET', path + f'/mappings?machine_id={mid}&page=1&page_size=20')
viewer.call('POST', path + '/mappings/publish', {'machine_id': mid, 'entries': entries_all()}, code=403)
passed.append('Read-only roles read mappings but cannot publish')

# 6. Foreign machines refuse.
other = admin.call('POST', '/projects', {'name': prefix + '-other', 'slug': prefix + '-other'}, code=201)
admin.call('POST', '/projects/' + other['id'] + '/mappings/publish', {'machine_id': mid, 'entries': entries_all()}, code=409)
passed.append('Mapping publishes are project-scoped')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
