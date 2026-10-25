#!/usr/bin/env python3
"""Isolated real PostgreSQL/API/browser model publication verification."""
import argparse,json,os,secrets,socket,subprocess,tempfile,time,urllib.parse,urllib.request,uuid
from pathlib import Path
parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--global-models-only',action='store_true',help='Run only the global-model browser check in a fresh isolated API database');parser.add_argument('--publication-only',action='store_true',help='Run only the complete-publication browser check in a fresh isolated API database');parser.add_argument('--asset-models-only',action='store_true',help='Run only the pinned asset model browser check');args=parser.parse_args();assert sum([args.global_models_only,args.publication_only,args.asset_models_only])<=1, 'Choose one focused browser check'
os.umask(0o077);root=Path(__file__).resolve().parents[1];os.chdir(root)
probe=socket.socket();probe.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1);probe.bind(('127.0.0.1',3102));probe.close()
record=json.loads(subprocess.check_output(['docker','inspect','keel-auth-test-postgres']))[0]
assert record['Config']['Labels'].get('io.keel.test-environment')=='disposable'
config=dict(v.split('=',1) for v in record['Config']['Env'] if '=' in v);owner,password=config['POSTGRES_USER'],config['POSTGRES_PASSWORD']
database='keel_test_models_'+uuid.uuid4().hex[:12];unit_database=database+'_unit';work=Path(tempfile.mkdtemp(prefix='keel-models-'));api=None
databases=[database] if (args.publication_only or args.global_models_only or args.asset_models_only) else [database,unit_database]
print('Model evidence directory: '+str(work),flush=True)
def checked(command,env=None):
 result=subprocess.run(command,env=env,text=True,capture_output=True)
 if result.returncode:
  (work/'failure.log').write_text(result.stdout+result.stderr)
  raise RuntimeError('Model verification failed: '+command[0]+'; inspect protected failure.log')
 return result.stdout
try:
 for name in databases:checked(['docker','exec','keel-auth-test-postgres','createdb','-U',owner,name])
 dsn='postgresql://'+urllib.parse.quote(owner,safe='')+':'+urllib.parse.quote(password,safe='')+'@127.0.0.1:15433/'
 env=os.environ.copy();env.update(KEEL_DATABASE_URL=dsn+database+'?sslmode=disable',KEEL_TEST_DATABASE_URL=dsn+unit_database+'?sslmode=disable',KEEL_ENV='development',KEEL_INSECURE_DEV_COOKIE='true',KEEL_PUBLIC_ORIGINS='http://127.0.0.1:3102',KEEL_LISTEN_ADDR='127.0.0.1:3102',KEEL_STATIC_DIR=str(root/'web/dist'))
 for target in ['api','admin']:checked(['go','build','-o',str(work/('keel-'+target)),'./cmd/keel-'+target],env)
 if not (args.publication_only or args.global_models_only or args.asset_models_only):
  output=checked(['go','test','-race','./internal/platform','-run','TestBOMResolvedComponentMACs|TestBOMComponentSummary|TestAssetModelBinding|TestModelPanelLayout|TestModelSlotIdentifiers|TestModelGlobalScope|TestModelPayloadCanonicalHash|TestModelPublicationPreservesAndSeals|TestServerModelHardware|TestModelNICCount|TestModelMACPrefix|TestModelGPU|TestServerGPU|TestBOMPackage|TestLegacyBOMAtomic|TestSwitchModel','-count=1'],env);(work/'go-integration.log').write_text(output);print(output,flush=True)
 checked([str(work/'keel-admin'),'migrate'],env)
 pw=secrets.token_urlsafe(28);secret=work/'password';secret.write_text(pw)
 checked([str(work/'keel-admin'),'init-admin','--username','modeladmin','--display-name','Model Admin','--password-file',str(secret)],env)
 api=subprocess.Popen([str(work/'keel-api')],env=env,stdout=(work/'api.log').open('w'),stderr=subprocess.STDOUT)
 for _ in range(100):
  if api.poll() is not None:raise RuntimeError('Isolated API exited; inspect api.log')
  try:urllib.request.urlopen('http://127.0.0.1:3102/healthz',timeout=1);break
  except Exception:time.sleep(.1)
 else:raise RuntimeError('API did not become ready')
 credentials=work/'credentials.json';credentials.write_text(json.dumps(dict(base_url='http://127.0.0.1:3102',username='modeladmin',password=pw)))
 env.update(KEEL_AUTH_BROWSER_CREDENTIALS_FILE=str(credentials),KEEL_WORKSPACE_EVIDENCE_DIR=str(work))
 checks=[(['python3','scripts/check-model-integration.py'],'model-http'),(['python3','scripts/check-model-import.py'],'import-http'),(['node','scripts/check-model-import-browser.mjs'],'import-browser'),(['node','scripts/check-model-publication-browser.mjs'],'publication-browser')]
 checks.append((['node','scripts/check-asset-model-browser.mjs'],'asset-models-browser'))
 checks.append((['node','scripts/check-compute-tray-browser.mjs'],'compute-tray-browser'))
 checks.append((['node','scripts/check-global-models-browser.mjs'],'global-models-browser'))
 focus='global-models-browser' if args.global_models_only else 'publication-browser' if args.publication_only else 'asset-models-browser' if args.asset_models_only else None
 for command,name in checks:
  if focus and name!=focus:continue
  output=checked(command,env);(work/(name+'.log')).write_text(output);print(output,flush=True)
finally:
 if api is not None:
  api.terminate()
  try:api.wait(timeout=10)
  except subprocess.TimeoutExpired:api.kill();api.wait()
 for name in databases:subprocess.run(['docker','exec','keel-auth-test-postgres','dropdb','--if-exists','-U',owner,name],capture_output=True)
 for name in ['password','credentials.json']:(work/name).unlink(missing_ok=True)
 print('Isolated API/databases/temporary credentials removed; evidence retained.',flush=True)
