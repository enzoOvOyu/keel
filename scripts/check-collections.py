#!/usr/bin/env python3
"""Real PostgreSQL Redfish-collection checks: levels, revisions, failure classes, freshness."""
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
prefix = 'coll-' + secrets.token_hex(5)
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
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'Coll viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.61')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})

admin.call('POST', path + '/models', {'name': 'B300-8U', 'kind': 'server'}, code=201)


def machine(sn, host, mac):
    return {'serial_number': sn, 'rack': 'R05', 'unit_position': 'U1', 'bmc_address': '', 'hostname': host, 'ip_reference': '', 'model_name': 'B300-8U',
            'ports': [{'mac_address': mac, 'port_name': 'port1', 'rail': 0}]}


bom = {'file_version': 'v1', 'machines': [machine('SN-C1', 'coll-node-1', 'aa:bb:cc:40:00:01'), machine('SN-C2', 'coll-node-2', 'aa:bb:cc:40:00:02')]}
admin.call('POST', path + '/bom/commit', bom, code=201)
assets = admin.call('GET', path + '/assets?page=1&page_size=20')['items']
ids = [a['machine_id'] for a in assets]


def collect(level, machine_ids, inject='', code=200):
    return admin.call('POST', path + '/collections', {'level': level, 'machine_ids': machine_ids, 'inject_failure': inject}, code=code)


def matrix(level, page=1):
    return admin.call('GET', path + f'/collections?level={level}&page={page}&page_size=20')['items']


# 1. Uncollected machines surface as never_collected/unknown, not as PASS.
rows = matrix('L1')
assert len(rows) == 2 and all(r['status'] == 'never_collected' and r['freshness'] == 'unknown' for r in rows), rows
passed.append('Uncollected machines read as unknown, never as PASS')

# 2. L1 collection succeeds and the revision advances on re-collection.
r = collect('L1', ids)
assert all(x['status'] == 'success' and x['collection_revision'] == 1 for x in r['results']), r
r = collect('L1', ids)
assert all(x['status'] == 'success' and x['collection_revision'] == 2 for x in r['results']), r
rows = matrix('L1')
assert all(x['status'] == 'success' and x['freshness'] == 'fresh' and x['collection_revision'] == 2 for x in rows), rows
passed.append('Successful collections increment the revision and read fresh')

# 3. Level scope is independent: L2 stays unknown until collected.
rows = matrix('L2')
assert all(x['status'] == 'never_collected' for x in rows), rows
r = collect('L2', ids[:1])
assert r['results'][0]['status'] == 'success'
rows = matrix('L2')
by_host = {x['hostname']: x for x in rows}
assert by_host['coll-node-1']['status'] == 'success' and by_host['coll-node-2']['status'] == 'never_collected'
passed.append('Levels are independently scoped and scheduled')

# 4. Failure classification records the class and leaves revision untouched.
r = collect('L1', ids, inject='credential_expired')
assert all(x['status'] == 'failed' and x['failure_class'] == 'credential_expired' for x in r['results']), r
rows = matrix('L1')
assert all(x['status'] == 'failed' and x['freshness'] == 'unknown' for x in rows), rows
# The last successful revision is still recoverable by re-collecting.
r = collect('L1', ids)
assert all(x['status'] == 'success' and x['collection_revision'] == 3 for x in r['results']), r
passed.append('Failures are classified, retryable, and do not corrupt revisions')

# 5. Invalid levels and failure classes are rejected.
collect('L9', ids, code=400)
collect('L1', ids, inject='bogus', code=400)
passed.append('Invalid levels and failure classes are rejected')

# 6. Read-only roles see the matrix but cannot collect.
viewer.call('GET', path + '/collections?level=L1&page=1&page_size=20')
viewer.call('POST', path + '/collections', {'level': 'L1', 'machine_ids': ids}, code=403)
passed.append('Read-only roles read the matrix but cannot collect')

# 7. Foreign machines are refused without leaking existence.
other = admin.call('POST', '/projects', {'name': prefix + '-other', 'slug': prefix + '-other'}, code=201)
other_path = '/projects/' + other['id']
r = admin.call('POST', other_path + '/collections', {'level': 'L1', 'machine_ids': ids}, code=200)
assert all(x['status'] == 'failed' for x in r['results']), r
passed.append('Collections are project-scoped and refuse foreign machines')

# 8. The structured view carries level-appropriate detail.
r = collect('L3', ids[:1])
rows = matrix('L3')
detail = json.loads(rows[0]['structured'])
assert detail.get('processors') == 2 and detail.get('memory_dimms') == 16, detail
l1 = matrix('L1')
l1_detail = json.loads([x for x in l1 if x['hostname'] == 'coll-node-1'][0]['structured'])
assert 'processors' not in l1_detail, l1_detail
passed.append('L3 detail exceeds L1 per the level scope contract')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
