#!/usr/bin/env python3
"""Isolated real API/worker/PostgreSQL + TLS protocol fixture. Requires the
labeled disposable test PostgreSQL and a built web/dist; never uses runtime DB.
Use --sim-regression for the existing execution boundary regression at 24 targets.
"""
import os,json,secrets,subprocess,tempfile,time,urllib.parse,urllib.request,socket
from pathlib import Path
os.umask(0o077)
root=Path(__file__).resolve().parents[1];os.chdir(root)
probe=socket.socket();probe.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1);probe.bind(('127.0.0.1',3102));probe.close()
record=json.loads(subprocess.check_output(['docker','inspect','keel-auth-test-postgres']))[0]
assert record['Config']['Labels'].get('io.keel.test-environment')=='disposable'
config=dict(value.split('=',1) for value in record['Config']['Env'] if '=' in value)
owner=config['POSTGRES_USER'];password=config['POSTGRES_PASSWORD']
database='keel_test_kea_'+str(int(time.time()))
work=Path(tempfile.mkdtemp(prefix='keel-shell-'));api=None;worker=None;fixture=None
print('Kea integration evidence directory: '+str(work),flush=True)
try:
 subprocess.run(['docker','exec','keel-auth-test-postgres','createdb','-U',owner,database],check=True,capture_output=True)
 env=os.environ.copy();env.update(KEEL_DATABASE_URL='postgresql://'+urllib.parse.quote(owner,safe='')+':'+urllib.parse.quote(password,safe='')+'@127.0.0.1:15433/'+database+'?sslmode=disable',KEEL_ENV='development',KEEL_INSECURE_DEV_COOKIE='true',KEEL_PUBLIC_ORIGINS='http://127.0.0.1:3102,http://localhost:3102',KEEL_LISTEN_ADDR='127.0.0.1:3102',KEEL_STATIC_DIR=str(root/'web/dist'))
 import importlib.util
 spec=importlib.util.spec_from_file_location('kea_fixture',root/'scripts/kea-contract-fixture.py')
 module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
 fixture=module.KeaFixture(work)
 env['KEEL_FABRIC_PROBES_FILE']=str(fixture.configuration)
 for target in ['api','admin','worker']:
  subprocess.run(['go','build','-o',str(work/('keel-'+target)),'./cmd/keel-'+target],check=True,capture_output=True)
 subprocess.run([str(work/'keel-admin'),'migrate'],env=env,check=True,capture_output=True)
 pw=secrets.token_urlsafe(28);secret=work/'password';secret.write_text(pw);secret.chmod(0o600)
 subprocess.run([str(work/'keel-admin'),'init-admin','--username','shelladmin','--display-name','Shell Admin','--password-file',str(secret)],env=env,check=True,capture_output=True)

 import uuid
 ids=[uuid.uuid4().hex for _ in range(6)]
 project,profile,pkg,switch1,switch2,member=ids
 sql=f"""
 INSERT INTO projects(id,name,slug,description,state,created_by,created_at,updated_at) SELECT '{project}','Planning fixture','planning-fixture','','planning',id,now(),now() FROM users WHERE username='shelladmin';
 INSERT INTO memberships(id,user_id,project_id,role,created_at) SELECT '{member}',id,'{project}','Admin',now() FROM users WHERE username='shelladmin';
 INSERT INTO bom_mapping_profiles(id,project_id,version_ref,state,created_by,created_at) SELECT '{profile}','{project}','fixture-mapping','approved',id,now() FROM users WHERE username='shelladmin';
 INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at) SELECT '{pkg}','{project}','fixture-bom','bom-import/v1','fixture-mapping','sha256:{'a'*64}','design_institute','{{}}',2,'committed',id,now() FROM users WHERE username='shelladmin';
 """
 for index,item in enumerate([switch1,switch2],2):
  sql+=f"""
 INSERT INTO bom_items(item_uid,category,serial,position,pod,role,source_of_record,first_project_id,created_at,updated_at) VALUES('{item}','switch','FIXTURE-SW-{index}','{{"dc":"DC1","rack":"R1","idx":"{index}"}}','P1','leaf','design_bom','{project}',now(),now());
 INSERT INTO switches(id,serial_number,hostname,created_at,updated_at) VALUES('{item}','FIXTURE-SW-{index}','fixture-{index}',now(),now());
 INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at) VALUES('{pkg}','{item}',{index},'{project}','{{}}',now());
 INSERT INTO switch_mac_bindings(switch_id,kind,mac,package_id,updated_at) VALUES('{item}','mgmt','02:00:00:00:00:{index:02x}','{pkg}',now());
 INSERT INTO switch_mac_bindings(switch_id,kind,mac,package_id,updated_at) VALUES('{item}','base','02:00:00:00:01:{index:02x}','{pkg}',now());
 """
 # Versioned BOM input fixture for switch interconnect and uninstalled server.
 port_a,port_b,port_c,server_id,host_port,cable_a,cable_b=[uuid.uuid4().hex for _ in range(7)]
 sql+=f"INSERT INTO bom_items(item_uid,category,serial,source_of_record,first_project_id,created_at,updated_at) VALUES('{server_id}','server','FIXTURE-SERVER','design_bom','{project}',now(),now());"
 for uid,parent,index,name in [(port_a,switch1,'1','swp1'),(port_b,switch2,'1','swp1'),(port_c,switch1,'2','swp2'),(host_port,server_id,'1','eth1')]:
  sql+=f"INSERT INTO bom_items(item_uid,category,parent_uid,port_index,port_name,source_of_record,first_project_id,created_at,updated_at) VALUES('{uid}','port','{parent}','{index}','{name}','design_bom','{project}',now(),now());"
 for uid,left,right in [(cable_a,port_a,port_b),(cable_b,port_c,host_port)]:
  left,right=sorted([left,right]);sql+=f"INSERT INTO bom_items(item_uid,category,endpoint_a_uid,endpoint_b_uid,source_of_record,first_project_id,created_at,updated_at) VALUES('{uid}','cable','{left}','{right}','design_bom','{project}',now(),now());"
 for row,uid in enumerate([server_id,port_a,port_b,port_c,host_port,cable_a,cable_b],4):
  sql+=f"INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at) VALUES('{pkg}','{uid}',{row},'{project}','{{}}',now());"
 subprocess.run(['docker','exec','-i','keel-auth-test-postgres','psql','-v','ON_ERROR_STOP=1','-U',owner,'-d',database],input=sql,text=True,check=True,capture_output=True)
 api=subprocess.Popen([str(work/'keel-api')],env=env,stdout=(work/'api.log').open('w'),stderr=subprocess.STDOUT)
 for _ in range(50):
  if api.poll() is not None:raise RuntimeError('Isolated API exited; inspect protected process log')
  try:
   urllib.request.urlopen('http://127.0.0.1:3102/healthz',timeout=1);break
  except Exception:time.sleep(.2)
 else:raise RuntimeError('Isolated API did not become ready')
 env.update(KEEL_WORKER_ID='kea-contract-worker',KEEL_WORKER_CONCURRENCY='4',KEEL_WORKER_LEASE='2s')
 worker=subprocess.Popen([str(work/'keel-worker')],env=env,stdout=(work/'worker.log').open('w'),stderr=subprocess.STDOUT)
 credentials=work/'credentials.json';credentials.write_text(json.dumps(dict(base_url='http://127.0.0.1:3102',username='shelladmin',password=pw,management_project_id=project,fault_file=str(fixture.fault),command_log=str(fixture.log))));credentials.chmod(0o600)
 env.update(KEEL_AUTH_BROWSER_CREDENTIALS_FILE=str(credentials),KEEL_WORKSPACE_EVIDENCE_DIR=str(work))
 import sys
 command=['node','scripts/check-kea-browser.mjs']
 if '--fabric' in sys.argv:command=['node','scripts/check-fabric-browser.mjs']
 if '--sim-regression' in sys.argv:
  command=['python3','scripts/check-execution-integration.py'];env['KEEL_SIM_TEST_TARGETS']='24'
 result=subprocess.run(command,env=env,capture_output=True,text=True)
 (work/'browser.log').write_text(result.stdout+result.stderr)
 print(result.stdout,flush=True)
 if result.returncode:
  print(result.stderr.replace(pw,'[redacted]').replace(password,'[redacted]'),flush=True)
  raise RuntimeError('Isolated Kea/Execution integration failed')
finally:
 if worker is not None:
  worker.terminate()
  try:worker.wait(timeout=15)
  except subprocess.TimeoutExpired:worker.kill();worker.wait()
 if fixture is not None:fixture.close()
 if api is not None:
  api.terminate()
  try:api.wait(timeout=10)
  except subprocess.TimeoutExpired:api.kill();api.wait()
 subprocess.run(['docker','exec','keel-auth-test-postgres','dropdb','--if-exists','-U',owner,database],capture_output=True)
 for name in ['password','credentials.json','tls.key','probes.json']:(work/name).unlink(missing_ok=True)
 print('Isolated API/database and temporary credentials removed; evidence retained.',flush=True)
