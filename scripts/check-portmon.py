#!/usr/bin/env python3
"""Port panel aggregation: mapping interface, link provenance, LLDP peer, alerts."""
import http.client, http.cookies, json, os, secrets, urllib.parse
from pathlib import Path

file = Path(os.environ.get('KEEL_AUTH_BROWSER_CREDENTIALS_FILE', '/tmp/keel-auth-browser-credentials.json'))
assert file.stat().st_mode & 0o077 == 0, 'Private isolated credentials required'
credentials = json.loads(file.read_text())
base = urllib.parse.urlsplit(credentials['base_url'])
assert base.scheme == 'http' and base.hostname in ('127.0.0.1', 'localhost') and base.port == 3102, 'Isolated API only'
prefix = 'pp-' + secrets.token_hex(5)
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
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'PP viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.61')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})

admin.call('POST', path + '/models', {'name': 'B300-8U', 'kind': 'server'}, code=201)
bom = {'file_version': 'v1', 'machines': [
    {'serial_number': 'SN-PP1', 'rack': '', 'unit_position': '', 'bmc_address': '', 'hostname': 'pp-node-1', 'ip_reference': '', 'model_name': 'B300-8U',
     'ports': [{'mac_address': 'aa:bb:cc:d0:00:01', 'port_name': 'port1', 'rail': 0}, {'mac_address': 'aa:bb:cc:d0:00:02', 'port_name': 'port2', 'rail': 1}]},
]}
admin.call('POST', path + '/bom/commit', bom, code=201)
mid = admin.call('GET', path + '/assets?page=1&page_size=20')['items'][0]['machine_id']
ports = {p['port_name']: p['id'] for p in admin.call('GET', path + f'/machines/{mid}/ports')['items']}

# 1. Before any data: panel answers unknown link states, no fabricated data.
panel = admin.call('GET', path + f'/machines/{mid}/port-panel')
assert panel['monitored'] is False and len(panel['ports']) == 2, panel
assert all(p['link_state'] == 'unknown' and not p.get('link_source') for p in panel['ports']), panel['ports']
passed.append('Empty panels answer unknown without fabricating data')

# 2. Seed the full chain: collections, G1, LLDP peer, mapping, mon samples.
admin.call('POST', path + '/collections', {'level': 'L1', 'machine_ids': [mid]})
admin.call('POST', path + '/collections', {'level': 'L2', 'machine_ids': [mid]})
admin.call('POST', path + '/gates/evaluate', {'gate': 'G1', 'machine_ids': [mid]})
admin.call('POST', path + '/lldp/snapshots', {'snapshots': [{'machine_id': mid, 'ports': {
    'port1': {'peer': 'sw1', 'peer_port': 'Eth1/1', 'link_up': True, 'dom_warning': False, 'flapping': False},
    'port2': {'peer': 'sw1', 'peer_port': 'Eth1/2', 'link_up': True, 'dom_warning': False, 'flapping': False},
}}]})
admin.call('POST', path + '/mappings/publish', {'machine_id': mid, 'entries': [
    {'port_id': ports['port1'], 'os_interface': 'nic-r0-p1', 'mac_address': 'aa:bb:cc:d0:00:01', 'bdf': '18:00.0', 'switch_port': 'sw1 Eth1/1', 'gpu_affinity': 'gpu-0'},
    {'port_id': ports['port2'], 'os_interface': 'nic-r0-p2', 'mac_address': 'aa:bb:cc:d0:00:02', 'bdf': '18:00.1', 'switch_port': 'sw1 Eth1/2', 'gpu_affinity': 'gpu-4'},
], 'note': 'panel fixture'}, code=201)
admin.call('POST', path + '/mon/ingest', {'machine_id': mid, 'stage': 'M2', 'changed': [
    {'port': 'port1', 'metric': 'link', 'value': {'state': 'up', 'speed': '200Gb/s'}},
    {'port': 'port2', 'metric': 'link', 'value': {'state': 'up', 'speed': '200Gb/s'}},
    {'port': 'port1', 'metric': 'dom', 'value': {'temperature_c': 38, 'tx_power_dbm': -2.1, 'rx_power_dbm': -2.4}},
]})
panel = admin.call('GET', path + f'/machines/{mid}/port-panel')
assert panel['monitored'] is True, panel
by_port = {p['port_name']: p for p in panel['ports']}
assert by_port['port1']['os_interface'] == 'nic-r0-p1', by_port['port1']
assert by_port['port1']['link_state'] == 'up' and by_port['port1']['link_source'] == 'agent', by_port['port1']
assert by_port['port1']['lldp_peer'] == 'sw1 Eth1/1', by_port['port1']
assert 'temperature_c' in by_port['port1']['dom_summary'], by_port['port1']
assert by_port['port2'].get('dom_summary', '') == '' and by_port['port2']['link_at'], by_port['port2']
passed.append('The panel joins mapping interfaces, link provenance, DOM and LLDP peers')

# 3. A flapping alert attaches to its port; the latest down state wins.
for _ in range(3):
    admin.call('POST', path + '/mon/ingest', {'machine_id': mid, 'stage': 'M2', 'changed': [{'port': 'port2', 'metric': 'link', 'value': {'state': 'down'}}]})
panel = admin.call('GET', path + f'/machines/{mid}/port-panel')
by_port = {p['port_name']: p for p in panel['ports']}
assert by_port['port2']['link_state'] == 'down', by_port['port2']
assert by_port['port2']['alert_rule'] == 'flapping', by_port['port2']
assert by_port['port1'].get('alert_rule', '') == '', by_port['port1']
passed.append('Flapping alerts attach to their port with the latest state')

# 4. Members read; foreign machines refuse.
viewer.call('GET', path + f'/machines/{mid}/port-panel')
other = admin.call('POST', '/projects', {'name': prefix + '-other', 'slug': prefix + '-other'}, code=201)
admin.call('GET', '/projects/' + other['id'] + f'/machines/{mid}/port-panel', code=409)
passed.append('The panel is project-scoped and member-readable')

print(json.dumps({'status': 'passed', 'groups': passed}, ensure_ascii=False, indent=1))
