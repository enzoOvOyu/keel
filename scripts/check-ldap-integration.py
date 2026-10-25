#!/usr/bin/env python3
"""Verified LDAPS binds, PostgreSQL login boundaries and real browser proof."""
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
work = Path(tempfile.mkdtemp(prefix='keel-ldap-integration-'))
container = 'keel-auth-test-postgres'
database = 'keel_test_ldap_' + uuid.uuid4().hex[:12]
created = False
try:
    record = json.loads(subprocess.check_output(['docker', 'inspect', container]))[0]
    assert record['Config'].get('Labels', {}).get('io.keel.test-environment') == 'disposable'
    config = dict(value.split('=', 1) for value in record['Config']['Env'] if '=' in value)
    owner = config['POSTGRES_USER']
    subprocess.run(['docker', 'exec', container, 'createdb', '-U', owner, database], check=True, capture_output=True)
    created = True
    env = {key: value for key, value in os.environ.items() if not key.startswith('KEEL_')}
    env['KEEL_TEST_LDAP_DATABASE_URL'] = 'postgresql://' + urllib.parse.quote(owner, safe='') + ':' + urllib.parse.quote(config['POSTGRES_PASSWORD'], safe='') + '@127.0.0.1:15433/' + database + '?sslmode=disable'
    env['KEEL_TEST_LDAP_STATIC_DIR'] = str(root / 'web/dist')
    env['KEEL_TEST_LDAP_BROWSER_SCRIPT'] = str(root / 'scripts/check-ldap-browser.mjs')
    assert (root / 'web/dist/index.html').is_file(), 'Build current frontend before browser verification'
    with (work / 'go-integration.log').open('x') as log:
        result = subprocess.run(['go', 'test', '-race', '-count=1', '-v', './internal/ldapauth'], env=env, stdout=log, stderr=subprocess.STDOUT)
    (work / 'result.json').write_text(json.dumps({'exit_code': result.returncode, 'real_postgresql': True, 'real_loopback_ldaps_fixture': True, 'browser': True, 'external_directory_acceptance': False}))
    print('LDAP evidence:', work, 'exit:', result.returncode)
    raise SystemExit(result.returncode)
finally:
    if created:
        subprocess.run(['docker', 'exec', container, 'dropdb', '-U', owner, database], check=True, capture_output=True)
