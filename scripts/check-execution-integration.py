#!/usr/bin/env python3
"""Real API/worker/PostgreSQL simulation checks; never contacts delivery hardware."""
import copy
import http.client
import http.cookies
import json
import os
from pathlib import Path
import platform
import secrets
import time
import urllib.parse

file=Path(os.environ.get('KEEL_AUTH_BROWSER_CREDENTIALS_FILE','/tmp/keel-auth-browser-credentials.json'))
assert file.stat().st_mode & 0o077 == 0, 'Private isolated credentials required'
credentials=json.loads(file.read_text())
base=urllib.parse.urlsplit(credentials['base_url'])
assert base.scheme=='http' and base.hostname in ('127.0.0.1','localhost') and base.port==3102, 'Isolated API only'
prefix='exec-'+secrets.token_hex(5)
checks=[]
class Client:
    def __init__(self,ip='127.0.0.50'):
        self.cookies={};self.csrf=None;self.ip=ip
    def call(self,method,path,body=None,key=None,code=200):
        headers={'Origin':credentials['base_url']}
        if self.cookies:headers['Cookie']='; '.join(k+'='+v for k,v in self.cookies.items())
        if self.csrf:headers['X-CSRF-Token']=self.csrf
        if method!='GET':headers['Idempotency-Key']=key or secrets.token_hex(16)
        if body is not None:headers['Content-Type']='application/json'
        conn=http.client.HTTPConnection(base.hostname,base.port,timeout=60,source_address=(self.ip,0))
        try:
            conn.request(method,'/api/v1'+path,body=None if body is None else json.dumps(body),headers=headers)
            response=conn.getresponse();status=response.status;raw=response.read()
            for name,value in response.getheaders():
                if name.lower()=='set-cookie':
                    for name,m in http.cookies.SimpleCookie(value).items():
                        if not m.value:self.cookies.pop(name,None)
                        else:self.cookies[name]=m.value
        finally:conn.close()
        value=json.loads(raw) if raw else None
        if isinstance(value,dict) and 'csrf_token' in value:self.csrf=value['csrf_token']
        assert status==code, f'{method} {path}: {status}, expected {code}; code={value.get("code") if isinstance(value,dict) else None}'
        return value
    def login(self,username,password):
        self.call('GET','/auth/csrf')
        self.call('POST','/auth/login',{'method':'local','username':username,'password':password})

def passed(label):checks.append(label);print('PASS '+label,flush=True)
admin=Client();admin.login(credentials['username'],credentials['password'])
p=admin.call('POST','/projects',{'name':'执行引擎隔离模拟验收','slug':prefix},code=201)
path='/projects/'+p['id']
count=int(os.environ.get('KEEL_SIM_TEST_TARGETS','200'))
assert 1<=count<=2000
registration={'prefix':prefix,'count':count,'seed':42};key=secrets.token_hex(16)
targets=admin.call('POST',path+'/sim-targets',registration,key=key,code=201)['items']
again=admin.call('POST',path+'/sim-targets',registration,key=key,code=201)['items']
assert targets==again and len(targets)==count
page=admin.call('GET',path+'/sim-targets?page=1&page_size=1')
assert page['total']==count and len(page['items'])==1
admin.call('GET',path+'/sim-targets?page_size=101',code=400)
changed={**registration,'seed':43};admin.call('POST',path+'/sim-targets',changed,key=key,code=409)
passed('Bounded target pagination and durable registration idempotency')

def definition(outcome='success',duration=10,retry=0,fail_first=0,kind='sim.read'):
    return {'api_version':'keel.sim/v1','inputs':{'profile':{'type':'enum','required':True,'enum':['smoke','capacity']}},'tasks':[{'id':'inspect','type':kind,'retry':retry,'idempotent':True,'timeout_ms':5000,'resources':['machine'],'sim':{'outcome':outcome,'duration_ms':duration,'seed':42,'fail_first':fail_first}}]}
