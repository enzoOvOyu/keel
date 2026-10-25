#!/usr/bin/env python3
"""Real isolated PostgreSQL/API/browser checks for shared controls and fonts."""
import json, os, secrets, socket, subprocess, tempfile, time, urllib.parse, urllib.request, uuid
from pathlib import Path
os.umask(0o077)
root = Path(__file__).resolve().parents[1]
os.chdir(root)
with socket.socket() as probe:
    probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    probe.bind(('127.0.0.1', 3109))
container = os.environ.get('KEEL_UI_TEST_PG_CONTAINER', 'keel-auth-test-postgres')
record = json.loads(subprocess.check_output(['docker', 'inspect', container]))[0]
assert record['Config']['Labels'].get('io.keel.test-environment') == 'disposable'
config = dict(v.split('=', 1) for v in record['Config']['Env'] if '=' in v)
owner = config['POSTGRES_USER']
database = 'keel_test_ui_' + uuid.uuid4().hex[:12]
work = Path(tempfile.mkdtemp(prefix='keel-ui-'))
# Keep large Go binaries off memory-backed /tmp on development hosts.
binary_root = Path.home() / '.local/state/keel/evidence/ui-binaries'
binary_root.mkdir(parents=True, exist_ok=True, mode=0o700)
binary_dir = Path(tempfile.mkdtemp(prefix='ui-', dir=binary_root))
created = False
api = None
print('UI evidence directory:', work, flush=True)
try:
    subprocess.run(['docker', 'exec', container, 'createdb', '-U', owner, database], check=True, capture_output=True)
    created = True
    env = {k: v for k, v in os.environ.items() if not k.startswith('KEEL_')}
    env.update(KEEL_DATABASE_URL='postgresql://' + urllib.parse.quote(owner, safe='') + ':' + urllib.parse.quote(config['POSTGRES_PASSWORD'], safe='') + '@127.0.0.1:15433/' + database + '?sslmode=disable', KEEL_ENV='development', KEEL_INSECURE_DEV_COOKIE='true', KEEL_PUBLIC_ORIGINS='http://127.0.0.1:3109', KEEL_LISTEN_ADDR='127.0.0.1:3109', KEEL_STATIC_DIR=str(root / 'web/dist'))
    with (work / 'setup.log').open('x') as log:
        for target in ['api', 'admin']:
            subprocess.run(['go', 'build', '-o', str(binary_dir / ('keel-' + target)), './cmd/keel-' + target], check=True, env=env, stdout=log, stderr=log)
        subprocess.run([str(binary_dir / 'keel-admin'), 'migrate'], env=env, check=True, stdout=log, stderr=log)
        password = secrets.token_urlsafe(28)
        secret = work / 'password'
        secret.write_text(password)
        subprocess.run([str(binary_dir / 'keel-admin'), 'init-admin', '--username', 'uiadmin', '--display-name', 'UI Admin', '--password-file', str(secret)], env=env, check=True, stdout=log, stderr=log)
    api = subprocess.Popen([str(binary_dir / 'keel-api')], env=env, stdout=(work / 'api.log').open('w'), stderr=subprocess.STDOUT)
    for _ in range(150):
        if api.poll() is not None:
            raise RuntimeError('Isolated API exited; inspect private evidence')
        try:
            urllib.request.urlopen('http://127.0.0.1:3109/healthz', timeout=1)
            break
        except Exception:
            time.sleep(.1)
    else:
        raise RuntimeError('Isolated API did not become ready')
    credentials = work / 'credentials.json'
    credentials.write_text(json.dumps(dict(base_url='http://127.0.0.1:3109', username='uiadmin', password=password)))
    env.update(KEEL_AUTH_BROWSER_CREDENTIALS_FILE=str(credentials), KEEL_WORKSPACE_EVIDENCE_DIR=str(work))
    with (work / 'browser.log').open('x') as log:
        result = subprocess.run(['node', 'scripts/check-ui-migration-browser.mjs'], env=env, stdout=log, stderr=log)
    print('Browser exit:', result.returncode, flush=True)
    raise SystemExit(result.returncode)
finally:
    if api is not None:
        api.terminate()
        try:
            api.wait(timeout=15)
        except subprocess.TimeoutExpired:
            api.kill()
            api.wait()
    if created:
        subprocess.run(['docker', 'exec', container, 'dropdb', '-U', owner, database], check=True, capture_output=True)
    for name in ['password', 'credentials.json']:
        (work / name).unlink(missing_ok=True)
