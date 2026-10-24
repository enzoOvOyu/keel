#!/usr/bin/env python3
"""Real PostgreSQL + disposable MinIO + API + browser media verification."""
import argparse,json,os,secrets,socket,subprocess,tempfile,time,urllib.parse,urllib.request,uuid
from pathlib import Path
parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--catalog-only',action='store_true',help='Run only the catalog API/worker/browser check in a fresh isolated environment; skip Go and unchanged media checks');parser.add_argument('--management-only',action='store_true',help='Run only the catalog management browser check in a fresh isolated environment');parser.add_argument('--global-media-only',action='store_true',help='Run only global media API/browser checks in a fresh isolated environment');parser.add_argument('--media-conflict-only',action='store_true',help='Run only media confirmation concurrency browser checks');args=parser.parse_args()
if sum([args.catalog_only,args.management_only,args.global_media_only,args.media_conflict_only])>1:parser.error('Choose one focused check')
os.umask(0o077);root=Path(__file__).resolve().parents[1];os.chdir(root)
probe=socket.socket();probe.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1);probe.bind(('127.0.0.1',3102));probe.close()
record=json.loads(subprocess.check_output(['docker','inspect','keel-auth-test-postgres']))[0]
assert record['Config']['Labels'].get('io.keel.test-environment')=='disposable'
config=dict(v.split('=',1) for v in record['Config']['Env'] if '=' in v);owner,password=config['POSTGRES_USER'],config['POSTGRES_PASSWORD']
database='keel_test_media_'+uuid.uuid4().hex[:12];unit_database=database+'_unit';work=Path(tempfile.mkdtemp(prefix='keel-media-'));api=worker=None
print('Media evidence directory: '+str(work),flush=True)
def checked(command,env=None):
 result=subprocess.run(command,env=env,text=True,capture_output=True)
 if result.returncode:
  (work/'failure.log').write_text(result.stdout+result.stderr)
  raise RuntimeError('Media integration command failed: '+command[0]+'; inspect protected failure.log')
 return result.stdout
try:
 for name in [database,unit_database]:checked(['docker','exec','keel-auth-test-postgres','createdb','-U',owner,name])
 env=os.environ.copy();env.update(KEEL_ENV='development',KEEL_INSECURE_DEV_COOKIE='true',KEEL_PUBLIC_ORIGINS='http://127.0.0.1:3102',KEEL_LISTEN_ADDR='127.0.0.1:3102',KEEL_STATIC_DIR=str(root/'web/dist'))
 checked(['python3','scripts/test-media-environment.py','start','--directory',str(work)])
 env.update(dict(line.split('=',1) for line in (work/'media.env').read_text().splitlines()))
 dsn='postgresql://'+urllib.parse.quote(owner,safe='')+':'+urllib.parse.quote(password,safe='')+'@127.0.0.1:15433/'
 env.update(KEEL_DATABASE_URL=dsn+database+'?sslmode=disable',KEEL_TEST_DATABASE_URL=dsn+unit_database+'?sslmode=disable')
 for target in ['api','admin','worker']:checked(['go','build','-o',str(work/('keel-'+target)),'./cmd/keel-'+target],env)
 checked([str(work/'keel-admin'),'media-init'],env)
 if not (args.catalog_only or args.management_only or args.global_media_only or args.media_conflict_only):
  result=checked(['go','test','-race','./internal/mediaartifact','./internal/platform','-run','TestRejectUnsafeMediaConfiguration|TestRealMinIOVersionPinning|TestMediaArtifactsLifecycleAndIntegrity|TestMediaConditionalTransitions|TestCatalogArtifactAdmission|TestCatalogCorrelatedCombinations|TestCatalogManagement|TestGlobalCatalogScopeBoundaries|TestCompatibility|TestZTP239ApprovedIsolationScript|TestSZTP143|TestZTPMockKeaDurableStageCommit|TestBOMPackage','-count=1'],env)
  (work/'go-integration.log').write_text(result);print(result,flush=True)
 checked([str(work/'keel-admin'),'migrate'],env)
 pw=secrets.token_urlsafe(28);secret=work/'password';secret.write_text(pw)
 checked([str(work/'keel-admin'),'init-admin','--username','mediaadmin','--display-name','Media Admin','--password-file',str(secret)],env)
 api=subprocess.Popen([str(work/'keel-api')],env=env,stdout=(work/'api.log').open('w'),stderr=subprocess.STDOUT)
 for _ in range(100):
  if api.poll() is not None:raise RuntimeError('Isolated API exited; inspect protected api.log')
  try:urllib.request.urlopen('http://127.0.0.1:3102/healthz',timeout=1);break
  except Exception:time.sleep(.1)
 else:raise RuntimeError('API did not become ready')
 worker=subprocess.Popen([str(work/'keel-worker')],env=env,stdout=(work/'worker.log').open('w'),stderr=subprocess.STDOUT)
 credentials=work/'credentials.json';credentials.write_text(json.dumps(dict(base_url='http://127.0.0.1:3102',username='mediaadmin',password=pw)))
 env.update(KEEL_AUTH_BROWSER_CREDENTIALS_FILE=str(credentials),KEEL_WORKSPACE_EVIDENCE_DIR=str(work))
 checks=[(['python3','scripts/check-media-integration.py'],'legacy-media'),(['python3','scripts/check-param-integration.py'],'parameter-regression'),(['node','scripts/check-media-browser.mjs'],'legacy-browser'),(['node','scripts/check-media-artifacts-browser.mjs'],'artifact-browser'),(['node','scripts/check-catalog-browser.mjs'],'catalog-browser'),(['node','scripts/check-catalog-management-browser.mjs'],'catalog-management-browser'),(['node','scripts/check-global-media-browser.mjs'],'global-media-browser'),(['node','scripts/check-media-conflict-browser.mjs'],'media-conflict-browser')]
 for command,name in ([check for check in checks if check[1] == 'media-conflict-browser'] if args.media_conflict_only else [check for check in checks if check[1] == 'global-media-browser'] if args.global_media_only else [check for check in checks if check[1] == 'catalog-management-browser'] if args.management_only else [check for check in checks if check[1].startswith('catalog-')] if args.catalog_only else checks):
  result=checked(command,env);(work/(name+'.log')).write_text(result);print(result,flush=True)
finally:
 if worker is not None:
  worker.terminate()
  try:worker.wait(timeout=10)
  except subprocess.TimeoutExpired:worker.kill();worker.wait()
 if api is not None:
  api.terminate()
  try:api.wait(timeout=10)
  except subprocess.TimeoutExpired:api.kill();api.wait()
 if (work/'media-container').exists():subprocess.run(['python3','scripts/test-media-environment.py','stop','--directory',str(work)],capture_output=True)
 for name in [database,unit_database]:subprocess.run(['docker','exec','keel-auth-test-postgres','dropdb','--if-exists','-U',owner,name],capture_output=True)
 for name in ['password','credentials.json']:(work/name).unlink(missing_ok=True)
 print('Isolated API/databases/MinIO and temporary credentials removed; evidence retained.',flush=True)
