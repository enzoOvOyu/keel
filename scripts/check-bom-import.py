#!/usr/bin/env python3
"""Real PostgreSQL BOM import checks: preview classification, idempotent commit, model binding, isolation."""
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
prefix = 'bom-' + secrets.token_hex(5)
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
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'BOM viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.61')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})

# Register the model that BOM rows will bind to (project scope).
model = admin.call('POST', path + '/models', {'name': 'B300-8U', 'kind': 'server'}, code=201)


def machine(sn, host, macs, model_name='B300-8U'):
    return {
        'serial_number': sn, 'rack': 'R05', 'unit_position': 'U12', 'bmc_address': '10.0.0.9',
        'hostname': host, 'ip_reference': '10.1.1.10', 'model_name': model_name,
        'ports': [{'mac_address': m, 'port_name': f'port{i + 1}', 'rail': i % 4} for i, m in enumerate(macs)],
    }


def bom(file_version, machines):
    return {'file_version': file_version, 'machines': machines}


mac_a = 'aa:bb:cc:00:00:01'
mac_a2 = 'AA:BB:CC:00:00:02'  # normalized-equal variants must not duplicate
mac_b = 'aa:bb:cc:00:00:03'

# 1. Preview classifies file-level conflicts without writing.
bad = bom('v-bad', [
    machine('SN-001', 'node-01', [mac_a]),
    machine('SN-001', 'node-02', [mac_b]),            # duplicate SN in file
    machine('SN-bad!', 'node-03', []),                 # invalid SN + no ports
    machine('SN-004', 'Node-Bad', [mac_a]),           # invalid hostname + duplicate mac in file
])
preview = admin.call('POST', path + '/bom/preview', bad)
reasons = sorted({i['reason'] for i in preview['issues'] if i['reason']})
assert 'duplicate_serial_in_file' in reasons and 'invalid_serial' in reasons, reasons
port_reasons = {p['reason'] for i in preview['issues'] for p in i.get('ports', [])}
assert 'mac_assigned_to_other_machine_in_file' in port_reasons, port_reasons
assets = admin.call('GET', path + '/assets?page=1&page_size=20')
assert assets['items'] == [], 'preview must not write'
passed.append('Preview classifies SN/hostname/MAC conflicts without writing')

# 2. Commit a clean batch; machines, ports and assets materialize with model binding.
good = bom('v1', [machine('SN-100', 'node-100', [mac_a, mac_a2]), machine('SN-101', 'node-101', [mac_b])])
result = admin.call('POST', path + '/bom/commit', good, code=201)
assert result['machines_created'] == 2 and result['ports_created'] == 3 and result['assets_created'] == 2, result
assets = admin.call('GET', path + '/assets?page=1&page_size=20')
assert len(assets['items']) == 2 and all(a['model_name'] == 'B300-8U' for a in assets['items']), assets
assert all(a['state'] == 'planned' and a['version'] == 1 for a in assets['items'])
passed.append('Commit creates machines, ports and model-bound assets')

# 3. Re-committing the same file version is rejected (idempotent by version).
rej = admin.call('POST', path + '/bom/commit', good, code=409)
assert rej['code'] == 'BOM_VERSION_EXISTS', rej
passed.append('Duplicate file version commits are rejected')

# 4. A new file version updates existing assets instead of duplicating them.
updated = bom('v2', [machine('SN-100', 'node-100', [mac_a, mac_a2]), machine('SN-101', 'node-102', [mac_b])])
result2 = admin.call('POST', path + '/bom/commit', updated, code=201)
assert result2['machines_created'] == 0 and result2['assets_updated'] == 2, result2
assets = admin.call('GET', path + '/assets?page=1&page_size=20')
assert len(assets['items']) == 2
by_host = {a['hostname']: a for a in assets['items']}
assert by_host['node-102']['version'] == 2, by_host
passed.append('Re-imported machines update assets in place without duplicating identity')

# 5. MAC reuse across machines is rejected at commit (database-level uniqueness).
steal = bom('v-steal', [machine('SN-200', 'node-200', [mac_a])])
rej2 = admin.call('POST', path + '/bom/commit', steal, code=409)
assert rej2['code'] == 'BOM_VALIDATION_FAILED', rej2
passed.append('MAC uniqueness is enforced across machines at commit')

# 6. Unknown model names surface as preview issues; commits refuse them.
unknown = bom('v-unk', [machine('SN-300', 'node-300', ['aa:bb:cc:00:00:10'], model_name='Does-Not-Exist')])
preview2 = admin.call('POST', path + '/bom/preview', unknown)
assert any(i['reason'].startswith('unknown_model:') for i in preview2['issues']), preview2
rej3 = admin.call('POST', path + '/bom/commit', unknown, code=409)
assert rej3['code'] == 'BOM_VALIDATION_FAILED', rej3
passed.append('Unknown model names are rejected in preview and commit')

# 7. Viewer can list assets but cannot preview or commit.
viewer.call('GET', path + '/assets?page=1&page_size=20')
rej_preview = viewer.call('POST', path + '/bom/preview', good, code=403)
assert rej_preview['code'] == 'FORBIDDEN', rej_preview
viewer.call('POST', path + '/bom/commit', good, code=403)
passed.append('Read-only roles list assets but cannot import')

# 8. Machine identity is global; a second project reuses the machine but owns its own asset.
other = admin.call('POST', '/projects', {'name': prefix + '-other', 'slug': prefix + '-other'}, code=201)
other_path = '/projects/' + other['id']
other_model = admin.call('POST', other_path + '/models', {'name': 'B300-8U', 'kind': 'server'}, code=201)
shared = bom('v1', [machine('SN-100', 'node-900', [mac_a, mac_a2])])
result3 = admin.call('POST', other_path + '/bom/commit', shared, code=201)
assert result3['machines_created'] == 0 and result3['assets_created'] == 1, result3
other_assets = admin.call('GET', other_path + '/assets?page=1&page_size=20')
assert len(other_assets['items']) == 1 and other_assets['items'][0]['hostname'] == 'node-900'
mine = admin.call('GET', path + '/assets?page=1&page_size=20')
assert len(mine['items']) == 2, 'the first project keeps its own assets untouched'
passed.append('Machine identity is global while assets stay project-scoped')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
