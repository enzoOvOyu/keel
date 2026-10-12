#!/usr/bin/env python3
"""Exercise real local auth/project boundaries against an isolated test database.

Required: KEEL_TEST_DATABASE_URL ending /keel_test_http; a migrated or empty,
separately running test API on loopback port 3102; KEEL_ADMIN_BINARY pointing
at the matching built admin CLI. Refuses normal deployment/development ports.
The script creates random test accounts/projects only in that disposable DB.
It does not create/drop a database or touch the deployment .env file.
Optional KEEL_AUTH_BROWSER_CREDENTIALS_FILE retains a mode-0600 test admin
credential file for a following browser check. The caller must remove it.
"""
import concurrent.futures
import datetime
import http.client
import http.cookies
import json
import os
from pathlib import Path
import secrets
import subprocess
import tempfile
import urllib.parse

BASE = urllib.parse.urlsplit(os.environ.get('KEEL_AUTH_TEST_BASE_URL', 'http://127.0.0.1:3102'))
DATABASE = os.environ.get('KEEL_TEST_DATABASE_URL', '')
DB = urllib.parse.urlsplit(DATABASE)
if DB.scheme not in ('postgres', 'postgresql') or urllib.parse.unquote(DB.path) != '/keel_test_http':
    raise SystemExit('Refusing: KEEL_TEST_DATABASE_URL must name isolated keel_test_http database')
if BASE.scheme != 'http' or BASE.hostname not in ('127.0.0.1', 'localhost') or BASE.port != 3102:
    raise SystemExit('Refusing: test API must be http://127.0.0.1:3102 or http://localhost:3102')
if DB.hostname not in ('127.0.0.1', 'localhost') or DB.port != 15433:
    raise SystemExit('Refusing: test database must use the isolated loopback port 15433')
ADMIN = os.environ.get('KEEL_ADMIN_BINARY', '')
if not ADMIN or not Path(ADMIN).is_file():
    raise SystemExit('Set KEEL_ADMIN_BINARY to the matching built keel-admin executable')
ORIGIN = f'{BASE.scheme}://{BASE.netloc}'
PREFIX = 'probe-' + secrets.token_hex(5)
PASSED = []
SECRET_VALUES = {DATABASE}
if DB.password:
    SECRET_VALUES.add(urllib.parse.unquote(DB.password))
ALL_PAYLOADS = []
DEFAULT = object()


def check(condition, label):
    if not condition:
        raise AssertionError(label)


def passed(label):
    PASSED.append(label)
    print('PASS ' + label, flush=True)