def publish(name,d):return admin.call('POST',path+'/flows',{'name':prefix+' '+name,'definition':d},code=201)
def execute(flow,selected=None,**extra):
    body={'mode':'sim','flow_version_id':flow['id'],'target_ids':[t['id'] for t in (selected or targets[:1])],'parameters':{'profile':'capacity' if count>=200 else 'smoke'},'concurrency':64,**extra}
    return admin.call('POST',path+'/executions',body,code=201)
def detail(e):return admin.call('GET',path+'/executions/'+e['id'])
def wait(e,states,timeout=60):
    deadline=time.monotonic()+timeout;last=time.monotonic()
    while time.monotonic()<deadline:
        d=detail(e)
        if d['execution']['state'] in states:return d
        if time.monotonic()-last>25:
            print('WAIT simulation '+str(d['counts']),flush=True);last=time.monotonic()
        time.sleep(.25 if count<100 else 1)
    raise AssertionError('Simulation did not reach '+str(states)+'; last state '+d['execution']['state'])
flow=publish('capacity',definition())
cycle=definition();cycle['tasks'][0]['depends_on']=['inspect']
admin.call('POST',path+'/flows',{'name':prefix+' invalid DAG','definition':cycle},code=400)
admin.call('POST',path+'/flows',{'name':prefix+' alias','yaml_source':'a: &a [*a]'},code=400)
admin.call('POST',path+'/executions',{'mode':'hardware','flow_version_id':flow['id'],'target_ids':[targets[0]['id']]},code=400)
admin.call('POST',path+'/executions',{'mode':'sim','flow_version_id':flow['id'],'target_ids':[targets[0]['id']],'parameters':{'profile':'invalid'}},code=400)
new=admin.call('POST',path+'/flows/'+flow['flow_id']+'/versions',{'name':flow['name'],'definition':definition(duration=11),'expected_version':flow['version']},code=201)
admin.call('POST',path+'/flows/'+flow['flow_id']+'/versions',{'name':flow['name'],'definition':definition(),'expected_version':flow['version']},code=409)
assert new['version']==2
passed('Immutable Flow versions, typed parameters, DAG/YAML validation and simulation admission')

start=time.monotonic();run=execute(flow,targets);done=wait(run,{'success','failed','paused','reconciling'},timeout=900)
if done['execution']['state']!='success' or done['counts'].get('success')!=count:
    failure_file=Path(os.environ.get('KEEL_SIM_REPORT_FILE','/tmp/keel-simulation-report.json')).with_name('simulation-failure.json')
    failure_file.write_text(json.dumps(done,ensure_ascii=False,indent=2)+'\n');failure_file.chmod(0o600)
    raise AssertionError(f"Simulation did not pass: state={done['execution']['state']}, counts={done['counts']}; private evidence {failure_file}")
assert done['execution']['snapshot']['flow']['id']==flow['id'] and done['execution']['snapshot']['flow']['version']==1
assert len(done['execution']['snapshot']['targets'])==count
attempt_page=admin.call('GET',path+'/executions/'+run['id']+'/attempts?page=2&page_size=1')
assert attempt_page['total']==count and len(attempt_page['items'])==(1 if count>1 else 0)
elapsed=round(time.monotonic()-start,3)
report={'mode':'sim','hardware_evidence':False,'logical_targets':count,'real_device_connections':0,'real_hardware_transport_bytes':0,'seed':42,'simulation_task_ms':10,'tasks_per_target':1,'worker_concurrency':os.environ.get('KEEL_WORKER_CONCURRENCY','8'),'execution_concurrency':64,'success':count,'success_percent':100,'elapsed_seconds':elapsed,'architecture':platform.machine(),'logical_cpus':os.cpu_count(),'memory_total_kib':int(next(line.split()[1] for line in Path('/proc/meminfo').read_text().splitlines() if line.startswith('MemTotal:'))),'execution_id':run['id']}
report_path=Path(os.environ.get('KEEL_SIM_REPORT_FILE','/tmp/keel-simulation-report.json'))
report_path.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n');report_path.chmod(0o600)
passed(f'{count} persisted logical targets completed at 100%; frozen version and paginated attempts ({elapsed}s, simulation only)')

