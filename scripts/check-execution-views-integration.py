#!/usr/bin/env python3
"""Real API/worker/PostgreSQL/browser verification of the Flow workspace.
Uses only a labeled disposable PostgreSQL container and API3104, never runtime DB.
"""
import json,os,secrets,socket,subprocess,tempfile,time,urllib.parse,urllib.request,uuid,threading
from pathlib import Path
os.umask(0o077)
root=Path(__file__).resolve().parents[1];os.chdir(root)
probe=socket.socket();probe.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1);probe.bind(('127.0.0.1',3104));probe.close()
record=json.loads(subprocess.check_output(['docker','inspect','keel-auth-test-postgres']))[0]
assert record['Config']['Labels'].get('io.keel.test-environment')=='disposable'
config=dict(value.split('=',1) for value in record['Config']['Env'] if '=' in value)
owner,password=config['POSTGRES_USER'],config['POSTGRES_PASSWORD'];database='keel_test_views_'+uuid.uuid4().hex[:12]
work=Path(tempfile.mkdtemp(prefix='keel-views-'));api=worker=None;worker_start_thread=None;worker_stop=threading.Event()
print('Execution views evidence directory: '+str(work),flush=True)
try:
 subprocess.run(['docker','exec','keel-auth-test-postgres','createdb','-U',owner,database],check=True,capture_output=True)
 env=os.environ.copy();env.update(KEEL_DATABASE_URL='postgresql://'+urllib.parse.quote(owner,safe='')+':'+urllib.parse.quote(password,safe='')+'@127.0.0.1:15433/'+database+'?sslmode=disable',KEEL_ENV='development',KEEL_INSECURE_DEV_COOKIE='true',KEEL_PUBLIC_ORIGINS='http://127.0.0.1:3104',KEEL_LISTEN_ADDR='127.0.0.1:3104',KEEL_STATIC_DIR=str(root/'web/dist'))
 for target in ['api','admin','worker']:subprocess.run(['go','build','-o',str(work/('keel-'+target)),'./cmd/keel-'+target],check=True,capture_output=True)
 subprocess.run([str(work/'keel-admin'),'migrate'],env=env,check=True,capture_output=True)
 pw=secrets.token_urlsafe(28);secret=work/'password';secret.write_text(pw)
 subprocess.run([str(work/'keel-admin'),'init-admin','--username','flowadmin','--display-name','Flow Admin','--password-file',str(secret)],env=env,check=True,capture_output=True)
 project,pkg=uuid.uuid4().hex,uuid.uuid4().hex
 sql=f"INSERT INTO projects(id,name,slug,description,state,created_by,created_at,updated_at) SELECT '{project}','Flow fixture','flow-fixture','','planning',id,now(),now() FROM users WHERE username='flowadmin'; INSERT INTO memberships(id,user_id,project_id,role,created_at) SELECT '{pkg}',id,'{project}','Admin',now() FROM users WHERE username='flowadmin';"
 viewer,member=uuid.uuid4().hex,uuid.uuid4().hex
 sql+=f"INSERT INTO users(id,username,display_name,password_hash,is_admin,enabled,security_version,created_at) SELECT '{viewer}','flowviewer','View Reader',password_hash,false,true,1,now() FROM users WHERE username='flowadmin'; INSERT INTO memberships(id,user_id,project_id,role,created_at) VALUES('{member}','{viewer}','{project}','Viewer',now());"
 result=subprocess.run(['docker','exec','-i','keel-auth-test-postgres','psql','-v','ON_ERROR_STOP=1','-U',owner,'-d',database],input=sql,text=True,capture_output=True)
 if result.returncode:raise RuntimeError(result.stderr.replace(password,'[redacted]'))
 api=subprocess.Popen([str(work/'keel-api')],env=env,stdout=(work/'api.log').open('w'),stderr=subprocess.STDOUT)
 for _ in range(100):
  if api.poll() is not None:raise RuntimeError('Isolated API exited; inspect protected process log')
  try:urllib.request.urlopen('http://127.0.0.1:3104/healthz',timeout=1);break
  except Exception:time.sleep(.1)
 else:raise RuntimeError('Isolated API did not become ready')
 env.update(KEEL_WORKER_ID='flow-contract-worker',KEEL_WORKER_CONCURRENCY='4',KEEL_WORKER_LEASE='2s')
 def start_worker():
  global worker
  while not worker_stop.is_set():
   if (work/'start-worker').exists():
    worker=subprocess.Popen([str(work/'keel-worker')],env=env,stdout=(work/'worker.log').open('w'),stderr=subprocess.STDOUT)
    return
   worker_stop.wait(.05)
 worker_start_thread=threading.Thread(target=start_worker,daemon=True);worker_start_thread.start()

 credentials=work/'credentials.json';credentials.write_text(json.dumps(dict(base_url='http://127.0.0.1:3104',username='flowadmin',password=pw,project_id=project,worker_start_file=str(work/'start-worker'))))
 env.update(KEEL_AUTH_BROWSER_CREDENTIALS_FILE=str(credentials),KEEL_WORKSPACE_EVIDENCE_DIR=str(work))
 result=subprocess.run(['node','scripts/check-execution-views-browser.mjs'],env=env,capture_output=True,text=True)
 (work/'browser.log').write_text(result.stdout+result.stderr);print(result.stdout,flush=True)
 if result.returncode:print(result.stderr.replace(pw,'[redacted]').replace(password,'[redacted]'),flush=True);raise RuntimeError('Execution views integration failed')
finally:
 worker_stop.set()
 if worker_start_thread is not None:worker_start_thread.join(timeout=2)
 for process in [worker,api]:
  if process is not None:
   process.terminate()
   try:process.wait(timeout=15)
   except subprocess.TimeoutExpired:process.kill();process.wait()
 subprocess.run(['docker','exec','keel-auth-test-postgres','dropdb','--if-exists','-U',owner,database],capture_output=True)
 for name in ['password','credentials.json']:(work/name).unlink(missing_ok=True)
 print('Isolated API/database and temporary credentials removed; evidence retained.',flush=True)
