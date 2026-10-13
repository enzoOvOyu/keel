#!/usr/bin/env python3
"""FR-1.4 rerun semantics: single-target rerun, failed-only rerun, guards, breaker during rerun."""
import http.client, http.cookies, json, os, secrets, time, urllib.parse
from pathlib import Path

file = Path(os.environ.get('KEEL_AUTH_BROWSER_CREDENTIALS_FILE', '/tmp/keel-auth-browser-credentials.json'))
assert file.stat().st_mode & 0o077 == 0, 'Private isolated credentials required'
credentials = json.loads(file.read_text())
base = urllib.parse.urlsplit(credentials['base_url'])
assert base.scheme == 'http' and base.hostname in ('127.0.0.1', 'localhost') and base.port == 3102, 'Isolated API only'
prefix = 'rr-' + secrets.token_hex(5)
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

targets = admin.call('POST', path + '/sim-targets', {'prefix': prefix, 'count': 10, 'seed': 42}, key=secrets.token_hex(16), code=201)['items']
tids = [t['id'] for t in targets]


def definition(fail_first=0, outcome='success'):
    return {'api_version': 'keel.sim/v1', 'inputs': {'profile': {'type': 'enum', 'required': True, 'enum': ['smoke']}}, 'tasks': [
        {'id': 'inspect', 'type': 'sim.read', 'retry': 0, 'idempotent': True, 'timeout_ms': 5000, 'resources': ['machine'], 'sim': {'outcome': outcome, 'duration_ms': 10, 'seed': 42, 'fail_first': fail_first}}]}


def publish(name, fail_first=0, outcome='success'):
    return admin.call('POST', path + '/flows', {'name': prefix + ' ' + name, 'definition': definition(fail_first, outcome)}, code=201)


def execute(flow, threshold=100, sel=None):
    return admin.call('POST', path + '/executions', {'flow_version_id': flow['id'], 'parameters': {'profile': 'smoke'}, 'mode': 'sim', 'target_ids': sel or tids, 'concurrency': 4, 'circuit_breaker_percent': threshold}, code=201)


def detail(run):
    return admin.call('GET', path + '/executions/' + run['id'])['execution']


def wait_terminal(run, seconds=60):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        ex = detail(run)
        if ex['state'] in ('success', 'failed', 'killed', 'partial'):
            return ex
        time.sleep(0.4)
    raise AssertionError('never terminal: ' + json.dumps(detail(run))[:300])


def rerun(run, body, code=200):
    return admin.call('POST', path + '/executions/' + run['id'] + '/rerun', body, code=code)


# 1. Single-target rerun on a success execution: only that target gets a new chain.
flow_ok = publish('single')
run = execute(flow_ok)
ex = wait_terminal(run)
assert ex['state'] == 'success'
before = admin.call('GET', path + '/executions/' + run['id'] + '/attempts?page=1&page_size=50')['total']
updated = rerun(run, {'target_ids': [tids[0]]})
assert updated['execution']['state'] in ('dispatching', 'running'), updated['execution']['state']
ex = wait_terminal(run)
after = admin.call('GET', path + '/executions/' + run['id'] + '/attempts?page=1&page_size=50')['total']
assert after == before + 1, (before, after)
passed.append('Single-target rerun appends exactly one fresh attempt chain')

# 2. failed_only rerun picks exactly the failed targets of a partial run.
flow_partial = publish('partial', fail_first=1)
# fail_first=1 fails only the first target's first attempt chain.
run2 = execute(flow_partial, threshold=100)
ex2 = wait_terminal(run2)
attempts = admin.call('GET', path + '/executions/' + run2['id'] + '/attempts?page=1&page_size=50')['items']
failed_targets = {a['target_id'] for a in attempts if a['state'] == 'failed'}
assert failed_targets, attempts
updated = rerun(run2, {'failed_only': True})
ex2 = wait_terminal(run2)
attempts = admin.call('GET', path + '/executions/' + run2['id'] + '/attempts?page=1&page_size=50')['items']
rerun_rows = [a for a in attempts if a['attempt_no'] == 2]
assert len(rerun_rows) == len(failed_targets), (len(rerun_rows), failed_targets)
assert {a['target_id'] for a in rerun_rows} == failed_targets
assert all(a['state'] == 'success' for a in rerun_rows), rerun_rows
passed.append('failed_only rerun re-opens exactly the failed targets')

# 3. Active executions refuse rerun; foreign targets refuse; empty selection refuses.
flow_long = publish('long')
run3 = execute(flow_long, sel=tids[:1])
rej = rerun(run3, {'target_ids': [tids[0]]}, code=409)
assert 'finished' in rej['message'], rej
admin.call('POST', path + '/executions/' + run3['id'] + '/control', {'action': 'cancel', 'expected_version': detail(run3)['state_version']})
wait_terminal(run3)
rej = rerun(run3, {'target_ids': ['foreign-target']}, code=409)
assert 'does not belong' in rej['message'], rej
rej = rerun(run3, {'target_ids': []}, code=400)
passed.append('Rerun guards: active executions, foreign targets and empty selections refuse')

# 4. The circuit breaker guards the rerun pass too: a failing flow rerun
#    with a low threshold pauses again.
flow_break = publish('break', outcome='failed')
run4 = execute(flow_break, threshold=100)  # finish first despite failures
wait_terminal(run4)
updated = rerun(run4, {'target_ids': tids[:2]})
deadline = time.monotonic() + 30
while time.monotonic() < deadline:
    ex4 = detail(run4)
    if ex4['desired_state'] == 'paused':
        break
    time.sleep(0.4)
assert ex4['desired_state'] == 'paused', ex4['desired_state']
passed.append('The circuit breaker guards rerun passes exactly like originals')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