retry=execute(publish('retry',definition(retry=1,fail_first=1)),circuit_breaker_percent=100)
d=wait(retry,{'success','failed','reconciling'})
assert d['execution']['state']=='success' and d['attempts_total']==2
assert sorted(a['attempt_no'] for a in d['attempts'])==[1,2]
passed('Known failed idempotent simulation retries once without duplicating successful attempts')

# Settlement must use the latest attempt per task and the submitted frozen DAG.
# An earlier failed attempt with an admitted retry cannot skip its dependant.
dag=definition(retry=1,fail_first=1)
dag['tasks'].append({'id':'verify','type':'sim.read','depends_on':['inspect'],'retry':0,'idempotent':True,'timeout_ms':5000,'resources':['machine'],'sim':{'outcome':'success','duration_ms':10,'seed':42}})
dag_flow=publish('frozen retry DAG',dag)
dag_run=execute(dag_flow,circuit_breaker_percent=100)
changed_dag=copy.deepcopy(dag);changed_dag['tasks'][1]['sim']['outcome']='failed'
admin.call('POST',path+'/flows/'+dag_flow['flow_id']+'/versions',{'name':dag_flow['name'],'definition':changed_dag,'expected_version':1},code=201)
d=wait(dag_run,{'success','failed','paused','reconciling'})
assert d['execution']['state']=='success' and d['attempts_total']==3
assert d['execution']['snapshot']['flow']['id']==dag_flow['id']
assert sorted((a['task_id'],a['attempt_no'],a['state']) for a in d['attempts'])==[('inspect',1,'failed'),('inspect',2,'success'),('verify',1,'success')]
passed('Settlement keeps retry dependants pending and uses the original frozen DAG after a newer Flow publication')

blocked=copy.deepcopy(dag);blocked['tasks'][0]['retry']=0;blocked['tasks'][0]['sim']['outcome']='failed'
blocked_run=execute(publish('dependency failure',blocked),circuit_breaker_percent=100)
d=wait(blocked_run,{'failed','success','paused','reconciling'})
assert d['execution']['state']=='failed' and d['counts'].get('failed')==1 and d['counts'].get('skipped')==1
assert d['attempts_total']==2
passed('Failed dependency skips the pending child without dispatch or a fabricated receipt')

breaker_run=execute(publish('frozen target circuit breaker',definition('failed')),targets[:2],concurrency=1,circuit_breaker_percent=1)
d=wait(breaker_run,{'paused','failed','success','reconciling'})
assert d['execution']['state']=='paused' and d['counts'].get('failed')==1 and d['counts'].get('pending')==1
assert len(d['execution']['snapshot']['targets'])==2
passed('Circuit breaker uses the frozen target denominator and stops pending work after a failure')

slow=publish('control',definition(duration=3000))
r=execute(slow,targets[:min(3,count)],concurrency=1)
d=wait(r,{'running'})
admin.call('POST',path+'/executions/'+r['id']+'/control',{'action':'pause','expected_version':d['execution']['state_version']+999},code=409)
a=admin.call('POST',path+'/executions/'+r['id']+'/control',{'action':'pause','expected_version':d['execution']['state_version']})
d=wait(r,{'paused','success'})
if d['execution']['state']=='paused':
    assert d['counts'].get('running',0)==0
    a=admin.call('POST',path+'/executions/'+r['id']+'/control',{'action':'resume','expected_version':d['execution']['state_version']})
    # Control uses state CAS; retry only refreshing an explicit newer state, not replaying stale payload.
    for _ in range(10):
        d=detail(r)
        if d['execution']['state']=='success':break
        try:
            a=admin.call('POST',path+'/executions/'+r['id']+'/control',{'action':'cancel','expected_version':d['execution']['state_version']});break
        except AssertionError as e:
            if '409' not in str(e):raise
    d=wait(r,{'killed','success'})
    assert d['counts'].get('running',0)==0 and d['counts'].get('unknown',0)==0
