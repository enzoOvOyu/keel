#!/usr/bin/env python3
"""FR-1.4 circuit breaker: failure share over threshold auto-pauses the execution."""
import http.client, http.cookies, json, os, secrets, time, urllib.parse
from pathlib import Path

file = Path(os.environ.get('KEEL_AUTH_BROWSER_CREDENTIALS_FILE', '/opt/keel-tools/state/keel-test-db.env'))
# Fall back to the suite-provided credentials file for HTTP checks.
file = Path(os.environ.get('KEEL_AUTH_BROWSER_CREDENTIALS_FILE', '/tmp/keel-auth-browser-credentials.json'))
assert file.stat().st_mode & 0o077 == 0, 'Private isolated credentials required'
credentials = json.loads(file.read_text())
base = urllib.parse.urlsplit(credentials['base_url'])
assert base.scheme == 'http' and base.hostname in ('127.0.0.1', 'localhost') and base.port == 3102, 'Isolated API only'
prefix = 'cb-' + secrets.token_hex(5)
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
            status, raw = response.status, response.read()
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

# 20 logical targets.
targets = admin.call('POST', path + '/sim-targets', {'prefix': prefix, 'count': 20, 'seed': 42}, key=secrets.token_hex(16), code=201)['items']
assert len(targets) == 20


def definition(outcome='success'):
    return {'api_version': 'keel.sim/v1', 'inputs': {'profile': {'type': 'enum', 'required': True, 'enum': ['smoke']}}, 'tasks': [
        {'id': 'inspect', 'type': 'sim.read', 'retry': 0, 'idempotent': True, 'timeout_ms': 5000, 'resources': ['machine'], 'sim': {'outcome': outcome, 'duration_ms': 10, 'seed': 42}}]}


def publish(name, outcome):
    return admin.call('POST', path + '/flows', {'name': name, 'definition': definition(outcome)}, code=201)


def execute(flow, threshold):
    return admin.call('POST', path + '/executions', {'flow_version_id': flow['id'], 'parameters': {'profile': 'smoke'}, 'mode': 'sim', 'target_ids': [t['id'] for t in targets], 'concurrency': 4, 'circuit_breaker_percent': threshold}, code=201)


def detail(run):
    return admin.call('GET', path + '/executions/' + run['id'])


def wait_for_paused(run, seconds=60):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        d = detail(run)
        ex = d['execution']
        if ex['desired_state'] == 'paused' or ex['state'] in ('paused', 'partial'):
            return ex
        time.sleep(0.5)
    raise AssertionError('execution never paused; state=' + json.dumps(detail(run)['execution'])[:300])


# 1. A failing flow over the default 5% threshold auto-pauses the execution.
flow_fail = publish('cb-fail', 'failed')
run = execute(flow_fail, 5)
ex = wait_for_paused(run)
assert ex['desired_state'] == 'paused', ex['desired_state']
passed.append('Failure share over the threshold flips the execution to paused automatically')

# 2. The circuit-breaker event is recorded with the failure accounting.
events = admin.call('GET', path + '/executions/' + run['id'] + '/events?after=0')['items']
opened = [e for e in events if e.get('type') == 'execution.circuit_breaker_open']
assert opened, [e.get('type') for e in events][:6]
payload = opened[0].get('payload') or {}
assert int(payload.get('threshold_percent', 0)) == 5, payload
passed.append('The breaker event carries the failure accounting and threshold')

# 3. A healthy flow with the same threshold runs to success (no false trips).
flow_ok = publish('cb-ok', 'success')
run_ok = execute(flow_ok, 5)
deadline = time.monotonic() + 60
state = None
while time.monotonic() < deadline:
    d = detail(run_ok)
    state = d['execution']['state']
    if state in ('success', 'failed'):
        break
    time.sleep(0.5)
assert state == 'success', state
passed.append('Healthy executions never trip the breaker')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
