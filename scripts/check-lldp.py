#!/usr/bin/env python3
"""Real PostgreSQL LLDP/G2 checks: snapshot versioning, T1-T5 classification, targeted re-verification."""
import http.client
import http.cookies
import json
import os
import secrets
import urllib.parse
from pathlib import Path

file = Path(os.environ.get('KEEL_AUTH_BROWSER_CREDENTIALS_FILE', '/tmp/keel-auth-browser-credentials.json'))
assert file.stat().st_mode & 0o077 == 0, 'Private isolated credentials required'
credentials = json.loads(file.read_text())
base = urllib.parse.urlsplit(credentials['base_url'])
assert base.scheme == 'http' and base.hostname in ('127.0.0.1', 'localhost') and base.port == 3102, 'Isolated API only'
prefix = 'lldp-' + secrets.token_hex(5)
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
            status = response.status
            raw = response.read()
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
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'LLDP viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.61')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})

admin.call('POST', path + '/models', {'name': 'B300-8U', 'kind': 'server'}, code=201)


def machine(sn, host, macs):
    return {'serial_number': sn, 'rack': '', 'unit_position': '', 'bmc_address': '', 'hostname': host, 'ip_reference': '', 'model_name': 'B300-8U',
            'ports': [{'mac_address': m, 'port_name': f'port{i + 1}', 'rail': i} for i, m in enumerate(macs)]}


bom = {'file_version': 'v1', 'machines': [
    machine('SN-L1', 'lldp-node-1', ['aa:bb:cc:70:00:01', 'aa:bb:cc:70:00:02']),
    machine('SN-L2', 'lldp-node-2', ['aa:bb:cc:70:00:03', 'aa:bb:cc:70:00:04']),
]}
admin.call('POST', path + '/bom/commit', bom, code=201)
assets = admin.call('GET', path + '/assets?page=1&page_size=20')['items']
ids = {a['hostname']: a['machine_id'] for a in assets}


def snap(machine_id, ports):
    return {'machine_id': machine_id, 'ports': ports}


def healthy(peer='sw1', port='Eth1/1'):
    return {'peer': peer, 'peer_port': port, 'link_up': True, 'dom_warning': False, 'flapping': False}


def submit(snapshots, code=200):
    return admin.call('POST', path + '/lldp/snapshots', {'snapshots': snapshots}, code=code)


def differences(page=1):
    return admin.call('GET', path + f'/lldp/differences?page={page}&page_size=50')['items']


# 1. A healthy snapshot with no expectation mismatch records zero differences and revision 1.
r = submit([snap(ids['lldp-node-1'], {'port1': healthy(), 'port2': healthy(port='Eth1/2')})])
assert r['results'][0]['revision'] == 1 and r['results'][0]['new_differences'] == 0, r
passed.append('Healthy snapshots record no differences and start the revision chain')

# 2. T1..T4 classification per port state.
r = submit([snap(ids['lldp-node-2'], {
    'port1': {'peer': '', 'peer_port': '', 'link_up': False, 'dom_warning': False, 'flapping': False},  # T1
    'port2': {'peer': 'sw9', 'peer_port': 'Eth9/9', 'link_up': True, 'dom_warning': True, 'flapping': False},  # T3 wins over T2
})])
result = r['results'][0]
cats = {d['local_port']: d['category'] for d in result['differences']}
assert cats.get('port1') == 'T1' and cats.get('port2') == 'T3', cats
passed.append('Dead links read T1 and DOM warnings read T3')

# 3. Flapping (T4) and full-down machines escalate to T5 NIC failure.
r = submit([snap(ids['lldp-node-2'], {
    'port1': {'peer': 'sw1', 'peer_port': 'Eth1/1', 'link_up': True, 'dom_warning': False, 'flapping': True},
    'port2': {'peer': 'sw1', 'peer_port': 'Eth1/2', 'link_up': True, 'dom_warning': False, 'flapping': True},
})])
cats = {d['local_port']: d['category'] for d in r['results'][0]['differences']}
assert cats.get('port1') == 'T4' and cats.get('port2') == 'T4', cats
r = submit([snap(ids['lldp-node-2'], {
    'port1': {'peer': '', 'peer_port': '', 'link_up': False, 'dom_warning': False, 'flapping': False},
    'port2': {'peer': '', 'peer_port': '', 'link_up': False, 'dom_warning': False, 'flapping': False},
})])
cats = [d['category'] for d in r['results'][0]['differences']]
assert cats == ['T5', 'T5'], cats
passed.append('Flapping reads T4 and an all-ports-down machine escalates to T5')

# 4. T2 requires a BOM peer expectation; the mock BOM has none, so a healthy
#    neighbor with a different peer is NOT a difference.
r = submit([snap(ids['lldp-node-1'], {'port1': healthy(peer='sw-other', port='Eth7/7'), 'port2': healthy()})])
assert r['results'][0]['new_differences'] == 0, r
passed.append('Healthy neighbors never classify as T2 without a BOM peer expectation')

# 5. Targeted re-verification resolves prior differences with a note.
before = differences()
assert before and any(d.get('resolved_at') is None for d in before), before[:2]
r = submit([snap(ids['lldp-node-2'], {'port1': healthy(), 'port2': healthy(port='Eth1/2')})])
assert r['results'][0]['resolved'] >= 1, r
after = differences()
resolved = [d for d in after if d['resolved_at']]
assert resolved and all('resolved by re-run' in d['resolution_note'] for d in resolved), resolved[:1]
passed.append('Re-runs close non-reproduced differences with a resolution note')

# 6. History stays visible: resolved differences are never deleted.
all_rows = differences()
assert len(all_rows) >= 5, len(all_rows)
assert any(d['resolved_at'] for d in all_rows)
passed.append('Resolved differences stay in history for audit')

# 7. Read-only roles read differences but cannot submit snapshots.
viewer.call('GET', path + '/lldp/differences?page=1&page_size=20')
viewer.call('POST', path + '/lldp/snapshots', {'snapshots': [snap(ids['lldp-node-1'], {})]}, code=403)
passed.append('Read-only roles read differences but cannot submit snapshots')

# 8. Foreign machines are refused.
other = admin.call('POST', '/projects', {'name': prefix + '-other', 'slug': prefix + '-other'}, code=201)
other_path = '/projects/' + other['id']
admin.call('POST', other_path + '/lldp/snapshots', {'snapshots': [snap(ids['lldp-node-1'], {})]}, code=409)
passed.append('Snapshots are project-scoped and refuse foreign machines')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
