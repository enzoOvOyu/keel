#!/usr/bin/env python3
"""Real PostgreSQL dashboard checks: gate card states, KPI aggregation, honest not-implemented."""
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
prefix = 'dash-' + secrets.token_hex(5)
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
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'Dash viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.61')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})

admin.call('POST', path + '/models', {'name': 'B300-8U', 'kind': 'server'}, code=201)
bom = {'file_version': 'v1', 'machines': [
    {'serial_number': 'SN-D1', 'rack': '', 'unit_position': '', 'bmc_address': '', 'hostname': 'dash-node-1', 'ip_reference': '', 'model_name': 'B300-8U',
     'ports': [{'mac_address': 'aa:bb:cc:90:00:01', 'port_name': 'port1', 'rail': 0}]},
    {'serial_number': 'SN-D2', 'rack': '', 'unit_position': '', 'bmc_address': '', 'hostname': 'dash-node-2', 'ip_reference': '', 'model_name': 'B300-8U',
     'ports': [{'mac_address': 'aa:bb:cc:90:00:02', 'port_name': 'port1', 'rail': 0}]},
]}
admin.call('POST', path + '/bom/commit', bom, code=201)
assets = admin.call('GET', path + '/assets?page=1&page_size=20')['items']
ids = [a['machine_id'] for a in assets]

# 1. Empty project: every card reads honestly, G1 without evidence, G2-G6 not implemented.
board = admin.call('GET', path + '/dashboard')
cards = board['cards']
assert cards['G1']['state'] == 'no_evidence' and cards['G2']['state'] == 'no_evidence', cards
assert cards['G3']['state'] == 'not_implemented' and cards['G4']['state'] == 'not_implemented', cards
assert cards['G5']['state'] == 'not_implemented' and cards['G6']['state'] == 'not_implemented', cards
assert all(c['pass_rate'] == 0 for c in cards.values())
assert board['kpis']['assets_total'] == 2 and board['kpis']['trust_unverified'] == 2, board['kpis']
passed.append('Empty boards stay honest: no fabricated passes anywhere')

# 2. A failed G1 evaluation flips the card to fail with zero pass rate.
r = admin.call('POST', path + '/gates/evaluate', {'gate': 'G1', 'machine_ids': ids})
assert r['conclusion'] == 'FAIL'
board = admin.call('GET', path + '/dashboard')
assert board['cards']['G1']['state'] == 'fail' and board['cards']['G1']['pass_rate'] == 0, board['cards']['G1']
passed.append('A failed evaluation flips the G1 card to fail')

# 3. A passing G1 flips the card to pass with 100% rate; trust KPIs track bootstrap.
admin.call('POST', path + '/collections', {'level': 'L1', 'machine_ids': ids})
admin.call('POST', path + '/collections', {'level': 'L2', 'machine_ids': ids})
r = admin.call('POST', path + '/gates/evaluate', {'gate': 'G1', 'machine_ids': ids})
assert r['conclusion'] == 'PASS', r
FP = 'SHA256:' + secrets.token_hex(32)
admin.call('POST', path + '/trust/actions', {'action': 'trust.bootstrap', 'machine_ids': ids, 'fingerprint': FP})
board = admin.call('GET', path + '/dashboard')
assert board['cards']['G1']['state'] == 'pass' and board['cards']['G1']['pass_rate'] == 100, board['cards']['G1']
assert board['kpis']['trust_established'] == 2 and board['kpis']['trust_unverified'] == 0, board['kpis']
passed.append('A passing evaluation and trust bootstrap drive the card and KPIs')

# 4. Open issues and LLDP differences count into the KPIs.
r = admin.call('POST', path + '/lldp/snapshots', {'snapshots': [{'machine_id': ids[0], 'ports': {
    'port1': {'peer': '', 'peer_port': '', 'link_up': False, 'dom_warning': False, 'flapping': False},
}}]})
diff = r['results'][0]['differences'][0]
admin.call('POST', path + '/issues', {'difference_id': diff['id'], 'source': 'g2', 'phase': 'delivery'}, code=201)
board = admin.call('GET', path + '/dashboard')
assert board['kpis']['open_issues'] == 1 and board['kpis']['lldp_open_diffs'] == 1, board['kpis']
passed.append('Open issues and LLDP differences surface in the KPIs')

# 5. A waiver marks the G1 card waived, never as pass.
admin.call('POST', path + '/gates/waive', {'gate': 'G1', 'machine_ids': ids, 'reason': 'DOM unreadable on one port, RMA scheduled', 'scope': 'dash-node-1', 'waiver_expires_hours': 24})
board = admin.call('GET', path + '/dashboard')
assert board['cards']['G1']['state'] == 'waived', board['cards']['G1']
passed.append('A waiver marks the card waived, never pass')

# 6. Read-only roles read the dashboard; other projects stay isolated.
viewer.call('GET', path + '/dashboard')
other = admin.call('POST', '/projects', {'name': prefix + '-other', 'slug': prefix + '-other'}, code=201)
other_board = admin.call('GET', '/projects/' + other['id'] + '/dashboard')
assert other_board['kpis']['assets_total'] == 0 and other_board['cards']['G1']['state'] == 'no_evidence'
passed.append('Dashboards are project-scoped and readable by members')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
