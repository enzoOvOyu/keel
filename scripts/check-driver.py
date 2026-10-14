#!/usr/bin/env python3
"""Driver upgrade chain (WP-3.4, FR-5.6): media:// admission, precheck matrix, canary gate, batch window, cache-hit accounting, read-back, reconcile, rollback/manual recovery."""
import http.client, http.cookies, json, os, secrets, urllib.parse
from pathlib import Path

file = Path(os.environ.get('KEEL_AUTH_BROWSER_CREDENTIALS_FILE', '/tmp/keel-auth-browser-credentials.json'))
assert file.stat().st_mode & 0o077 == 0, 'Private isolated credentials required'
credentials = json.loads(file.read_text())
base = urllib.parse.urlsplit(credentials['base_url'])
assert base.scheme == 'http' and base.hostname in ('127.0.0.1', 'localhost') and base.port == 3102, 'Isolated API only'
prefix = 'dr-' + secrets.token_hex(5)
groups = []


def group(label):
    groups.append(label)
    print('PASS ' + label, flush=True)


class Client:
    def __init__(self, ip='127.0.0.72'):
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

approver_name = prefix + '-approver'
approver_password = secrets.token_urlsafe(18)
admin.call('POST', '/admin/users', {'username': approver_name, 'display_name': 'DR approver', 'password': approver_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': approver_name, 'role': 'Admin'}, code=201)
approver = Client(ip='127.0.0.73')
approver.call('GET', '/auth/csrf')
approver.call('POST', '/auth/login', {'method': 'local', 'username': approver_name, 'password': approver_password})

viewer_name = prefix + '-viewer'
viewer_password = secrets.token_urlsafe(18)
admin.call('POST', '/admin/users', {'username': viewer_name, 'display_name': 'DR viewer', 'password': viewer_password, 'is_admin': False}, code=201)
admin.call('POST', f'/projects/{project["id"]}/members', {'username': viewer_name, 'role': 'Viewer'}, code=201)
viewer = Client(ip='127.0.0.74')
viewer.call('GET', '/auth/csrf')
viewer.call('POST', '/auth/login', {'method': 'local', 'username': viewer_name, 'password': viewer_password})

# Fixture: four machines and approved media for the GPU driver and the CX8
# firmware. Media approval needs a second reviewer (self-approval blocked).
SLOTS = [
    {'slot_no': 1, 'pcie_slot': '01', 'expect_nic_model': 'cx7', 'panel_port': 'port1', 'default_role': 'management'},
    {'slot_no': 2, 'pcie_slot': '02', 'expect_nic_model': 'x710', 'panel_port': 'port2', 'default_role': 'storage'},
    {'slot_no': 3, 'pcie_slot': '03', 'expect_nic_model': 'cx7', 'panel_port': 'port3', 'default_role': 'compute'},
    {'slot_no': 4, 'pcie_slot': '04', 'expect_nic_model': 'cx7', 'panel_port': 'port4', 'default_role': 'compute'},
]
model = admin.call('POST', path + '/models', {'name': 'B300-8U', 'kind': 'server'}, code=201)
admin.call('POST', f'{path}/models/{model["id"]}/versions',
           {'hw_spec': {'gpus': 8}, 'capability_matrix': {}, 'slots': SLOTS}, code=201)
admin.call('POST', path + '/bom/commit', {'file_version': 'v1', 'machines': [
    {'serial_number': f'SN-DR{i}', 'rack': '', 'unit_position': '', 'bmc_address': '', 'hostname': f'dr-node-{i}',
     'ip_reference': '', 'model_name': 'B300-8U',
     'ports': [{'mac_address': f'aa:bb:cc:50:00:0{i}', 'port_name': 'port1', 'rail': 0}]}
    for i in range(1, 5)]}, code=201)
assets = {a['hostname']: a['machine_id'] for a in admin.call('GET', path + '/assets?page=1&page_size=20')['items']}
M = [assets[f'dr-node-{i}'] for i in range(1, 5)]
DRIVER_SHA, FIRMWARE_SHA = 'd1' * 32, 'f1' * 32


def register(name, version, mtype, sha, size=409600):
    media = admin.call('POST', path + '/media', {'type': mtype, 'name': name, 'version': version, 'sha256': sha,
                       'size': size, 'arch': 'amd64', 'os_scope': 'ubuntu22.04',
                       'deps': {'kernel': '5.15', 'cuda': '13.1'}, 'manifest_ref': 'minio://media/pkg.tar.gz'}, code=201)
    admin.call('POST', path + f'/media/{media["id"]}/transition', {'state': 'verified'})
    approver.call('POST', path + f'/media/{media["id"]}/transition', {'state': 'approved'})
    return media


driver = register('nvidia-gpu-driver', 'R580', 'driver', DRIVER_SHA)
firmware = register('cx8-firmware', '3.42.0', 'firmware', FIRMWARE_SHA, size=204800)
pending = admin.call('POST', path + '/media', {'type': 'driver', 'name': 'pending-driver', 'version': 'R590',
                    'sha256': 'e1' * 32, 'size': 1, 'arch': 'amd64', 'manifest_ref': 'minio://media/p.tar.gz'}, code=201)


def plan(name, components, machines, baseline=None, canary=None, batch_size=2, backup='pre-upgrade backup to /opt/keel-cache/backup', code=201):
    return admin.call('POST', path + '/driver/plans', {
        'name': name, 'components': components, 'machines': machines, 'baseline': baseline or {},
        'canary_machine_id': canary or machines[0], 'batch_size': batch_size, 'backup_plan': backup}, code=code)


def advance(plan_id, results, code=200):
    return admin.call('POST', path + f'/driver/plans/{plan_id}/advance', {'results': results}, code=code)


# 1. The capability matrix is platform knowledge: firmware is never assumed
# rollback-able and the driver stack declares its backup requirement.
components = admin.call('GET', '/driver/components')['components']
matrix = {c['key']: c for c in components}
assert set(matrix) == {'gpu_driver', 'cuda', 'ofed', 'doca', 'cx8_firmware', 'kernel'}, matrix
assert matrix['cx8_firmware']['rollback'] == 'manual_only' and matrix['cx8_firmware']['backup_required'] is True
assert matrix['gpu_driver']['rollback'] == 'rollback_verified' and matrix['gpu_driver']['backup_required'] is True
assert matrix['kernel']['rollback'] == 'manual_only'
group('The component capability matrix declares rollback and backup semantics')

# 2. F-1.8: only approved media:// references are admitted; local paths,
# unapproved versions, malformed URIs and type mismatches are refused.
assert plan('local-path', {'gpu_driver': {'uri': '/opt/pkg/run'}}, [M[0]], code=400)['code'] == 'MEDIA_REFERENCE_REJECTED'
assert plan('unapproved', {'gpu_driver': {'media_id': pending['id']}}, [M[0]], code=409)['code'] == 'MEDIA_NOT_APPROVED'
assert plan('type-mismatch', {'gpu_driver': {'media_id': firmware['id']}}, [M[0]], code=400)['code'] == 'MEDIA_TYPE_MISMATCH'
assert plan('malformed-uri', {'gpu_driver': {'uri': 'media://driver'}}, [M[0]], code=400)['code'] == 'MEDIA_REFERENCE_REJECTED'
assert plan('no-ref', {'gpu_driver': {}}, [M[0]], code=400)['code'] == 'VALIDATION_ERROR'
uri_plan = plan('uri-plan', {'gpu_driver': {'uri': 'media://driver/nvidia-gpu-driver@R580'}}, [M[0]])
assert uri_plan['components']['gpu_driver']['uri'] == 'media://driver/nvidia-gpu-driver@R580'
assert uri_plan['components']['gpu_driver']['sha256'] == DRIVER_SHA
# A component whose recovery depends on a backup cannot be planned blind.
assert plan('no-backup', {'gpu_driver': {'media_id': driver['id']}}, [M[0]], backup='', code=400)['code'] == 'VALIDATION_ERROR'
# Cross-project media is invisible to this plan.
other = admin.call('POST', '/projects', {'name': prefix + '-other', 'slug': prefix + '-other'}, code=201)
admin.call('POST', f'/projects/{other["id"]}/members', {'username': approver_name, 'role': 'Admin'}, code=201)
# A membership grant revokes live sessions so the new scope takes effect.
approver.call('GET', '/auth/csrf')
approver.call('POST', '/auth/login', {'method': 'local', 'username': approver_name, 'password': approver_password})
foreign = admin.call('POST', f'/projects/{other["id"]}/media', {'type': 'driver', 'name': 'foreign-driver', 'version': 'R580',
                  'sha256': 'a1' * 32, 'size': 1, 'arch': 'amd64', 'manifest_ref': 'minio://media/f.tar.gz'}, code=201)
admin.call('POST', f'/projects/{other["id"]}/media/{foreign["id"]}/transition', {'state': 'verified'})
approver.call('POST', f'/projects/{other["id"]}/media/{foreign["id"]}/transition', {'state': 'approved'})
assert plan('foreign-media', {'gpu_driver': {'media_id': foreign['id']}}, [M[0]], code=403)['code'] == 'MEDIA_SCOPE'
group('media:// admission gate: approved references only, local paths rejected')

# 3. The precheck matrix blocks honestly: a missing baseline and a
# companion-rule violation are never a PASS; the verified combination
# (R580 with CUDA 13.x) passes.
blocked = plan('blocked-companion', {'gpu_driver': {'media_id': driver['id']}}, M,
               baseline={'gpu_driver': 'R550', 'cuda': '12.4'})
detail = admin.call('POST', path + f'/driver/plans/{blocked["id"]}/precheck')
reasons = [r for m in detail['plan']['precheck']['machines'] for r in m['reasons']]
assert any(r.startswith('companion_rule:gpu_driver') for r in reasons), reasons
assert detail['plan']['state'] == 'draft'
missing = plan('blocked-missing', {'gpu_driver': {'media_id': driver['id']}}, [M[0]], baseline={'cuda': '13.1'})
detail = admin.call('POST', path + f'/driver/plans/{missing["id"]}/precheck')
assert detail['plan']['precheck']['machines'][0]['reasons'] == ['baseline_missing:gpu_driver']
assert detail['plan']['state'] == 'draft'
group('The precheck matrix blocks missing baselines and companion-rule violations')

# 4. Canary gate: the batch never starts before the canary verifies, and a
# single canary-only plan completes on the canary alone.
main = plan('main-rollout', {'gpu_driver': {'media_id': driver['id']}}, M,
            baseline={'gpu_driver': 'R550', 'cuda': '13.1'}, canary=M[0], batch_size=2)
detail = admin.call('POST', path + f'/driver/plans/{main["id"]}/precheck')
assert detail['plan']['state'] == 'prechecked'
assert all(m['state'] == 'pass' for m in detail['plan']['precheck']['machines'])
detail = admin.call('POST', path + f'/driver/plans/{main["id"]}/start')
assert detail['plan']['state'] == 'canary_running'
roles = {t['machine_id']: t['role'] for t in detail['targets']}
assert roles[M[0]] == 'canary' and roles[M[1]] == 'batch'
assert admin.call('POST', path + f'/driver/plans/{main["id"]}/advance',
                  {'results': [{'machine_id': M[1], 'outcome': 'success', 'readback': {'gpu_driver': 'R580'}}]},
                  code=409)['code'] == 'CANARY_FIRST'
detail = advance(main['id'], [{'machine_id': M[0], 'outcome': 'success', 'readback': {'gpu_driver': 'R580'}, 'cache': [DRIVER_SHA]}])
assert detail['plan']['state'] == 'rolling'
canary = [t for t in detail['targets'] if t['machine_id'] == M[0]][0]
assert canary['state'] == 'verified' and canary['cache_hit'] is True and canary['bytes_transferred'] == 0
group('The canary gates the batch and verifies with a cache hit')

# 5. Batch window: at most batch_size machines report, in plan order.
assert advance(main['id'], [{'machine_id': M[i], 'outcome': 'success', 'readback': {'gpu_driver': 'R580'}, 'cache': [DRIVER_SHA]}
                            for i in (1, 2, 3)], code=409)['code'] == 'BATCH_LIMIT'
assert advance(main['id'], [{'machine_id': M[3], 'outcome': 'success', 'readback': {'gpu_driver': 'R580'}, 'cache': [DRIVER_SHA]}],
               code=409)['code'] == 'OUT_OF_BATCH'
detail = advance(main['id'], [{'machine_id': M[i], 'outcome': 'success', 'readback': {'gpu_driver': 'R580'}, 'cache': [DRIVER_SHA]}
                              for i in (1, 2)])
assert all(t['state'] == 'verified' for t in detail['targets'] if t['machine_id'] in (M[1], M[2]))
group('The batch window enforces size and plan order')

# 6. Distribution accounting: a cache hit skips the transfer, a miss moves
# the package bytes, and a read-back mismatch fails the target.
detail = advance(main['id'], [{'machine_id': M[3], 'outcome': 'success', 'readback': {'gpu_driver': 'R550'}}])
last = [t for t in detail['targets'] if t['machine_id'] == M[3]][0]
assert last['state'] == 'rolled_back' and last['failure_class'] == 'readback_mismatch'
assert last['evidence']['auto_rollback']['restored_from']['gpu_driver'] == 'R550'
assert last['cache_hit'] is False and last['bytes_transferred'] == 409600
summary = detail['summary']
assert summary['transfer_hits'] == 3 and summary['transfer_misses'] == 1, summary
assert summary['bytes_transferred'] == 409600
assert detail['plan']['state'] == 'completed' and detail['plan']['conclusion'] == 'FAIL'
hits = [t for t in detail['transfers'] if t['cache_hit']]
assert hits and all(t['bytes'] == 0 and t['checksum'] == '' for t in hits)
group('Cache hits skip the transfer and a read-back mismatch fails the target')

# 7. Timeout/unknown: query first, retry never. A blind re-report is
# refused until a reconciled (queried) result resolves the target.
solo = plan('solo-timeout', {'gpu_driver': {'media_id': driver['id']}}, [M[0]],
            baseline={'gpu_driver': 'R550', 'cuda': '13.1'}, batch_size=1)
admin.call('POST', path + f'/driver/plans/{solo["id"]}/precheck')
admin.call('POST', path + f'/driver/plans/{solo["id"]}/start')
detail = advance(solo['id'], [{'machine_id': M[0], 'outcome': 'timeout'}])
assert [t for t in detail['targets'] if t['machine_id'] == M[0]][0]['state'] == 'reconcile'
assert advance(solo['id'], [{'machine_id': M[0], 'outcome': 'success', 'readback': {'gpu_driver': 'R580'}}],
               code=409)['code'] == 'RECONCILE_REQUIRED'
detail = advance(solo['id'], [{'machine_id': M[0], 'outcome': 'reconciled', 'readback': {'gpu_driver': 'R580'}}])
assert [t for t in detail['targets'] if t['machine_id'] == M[0]][0]['state'] == 'verified'
assert detail['plan']['state'] == 'completed' and detail['plan']['conclusion'] == 'PASS'
group('A timed-out step reconciles by query and refuses blind retries')

# 8. F-1.6: a corrupt artifact retries a bounded number of times, then
# isolates the target; the digest is never waived and the plan waits.
corrupt = plan('corrupt-isolate', {'gpu_driver': {'media_id': driver['id']}}, [M[0], M[1]],
               baseline={'gpu_driver': 'R550', 'cuda': '13.1'}, canary=M[0], batch_size=1)
admin.call('POST', path + f'/driver/plans/{corrupt["id"]}/precheck')
admin.call('POST', path + f'/driver/plans/{corrupt["id"]}/start')
advance(corrupt['id'], [{'machine_id': M[0], 'outcome': 'success', 'readback': {'gpu_driver': 'R580'}, 'cache': [DRIVER_SHA]}])
detail = advance(corrupt['id'], [{'machine_id': M[1], 'outcome': 'success', 'readback': {'gpu_driver': 'R580'}, 'corrupt': ['gpu_driver']}])
isolated = [t for t in detail['targets'] if t['machine_id'] == M[1]][0]
assert isolated['state'] == 'failed' and isolated['failure_class'] == 'checksum_mismatch'
log = [t for t in detail['transfers'] if t['machine_id'] == M[1]][0]
assert log['retries'] == 2 and log['checksum'] == 'mismatch' and log['cache_hit'] is False
assert detail['plan']['state'] == 'rolling'
assert advance(corrupt['id'], [{'machine_id': M[1], 'outcome': 'success', 'readback': {'gpu_driver': 'R580'}}],
               code=409)['code'] == 'PLAN_STATE'
detail = admin.call('POST', path + f'/driver/plans/{corrupt["id"]}/targets/{M[1]}/rollback', {})
assert [t for t in detail['targets'] if t['machine_id'] == M[1]][0]['state'] == 'rolled_back'
assert detail['plan']['state'] == 'completed' and detail['plan']['conclusion'] == 'FAIL'
group('A persistent checksum mismatch isolates the target and blocks the plan')

# 9. Firmware: no verified rollback path. An install failure routes to
# manual handling, and the rollback call is refused for it.
fw = plan('firmware-manual', {'cx8_firmware': {'media_id': firmware['id']}}, [M[0]],
          baseline={'cx8_firmware': '3.40.0'}, batch_size=1)
admin.call('POST', path + f'/driver/plans/{fw["id"]}/precheck')
admin.call('POST', path + f'/driver/plans/{fw["id"]}/start')
detail = advance(fw['id'], [{'machine_id': M[0], 'outcome': 'failed'}])
fw_target = [t for t in detail['targets'] if t['machine_id'] == M[0]][0]
assert fw_target['state'] == 'manual' and fw_target['failure_class'] == 'install_failed'
assert 'cx8_firmware' in fw_target['evidence']['manual_required']['components']
assert detail['plan']['state'] == 'manual_required'
assert admin.call('POST', path + f'/driver/plans/{fw["id"]}/targets/{M[0]}/rollback', {},
                  code=409)['code'] == 'MANUAL_RECOVERY_REQUIRED'
assert admin.call('POST', path + f'/driver/plans/{fw["id"]}/targets/{M[0]}/manual', {'note': ''}, code=400)['code'] == 'VALIDATION_ERROR'
detail = admin.call('POST', path + f'/driver/plans/{fw["id"]}/targets/{M[0]}/manual', {'note': 'vendor reflashed the CX8 firmware on site'})
assert [t for t in detail['targets'] if t['machine_id'] == M[0]][0]['evidence']['manual_recovery']['note'].startswith('vendor')
group('Firmware failures route to manual recovery and refuse auto-rollback')

# 10. Abort stops a prechecked plan; a finished plan refuses to advance.
aborted = plan('abort-me', {'gpu_driver': {'media_id': driver['id']}}, [M[0]],
               baseline={'gpu_driver': 'R550', 'cuda': '13.1'}, batch_size=1)
admin.call('POST', path + f'/driver/plans/{aborted["id"]}/precheck')
detail = admin.call('POST', path + f'/driver/plans/{aborted["id"]}/abort', {})
assert detail['plan']['state'] == 'aborted' and detail['plan']['finished_at']
assert admin.call('POST', path + f'/driver/plans/{aborted["id"]}/advance',
                  {'results': [{'machine_id': M[0], 'outcome': 'success', 'readback': {'gpu_driver': 'R580'}}]},
                  code=409)['code'] == 'PLAN_STATE'
assert admin.call('POST', path + f'/driver/plans/{uri_plan["id"]}/abort', {}, code=409)['code'] == 'PLAN_STATE'
group('Abort stops a prechecked plan and a finished plan refuses to advance')

# 11. Read-only roles see the matrix and the plans but cannot mutate.
viewer.call('GET', '/driver/components')
viewer.call('GET', path + '/driver/plans')
viewer.call('GET', path + f'/driver/plans/{main["id"]}')
assert viewer.call('POST', path + '/driver/plans', {'name': 'viewer-plan', 'components': {'gpu_driver': {'media_id': driver['id']}},
                                                    'machines': [M[0]], 'baseline': {}, 'canary_machine_id': M[0],
                                                    'batch_size': 1, 'backup_plan': 'x'}, code=403)['code'] == 'FORBIDDEN'
assert viewer.call('POST', path + f'/driver/plans/{solo["id"]}/precheck', {}, code=403)['code'] == 'FORBIDDEN'
assert viewer.call('POST', path + f'/driver/plans/{solo["id"]}/advance',
                   {'results': [{'machine_id': M[0], 'outcome': 'success', 'readback': {'gpu_driver': 'R580'}}]},
                   code=403)['code'] == 'FORBIDDEN'
group('Read-only roles see the upgrade chain but cannot mutate it')

print(json.dumps({'status': 'passed', 'groups': groups}, ensure_ascii=False, indent=1))
