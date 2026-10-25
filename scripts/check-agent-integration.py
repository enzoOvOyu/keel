#!/usr/bin/env python3
"""Real PostgreSQL + verified TLS/gRPC identity, never a hardware acceptance."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import urllib.parse
import uuid

os.umask(0o077)
root = Path(__file__).resolve().parents[1]
os.chdir(root)
work = Path(tempfile.mkdtemp(prefix='keel-agent-integration-'))
container = 'keel-auth-test-postgres'
database = 'keel_test_agent_' + uuid.uuid4().hex[:12]
created = False
try:
    record = json.loads(subprocess.check_output(['docker', 'inspect', container]))[0]
    assert record['Config'].get('Labels', {}).get('io.keel.test-environment') == 'disposable'
    config = dict(value.split('=', 1) for value in record['Config']['Env'] if '=' in value)
    owner = config['POSTGRES_USER']
    subprocess.run(['docker', 'exec', container, 'createdb', '-U', owner, database], check=True, capture_output=True)
    created = True
    env = {key: value for key, value in os.environ.items() if not key.startswith('KEEL_')}
    env['KEEL_TEST_AGENT_DATABASE_URL'] = 'postgresql://' + urllib.parse.quote(owner, safe='') + ':' + urllib.parse.quote(config['POSTGRES_PASSWORD'], safe='') + '@127.0.0.1:15433/' + database + '?sslmode=disable'
    with (work / 'go-integration.log').open('x') as log:
        for name in ('admin', 'agent'):
            binary = work / ('keel-' + name)
            subprocess.run(['go', 'build', '-o', str(binary), './cmd/keel-' + name], env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
            env['KEEL_TEST_AGENT_' + name.upper() + '_BIN'] = str(binary)
        result = subprocess.run(['go', 'test', '-p=1', '-race', '-count=1', '-v', './internal/agentpki', './internal/agentidentity', './internal/agentclient', './internal/platform', '-run', 'TestAgent|TestProtected|TestWriteNew|TestAuthority|TestIssued|TestCSR'], env=env, stdout=log, stderr=subprocess.STDOUT)
    (work / 'result.json').write_text(json.dumps({'exit_code': result.returncode, 'real_postgresql': True, 'verified_loopback_tls_grpc': True, 'physical_device_acceptance': False}))
    print('Agent evidence:', work, 'exit:', result.returncode)
    raise SystemExit(result.returncode)
finally:
    if created:
        subprocess.run(['docker', 'exec', container, 'dropdb', '-U', owner, database], check=True, capture_output=True)
