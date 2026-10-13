#!/usr/bin/env python3
"""Real PostgreSQL scan-import checks: auto model creation, draft idempotency, ALREADY_CURRENT, discard isolation."""
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
prefix = 'model-import-' + secrets.token_hex(5)
passed = []


class Client:
    def __init__(self, ip='127.0.0.60'):
        self.cookies = {}
        self.csrf = self._csrf = None
        self.ip = ip

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

# Provision a Viewer member to prove the read-only boundary.
viewer_name = prefix + '-viewer'
viewer_password = secrets.token_urlsafe(18)
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'Import viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.61')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})


def scan_payload(host, slots=2, spec='v1'):
    return {
        'source_host': host,
        'hw_spec': {'vendor': 'NVIDIA', 'model': 'B300-8U', 'kind': 'server', 'spec': spec},
        'slots': [
            {'slot_no': i + 1, 'pcie_slot': f'{i + 1:02d}', 'expect_nic_model': 'cx7', 'panel_port': f'p{i + 1}', 'default_role': 'compute'}
            for i in range(slots)
        ],
    }


# 1. Import creates the model and a draft from one server scan.
first = admin.call('POST', path + '/models/import', scan_payload('poc-node-01'), code=201)
assert first['model_name'] == 'B300-8U' and first['source_host'] == 'poc-node-01', first
assert first['content_hash'] and first['hw_spec']['vendor'] == 'NVIDIA', first
model_id = first['model_id']
passed.append('Scan import auto-creates the model and returns a draft')

# 2. Re-importing the identical inventory returns the same draft (idempotent).
second = admin.call('POST', path + '/models/import', scan_payload('poc-node-01'), code=201)
assert second['id'] == first['id'] and second['content_hash'] == first['content_hash']
passed.append('Re-importing an identical inventory reuses the existing draft')

# 3. A different inventory creates a second draft on the same model.
variant = admin.call('POST', path + '/models/import', scan_payload('poc-node-02', spec='v2'), code=201)
assert variant['model_id'] == model_id and variant['id'] != first['id']
passed.append('A changed inventory opens a distinct draft on the same model')

# 4. Publishing the draft content then re-importing it yields ALREADY_CURRENT.
publish = admin.call('POST', f'{path}/models/{model_id}/versions', {
    'hw_spec': scan_payload('poc-node-05')['hw_spec'],
    'slots': scan_payload('poc-node-05')['slots'],
}, code=201)
assert publish['version'] == 1, publish
again = admin.call('POST', path + '/models/import', scan_payload('poc-node-03'), code=409)
assert again['code'] == 'ALREADY_CURRENT', again
passed.append('Importing content identical to the latest published version is rejected as current')

# 5. Drafts list newest-first with model names.
drafts = admin.call('GET', f'{path}/models/{model_id}/drafts', code=200)['items']
assert [d['id'] for d in drafts] == [variant['id'], first['id']], drafts
assert all(d['model_name'] == 'B300-8U' for d in drafts)
passed.append('Drafts list newest-first with the owning model')

# 6. Discard removes one draft; the other survives; published versions untouched.
admin.call('POST', f"{path}/models/{model_id}/drafts/{first['id']}/discard", code=200)
drafts = admin.call('GET', f'{path}/models/{model_id}/drafts', code=200)['items']
assert [d['id'] for d in drafts] == [variant['id']]
versions = admin.call('GET', f'{path}/models/{model_id}/versions', code=200)['items']
assert len(versions) == 1 and versions[0]['version'] == 1
passed.append('Discard removes only the targeted draft and never touches published versions')

# 7. Viewer (model:read) lists drafts but cannot import or discard.
viewer.call('GET', f'{path}/models/{model_id}/drafts', code=200)
assert viewer.call('POST', path + '/models/import', scan_payload('poc-node-09'), code=403)['code'] == 'FORBIDDEN'
viewer.call('POST', f"{path}/models/{model_id}/drafts/{variant['id']}/discard", code=403)
passed.append('Read-only roles list drafts but cannot import or discard')

# 8. Cross-project isolation: another project never sees this model or drafts.
other = admin.call('POST', '/projects', {'name': prefix + '-other', 'slug': prefix + '-other'}, code=201)
other_path = '/projects/' + other['id']
admin.call('GET', f'{other_path}/models/{model_id}/drafts', code=404)
admin.call('POST', f"{other_path}/models/{model_id}/drafts/{variant['id']}/discard", code=404)
foreign = admin.call('POST', other_path + '/models/import', scan_payload('poc-node-04'), code=201)
assert foreign['model_id'] != model_id
passed.append('Drafts stay project-scoped; foreign projects get their own model identity')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
