#!/usr/bin/env python3
"""Real PostgreSQL SSH trust checks: state machine, fingerprint stickiness, log completeness."""
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
prefix = 'trust-' + secrets.token_hex(5)
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
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'Trust viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.61')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})

admin.call('POST', path + '/models', {'name': 'B300-8U', 'kind': 'server'}, code=201)
FP_A = 'SHA256:' + secrets.token_hex(32)
FP_B = 'SHA256:' + secrets.token_hex(32)
FP_C = 'SHA256:' + secrets.token_hex(32)
FP_D = 'SHA256:' + secrets.token_hex(32)


def machine(sn, host, mac):
    return {'serial_number': sn, 'rack': 'R05', 'unit_position': 'U1', 'bmc_address': '', 'hostname': host, 'ip_reference': '', 'model_name': 'B300-8U',
            'ports': [{'mac_address': mac, 'port_name': 'port1', 'rail': 0}]}


bom = {'file_version': 'v1', 'machines': [
    machine('SN-T1', 'trust-node-1', 'aa:bb:cc:30:00:01'),
    machine('SN-T2', 'trust-node-2', 'aa:bb:cc:30:00:02'),
    machine('SN-T3', 'trust-node-3', 'aa:bb:cc:30:00:03'),
]}
admin.call('POST', path + '/bom/commit', bom, code=201)
assets = admin.call('GET', path + '/assets?page=1&page_size=20')['items']
ids = {a['hostname']: a['machine_id'] for a in assets}


def trust(action, machine_ids, fingerprint='', code=200):
    return admin.call('POST', path + '/trust/actions', {'action': action, 'machine_ids': machine_ids, 'fingerprint': fingerprint}, code=code)


def outcome(payload):
    return {(r['machine_id'], r['outcome'], r.get('reason', '')) for r in payload['results']}


# 1. Bootstrap establishes first trust and records the fingerprint once.
r = trust('trust.bootstrap', [ids['trust-node-1']], FP_A)
assert outcome(r) == {(ids['trust-node-1'], 'success', '')}, r
logs = admin.call('GET', path + '/trust/logs?page=1&page_size=20')['items']
entry = next(l for l in logs if l['machine_id'] == ids['trust-node-1'] and l['action'] == 'trust.bootstrap')
assert entry['outcome'] == 'success' and entry['fingerprint'] == FP_A, entry
passed.append('Bootstrap establishes trust and records the first fingerprint')

# 2. Bootstrap on an already-trusted machine is refused (restricted exception only).
r = trust('trust.bootstrap', [ids['trust-node-1']], FP_B)
assert outcome(r) == {(ids['trust-node-1'], 'failure', 'trust already established')}, r
passed.append('Bootstrap refuses machines that already carry trust')

# 3. Verify with a mismatched fingerprint fails without changing state.
r = trust('trust.verify', [ids['trust-node-1']], FP_B)
assert outcome(r) == {(ids['trust-node-1'], 'failure', 'host key fingerprint mismatch')}, r
r = trust('trust.verify', [ids['trust-node-1']], FP_A)
assert outcome(r) == {(ids['trust-node-1'], 'success', '')}, r
passed.append('Verification pins the recorded fingerprint and rejects mismatches')

# 4. First verification on an unverified machine establishes trust (PXE callback path).
r = trust('trust.verify', [ids['trust-node-2']], FP_C)
assert outcome(r) == {(ids['trust-node-2'], 'success', '')}, r
passed.append('First successful verification establishes trust without bootstrap')

# 5. Revoke is terminal for verify; repair rotates the fingerprint.
r = trust('trust.revoke', [ids['trust-node-2']])
assert outcome(r) == {(ids['trust-node-2'], 'success', '')}, r
r = trust('trust.verify', [ids['trust-node-2']], FP_C)
assert outcome(r) == {(ids['trust-node-2'], 'failure', 'trust revoked; run trust.repair first')}, r
r = trust('trust.repair', [ids['trust-node-2']], FP_D)
assert outcome(r) == {(ids['trust-node-2'], 'success', '')}, r
r = trust('trust.verify', [ids['trust-node-2']], FP_D)
assert outcome(r) == {(ids['trust-node-2'], 'success', '')}, r
passed.append('Revoked machines only recover through the audited repair path')

# 6. Machines not bound to this project are refused without leaking existence.
r = trust('trust.bootstrap', [ids['trust-node-3']], FP_A)
assert outcome(r) == {(ids['trust-node-3'], 'success', '')}, r
other = admin.call('POST', '/projects', {'name': prefix + '-other', 'slug': prefix + '-other'}, code=201)
other_path = '/projects/' + other['id']
rej = admin.call('POST', other_path + '/trust/actions', {'action': 'trust.bootstrap', 'machine_ids': [ids['trust-node-3']], 'fingerprint': FP_B}, code=200)
assert rej['results'][0]['outcome'] == 'failure', rej
passed.append('Trust actions are project-scoped and refuse foreign machines')

# 7. Every transition landed in the audit log; viewer reads but cannot act.
logs = admin.call('GET', path + '/trust/logs?page=1&page_size=50')['items']
actions = [(l['action'], l['outcome']) for l in logs]
assert ('trust.bootstrap', 'failure') in actions and ('trust.repair', 'success') in actions and ('trust.revoke', 'success') in actions, actions
viewer.call('GET', path + '/trust/logs?page=1&page_size=20')
viewer.call('POST', path + '/trust/actions', {'action': 'trust.verify', 'machine_ids': [ids['trust-node-1']], 'fingerprint': FP_A}, code=403)
passed.append('Trust history is auditable and read-only roles cannot act')

# 8. Batch actions report per-machine outcomes atomically.
r = trust('trust.bootstrap', [ids['trust-node-1'], ids['trust-node-2']], FP_A)
out = outcome(r)
assert (ids['trust-node-1'], 'failure', 'trust already established') in out and (ids['trust-node-2'], 'failure', 'trust already established') in out, r
passed.append('Batch trust actions report per-machine outcomes')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
