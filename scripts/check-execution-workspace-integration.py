#!/usr/bin/env python3
"""Run the existing execution acceptance against an owned, disposable API/worker/database.

Default: 2,000 logical targets. No production database, hardware adapters, or
transport credentials are inherited. Logs and resource evidence stay private.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import secrets
import shutil
import signal
import socket
import subprocess
import tempfile
import time
import urllib.parse
import urllib.request
import uuid

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--targets', type=int, choices=[100, 2000], default=2000)
args = parser.parse_args()
os.umask(0o077)
root = Path(__file__).resolve().parents[1]
os.chdir(root)
work = Path(tempfile.mkdtemp(prefix='keel-execution-capacity-'))
# Retain large binaries on disk without changing private /tmp evidence paths.
evidence_root = Path.home() / '.local/state/keel/evidence/capacity'
evidence_root.mkdir(parents=True, exist_ok=True, mode=0o700)
evidence_root.chmod(0o700)
binary_directory = evidence_root / work.name
binary_directory.mkdir(mode=0o700)
lock = Path('/tmp/keel-isolated-api-3102.lock')
container = 'keel-auth-test-postgres'
database = 'keel_test_capacity_' + uuid.uuid4().hex[:12]
processes, peaks = {}, {}
locked = created = False
owner = None
print('Execution evidence directory: ' + str(work), flush=True)


def checked(command, env=None, name='setup'):
    with (work / (name + '.log')).open('w') as log:
        result = subprocess.run(command, env=env, stdout=log, stderr=subprocess.STDOUT)
    if result.returncode:
        raise RuntimeError(name + ' failed; inspect private ' + str(work / (name + '.log')))


def database_size():
    return int(subprocess.check_output(['docker', 'exec', container, 'psql', '-U', owner, '-d', database, '-Atc', 'SELECT pg_database_size(current_database())'], text=True).strip())


def resources():
    disk = shutil.disk_usage(root)
    net = next(line.split(':', 1)[1].split() for line in Path('/proc/net/dev').read_text().splitlines() if line.split(':', 1)[0].strip() == 'lo')
    stats = json.loads(subprocess.check_output(['docker', 'stats', '--no-stream', '--format', '{{json .}}', container], text=True))
    db_resources = {key: stats[key] for key in ['CPUPerc', 'MemUsage', 'BlockIO', 'NetIO', 'PIDs']}
    return {'database_container_observed': db_resources, 'utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'disk_total_bytes': disk.total, 'disk_free_bytes': disk.free, 'host_loopback_rx_bytes': int(net[0]), 'host_loopback_tx_bytes': int(net[8]), 'database_bytes': database_size()}


def sample_processes():
    for name, process in processes.items():
        try:
            status = dict(line.split(':', 1) for line in Path(f'/proc/{process.pid}/status').read_text().splitlines() if ':' in line)
            stat = Path(f'/proc/{process.pid}/stat').read_text().rsplit(')', 1)[1].split()
            item = peaks.setdefault(name, {'peak_rss_kib': 0, 'cpu_seconds_observed': 0})
            item['peak_rss_kib'] = max(item['peak_rss_kib'], int(status.get('VmHWM', '0 kB').split()[0]))
            item['cpu_seconds_observed'] = (int(stat[11]) + int(stat[12])) / os.sysconf('SC_CLK_TCK')
        except (FileNotFoundError, ProcessLookupError):
            pass


def interrupted(_signum, _frame):
    raise KeyboardInterrupt('Interrupted; removing only owned test processes/database')


signal.signal(signal.SIGTERM, interrupted)
try:
    lock.mkdir(mode=0o700)
    locked = True
    with socket.socket() as probe:
        probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        probe.bind(('127.0.0.1', 3102))
    record = json.loads(subprocess.check_output(['docker', 'inspect', container]))[0]
    assert record['Config'].get('Labels', {}).get('io.keel.test-environment') == 'disposable', 'Disposable database label required'
    config = dict(value.split('=', 1) for value in record['Config']['Env'] if '=' in value)
    owner, password = config['POSTGRES_USER'], config['POSTGRES_PASSWORD']
    checked(['docker', 'exec', container, 'createdb', '-U', owner, database], name='create-database')
    created = True
    # Keep toolchain settings (including an explicitly supplied Go overlay), but
    # never let a production KEEL_* setting enable a hardware adapter or service.
    env = {key: value for key, value in os.environ.items() if not key.startswith('KEEL_')}
    env.update(KEEL_DATABASE_URL='postgresql://' + urllib.parse.quote(owner, safe='') + ':' + urllib.parse.quote(password, safe='') + '@127.0.0.1:15433/' + database + '?sslmode=disable',
               KEEL_ENV='development', KEEL_INSECURE_DEV_COOKIE='true', KEEL_PUBLIC_ORIGINS='http://127.0.0.1:3102', KEEL_LISTEN_ADDR='127.0.0.1:3102', KEEL_STATIC_DIR=str(root / 'web/dist'),
               KEEL_WORKER_ID='capacity-' + uuid.uuid4().hex[:12], KEEL_WORKER_CONCURRENCY='8', KEEL_WORKER_LEASE='5s', KEEL_SIM_TEST_TARGETS=str(args.targets), KEEL_SIM_REPORT_FILE=str(work / 'simulation-report.json'))
    for target in ['api', 'admin', 'worker']:
        executable = binary_directory / ('keel-' + target)
        checked(['go', 'build', '-o', str(executable), './cmd/keel-' + target], env, 'build-' + target)
        (work / ('keel-' + target)).symlink_to(executable)
    checked([str(work / 'keel-admin'), 'migrate'], env, 'migrate')
    secret = work / 'password'
    secret.write_text(secrets.token_urlsafe(32))
    checked([str(work / 'keel-admin'), 'init-admin', '--username', 'capacityadmin', '--display-name', 'Capacity Admin', '--password-file', str(secret)], env, 'init-admin')
    credentials = work / 'credentials.json'
    credentials.write_text(json.dumps({'base_url': 'http://127.0.0.1:3102', 'username': 'capacityadmin', 'password': secret.read_text()}))
    env['KEEL_AUTH_BROWSER_CREDENTIALS_FILE'] = str(credentials)
    for name in ['api', 'worker']:
        processes[name] = subprocess.Popen([str(work / ('keel-' + name))], env=env, stdout=(work / (name + '.log')).open('w'), stderr=subprocess.STDOUT)
    for _ in range(100):
        assert all(p.poll() is None for p in processes.values()), 'Owned API/worker exited; inspect private logs'
        try:
            urllib.request.urlopen('http://127.0.0.1:3102/healthz', timeout=1).close()
            break
        except OSError:
            time.sleep(.1)
    else:
        raise RuntimeError('Isolated API readiness timed out')
    for _ in range(50):
        heartbeat = subprocess.run([str(work / 'keel-worker'), '--healthcheck'], env=env, capture_output=True)
        if heartbeat.returncode == 0:
            break
        time.sleep(.1)
    else:
        raise RuntimeError('Owned worker heartbeat not ready')
    source = root / 'scripts/check-execution-integration.py'
    evidence = {'scope': 'isolated real API + PostgreSQL + worker; simulated commands only', 'source_revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(),
                'tracked_workspace_modified': bool(subprocess.check_output(['git', 'diff', 'HEAD', '--name-only'])), 'tracked_diff_sha256': hashlib.sha256(subprocess.check_output(['git', 'diff', 'HEAD', '--binary'])).hexdigest(), 'go_overlay_applied': '-overlay' in env.get('GOFLAGS', ''), 'binaries_sha256': {name: hashlib.sha256((work / ('keel-' + name)).read_bytes()).hexdigest() for name in ['api', 'admin', 'worker']}, 'runner_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), 'check_sha256': hashlib.sha256(source.read_bytes()).hexdigest(),
                'architecture': platform.machine(), 'logical_cpus': os.cpu_count(), 'memory_total_kib': int(next(line.split()[1] for line in Path('/proc/meminfo').read_text().splitlines() if line.startswith('MemTotal:'))),
                'database_container': {'image': record['Config']['Image'], 'cpu_quota': record['HostConfig']['CpuQuota'], 'nano_cpus': record['HostConfig']['NanoCpus'], 'memory_limit_bytes': record['HostConfig']['Memory']},
                'worker_lease_seconds': 5, 'go_memory_limit': env.get('GOMEMLIMIT', 'Go default'), 'go_max_procs': env.get('GOMAXPROCS', 'Go default'), 'retained_binaries_directory': str(binary_directory), 'hardware_adapters_enabled': False, 'redis_used': False, 'object_storage_used': False, 'shared_host_resources': True, 'network_counter_scope': 'host loopback includes unrelated shared-host traffic; not hardware transport bytes', 'before': resources()}
    started = time.monotonic()
    with (work / 'http.log').open('w') as log:
        processes['acceptance'] = subprocess.Popen(['python3', str(source)], env=env, stdout=log, stderr=subprocess.STDOUT)
        while processes['acceptance'].poll() is None:
            sample_processes()
            assert processes['api'].poll() is None and processes['worker'].poll() is None, 'Owned API/worker exited during acceptance'
            if time.monotonic() - started > 1200:
                raise RuntimeError('Acceptance timeout; inspect private http.log')
            time.sleep(.25)
    sample_processes()
    evidence.update(elapsed_seconds=round(time.monotonic() - started, 3), after=resources(), process_resources=peaks, acceptance_exit_code=processes['acceptance'].returncode)
    (work / 'resource-report.json').write_text(json.dumps(evidence, indent=2) + '\n')
    if processes['acceptance'].returncode != 0:
        diagnostic = subprocess.check_output(['docker', 'exec', container, 'psql', '-U', owner, '-d', database, '-Atc', "SELECT coalesce(json_agg(row_to_json(d)),'[]'::json) FROM (SELECT a.id,a.state,a.lease_until,a.started_at,r.result AS receipt_result,r.observed_at,r.payload->>'finished_at' AS receipt_finished_at FROM exec_task_attempts a LEFT JOIN exec_runner_receipts r ON r.attempt_id=a.id WHERE a.state IN('unknown','running') ORDER BY a.started_at) d"], text=True)
        (work / 'failure-attempts.json').write_text(diagnostic)
        raise AssertionError('Execution acceptance failed; inspect private ' + str(work / 'http.log'))
    report = json.loads((work / 'simulation-report.json').read_text())
    assert report['logical_targets'] == args.targets and report['success'] == args.targets and report['hardware_evidence'] is False
    print((work / 'http.log').read_text(), flush=True)
    print('PASS resources recorded separately for API, worker, test client and disposable database; hardware transports disabled', flush=True)
finally:
    for name in ['acceptance', 'worker', 'api']:
        process = processes.get(name)
        if process is not None and process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
    try:
        if created:
            assert database.startswith('keel_test_capacity_') and len(database) == len('keel_test_capacity_') + 12
            checked(['docker', 'exec', container, 'dropdb', '--if-exists', '-U', owner, database], name='drop-database')
    finally:
        for name in ['password', 'credentials.json']:
            (work / name).unlink(missing_ok=True)
        if locked:
            lock.rmdir()
    print('Owned API/worker/database and temporary credentials removed; private evidence retained.', flush=True)
