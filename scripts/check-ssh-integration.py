#!/usr/bin/env python3
"""Run the SSH/Execution contract on an owned disposable PostgreSQL database.

The Go test owns loopback SSH + HTTP listeners and private ephemeral keys. It
seeds a software target identity; it does not bootstrap or contact real devices.
"""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import urllib.parse
import uuid
import shutil
import datetime
import argparse

parser = argparse.ArgumentParser()
parser.add_argument('--run', default='', help='Run only matching Go SSH tests; evidence records this narrower scope')
args = parser.parse_args()

os.umask(0o077)
root = Path(__file__).resolve().parents[1]
os.chdir(root)
work = Path(tempfile.mkdtemp(prefix='keel-ssh-integration-'))
container = 'keel-auth-test-postgres'
database = 'keel_test_ssh_' + uuid.uuid4().hex[:12]
created = False
owner = None
master_key = work / 'cli-master.key'
try:
    record = json.loads(subprocess.check_output(['docker', 'inspect', container]))[0]
    assert record['Config'].get('Labels', {}).get('io.keel.test-environment') == 'disposable', 'Disposable database label required'
    config = dict(value.split('=', 1) for value in record['Config']['Env'] if '=' in value)
    owner = config['POSTGRES_USER']
    subprocess.run(['docker', 'exec', container, 'createdb', '-U', owner, database], check=True, capture_output=True)
    created = True
    env = {key: value for key, value in os.environ.items() if not key.startswith('KEEL_')}
    env['KEEL_TEST_SSH_DATABASE_URL'] = 'postgresql://' + urllib.parse.quote(owner, safe='') + ':' + urllib.parse.quote(config['POSTGRES_PASSWORD'], safe='') + '@127.0.0.1:15433/' + database + '?sslmode=disable'
    env['KEEL_TEST_SSH_STATIC_DIR'] = str(root / 'web/dist')
    env['KEEL_TEST_SSH_BROWSER_SCRIPT'] = str(root / 'scripts/check-ssh-browser.mjs')
    env['KEEL_TEST_SSH_TRUST_BROWSER_SCRIPT'] = str(root / 'scripts/check-ssh-trust-browser.mjs')
    env['KEEL_TEST_SSH_DOMAIN_BROWSER_SCRIPT'] = str(root / 'scripts/check-ssh-domain-keys-browser.mjs')
    env['KEEL_TEST_SSH_INSTALL_BROWSER_SCRIPT'] = str(root / 'scripts/check-ssh-install-browser.mjs')
    assert (root / 'web/dist/index.html').is_file(), 'Build the current frontend first'
    with (work / 'go-integration.log').open('w') as log:
        command = ['go', 'test', '-race', '-count=1', '-v', './internal/taskrunner']
        if args.run:
            command += ['-run', args.run]
        result = subprocess.run(command, env=env, stdout=log, stderr=subprocess.STDOUT)
    if result.returncode == 0:
        binaries = Path.home() / '.local/state/keel/evidence/ssh' / work.name
        binaries.mkdir(mode=0o700, parents=True)
        binary = binaries / 'keel-ssh-runner'
        with (work / 'build-wrapper.log').open('w') as log:
            subprocess.run(['go', 'build', '-trimpath', '-o', str(binary), './cmd/keel-ssh-runner'], env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
        admin_binary = binaries / 'keel-admin'
        with (work / 'build-admin.log').open('w') as log:
            subprocess.run(['go', 'build', '-trimpath', '-o', str(admin_binary), './cmd/keel-admin'], env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
        project = subprocess.check_output(['docker', 'exec', container, 'psql', '-X', '-U', owner, '-d', database, '-Atc', 'SELECT id FROM projects ORDER BY created_at LIMIT 1'], text=True).strip()
        assert len(project) == 32, 'Actual SSH tests must create a project before CLI verification'
        admin_env = dict(env, KEEL_DATABASE_URL=env['KEEL_TEST_SSH_DATABASE_URL'], KEEL_CREDENTIAL_MASTER_KEY_FILE=str(master_key))
        def admin(*args, success=True):
            process = subprocess.run([str(admin_binary), *args], text=True, capture_output=True, env=admin_env, timeout=30)
            assert (process.returncode == 0) == success, 'Credential CLI command contract failed: ' + args[0]
            assert 'PRIVATE KEY' not in process.stdout and 'ciphertext' not in process.stdout
            return json.loads(process.stdout) if success else None
        admin('credential-master-keygen', '--output', str(master_key))
        original_master = master_key.read_bytes()
        admin('credential-master-keygen', '--output', str(master_key), success=False)
        assert master_key.read_bytes() == original_master and master_key.stat().st_mode & 0o077 == 0
        expiry = (datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(hours=1)).isoformat()
        key = admin('ssh-key-create', '--project', project, '--ref', 'keel-ctrl-cli', '--version', 'v1', '--expires-at', expiry)
        assert key['public_key'].startswith('ssh-ed25519 ') and key['state_version'] == 1
        admin('ssh-key-create', '--project', project, '--ref', 'keel-ctrl-cli', '--version', 'v1', '--expires-at', expiry, success=False)
        assert any(item['id'] == key['id'] for item in admin('ssh-key-list', '--project', project))
        revoke_args = ('ssh-key-revoke', '--project', project, '--id', key['id'], '--expected-version', '1', '--reason', 'Disposable CLI lifecycle verification')
        revoked = admin(*revoke_args)
        assert revoked['state_version'] == 2 and revoked['revoked_at']
        assert admin(*revoke_args) == revoked, 'Exact revoke retry must preserve evidence'
        (work / 'credential-cli-proof.json').write_text(json.dumps({'status': 'passed', 'exclusive_master_creation': True, 'public_output_only': True, 'version_reuse_rejected': True, 'idempotent_revocation': True}) + '\n')
        master_key.unlink()
        receipts = work / 'cli-receipts'
        receipts.mkdir(mode=0o700)
        cli_env = dict(env, KEEL_SSH_RECEIPT_DIRECTORY=str(receipts))
        command = dict(api_version='keel.task/v1', project_id=uuid.uuid4().hex, machine_id=uuid.uuid4().hex, command_id=uuid.uuid4().hex, attempt_id=uuid.uuid4().hex, fencing_token=1, operation='host.identity', timeout_ms=5000, wait_ms=0)
        def invoke(action):
            process = subprocess.run([str(binary)], input=json.dumps(dict(action=action, command=command)), text=True, capture_output=True, env=cli_env, timeout=10, check=True)
            return json.loads(process.stdout)
        first, second, third = invoke('run'), invoke('query'), invoke('run')
        assert first == second == third and first['state'] == 'success', 'Separate wrapper processes must reuse the exact persisted receipt'
        (work / 'wrapper-process-proof.json').write_text(json.dumps(first, indent=2) + '\n')
        shutil.rmtree(receipts)  # Only this invocation's explicitly owned fixture directory.
    proof = {'source_revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(), 'tracked_changes': bool(subprocess.check_output(['git', 'diff', 'HEAD', '--name-only'])), 'scope': 'real PostgreSQL + loopback SSH/HTTP protocol; seeded software identity, not real device/trust bootstrap', 'test_filter': args.run or 'all', 'exit_code': result.returncode}
    (work / 'report.json').write_text(json.dumps(proof, indent=2) + '\n')
    if result.returncode:
        raise SystemExit('FAIL: inspect private evidence ' + str(work))
    print('PASS actual PostgreSQL/SSH/HTTP contract; evidence ' + str(work))
finally:
    master_key.unlink(missing_ok=True)
    if created:
        subprocess.run(['docker', 'exec', container, 'dropdb', '-U', owner, database], check=True, capture_output=True)
