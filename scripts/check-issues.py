#!/usr/bin/env python3
"""Real PostgreSQL issue-lifecycle checks: state machine, version lock, evidence, re-verification closure."""
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
prefix = 'issue-' + secrets.token_hex(5)
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
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'Issue viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.61')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})

admin.call('POST', path + '/models', {'name': 'B300-8U', 'kind': 'server'}, code=201)
bom = {'file_version': 'v1', 'machines': [
    {'serial_number': 'SN-I1', 'rack': '', 'unit_position': '', 'bmc_address': '', 'hostname': 'issue-node-1', 'ip_reference': '', 'model_name': 'B300-8U',
     'ports': [{'mac_address': 'aa:bb:cc:80:00:01', 'port_name': 'port1', 'rail': 0}, {'mac_address': 'aa:bb:cc:80:00:02', 'port_name': 'port2', 'rail': 1}]},
]}
admin.call('POST', path + '/bom/commit', bom, code=201)
assets = admin.call('GET', path + '/assets?page=1&page_size=20')['items']
HEALTHY2 = {'peer': 'sw1', 'peer_port': 'Eth1/2', 'link_up': True, 'dom_warning': False, 'flapping': False}
mid = assets[0]['machine_id']

# Produce an unresolved T1 difference.
r = admin.call('POST', path + '/lldp/snapshots', {'snapshots': [{'machine_id': mid, 'ports': {
    'port1': {'peer': '', 'peer_port': '', 'link_up': False, 'dom_warning': False, 'flapping': False},
    'port2': HEALTHY2}}]})
diff = r['results'][0]['differences'][0]

# 1. An issue created from a difference carries the evidence and dual markers.
issue = admin.call('POST', path + '/issues', {'difference_id': diff['id'], 'source': 'g2', 'phase': 'delivery'}, code=201)
assert issue['status'] == 'open' and issue['source'] == 'g2' and issue['phase'] == 'delivery' and issue['category'] == 'T1', issue
assert issue['version'] == 1 and 'issue-node-1' in issue['title'], issue
passed.append('Issues carry evidence, dual source/phase markers and start open')

# 2. Creating an issue for an already-resolved difference is refused.
snaps2 = {'snapshots': [{'machine_id': mid, 'ports': {'port1': {'peer': 'sw1', 'peer_port': 'Eth1/1', 'link_up': True, 'dom_warning': False, 'flapping': False}, 'port2': HEALTHY2}}]}
admin.call('POST', path + '/lldp/snapshots', snaps2)
r = admin.call('POST', path + '/lldp/snapshots', {'snapshots': [{'machine_id': mid, 'ports': {
    'port1': {'peer': '', 'peer_port': '', 'link_up': False, 'dom_warning': False, 'flapping': False},
    'port2': HEALTHY2}}]})
diff2 = r['results'][0]['differences'][0]
admin.call('POST', path + '/lldp/snapshots', snaps2)  # resolve it
rej = admin.call('POST', path + '/issues', {'difference_id': diff2['id'], 'source': 'g2', 'phase': 'delivery'}, code=409)
assert rej['code'] == 'VALIDATION_ERROR', rej
passed.append('Resolved differences cannot spawn issues')

# 3. Skipping states and stale versions are rejected.
rej = admin.call('POST', path + f'/issues/{issue["id"]}/transition?action=close', {'issue_id': issue['id'], 'version': 1}, code=409)
assert rej['code'] == 'ISSUE_STATE', rej
rej = admin.call('POST', path + f'/issues/{issue["id"]}/transition?action=assign', {'issue_id': issue['id'], 'version': 99}, code=409)
assert rej['code'] == 'ISSUE_VERSION', rej
passed.append('State skips and stale versions are rejected')