class Browser:
    def __init__(self, source_ip='127.0.0.1'):
        self.cookies = {}
        self.source_ip = source_ip
        self.csrf = None

    def clone(self):
        other = Browser(self.source_ip)
        other.cookies = dict(self.cookies)
        other.csrf = self.csrf
        return other

    def call(self, method, path, data=None, *, csrf=DEFAULT, origin=DEFAULT, headers=None, raw=None):
        headers = dict(headers or {})
        if data is not None:
            raw = json.dumps(data).encode()
        if raw is not None:
            headers.setdefault('Content-Type', 'application/json')
        if self.cookies:
            headers['Cookie'] = '; '.join(f'{k}={v}' for k, v in self.cookies.items())
        if method not in ('GET', 'HEAD'):
            if origin is DEFAULT:
                origin = ORIGIN
            if origin is not None:
                headers['Origin'] = origin
            if csrf is DEFAULT:
                csrf = self.csrf
            if csrf is not None:
                headers['X-CSRF-Token'] = csrf
        connection = http.client.HTTPConnection(BASE.hostname, BASE.port, timeout=25,
                                                source_address=(self.source_ip, 0))
        try:
            connection.request(method, path, body=raw, headers=headers)
            response = connection.getresponse()
            status = response.status
            response_headers = response.getheaders()
            payload = response.read()
        finally:
            connection.close()
        for name, value in response_headers:
            if name.lower() == 'set-cookie':
                parsed = http.cookies.SimpleCookie()
                parsed.load(value)
                for key, morsel in parsed.items():
                    if morsel['max-age'] == '-1' or not morsel.value:
                        self.cookies.pop(key, None)
                    else:
                        self.cookies[key] = morsel.value
                        SECRET_VALUES.add(morsel.value)
        content = json.loads(payload) if payload else None
        # Store only in test process memory; never print HTTP payloads/cookies.
        ALL_PAYLOADS.append(content)
        if content is not None and 'csrf_token' in content:
            self.csrf = content['csrf_token']
        lower_headers = {key.lower(): value for key, value in response_headers}
        if path.startswith('/api/'):
            check('no-store' in lower_headers.get('cache-control', ''), 'API responses must not be cached')
        if status >= 400:
            check(isinstance(content, dict), 'errors are JSON objects')
            check(set(('code', 'message', 'details', 'trace_id')).issubset(content), 'structured error shape')
            check(bool(content['trace_id']), 'errors contain trace ID')
        return status, content, lower_headers

    def prelogin(self):
        status, content, headers = self.call('GET', '/api/v1/auth/csrf')
        check(status == 200 and bool(content['csrf_token']), 'pre-login CSRF succeeds')
        check('keel_session' in self.cookies, 'pre-login cookie exists')
        cookie = headers.get('set-cookie', '').lower()
        if cookie:
            check('httponly' in cookie and 'samesite=lax' in cookie and 'path=/' in cookie, 'pre-login cookie attributes')
        return self.cookies['keel_session']

    def login(self, username, password):
        before = self.prelogin()
        status, content, headers = self.call('POST', '/api/v1/auth/login',
                                             {'username': username, 'password': password, 'method': 'local'})
        check(status == 200, 'valid local account can log in')
        check(self.cookies.get('keel_session') != before, 'login rotates session cookie')
        cookie = headers.get('set-cookie', '').lower()
        check('httponly' in cookie and 'samesite=lax' in cookie and 'path=/' in cookie, 'authenticated cookie attributes')
        check(set(('user', 'permissions', 'projects', 'csrf_token', 'expires_at')).issubset(content), 'session contract')
        return before, content


def public_error(response):
    return {key: response[key] for key in ('code', 'message', 'details')}


def timestamp(value):
    return datetime.datetime.fromisoformat(value.replace('Z', '+00:00'))


