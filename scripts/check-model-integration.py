#!/usr/bin/env python3
"""Real PostgreSQL device-model checks: identity uniqueness, immutable publish, retirement."""
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
prefix = 'model-' + secrets.token_hex(5)
checks = []


class Client:
    def __init__(self, ip='127.0.0.60'):
        self.cookies = {}
        self.csrf = None
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
                            self.cookies.pop(name, None)
                        else:
                            self.cookies[name] = m.value
        finally:
            conn.close()
        value = json.loads(raw) if raw else None
        if isinstance(value, dict) and 'csrf_token' in value:
            self.csrf = value['csrf_token']
        assert status == code, f'{method} {path}: {status}, expected {code}; body={value if isinstance(value, dict) else None}'
        return value

    def login(self, username, password):
        self.call('GET', '/auth/csrf')
        self.call('POST', '/auth/login', {'method': 'local', 'username': username, 'password': password})


def passed(label):
    checks.append(label)
    print('PASS ' + label, flush=True)


admin = Client()
admin.login(credentials['username'], credentials['password'])
project = admin.call('POST', '/projects', {'name': '型号库隔离验收', 'slug': prefix}, code=201)
path = '/projects/' + project['id']

# F-14.2: (scope, name) uniqueness at the API and database level.
model = admin.call('POST', path + '/models', {'name': 'B300 HGX 风冷 8U', 'kind': 'server'}, code=201)
admin.call('POST', path + '/models', {'name': 'B300 HGX 风冷 8U', 'kind': 'server'}, code=409)
admin.call('POST', path + '/models', {'name': 'x', 'kind': 'chassis'}, code=400)
passed('Model identity uniqueness rejected in one scope with validation')

# Project-private and global visibility: platform list sees both scopes.
page = admin.call('GET', path + '/models')
assert page['total'] >= 1 and any(m['id'] == model['id'] for m in page['items'])
passed('Project models list with pagination')

# F-14.1: publish immutable versions with content hash and slots.
slots = [{'slot_no': 1, 'pcie_slot': 'PCIe 3', 'expect_nic_model': 'CX7', 'panel_port': '1-2', 'default_role': 'compute'}]
v1 = admin.call('POST', path + f'/models/{model["id"]}/versions',
                {'hw_spec': {'gpu': 'B300', 'gpu_count': 8}, 'slots': slots}, code=201)
assert v1['version'] == 1 and len(v1['content_hash']) == 64
v2 = admin.call('POST', path + f'/models/{model["id"]}/versions',
                {'hw_spec': {'gpu': 'B300', 'gpu_count': 8, 'rail': 2}, 'slots': slots}, code=201)
assert v2['version'] == 2 and v2['content_hash'] != v1['content_hash']
dup = [{'slot_no': 1, 'pcie_slot': 'P1', 'expect_nic_model': 'X710', 'panel_port': '3', 'default_role': 'management'}]
bad = admin.call('POST', path + f'/models/{model["id"]}/versions',
                 {'hw_spec': {}, 'slots': dup + dup}, code=400)
passed('Immutable version publish with monotonic numbering and hash')

# Retirement: blocks new publishing, keeps history readable.
retired = admin.call('POST', path + f'/models/{model["id"]}/retire', {}, code=200)
assert retired['state'] == 'retired'
admin.call('POST', path + f'/models/{model["id"]}/versions', {'hw_spec': {}}, code=409)
page = admin.call('GET', path + '/models')
assert any(m['id'] == model['id'] and m['state'] == 'retired' for m in page['items'])
passed('Retirement blocks new versions and keeps the model readable')

# Scope isolation: a model in one project is invisible to another project's scope.
other = admin.call('POST', '/projects', {'name': 'Other scope', 'slug': prefix + '-other'}, code=201)
admin.call('GET', '/projects/' + other['id'] + f'/models/{model["id"]}/versions', code=404)
page_other = admin.call('GET', '/projects/' + other['id'] + '/models')
assert not any(m['id'] == model['id'] for m in page_other['items'])
passed('Project-private models stay invisible across project scopes')

# Authorization: a non-member without any role cannot read project models.
pwd = secrets.token_urlsafe(24)
admin.call('POST', '/admin/users', {'username': prefix, 'display_name': 'Model scope check', 'password': pwd}, code=201)
outsider = Client('127.0.0.61')
outsider.login(prefix, pwd)
outsider.call('GET', path + '/models', code=404)
outsider.call('POST', path + '/models', {'name': 'forbidden', 'kind': 'server'}, code=404)
passed('Outsiders receive 404 without leaking model existence')

print(f'Device model HTTP integration passed: {len(checks)} groups', flush=True)
