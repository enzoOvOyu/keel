#!/usr/bin/env python3
"""Real PostgreSQL parameter-catalog checks: typed keys, media-synced options, retirement."""
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
prefix = 'param-' + secrets.token_hex(5)
checks = []


class Client:
    def __init__(self, ip='127.0.0.80'):
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
project = admin.call('POST', '/projects', {'name': '参数目录隔离验收', 'slug': prefix}, code=201)
path = '/projects/' + project['id']
payload=b'keel verified parameter fixture'
digest=hashlib.sha256(payload).hexdigest()


def register_media(name, version):
    media=admin.call('POST', path + '/media', {
        'type': 'driver', 'name': name, 'version': version, 'sha256': digest,
        'size': len(payload), 'arch': 'amd64', 'manifest_ref': 'minio://media/x.tar.gz'}, code=201)
    admin.call('PUT',path+f'/media/{media["id"]}/content',payload)
    return media


def transition(media_id, state, client=None):
    return (client or admin).call('POST', path + f'/media/{media_id}/transition', {'state': state}, code=200)


# Registration validation: key naming, types, default-in-options, version needs media source.
admin.call('PUT', path + '/params', {'key': 'Bad Key', 'value_type': 'string', 'options': []}, code=400)
admin.call('PUT', path + '/params', {'key': 'os.version', 'value_type': 'version', 'options': []}, code=400)
admin.call('PUT', path + '/params', {'key': 'deploy.profile', 'value_type': 'enum', 'options': ['smoke', 'full'], 'default_value': 'missing'}, code=400)
passed('Parameter key validation enforces naming, typing and default-in-options')

# A version key with no approved media yields an empty option list, so any value is rejected.
key = admin.call('PUT', path + '/params', {'key': 'driver.version', 'value_type': 'version', 'source_media_type': 'driver'}, code=201)
admin.call('GET', path + '/params/driver.version/check?arch=amd64&value=580.82.07', code=422)
passed('Version options start empty without approved media')

# A separate reviewer approves media; the option list derives on read.
m1 = register_media('nvidia-driver', '580.82.07')
m2 = register_media('nvidia-driver', '575.51.03')
transition(m1['id'], 'verified')
pwd = secrets.token_urlsafe(24)
admin.call('POST', '/admin/users', {'username': prefix, 'display_name': 'Param reviewer', 'password': pwd}, code=201)
admin.call('POST', path + '/members', {'username': prefix, 'role': 'Admin'}, code=201)
reviewer = Client('127.0.0.81')
reviewer.login(prefix, pwd)
transition(m1['id'], 'approved', reviewer)
page = admin.call('GET', path + '/params')
entry = next(k for k in page['items'] if k['key'] == 'driver.version')
assert entry['options'] == ['580.82.07'], entry['options']
admin.call('GET', path + '/params/driver.version/check?arch=amd64&value=580.82.07', code=200)
admin.call('GET', path + '/params/driver.version/check?arch=amd64&value=575.51.03', code=422)
passed('Approved media versions sync into options and gate value checks')

# The reviewer approves the second media; the derived options extend immediately.
transition(m2['id'], 'verified', reviewer)
transition(m2['id'], 'approved', reviewer)
admin.call('GET', path + '/params/driver.version/check?arch=amd64&value=575.51.03', code=200)
page = admin.call('GET', path + '/params')
entry = next(k for k in page['items'] if k['key'] == 'driver.version')
assert entry['options'] == ['575.51.03', '580.82.07'], entry['options']
passed('A second approval extends options without manual edits')

# Retirement blocks checks and new references; the definition stays readable in history.
retired = admin.call('POST', path + f'/params/{key["id"]}/retire', {}, code=200)
assert retired['state'] == 'retired'
admin.call('GET', path + '/params/driver.version/check?arch=amd64&value=580.82.07', code=404)
resurrect = admin.call('PUT', path + '/params', {'key': 'driver.version', 'value_type': 'version', 'source_media_type': 'driver'}, code=409)
assert resurrect['code'] == 'PARAM_KEY_RETIRED', resurrect
admin.call('POST', path + f'/params/{key["id"]}/retire', {}, code=409)
passed('Retirement blocks value checks and rejects resurrection by upsert')

print(f'Parameter catalog HTTP integration passed: {len(checks)} groups', flush=True)
