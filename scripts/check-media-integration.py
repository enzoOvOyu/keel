#!/usr/bin/env python3
"""Real PostgreSQL media-library checks: lifecycle, uniqueness, approval gate, scope isolation."""
import hashlib
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
prefix = 'media-' + secrets.token_hex(5)
checks = []


class Client:
    def __init__(self, ip='127.0.0.70'):
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
            headers['Content-Type'] = 'application/octet-stream' if isinstance(body,bytes) else 'application/json'
        conn = http.client.HTTPConnection(base.hostname, base.port, timeout=60, source_address=(self.ip, 0))
        try:
            conn.request(method, '/api/v1' + path, body=None if body is None else body if isinstance(body,bytes) else json.dumps(body), headers=headers)
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
project = admin.call('POST', '/projects', {'name': '介质库隔离验收', 'slug': prefix}, code=201)
path = '/projects/' + project['id']
payload=b'keel verified driver fixture'
digest=hashlib.sha256(payload).hexdigest()


def register(name='nvidia-driver', version='580.82.07', manifest='minio://media/driver-580.tar.gz', code=201):
    return admin.call('POST', path + '/media', {
        'type': 'driver', 'name': name, 'version': version, 'sha256': digest,
        'size': len(payload), 'arch': 'amd64', 'os_scope': 'ubuntu22.04',
        'deps': {'compatibility': {'source': 'media-test-fixture', 'version': 'fixture-1', 'report_ref': 'report:fixture-1', 'requires': {'os.kernel': ['5.15'], 'cuda.version': ['13.1']}}}, 'manifest_ref': manifest}, code=code)


# F-1.6: registration with unique (scope, type, name, version); strict validation.
media = register()
register(code=409)
admin.call('POST', path + '/media', {'type': 'driver', 'name': 'x', 'version': 'v', 'sha256': 'zz',
                                     'size': 1, 'arch': 'amd64'}, code=400)
passed('Media registration uniqueness and digest validation')

# Lifecycle: uploaded -> verified needs a manifest; approval requires a different reviewer.
admin.call('POST', path + f'/media/{media["id"]}/transition', {'state': 'approved'}, code=409)
admin.call('POST', path + f'/media/{media["id"]}/transition', {'state': 'verified'}, code=409)
admin.call('PUT', path + f'/media/{media["id"]}/content',payload)
admin.call('POST', path + f'/media/{media["id"]}/transition', {'state': 'verified'}, code=200)
admin.call('POST', path + f'/media/{media["id"]}/transition', {'state': 'verified'}, code=409)
# Self-approval is a review violation: the uploader cannot approve.
self_approve = admin.call('POST', path + f'/media/{media["id"]}/transition', {'state': 'approved'}, code=409)
assert self_approve['code'] == 'MEDIA_SELF_APPROVAL', self_approve
passed('Verification requires actual bytes and blocks self-approval')

# A second reviewer approves; then retirement is irreversible.
pwd = secrets.token_urlsafe(24)
admin.call('POST', '/admin/users', {'username': prefix, 'display_name': 'Media reviewer', 'password': pwd, 'is_admin': False}, code=201)
admin.call('POST', path + '/members', {'username': prefix, 'role': 'Admin'}, code=201)
reviewer = Client('127.0.0.71')
reviewer.login(prefix, pwd)
approved = reviewer.call('POST', path + f'/media/{media["id"]}/transition', {'state': 'approved'}, code=200)
assert approved['approved_by'] and approved['approved_by'] != media['uploaded_by']
reviewer.call('POST', path + f'/media/{media["id"]}/transition', {'state': 'approved'}, code=409)
reviewer.call('POST', path + f'/media/{media["id"]}/transition', {'state': 'verified'}, code=409)
retired = reviewer.call('POST', path + f'/media/{media["id"]}/transition', {'state': 'retired'}, code=200)
assert retired['state'] == 'retired'
reviewer.call('POST', path + f'/media/{media["id"]}/transition', {'state': 'approved'}, code=409)
page = admin.call('GET', path + '/media')
assert any(m['id'] == media['id'] and m['state'] == 'retired' for m in page['items'])
passed('Approval by a second reviewer and irreversible retirement with readable history')

# Manifest-less media cannot be verified; scope isolation hides other projects.
bare = register(name='bare-artifact', version='1.0.0', manifest='')
admin.call('POST', path + f'/media/{bare["id"]}/transition', {'state': 'verified'}, code=409)
other = admin.call('POST', '/projects', {'name': 'Other media scope', 'slug': prefix + '-other'}, code=201)
admin.call('GET', '/projects/' + other['id'] + '/media', code=200)
page_other = admin.call('GET', '/projects/' + other['id'] + '/media')
assert not any(m['id'] == media['id'] for m in page_other['items'])
passed('Manifest verification gate and cross-project isolation')

print(f'Media HTTP integration passed: {len(checks)} groups', flush=True)
