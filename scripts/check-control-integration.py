#!/usr/bin/env python3
"""Black-box control management checks, only against the disposable HTTP API."""
import http.client
import http.cookies
import json
import os
from pathlib import Path
import secrets
import urllib.parse

credentials_file=Path(os.environ.get('KEEL_AUTH_BROWSER_CREDENTIALS_FILE','/tmp/keel-auth-browser-credentials.json'))
if credentials_file.stat().st_mode & 0o077: raise SystemExit('Test credentials must be private')
credentials=json.loads(credentials_file.read_text())
base=urllib.parse.urlsplit(credentials['base_url'])
if base.scheme!='http' or base.hostname not in ('127.0.0.1','localhost') or base.port!=3102:
    raise SystemExit('Refusing to test a non-isolated HTTP endpoint')
prefix='control-'+secrets.token_hex(5)
secrets_seen={credentials['password']}
checks=[]
def expect(ok,label):
    if not ok: raise AssertionError(label)
def passed(label):
    checks.append(label); print('PASS '+label,flush=True)

class Client:
    def __init__(self,ip='127.0.0.40'):
        self.cookies={};self.csrf=None;self.ip=ip
    def call(self,method,path,body=None,headers=None):
        h={'Origin':credentials['base_url']}
        if self.cookies:h['Cookie']='; '.join(k+'='+v for k,v in self.cookies.items())
        if self.csrf:h['X-CSRF-Token']=self.csrf
        if body is not None:h['Content-Type']='application/json'
        h.update(headers or {})
        conn=http.client.HTTPConnection(base.hostname,base.port,timeout=30,source_address=(self.ip,0))
        try:
            conn.request(method,'/api/v1'+path,body=json.dumps(body).encode() if body is not None else None,headers=h)
            res=conn.getresponse();status=res.status;data=res.read();rh=res.getheaders()
        finally:conn.close()
        for name,value in rh:
            if name.lower()!='set-cookie':continue
            for k,m in http.cookies.SimpleCookie(value).items():
                if not m.value or m['max-age']=='-1':self.cookies.pop(k,None)
                else:self.cookies[k]=m.value;secrets_seen.add(m.value)
        value=json.loads(data) if data else None
        if isinstance(value,dict) and 'csrf_token' in value:self.csrf=value['csrf_token'];secrets_seen.add(self.csrf)
        expect('no-store' in dict((k.lower(),v) for k,v in rh).get('cache-control',''),'API no-store')
        return status,value
    def login(self,username,password):
        secrets_seen.add(password)
        expect(self.call('GET','/auth/csrf')[0]==200,'pre-login CSRF')
        status,value=self.call('POST','/auth/login',{'method':'local','username':username,'password':password})
        expect(status==200,'valid account login')
        return value

admin=Client()
admin.login(credentials['username'],credentials['password'])
def admin_call(method,path,body=None,code=200):
    status,value=admin.call(method,path,body)
    expect(status==code,method+' '+path+' status '+str(status)+' expected '+str(code))
    return value
def account(user_id):
    result=admin_call('GET','/admin/users?q='+urllib.parse.quote(prefix))
    return next(u for u in result['items'] if u['id']==user_id)

users=admin_call('GET','/admin/users?page=1&page_size=1')
expect(len(users['items'])==1 and users['total']>=1 and users['page']==1,'account pagination')
expect(admin.call('GET','/admin/users?page=0')[0]==400,'invalid page rejected')
expect(admin.call('GET','/admin/users?page_size=101')[0]==400,'page limit enforced')
expect(admin.call('POST','/admin/users',{'username':prefix+'-bad','display_name':'bad','password':secrets.token_urlsafe(18)},{'X-CSRF-Token':'invalid'})[0]==403,'management requires CSRF')
passed('Account pagination, validation and CSRF')

password=secrets.token_urlsafe(20);secrets_seen.add(password)
user=admin_call('POST','/admin/users',{'username':prefix,'display_name':'管理验收账号','password':password},201)
expect(user['enabled'] and not user['is_admin'] and user['version']>=1,'explicit local account')
admin_call('POST','/admin/users',{'username':prefix,'display_name':'duplicate','password':password},409)
observer=Client('127.0.0.41');observer.login(prefix,password)
expect(observer.call('GET','/admin/users')[0]==403,'ordinary user cannot read global accounts')
expect(observer.call('GET','/admin/audits')[0]==403,'ordinary user cannot read global audits')
passed('Local account creation, uniqueness and global authorization')

status,project=admin.call('POST','/projects',{'name':'控制面验收','slug':prefix},{'Idempotency-Key':secrets.token_hex(16)})
expect(status==201,'test project created')
status,other=admin.call('POST','/projects',{'name':'隔离项目','slug':prefix+'-other'},{'Idempotency-Key':secrets.token_hex(16)})
expect(status==201,'second project created')
path='/projects/'+project['id']
member=admin_call('POST',path+'/members',{'username':prefix,'role':'Admin'},201)
expect(observer.call('GET','/auth/session')[0]==401,'member grant revokes prior session')
observer.login(prefix,password)
expect(observer.call('GET',path+'/members')[0]==200,'project Admin reads members')
expect(observer.call('GET','/projects/'+other['id']+'/members')[0]==404,'cross-project membership hidden')
expect(observer.call('GET','/admin/users')[0]==403,'project Admin is not platform Admin')
admin_call('POST',path+'/members',{'username':prefix,'role':'Viewer'},409)
passed('Project membership grant, session revocation and scope')