def main():
    cli_env = dict(os.environ)
    cli_env['KEEL_DATABASE_URL'] = DATABASE
    # A test invocation cannot accidentally pick up deployment configuration.
    cli_env['KEEL_ENV'] = 'development'
    with tempfile.TemporaryDirectory(prefix='keel-auth-probe-') as temporary:
        os.chmod(temporary, 0o700)

        def cli(command, *arguments, expect_success=True):
            result = subprocess.run([ADMIN, command, *arguments], env=cli_env,
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=40)
            check((result.returncode == 0) == expect_success, f'CLI {command} expected exit status')
            # Never render CLI output: accidental credential emission is a failed check.
            combined = result.stdout + result.stderr
            for value in SECRET_VALUES:
                check(value.encode() not in combined, 'CLI does not disclose secret values')
            return result

        def account(suffix, admin=False, first=False):
            username = f'{PREFIX}-{suffix}'
            password = secrets.token_urlsafe(36)
            SECRET_VALUES.add(password)
            path = Path(temporary) / (suffix + '.password')
            descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(descriptor, 'w') as stream:
                stream.write(password)
            arguments = ['--username', username, '--display-name', 'Integration ' + suffix,
                         '--password-file', str(path)]
            if not first:
                arguments.append('--admin=' + str(admin).lower())
            cli('init-admin' if first else 'user-create', *arguments)
            return username, password, str(path)

        cli('migrate')
        anonymous = Browser()
        status, providers, _ = anonymous.call('GET', '/api/v1/auth/providers')
        check(status == 200 and providers['status'] == 'setup_required' and providers['methods'] == [],
              'isolated empty database requires initialization')
        status, info, _ = anonymous.call('GET', '/api/v1')
        check(status == 200 and info['phase'] == 'p1-foundation' and info['authentication'] == 'setup_required'
              and info['business_ready'] is False, 'foundation metadata reflects uninitialized authentication')
        check(anonymous.call('GET', '/api/v1/projects')[0] == 401, 'unauthenticated business access rejected')
        admin_name, admin_password, admin_file = account('admin', admin=True, first=True)
        cli('init-admin', '--username', PREFIX + '-second-admin', '--display-name', 'Second',
            '--password-file', admin_file, expect_success=False)
        status, providers, _ = anonymous.call('GET', '/api/v1/auth/providers')
        check(status == 200 and providers['status'] == 'ready' and providers['methods'] == ['local'],
              'local provider ready after explicit administrator initialization')
        status, info, _ = anonymous.call('GET', '/api/v1')
        check(status == 200 and info['phase'] == 'p1-foundation' and info['authentication'] == 'ready'
              and info['business_ready'] is False, 'ready authentication does not claim complete business readiness')
        passed('one-time administrator initialization and private business boundary')

        credentials = {'username': admin_name, 'password': admin_password, 'method': 'local'}
        guard = Browser('127.0.0.2')
        guard.prelogin()
        for changes in ({'csrf': None}, {'csrf': 'wrong'}, {'origin': None},
                        {'origin': 'https://attacker.example'}, {'origin': ORIGIN + '.attacker.example'}):
            status, _, _ = guard.call('POST', '/api/v1/auth/login', credentials, **changes)
            check(status == 403, 'login rejects missing/invalid CSRF or Origin')
        check(guard.call('POST', '/api/v1/auth/login', dict(credentials, unexpected=True))[0] == 400,
              'login rejects unknown JSON fields')
        check(guard.call('POST', '/api/v1/auth/login', raw=b'{"padding":"' + b'x' * 65536 + b'"}')[0] == 413,
              'login enforces 64 KiB request limit')
        passed('pre-login CSRF, exact Origin, strict JSON and body-size enforcement')

        admin = Browser('127.0.0.3')
        old_cookie, session = admin.login(admin_name, admin_password)
        check(session['user']['is_admin'] and 'project:create' in session['permissions'], 'Admin permission contract')
        fixation = Browser('127.0.0.3')
        fixation.cookies['keel_session'] = old_cookie
        check(fixation.call('GET', '/api/v1/auth/session')[0] == 401, 'consumed pre-login cookie cannot authenticate')
        check(admin.call('GET', '/api/v1/auth/session')[0] == 200, 'server session survives an independent request')
        passed('session cookie rotation and fixation resistance')

        first_project = {'name': '隔离授权项目', 'slug': PREFIX + '-one', 'description': 'Integration fixture only'}
        key = secrets.token_urlsafe(24)
        create_headers = {'Idempotency-Key': key}
        check(admin.call('POST', '/api/v1/projects', first_project, csrf=None, headers=create_headers)[0] == 403,
              'project creation requires CSRF')
        check(admin.call('POST', '/api/v1/projects', first_project, csrf=guard.csrf, headers=create_headers)[0] == 403,
              'valid CSRF from a different session cannot authorize writes')
        check(admin.call('POST', '/api/v1/projects', first_project, origin='https://attacker.example', headers=create_headers)[0] == 403,
              'project creation rejects cross-origin requests')
        check(admin.call('POST', '/api/v1/projects', first_project)[0] == 400, 'project creation requires idempotency key')
        check(admin.call('POST', '/api/v1/projects', dict(first_project, role='Admin'), headers=create_headers)[0] == 400,
              'project request does not accept privilege fields')
        status, project, _ = admin.call('POST', '/api/v1/projects', first_project, headers=create_headers)
        check(status == 201 and project['state'] == 'planning' and project['role'] == 'Admin', 'project creation contract')
        check(project['version'] >= 1 and timestamp(project['created_at']) <= timestamp(project['updated_at']), 'project version and dates')
        project_id = project['id']
        status, repeated, _ = admin.call('POST', '/api/v1/projects', first_project, headers=create_headers)
        check(status == 201 and repeated['id'] == project_id, 'idempotent create returns original project')
        check(admin.call('POST', '/api/v1/projects', dict(first_project, name='Changed payload'), headers=create_headers)[0] == 409,
              'same idempotency key with different payload conflicts')
        check(admin.call('POST', '/api/v1/projects', dict(first_project, name=' ' + first_project['name'] + ' '),
                         headers=create_headers)[0] == 409, 'raw payload changes conflict even when trimmed names match')
        check(admin.call('POST', '/api/v1/projects', first_project,
                         headers={'Idempotency-Key': secrets.token_urlsafe(24)})[0] == 409, 'duplicate slug conflicts')
        passed('project write protection and idempotent create/conflict behavior')

        concurrent_project = {'name': 'Concurrent create', 'slug': PREFIX + '-concurrent'}
        concurrent_key = {'Idempotency-Key': secrets.token_urlsafe(24)}
        clients = [admin.clone() for _ in range(4)]
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            replies = list(pool.map(lambda client: client.call('POST', '/api/v1/projects', concurrent_project,
                                                              headers=concurrent_key), clients))
        check(all(reply[0] == 201 for reply in replies), 'concurrent retries succeed')
        check(len({reply[1]['id'] for reply in replies}) == 1, 'concurrent retries create exactly one resource')
        private_project_id = replies[0][1]['id']
        listed = admin.call('GET', '/api/v1/projects')[1]['items']
        check(sum(item['slug'] == concurrent_project['slug'] for item in listed) == 1, 'project list has no retry duplicates')
        passed('concurrent idempotency is atomic')

        member_name, member_password, member_file = account('member')
        member = Browser('127.0.0.4')
        member.login(member_name, member_password)
        check(member.call('GET', '/api/v1/projects')[1] == {'items': []}, 'unassigned account has explicit empty project list')
        missing = member.call('GET', '/api/v1/projects/00000000000000000000000000000000')
        unauthorized = member.call('GET', '/api/v1/projects/' + project_id)
        check(missing[0] == unauthorized[0] == 404 and public_error(missing[1]) == public_error(unauthorized[1]),
              'unauthorized and nonexistent projects do not reveal existence')
        for role in ('Viewer', 'DE', 'DL', 'NE', 'SRE', 'Admin'):
            cli('member-grant', '--username', member_name, '--project', project_id, '--role', role)
            check(member.call('GET', '/api/v1/auth/session')[0] == 401, 'membership change invalidates existing session')
            member.login(member_name, member_password)
            status, visible, _ = member.call('GET', '/api/v1/projects')
            check(status == 200 and len(visible['items']) == 1, 'list filters projects by membership')
            check(visible['items'][0]['id'] == project_id and visible['items'][0]['role'] == role, 'role scoped to correct project')
            check(member.call('GET', '/api/v1/projects/' + project_id)[0] == 200, 'project member can read granted project')
            check(member.call('GET', '/api/v1/projects/' + private_project_id)[0] == 404, 'other project is not accessible')
            check(member.call('POST', '/api/v1/projects', {'name': 'Forbidden', 'slug': PREFIX + '-forbidden'},
                              headers={'Idempotency-Key': secrets.token_urlsafe(24)})[0] == 403,
                  'project role does not grant global project creation')
        passed('all six project roles enforce membership and platform-action boundaries')

        cli('member-revoke', '--username', member_name, '--project', project_id)
        check(member.call('GET', '/api/v1/projects')[0] == 401, 'membership removal immediately invalidates session')
        member.login(member_name, member_password)
        check(member.call('GET', '/api/v1/projects')[1] == {'items': []}, 'revoked member has no stale cached authorization')
        check(member.call('GET', '/api/v1/projects/' + project_id)[0] == 404, 'revoked membership cannot regain resource access')
        previous_password = member_password
        member_password = secrets.token_urlsafe(36)
        SECRET_VALUES.add(member_password)
        Path(member_file).write_text(member_password)
        cli('user-password', '--username', member_name, '--password-file', member_file)
        check(member.call('GET', '/api/v1/auth/session')[0] == 401, 'password reset immediately invalidates old sessions')
        member.prelogin()
        check(member.call('POST', '/api/v1/auth/login',
                          {'username': member_name, 'password': previous_password, 'method': 'local'})[0] == 401,
              'prior password is rejected after reset')
        member.login(member_name, member_password)
        passed('administrator password recovery revokes sessions and replaces the password')
        cli('user-disable', '--username', member_name)
        check(member.call('GET', '/api/v1/auth/session')[0] == 401, 'disabled account session immediately rejected')
        passed('membership revocation and account disable take immediate effect')

        logout_client = admin.clone()
        saved = logout_client.clone()
        check(logout_client.call('POST', '/api/v1/auth/logout', csrf=None)[0] == 403, 'logout requires CSRF')
        check(logout_client.call('POST', '/api/v1/auth/logout')[0] == 204, 'logout revokes session')
        check(saved.call('GET', '/api/v1/auth/session')[0] == 401, 'logged-out cookie replay fails')
        passed('logout CSRF and server-side revocation')

        credential_probe = Browser('127.0.0.5')
        credential_probe.prelogin()
        status, wrong, _ = credential_probe.call('POST', '/api/v1/auth/login',
                                                {'username': admin_name, 'password': 'known-wrong-value', 'method': 'local'})
        check(status == 401, 'wrong password rejected')
        status, unknown, _ = credential_probe.call('POST', '/api/v1/auth/login',
                                                  {'username': PREFIX + '-missing', 'password': 'known-wrong-value', 'method': 'local'})
        check(status == 401 and public_error(wrong) == public_error(unknown), 'unknown account and wrong password responses match')
        status, disabled, _ = credential_probe.call('POST', '/api/v1/auth/login',
                                                   {'username': member_name, 'password': member_password, 'method': 'local'})
        check(status == 401 and public_error(wrong) == public_error(disabled), 'disabled account does not disclose account status')
        passed('credential errors do not enumerate accounts')

        throttle = Browser('127.0.0.6')
        throttle.prelogin()
        status_codes = []
        for index in range(6):
            status, _, headers = throttle.call('POST', '/api/v1/auth/login',
                                               {'username': PREFIX + '-rate', 'password': 'wrong-value', 'method': 'local'},
                                               headers={'X-Forwarded-For': f'192.0.2.{index + 1}'})
            status_codes.append(status)
            if status == 429:
                check(int(headers.get('retry-after', '0')) > 0, 'rate-limit response includes Retry-After')
        check(status_codes[:4] == [401] * 4 and 429 in status_codes[4:], 'default five-failure throttle enforced')
        # A new source must still respect the account lock; an IP lock must also
        # cover new usernames. Spoofed forwarding headers cannot bypass either.
        moved = Browser('127.0.0.7')
        moved.prelogin()
        check(moved.call('POST', '/api/v1/auth/login',
                         {'username': PREFIX + '-rate', 'password': 'wrong-value', 'method': 'local'})[0] == 429,
              'account throttle survives source change')
        check(throttle.call('POST', '/api/v1/auth/login',
                            {'username': PREFIX + '-different', 'password': 'wrong-value', 'method': 'local'})[0] == 429,
              'source throttle covers different usernames')
        passed('persistent account/source rate limits and forwarding-header resistance')

        def verify_public_shape(value):
            if isinstance(value, dict):
                private_keys = {'password', 'password_hash', 'session_hash', 'csrf_hash', 'session_token', 'database_url'}
                check(not private_keys.intersection(value), 'private fields are not nested in API responses')
                for item in value.values():
                    verify_public_shape(item)
            elif isinstance(value, list):
                for item in value:
                    verify_public_shape(item)

        for content in ALL_PAYLOADS:
            verify_public_shape(content)
            serialized = json.dumps(content, ensure_ascii=False)
            for secret in SECRET_VALUES:
                check(secret not in serialized, 'responses do not disclose passwords or opaque session cookies')
            if isinstance(content, dict):
                check(not set(('password', 'password_hash', 'session_hash', 'database_url')).intersection(content),
                      'private authentication fields are not returned')
        passed('API and CLI outputs do not leak credential values')

        browser_path = os.environ.get('KEEL_AUTH_BROWSER_CREDENTIALS_FILE')
        if browser_path:
            target = Path(browser_path)
            check(target.is_absolute() and str(target).startswith('/tmp/keel-'), 'browser credential path is a test-only temporary path')
            descriptor = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(descriptor, 'w') as stream:
                json.dump({'username': admin_name, 'password': admin_password, 'base_url': ORIGIN}, stream)
        print(json.dumps({'passed': len(PASSED), 'scope': 'isolated local-auth/project PostgreSQL HTTP integration'}))


if __name__ == '__main__':
    main()