# 4. The full lifecycle: assign -> start -> repair -> verify -> close.
issue = admin.call('POST', path + f'/issues/{issue["id"]}/transition?action=assign', {'issue_id': issue['id'], 'version': 1})
assert issue['status'] == 'assigned' and issue['assignee'], issue
rej = admin.call('POST', path + f'/issues/{issue["id"]}/transition?action=verify', {'issue_id': issue['id'], 'version': issue['version']}, code=409)
assert rej['code'] == 'ISSUE_STATE', rej  # no repair registered yet
issue = admin.call('POST', path + f'/issues/{issue["id"]}/transition?action=start', {'issue_id': issue['id'], 'version': issue['version']})
assert issue['status'] == 'in_repair', issue
issue = admin.call('POST', path + '/issues/repair', {'issue_id': issue['id'], 'action': 'reseat', 'note': '重插紧固 port1，旧标签 CAB-041 新标签 CAB-041', 'old_tag': 'CAB-041', 'new_tag': 'CAB-041'})
assert issue['repair_action'] == 'reseat', issue
issue = admin.call('POST', path + f'/issues/{issue["id"]}/transition?action=verify', {'issue_id': issue['id'], 'version': issue['version']})
assert issue['status'] == 'verify', issue
# The bound difference was resolved by the group-2 re-run, so close passes.
issue = admin.call('POST', path + f'/issues/{issue["id"]}/transition?action=close', {'issue_id': issue['id'], 'version': issue['version']})
assert issue['status'] == 'closed', issue
# The unresolved-difference gate: a fresh issue whose difference is NOT
# resolved yet must refuse to close even in verify state.
r = admin.call('POST', path + '/lldp/snapshots', {'snapshots': [{'machine_id': mid, 'ports': {
    'port1': {'peer': '', 'peer_port': '', 'link_up': False, 'dom_warning': False, 'flapping': False},
    'port2': HEALTHY2}}]})
fresh = admin.call('POST', path + '/issues', {'difference_id': r['results'][0]['differences'][0]['id'], 'source': 'g2', 'phase': 'delivery'}, code=201)
fresh = admin.call('POST', path + f'/issues/{fresh["id"]}/transition?action=assign', {'issue_id': fresh['id'], 'version': fresh['version']})
fresh = admin.call('POST', path + f'/issues/{fresh["id"]}/transition?action=start', {'issue_id': fresh['id'], 'version': fresh['version']})
fresh = admin.call('POST', path + '/issues/repair', {'issue_id': fresh['id'], 'action': 'replace_cable', 'note': '更换线缆 CAB-041 为 CAB-099', 'old_tag': 'CAB-041', 'new_tag': 'CAB-099'})
fresh = admin.call('POST', path + f'/issues/{fresh["id"]}/transition?action=verify', {'issue_id': fresh['id'], 'version': fresh['version']})
rej = admin.call('POST', path + f'/issues/{fresh["id"]}/transition?action=close', {'issue_id': fresh['id'], 'version': fresh['version']}, code=409)
assert 'not resolved' in rej['message'], rej
admin.call('POST', path + '/lldp/snapshots', snaps2)  # targeted re-verification passes
fresh = admin.call('POST', path + f'/issues/{fresh["id"]}/transition?action=close', {'issue_id': fresh['id'], 'version': fresh['version']})
assert fresh['status'] == 'closed', fresh
passed.append('The lifecycle walks assign-start-repair-verify-close with the re-verification gate')

# 5. Reopen works from closed only.
rej = admin.call('POST', path + f'/issues/{issue["id"]}/transition?action=reopen', {'issue_id': issue['id'], 'version': 999}, code=409)
issue = admin.call('POST', path + f'/issues/{issue["id"]}/transition?action=reopen', {'issue_id': issue['id'], 'version': issue['version']})
assert issue['status'] == 'open' and issue['version'] == 7, issue
passed.append('Reopen restarts the loop from closed with the version lock')

# 6. Repairs demand meaningful notes and known actions.
r2 = admin.call('POST', path + f'/issues/{issue["id"]}/transition?action=assign', {'issue_id': issue['id'], 'version': issue['version']})
r3 = admin.call('POST', path + f'/issues/{r2["id"] if "id" in r2 else issue["id"]}/transition?action=start', {'issue_id': issue['id'], 'version': r2['version']})
admin.call('POST', path + '/issues/repair', {'issue_id': issue['id'], 'action': 'bogus', 'note': 'meaningful note here'}, code=400)
admin.call('POST', path + '/issues/repair', {'issue_id': issue['id'], 'action': 'reseat', 'note': 'no'}, code=400)
passed.append('Repairs validate actions and demand meaningful notes')

# 7. Board filters by status/source/phase; read-only roles read but cannot act.
board = admin.call('GET', path + '/issues?status=in_repair&page=1&page_size=20')
assert board['items'] and all(i['status'] == 'in_repair' for i in board['items']), board['items'][:1]
viewer.call('GET', path + '/issues?page=1&page_size=20')
viewer.call('POST', path + f'/issues/{issue["id"]}/transition?action=assign', {'issue_id': issue['id'], 'version': issue['version']}, code=403)
passed.append('The board filters by status and enforces role boundaries')

# 8. Foreign differences cannot spawn issues here.
other = admin.call('POST', '/projects', {'name': prefix + '-other', 'slug': prefix + '-other'}, code=201)
other_path = '/projects/' + other['id']
admin.call('POST', other_path + '/issues', {'difference_id': diff['id'], 'source': 'g2', 'phase': 'delivery'}, code=409)
passed.append('Issues are project-scoped')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