passed('Pause drains in-flight work; resume/cancel require current state versions and safe completion')

unknown=execute(publish('unknown',definition('drop_receipt',kind='sim.write')))
d=wait(unknown,{'reconciling'},timeout=30)
attempt=next(a for a in d['attempts'] if a['state']=='unknown')
reconcile=path+'/executions/'+unknown['id']+'/attempts/'+attempt['id']+'/reconcile'
admin.call('POST',reconcile,{'result':'failed','evidence':'Conflicts with the durable successful simulation receipt','expected_version':attempt['state_version']},code=409)
assert detail(unknown)['execution']['state']=='reconciling'
admin.call('POST',reconcile,{'result':'success','evidence':'Verified persisted simulator command receipt and fencing token in isolated test','expected_version':attempt['state_version']})
d=wait(unknown,{'success'});assert d['attempts_total']==1
passed('Lost acknowledgement quarantines resources; contradictory reconciliation denied; matching durable receipt resolves without rerun')

pwd=secrets.token_urlsafe(24)
user=admin.call('POST','/admin/users',{'username':prefix,'display_name':'Execution scope check','password':pwd},code=201)
admin.call('POST',path+'/members',{'username':prefix,'role':'Viewer'},code=201)
viewer=Client('127.0.0.51');viewer.login(prefix,pwd)
viewer.call('GET',path+'/executions')
viewer.call('POST',path+'/sim-targets',{'prefix':'forbidden','count':1,'seed':1},code=403)
viewer.call('POST',path+'/flows',{'name':'forbidden','definition':definition()},code=403)
viewer.call('POST',path+'/executions',{'mode':'sim','flow_version_id':flow['id'],'target_ids':[targets[0]['id']],'parameters':{'profile':'smoke'}},code=403)
other=admin.call('POST','/projects',{'name':'Other execution scope','slug':prefix+'-other'},code=201)
viewer.call('GET','/projects/'+other['id']+'/executions',code=404)
admin.call('GET','/projects/'+other['id']+'/executions/'+run['id'],code=404)
passed('Viewer execution actions denied and foreign-project executions hidden')
admin.call('PUT',path+'/params',{'key':'sim_driver','value_type':'version','source_media_type':'driver'},code=201)
gated_def=definition()
gated_def['inputs']['sim_driver']={'type':'string','required':True}
gated_def['inputs']['cpu.arch']={'type':'enum','required':True,'enum':['amd64','arm64','noarch']}
gated=publish('gated',gated_def)
rej=admin.call('POST',path+'/executions',{'mode':'sim','flow_version_id':gated['id'],'target_ids':[targets[0]['id']],'parameters':{'profile':'smoke','sim_driver':'9.9.9-notapproved','cpu.arch':'amd64'}},code=422)
assert rej['code']=='PARAM_VALUE_REJECTED',rej
passed('Catalog-gated parameters reject unapproved media versions at execution admission')
events=admin.call('GET',path+'/executions/'+run['id']+'/events?after=0')
assert events['items'] and events['next_cursor']>=1
next_events=admin.call('GET',path+'/executions/'+run['id']+'/events?after='+str(events['next_cursor']))
assert all(e['sequence']>events['next_cursor'] for e in next_events['items'])
assert all(a['project_id']==p['id'] for a in admin.call('GET',path+'/audits?action=execution.create')['items'])
passed('Durable event cursor replay and project-scoped execution audits')
print(f'Execution HTTP integration passed: {len(checks)} groups; report {report_path}',flush=True)
