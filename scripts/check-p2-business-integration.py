#!/usr/bin/env python3
"""Disposable P2 business chain; external transports are software doubles.
No runtime database, hardware or PoC acceptance. Use --browser for actual UI.
"""
import os,json,subprocess,uuid,time,urllib.parse,sys
from pathlib import Path
os.umask(0o077)
root=Path(__file__).resolve().parents[1];work=Path('/home/ubuntu/.local/state/keel/evidence/p2')/('postgres-'+uuid.uuid4().hex[:10]);work.mkdir(mode=0o700)
container='keel-auth-test-postgres';record=json.loads(subprocess.check_output(['docker','inspect',container]))[0]
assert record['Config']['Labels'].get('io.keel.test-environment')=='disposable'
assert any(p.get('HostIp')=='127.0.0.1' and p.get('HostPort')=='15433' for p in record['NetworkSettings']['Ports']['5432/tcp'])
config=dict(s.split('=',1) for s in record['Config']['Env'] if '=' in s);owner=config['POSTGRES_USER'];password=config['POSTGRES_PASSWORD']
env={k:v for k,v in os.environ.items() if not k.startswith('KEEL_')};tmp=work/'go-tmp';tmp.mkdir(mode=0o700)
ignored=work/'ignored.go';ignored.write_text('//go:build ignore\n\npackage platform\n')
replacements={}
for draft in ['internal/platform/fabric_precheck.go','internal/platform/migrations/026_fabric_precheck.sql']:
 if (root/draft).exists():replacements[str(root/draft)]=str(ignored) if draft.endswith('.go') else ''
overlay=work/'overlay.json';overlay.write_text(json.dumps({'Replace':replacements}))
env.update(GOFLAGS='-overlay='+str(overlay)+' -p=1',GOMEMLIMIT='768MiB',GOMAXPROCS='2',GOTMPDIR=str(tmp))
import importlib.util
spec=importlib.util.spec_from_file_location('p2_fixture',root/'scripts/p2-business-protocol-fixture.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module);fixture=module.P2BusinessFixture(work)
env.update(KEEL_P2_BUSINESS_PROBES_FILE=str(fixture.configuration),KEEL_P2_BUSINESS_SCOPE_FILE=str(fixture.scope))
report={'evidence':str(work),'scope':'isolated PostgreSQL software; no running database, real hardware or PoC acceptance','checks':[]};names=[]
print('Evidence:',work,flush=True)
try:
 for label,command in [('positive-g1-topology',['go','test','-race','./internal/execution','-run','TestP2Positive','-count=1','-timeout=90s','-v'])]:
  name='keel_test_agent_p2_'+label.replace('-','_')+'_'+uuid.uuid4().hex[:8];subprocess.run(['docker','exec',container,'createdb','-U',owner,name],capture_output=True,check=True);names.append(name)
  dsn='postgresql://'+urllib.parse.quote(owner,safe='')+':'+urllib.parse.quote(password,safe='')+'@127.0.0.1:15433/'+name+'?sslmode=disable';current=dict(env,KEEL_DATABASE_URL=dsn,KEEL_TEST_DATABASE_URL=dsn,KEEL_TEST_AGENT_DATABASE_URL=dsn)
  begin=time.monotonic()
  with (work/(label+'.log')).open('w') as log:r=subprocess.run(command,cwd=root,env=current,stdout=log,stderr=subprocess.STDOUT)
  report['checks'].append({'name':label,'command':command,'returncode':r.returncode,'seconds':round(time.monotonic()-begin,2)})
  if r.returncode==0 and label=='schema':report['migration_count']=int(subprocess.check_output(['docker','exec',container,'psql','-U',owner,'-d',name,'-Atc','SELECT count(*) FROM keel_schema_migrations'],text=True).strip())
  (work/'report.json').write_text(json.dumps(report,indent=2)+'\n');print(label,'PASS' if r.returncode==0 else 'FAIL',report['checks'][-1]['seconds'],flush=True)
  if r.returncode:print((work/(label+'.log')).read_text()[-4500:].replace(password,'[redacted]'),flush=True)
  if r.returncode==0 and '--browser' in sys.argv:
   with (work/'business.dump').open('xb') as dump:subprocess.run(['docker','exec',container,'pg_dump','-U',owner,'-d',name,'-Fc'],stdout=dump,stderr=subprocess.PIPE,check=True)
   metadata=json.loads(subprocess.check_output(['docker','exec',container,'psql','-U',owner,'-d',name,'-Atc',"SELECT json_build_object('username',u.username,'project_id',p.id,'hostname',a.hostname) FROM users u JOIN projects p ON p.created_by=u.id JOIN project_assets a ON a.project_id=p.id WHERE u.username LIKE 'agent-kernel-%' AND a.state='mapped' LIMIT 1"],text=True))
   ui=current.copy();ui.update(KEEL_ENV='development',KEEL_INSECURE_DEV_COOKIE='true',KEEL_PUBLIC_ORIGINS='http://127.0.0.1:3109',KEEL_LISTEN_ADDR='127.0.0.1:3109',KEEL_STATIC_DIR=str(root/'web/dist'),KEEL_FABRIC_PROBES_FILE=str(fixture.configuration),KEEL_MONITOR_ENABLED='true',KEEL_WIRING_ENABLED='true',KEEL_PRECOLLECT_ENABLED='false',KEEL_FABRIC_ISSUE_VERIFY_ENABLED='true',KEEL_TOPOLOGY_FIRMWARE_MOCK_ENABLED='true')
   with (work/'api-build.log').open('w') as log: subprocess.run(['go','build','-o',str(work/'keel-api'),'./cmd/keel-api'],cwd=root,env=ui,stdout=log,stderr=log,check=True)
   api=subprocess.Popen([str(work/'keel-api')],env=ui,stdout=(work/'api.log').open('w'),stderr=subprocess.STDOUT)
   try:
    import urllib.request
    for _ in range(100):
     if api.poll() is not None:raise RuntimeError('Private API exited')
     try:urllib.request.urlopen('http://127.0.0.1:3109/healthz',timeout=1);break
     except Exception:time.sleep(.1)
    credentials=work/'browser-credentials.json';credentials.write_text(json.dumps(dict(metadata,base_url='http://127.0.0.1:3109',password='agent-kernel-test-only-long-password',evidence=str(work))))
    ui['KEEL_AUTH_BROWSER_CREDENTIALS_FILE']=str(credentials)
    with (work/'business-browser.log').open('w') as log:browser=subprocess.run(['node','scripts/check-p2-business-browser.mjs'],cwd=root,env=ui,stdout=log,stderr=log)
    report['checks'].append({'name':'business-browser','returncode':browser.returncode});(work/'report.json').write_text(json.dumps(report,indent=2)+'\n');print('business-browser',browser.returncode,flush=True)
    if browser.returncode: print((work/'business-browser.log').read_text()[-3000:],flush=True)
   finally:
    api.terminate();api.wait(timeout=15);(work/'browser-credentials.json').unlink(missing_ok=True)
  if r.returncode and label=='schema':break
finally:
 fixture.close()
 for name in names:subprocess.run(['docker','exec',container,'dropdb','-U',owner,name],capture_output=True,check=True)
 for private in ['tls.key','probes.json','browser-credentials.json']:(work/private).unlink(missing_ok=True)
print('Complete:',work,flush=True)
raise SystemExit(1 if any(check['returncode'] for check in report['checks']) else 0)
