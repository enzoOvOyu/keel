#!/usr/bin/env python3
"""FR-13 mock monitoring: stage tiers, changed-only ingest, flapping alerts, maintenance stop."""
import http.client, http.cookies, json, os, secrets, urllib.parse
from pathlib import Path

file = Path(os.environ.get('KEEL_AUTH_BROWSER_CREDENTIALS_FILE', '/tmp/keel-auth-browser-credentials.json'))
assert file.stat().st_mode & 0o077 == 0, 'Private isolated credentials required'
credentials = json.loads(file.read_text())
base = urllib.parse.urlsplit(credentials['base_url'])
assert base.scheme == 'http' and base.hostname in ('127.0.0.1', 'localhost') and base.port == 3102, 'Isolated API only'
prefix = 'mon-' + secrets.token_hex(5)
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

viewer_name = prefix + '-viewer'
viewer_password = secrets.token_urlsafe(18)
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'Mon viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.61')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})

admin.call('POST', path + '/models', {'name': 'B300-8U', 'kind': 'server'}, code=201)
bom = {'file_version': 'v1', 'machines': [
    {'serial_number': 'SN-M1', 'rack': '', 'unit_position': '', 'bmc_address': '', 'hostname': 'mon-node-1', 'ip_reference': '', 'model_name': 'B300-8U',
     'ports': [{'mac_address': 'aa:bb:cc:b0:00:01', 'port_name': 'port1', 'rail': 0}]},
]}
admin.call('POST', path + '/bom/commit', bom, code=201)
assets = admin.call('GET', path + '/assets?page=1&page_size=20')['items']
mid = assets[0]['machine_id']


def policy(stage, code=200):
    return admin.call('POST', path + '/mon/policies', {'stage': stage, 'burst_multiplier': 1}, code=code)


def ingest(changes, stage='M2', code=200):
    return admin.call('POST', path + '/mon/ingest', {'machine_id': mid, 'stage': stage, 'changed': changes}, code=code)


# 1. Stage tiers mirror the FR-13.2 table exactly.
expected = {
    'M1': (300, 600, 0, 0),
    'M2': (60, 300, 300, 0),
    'M3': (60, 300, 300, 120),
    'M4': (30, 120, 60, 60),
    'maintenance': (0, 0, 0, 0),
}
for stage, (d12, d3, d4, d5) in expected.items():
    p = policy(stage)
    assert (p['d1_d2_interval_seconds'], p['d3_interval_seconds'], p['d4_interval_seconds'], p['d5_interval_seconds']) == (d12, d3, d4, d5), (stage, p)
assert p['steady_budget_kbps'] == 50 and p['burst_budget_kbps'] == 200
passed.append('Stage tiers match the FR-13.2 table with fixed budgets')

# 2. Changed-only ingest accepts samples and counts them.
r = ingest([{'port': 'port1', 'metric': 'link', 'value': {'state': 'up', 'speed': '200Gb/s'}}])
assert r['accepted'] == 1 and r['alert_opened'] == 0, r
passed.append('Changed-only ingest records samples')

# 3. Flapping: three downs within the window open exactly one alert.
r = ingest([{'port': 'port1', 'metric': 'link', 'value': {'state': 'down'}}])
assert r['alert_opened'] == 0, r  # first down: 1 in window
r = ingest([{'port': 'port1', 'metric': 'link', 'value': {'state': 'down'}}])
assert r['alert_opened'] == 0, r  # second down: 2 in window
r = ingest([{'port': 'port1', 'metric': 'link', 'value': {'state': 'down'}}])
assert r['alert_opened'] == 1, r  # third down trips the rule
r = ingest([{'port': 'port1', 'metric': 'link', 'value': {'state': 'down'}}])
assert r['alert_opened'] == 0, r  # no duplicate while open
alerts = admin.call('GET', path + '/mon/alerts?page=1&page_size=20')['items']
flap = [a for a in alerts if a['rule'] == 'flapping' and a.get('closed_at') is None]
assert len(flap) == 1 and flap[0]['port'] == 'port1', alerts
passed.append('Flapping opens exactly one alert at three downs in the window')

# 4. Maintenance stops dynamic monitoring: ingest refused.
policy('maintenance')
rej = ingest([{'port': 'port1', 'metric': 'link', 'value': {'state': 'up'}}], stage='maintenance', code=409)
assert 'maintenance' in rej['message'], rej
passed.append('Maintenance stage refuses ingest (dynamic monitoring ended)')

# 5. Read-only roles read alerts but cannot set policies or ingest.
viewer.call('GET', path + '/mon/alerts?page=1&page_size=20')
viewer.call('POST', path + '/mon/policies', {'stage': 'M2'}, code=403)
viewer.call('POST', path + '/mon/ingest', {'machine_id': mid, 'stage': 'M2', 'changed': []}, code=403)
passed.append('Read-only roles read alerts only')

# 6. Foreign machines refuse ingest.
other = admin.call('POST', '/projects', {'name': prefix + '-other', 'slug': prefix + '-other'}, code=201)
admin.call('POST', '/projects/' + other['id'] + '/mon/ingest', {'machine_id': mid, 'stage': 'M2', 'changed': [{'port': 'p', 'metric': 'link', 'value': {'state': 'up'}}]}, code=409)
passed.append('Ingest is project-scoped')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
