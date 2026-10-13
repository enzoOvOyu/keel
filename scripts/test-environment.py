#!/usr/bin/env python3
"""Manage only the disposable, labelled Keel PostgreSQL test container."""
import argparse
import json
import os
from pathlib import Path
import secrets
import subprocess
import time

NAME = 'keel-auth-test-postgres'
LABEL = 'io.keel.test-environment'
IMAGE = 'postgres:16-alpine@sha256:721873c34ceb9f8d8fc265984940dc982404c105f19ad51be9fdc5970a6080ea'
FILES = [Path('/opt/keel-tools/state/keel-test-postgres.env'), Path('/opt/keel-tools/state/keel-test-db.env')]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('action', choices=['start', 'stop', 'status'])
args = parser.parse_args()

def run(command):
    result = subprocess.run(command, text=True, capture_output=True)
    if result.returncode:
        raise SystemExit('Test environment operation failed: ' + command[0] + ' ' + command[1] + '; inspect locally without printing credentials.')
    return result.stdout.strip()

def inspect():
    result = subprocess.run(['docker','inspect',NAME], text=True, capture_output=True)
    if result.returncode: return None
    record = json.loads(result.stdout)[0]
    if record.get('Config',{}).get('Labels',{}).get(LABEL) != 'disposable':
        raise SystemExit('Refusing to manage an existing container without the Keel disposable-test label.')
    return record

def private_file(path, value):
    if path.exists(): raise SystemExit('Refusing to overwrite existing test configuration: ' + str(path))
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd,'w') as out: out.write(value)

state = inspect()
if args.action == 'status':
    print('absent' if not state else state['State'].get('Health',{}).get('Status',state['State']['Status']))
    raise SystemExit(0)
if args.action == 'stop':
    if state: run(['docker','rm','-f',NAME])
    for path in FILES: path.unlink(missing_ok=True)
    print('Disposable test PostgreSQL removed; application data volumes were not involved.')
    raise SystemExit(0)
if state:
    if not all(p.is_file() and p.stat().st_mode & 0o077 == 0 for p in FILES):
        raise SystemExit('Existing test environment is missing private configuration; inspect before changing it.')
    print('Disposable test environment already exists; preserving its databases.')
    raise SystemExit(0)
if any(p.exists() for p in FILES):
    raise SystemExit('Orphaned test configuration exists; inspect/remove it before start.')
password = secrets.token_urlsafe(36)
private_file(FILES[0], 'POSTGRES_USER=keel_test\nPOSTGRES_DB=keel_test_auth\nPOSTGRES_PASSWORD='+password+'\n')
private_file(FILES[1], 'KEEL_TEST_DATABASE_URL=postgresql://keel_test:'+password+'@127.0.0.1:15433/keel_test_auth?sslmode=disable\n')
run(['docker','run','-d','--name',NAME,'--label',LABEL+'=disposable','--env-file',str(FILES[0]),
     '-p','127.0.0.1:15433:5432','--tmpfs','/var/lib/postgresql/data:rw',
     '--health-cmd','pg_isready -U keel_test -d keel_test_auth','--health-interval','2s','--health-timeout','3s','--health-retries','20',IMAGE])
for attempt in range(30):
    current = inspect()
    if current['State'].get('Health',{}).get('Status') == 'healthy': break
    time.sleep(1)
else:
    raise SystemExit('Disposable PostgreSQL did not become healthy; inspect the named container.')
for db in ('keel_test_http','keel_test_execution'):
    run(['docker','exec',NAME,'createdb','-U','keel_test',db])
print('Disposable PostgreSQL healthy on 127.0.0.1:15433: keel_test_auth, keel_test_http, keel_test_execution.')
print('Private test configuration: /opt/keel-tools/state/keel-test-db.env (0600).')
