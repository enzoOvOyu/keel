#!/usr/bin/env python3
"""ConfigSet engine: versioned approval, placeholder resolution, G3 gate, apply diffs."""
import http.client, http.cookies, json, os, secrets, urllib.parse
from pathlib import Path

file = Path(os.environ.get('KEEL_AUTH_BROWSER_CREDENTIALS_FILE', '/tmp/keel-auth-browser-credentials.json'))
assert file.stat().st_mode & 0o077 == 0, 'Private isolated credentials required'
credentials = json.loads(file.read_text())
base = urllib.parse.urlsplit(credentials['base_url'])
assert base.scheme == 'http' and base.hostname in ('127.0.0.1', 'localhost') and base.port == 3102, 'Isolated API only'
prefix = 'cs-' + secrets.token_hex(5)
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

# A second admin to approve (the creator cannot self-approve).
second_name = prefix + '-approver'
second_password = secrets.token_urlsafe(18)
admin.call('POST', '/admin/users', {'username': second_name, 'display_name': 'CS approver', 'password': second_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': second_name, 'role': 'Admin'}, code=201)
approver = Client(ip='127.0.0.62')
approver.call('GET', '/auth/csrf')
approver.call('POST', '/auth/login', {'method': 'local', 'username': second_name, 'password': second_password})

viewer_name = prefix + '-viewer'
viewer_password = secrets.token_urlsafe(18)
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'CS viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.61')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})

admin.call('POST', path + '/models', {'name': 'B300-8U', 'kind': 'server'}, code=201)
bom = {'file_version': 'v1', 'machines': [
    {'serial_number': 'SN-CS1', 'rack': '', 'unit_position': '', 'bmc_address': '', 'hostname': 'cs-node-1', 'ip_reference': '', 'model_name': 'B300-8U',
     'ports': [{'mac_address': 'aa:bb:cc:a5:00:01', 'port_name': 'port1', 'rail': 0}]},
]}
admin.call('POST', path + '/bom/commit', bom, code=201)
mid = admin.call('GET', path + '/assets?page=1&page_size=20')['items'][0]['machine_id']


def create(name, template, code=201):
    return admin.call('POST', path + '/config-sets', {'name': name, 'scope': 'machine', 'template': template}, code=code)


def approve(set_id, client=None, code=200):
    return (client or approver).call('POST', path + f'/config-sets/{set_id}/approve', {}, code=code)


def render(set_id, params, apply=False, code=200):
    suffix = '?apply=1' if apply else ''
    return admin.call('POST', path + '/config-sets/render' + suffix, {'config_set_id': set_id, 'machine_id': mid, 'params': params}, code=code)


# 1. A draft is created unapproved; self-approval is blocked; the approver
#    publishes it as an immutable approved revision.
draft = create('roce-baseline', 'mtu={{param.mtu}}\ndcqpn=15\nversion={{param.driver.version}}')
assert draft['status'] == 'draft' and draft['version'] == 1
rej = approve(draft['id'], client=admin, code=409)
assert rej['code'] == 'SELF_APPROVAL', rej
approved = approve(draft['id'])
assert approved['status'] == 'approved' and approved['approved_by'], approved
rej = approve(draft['id'], code=409)
assert 'only drafts' in rej['message'], rej
passed.append('Drafts publish through versioned approval with the self-approval block')

# 2. Rendering resolves parameter-catalog placeholders from the snapshot;
#    a missing parameter refuses instead of half-rendering.
r = render(draft['id'], {'mtu': '9000', 'driver': {'version': 'R580-1.0'}})
assert r['rendered'] == 'mtu=9000\ndcqpn=15\nversion=R580-1.0', r
assert r['diff'] == 'new', r
rej = render(draft['id'], {'mtu': '9000'}, code=409)
assert 'missing from the frozen snapshot' in rej['message'], rej
passed.append('Placeholders resolve from the frozen snapshot; missing parameters refuse')

# 3. The G3 gate: without a mapping the render refuses; with one it applies
#    and the second apply shows no diff.
rej = render(draft['id'], {'mtu': '9000', 'driver': {'version': 'R580-1.0'}}, apply=True, code=409)
assert rej['code'] == 'GATE_EVIDENCE', rej
# Publish the mapping to satisfy G3 (collections, G1, LLDP, mapping).
admin.call('POST', path + '/collections', {'level': 'L1', 'machine_ids': [mid]})
admin.call('POST', path + '/collections', {'level': 'L2', 'machine_ids': [mid]})
admin.call('POST', path + '/gates/evaluate', {'gate': 'G1', 'machine_ids': [mid]})
admin.call('POST', path + '/lldp/snapshots', {'snapshots': [{'machine_id': mid, 'ports': {
    'port1': {'peer': 'sw1', 'peer_port': 'Eth1/1', 'link_up': True, 'dom_warning': False, 'flapping': False}}}]})
ports = admin.call('GET', path + f'/machines/{mid}/ports')['items']
admin.call('POST', path + '/mappings/publish', {'machine_id': mid, 'entries': [
    {'port_id': ports[0]['id'], 'os_interface': 'nic-r0-p1', 'mac_address': 'aa:bb:cc:a5:00:01', 'bdf': '18:00.0', 'switch_port': 'sw1 Eth1/1', 'gpu_affinity': 'gpu-0'},
], 'note': 'configset fixture'}, code=201)
r = render(draft['id'], {'mtu': '9000', 'driver': {'version': 'R580-1.0'}}, apply=True)
assert r['applied'] is True and r['diff'] != '', r
r2 = render(draft['id'], {'mtu': '9000', 'driver': {'version': 'R580-1.0'}}, apply=True)
assert r2['diff'] == '', r2
# A changed parameter shows a diff against the newest apply.
r3 = render(draft['id'], {'mtu': '9100', 'driver': {'version': 'R580-1.0'}}, apply=True)
assert '+ mtu=9100' in r3['diff'] and '- mtu=9000' in r3['diff'], r3
passed.append('The G3 gate guards applies and diffs track the newest apply')

# 4. Members read; viewers cannot create/approve/apply.
viewer.call('GET', path + '/config-sets?page=1&page_size=20')
viewer.call('POST', path + '/config-sets', {'name': 'x', 'template': 'a'}, code=403)
viewer.call('POST', path + f'/config-sets/{draft["id"]}/approve', {}, code=403)
viewer.call('POST', path + '/config-sets/render', {'config_set_id': draft['id'], 'machine_id': mid, 'params': {}}, code=403)
passed.append('Read-only roles list config sets only')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
