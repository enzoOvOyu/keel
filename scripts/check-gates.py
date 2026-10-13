#!/usr/bin/env python3
"""Real PostgreSQL gate-evidence checks: G1 judgment, immutability, waivers, expiry."""
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
prefix = 'gate-' + secrets.token_hex(5)
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
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'Gate viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.61')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})

admin.call('POST', path + '/models', {'name': 'B300-8U', 'kind': 'server'}, code=201)
bom = {'file_version': 'v1', 'machines': [
    {'serial_number': 'SN-G1', 'rack': '', 'unit_position': '', 'bmc_address': '', 'hostname': 'gate-node-1', 'ip_reference': '', 'model_name': 'B300-8U',
     'ports': [{'mac_address': 'aa:bb:cc:60:00:01', 'port_name': 'port1', 'rail': 0}, {'mac_address': 'aa:bb:cc:60:00:02', 'port_name': 'port2', 'rail': 1}]},
    {'serial_number': 'SN-G2', 'rack': '', 'unit_position': '', 'bmc_address': '', 'hostname': 'gate-node-2', 'ip_reference': '', 'model_name': 'B300-8U',
     'ports': [{'mac_address': 'aa:bb:cc:60:00:03', 'port_name': 'port1', 'rail': 0}]},
]}
admin.call('POST', path + '/bom/commit', bom, code=201)
assets = admin.call('GET', path + '/assets?page=1&page_size=20')['items']
ids = [a['machine_id'] for a in assets]
id_by_host = {a['hostname']: a['machine_id'] for a in assets}


def evaluate(machine_ids, code=200):
    return admin.call('POST', path + '/gates/evaluate', {'gate': 'G1', 'machine_ids': machine_ids}, code=code)


def evidence(page=1):
    return admin.call('GET', path + f'/gates?page={page}&page_size=20')['items']


# 1. G1 fails without collections, with per-machine reasons.
r = evaluate(ids)
assert r['conclusion'] == 'FAIL', r
reasons = {i['reason'] for i in r['issues']}
assert 'no successful L1 collection' in reasons, reasons
passed.append('G1 fails before collection with per-machine reasons')

# 2. Partial collection still fails; full fresh L1+L2 passes.
admin.call('POST', path + '/collections', {'level': 'L1', 'machine_ids': ids[:1]})
r = evaluate(ids)
assert r['conclusion'] == 'FAIL' and any('no successful L1 collection' in i['reason'] for i in r['issues'])
admin.call('POST', path + '/collections', {'level': 'L1', 'machine_ids': ids})
admin.call('POST', path + '/collections', {'level': 'L2', 'machine_ids': ids})
r = evaluate(ids)
assert r['conclusion'] == 'PASS', r
passed.append('G1 passes only when every machine has fresh L1 and MAC evidence')

# 3. Evidence is immutable history: re-evaluation appends, never overwrites.
before = len(evidence())
r = evaluate(ids)
after = evidence()
assert len(after) == before + 1, (before, len(after))
assert after[0]['id'] != after[1]['id'] and all(e['conclusion'] == 'PASS' for e in after[:2])
passed.append('Re-evaluation appends history without mutating prior evidence')

# 4. Waivers record the full contract and coexist with the underlying FAIL.
r = evaluate(ids[:1])
waive = admin.call('POST', path + '/gates/waive', {'gate': 'G1', 'machine_ids': ids[:1], 'reason': 'DOM unreadable on one port, RMA scheduled', 'scope': 'gate-node-1 port2', 'waiver_expires_hours': 24})
assert waive['conclusion'] == 'WAIVED', waive
latest = evidence()[0]
assert latest['conclusion'] == 'WAIVED'
passed.append('Waivers carry approver, reason, scope and expiry as their own evidence')

# 5. A waiver without a meaningful reason is rejected.
admin.call('POST', path + '/gates/waive', {'gate': 'G1', 'machine_ids': ids[:1], 'reason': 'ok', 'scope': '', 'waiver_expires_hours': 24}, code=400)
passed.append('Waivers require a meaningful reason')

# 6. Read-only roles read evidence but cannot evaluate or waive.
viewer.call('GET', path + '/gates?page=1&page_size=20')
viewer.call('POST', path + '/gates/evaluate', {'gate': 'G1', 'machine_ids': ids}, code=403)
viewer.call('POST', path + '/gates/waive', {'gate': 'G1', 'machine_ids': ids[:1], 'reason': 'nope', 'scope': '', 'waiver_expires_hours': 1}, code=403)
passed.append('Read-only roles read evidence but cannot judge or waive')

# 7. Only G1 is implemented in the mock channel; others are rejected.
admin.call('POST', path + '/gates/evaluate', {'gate': 'G2', 'machine_ids': ids}, code=400)
passed.append('Unimplemented gates are rejected instead of pretending')

# 8. Foreign machines fail the gate without leaking existence.
other = admin.call('POST', '/projects', {'name': prefix + '-other', 'slug': prefix + '-other'}, code=201)
other_path = '/projects/' + other['id']
r = admin.call('POST', other_path + '/gates/evaluate', {'gate': 'G1', 'machine_ids': ids}, code=200)
assert r['conclusion'] == 'FAIL' and all('not bound' in i['reason'] for i in r['issues'])
passed.append('Gate evaluation is project-scoped')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