members=admin_call('GET',path+'/members')['items']
initial_admin=next(m for m in members if m['user']['username']==credentials['username'])
admin_call('DELETE',path+'/members/'+initial_admin['user']['id'],{'version':initial_admin['version']},204)
admin.login(credentials['username'],credentials['password'])
status,value=observer.call('DELETE',path+'/members/'+user['id'],{'version':member['version']})
expect(status==409 and value['code']=='LAST_PROJECT_ADMIN','last project Admin cannot remove self')
current=account(user['id'])
value=admin_call('PATCH','/admin/users/'+user['id'],{'enabled':False,'version':current['version']},409)
expect(value['code']=='LAST_PROJECT_ADMIN','indirect disable cannot remove last project Admin')
admin_call('POST',path+'/members',{'username':credentials['username'],'role':'Admin'},201)
admin.login(credentials['username'],credentials['password'])
passed('Last project administrator protected directly and indirectly')

admin_call('PATCH',path+'/members/'+user['id'],{'role':'Viewer','version':member['version']+999},409)
member=admin_call('PATCH',path+'/members/'+user['id'],{'role':'Viewer','version':member['version']})
expect(observer.call('GET','/auth/session')[0]==401,'role change revokes current session')
observer.login(prefix,password)
expect(observer.call('GET',path+'/members')[0]==403,'Viewer cannot read management members')
expect(observer.call('GET',path)[0]==200,'Viewer still reads authorized project')
admin_call('DELETE',path+'/members/'+user['id'],{'version':member['version']},204)
expect(observer.call('GET','/auth/session')[0]==401,'membership removal revokes current session')
observer.login(prefix,password)
expect(observer.call('GET',path)[0]==404,'removed member cannot read project')
rebuilt=admin_call('POST',path+'/members',{'username':prefix,'role':'Viewer'},201)
expect(rebuilt['version']>member['version'],'recreated membership advances monotonic version')
admin_call('PATCH',path+'/members/'+user['id'],{'role':'Admin','version':member['version']},409)
admin_call('DELETE',path+'/members/'+user['id'],{'version':member['version']},409)
admin_call('DELETE',path+'/members/'+user['id'],{'version':rebuilt['version']},204)
observer.login(prefix,password)
passed('Optimistic membership versions, delete/recreate conflicts and immediate privilege revocation')

current=account(user['id']);replacement=secrets.token_urlsafe(20);secrets_seen.add(replacement)
admin_call('POST','/admin/users/'+user['id']+'/password',{'password':replacement,'version':current['version']+1},409)
current=admin_call('POST','/admin/users/'+user['id']+'/password',{'password':replacement,'version':current['version']})
expect(observer.call('GET','/auth/session')[0]==401,'admin password reset revokes all sessions')
observer.login(prefix,replacement)
current=admin_call('PATCH','/admin/users/'+user['id'],{'enabled':False,'version':current['version']})
expect(observer.call('GET','/auth/session')[0]==401,'disabled account loses session')
current=admin_call('PATCH','/admin/users/'+user['id'],{'enabled':True,'version':current['version']})
observer.login(prefix,replacement)
passed('Account version conflicts, password reset and disable/enable')

status,value=observer.call('POST','/auth/password',{'current_password':'incorrect-current-password','new_password':password})
expect(status==403 and value['code']=='CURRENT_PASSWORD_INVALID','wrong current password denied')
changed=secrets.token_urlsafe(20);secrets_seen.add(changed)
expect(observer.call('POST','/auth/password',{'current_password':replacement,'new_password':changed})[0]==204,'self password change')
expect(observer.call('GET','/auth/session')[0]==401,'self change ends session')
observer.login(prefix,changed)
passed('Self-service password verification and session invalidation')

audits=admin_call('GET','/admin/audits?page_size=100')
filtered=admin_call('GET',path+'/audits?action=membership.grant')
expect(filtered['total']>=1 and all(a['project_id']==project['id'] and a['action']=='membership.grant' for a in filtered['items']),'audit filters honor project')
serialized=json.dumps([audits,filtered],ensure_ascii=False)
expect(not any(v and v in serialized for v in secrets_seen),'audit responses contain no credentials or session tokens')
expect(not any(k in serialized.lower() for k in ('password_hash','csrf_hash','session_token')),'audit metadata projection')
expect(observer.call('GET',path+'/audits')[0]==404,'unauthorized audit project hidden')
passed('Scoped audit filters and secret-free metadata')

current=account(user['id'])
unlocked=admin_call('POST','/admin/users/'+user['id']+'/unlock',{'version':current['version'],'ip':'127.0.0.41'})
expect(unlocked['version']>current['version'],'unlock version increases')
passed('Explicit audited account and IP unlock')
print('Control HTTP integration passed: '+str(len(checks))+' groups')
