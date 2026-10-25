#!/usr/bin/env python3
"""One 500,000,000-byte artifact through real private storage and offline bundle.
This is a bounded streaming check, not 10,000-target distribution or hardware proof.
"""
import hashlib
import http.client
import json
import os
from pathlib import Path
import shutil
import subprocess
import time
import urllib.parse
import uuid

os.umask(0o077)
credentials=Path(os.environ['KEEL_AUTH_BROWSER_CREDENTIALS_FILE'])
assert credentials.parent.parent==Path('/tmp') and credentials.parent.name.startswith('keel-media-') and credentials.stat().st_mode&0o077==0
config=json.loads(credentials.read_text());assert config['base_url']=='http://127.0.0.1:3102'
work=Path(os.environ['KEEL_WORKSPACE_EVIDENCE_DIR']);assert work==credentials.parent
size=500_000_000
root=Path.home()/'.local/state/keel/evidence/large-artifacts'/work.name
root.mkdir(parents=True,mode=0o700);root.chmod(0o700)
assert shutil.disk_usage(root).free>size*3+1024**3
assert shutil.disk_usage('/tmp').free>size*2+512*1024**2
available=int(next(line.split()[1] for line in Path('/proc/meminfo').read_text().splitlines() if line.startswith('MemAvailable:')))*1024
assert available>size*2+768*1024**2
origin=config['base_url'];timings={};cookies={};tokens={};generated=[]
def measured(name,operation):
 start=time.monotonic();result=operation();timings[name]=round(time.monotonic()-start,3);return result

def connection():return http.client.HTTPConnection('127.0.0.1',3102,timeout=300)
def call(method,path,body=None,actor='owner',expected=200):
 headers={'Accept':'application/json','Origin':origin,'Idempotency-Key':uuid.uuid4().hex}
 if actor in cookies:headers['Cookie']=cookies[actor]
 if actor in tokens:headers['X-CSRF-Token']=tokens[actor]
 data=None if body is None else json.dumps(body).encode()
 if data is not None:headers['Content-Type']='application/json'
 client=connection()
 try:
  client.request(method,'/api/v1'+path,body=data,headers=headers);response=client.getresponse();raw=response.read()
  cookie=response.getheader('Set-Cookie')
  if cookie:cookies[actor]=cookie.split(';',1)[0]
  assert response.status==expected,f'{method} {path}: HTTP {response.status}'
  return json.loads(raw) if raw else None
 finally:client.close()
def login(actor,username,password):
 tokens[actor]=call('GET','/auth/csrf',actor=actor)['csrf_token']
 tokens[actor]=call('POST','/auth/login',{'method':'local','username':username,'password':password},actor=actor)['csrf_token']
def transfer(method,path,file,actor='owner',range_header=None):
 client=connection();headers={'Origin':origin,'Cookie':cookies[actor],'X-CSRF-Token':tokens[actor]}
 if method=='PUT':headers.update({'Content-Type':'application/octet-stream','Content-Length':str(size)})
 if range_header:headers['Range']=range_header
 try:
  if method=='PUT':
   with file.open('rb') as source:client.request(method,'/api/v1'+path,body=source,headers=headers)
  else:client.request(method,'/api/v1'+path,headers=headers)
  response=client.getresponse()
  assert response.status==(200 if not range_header else 206),f'{method} HTTP {response.status}'
  if method=='PUT':return json.loads(response.read())
  digest=hashlib.sha256();count=0
  with file.open('wb') as output:
   while chunk:=response.read(1024*1024):output.write(chunk);digest.update(chunk);count+=len(chunk)
  return count,digest.hexdigest(),dict(response.getheaders())
 finally:client.close()

try:
 login('owner',config['username'],config['password'])
 suffix=uuid.uuid4().hex[:8];username='large-review-'+suffix;password=uuid.uuid4().hex+'-Review!'
 call('POST','/admin/users',{'username':username,'display_name':'Large artifact reviewer','password':password,'is_admin':True},expected=201)
 login('reviewer',username,password)
 project=call('POST','/projects',{'name':'Large artifact stream','slug':'large-'+suffix},expected=201);base='/projects/'+project['id']+'/media'
 source=root/'source.bin';generated.append(source);digest=hashlib.sha256();chunk=bytes(range(256))*4096
 def generate():
  remaining=size
  with source.open('wb') as output:
   while remaining:
    block=chunk[:min(len(chunk),remaining)];output.write(block);digest.update(block);remaining-=len(block)
 measured('generate_source',generate)
 sha=digest.hexdigest();item=call('POST',base,{'type':'tool','name':'large-stream-'+suffix,'version':'v1','arch':'noarch','sha256':sha,'size':size,'manifest_ref':'test:controlled-streaming-fixture'},expected=201);endpoint=base+'/'+item['id']
 bound=measured('upload_bind',lambda:transfer('PUT',endpoint+'/content',source));assert bound['artifact_stored'] is True and bound['state']=='uploaded'
 for state in ['verified','approved']:measured(state,lambda state=state:call('POST',endpoint+'/transition',{'state':state},actor='reviewer'))
 directory=root/'artifacts';directory.mkdir(mode=0o700);download=directory/(item['id']+'.bin');generated.append(download)
 count,observed,headers=measured('download',lambda:transfer('GET',endpoint+'/content',download));assert count==size and observed==sha
 range_file=root/'range.bin';generated.append(range_file);start=300_000_000;end=start+1024*1024-1
 count,_,range_headers=measured('range_download',lambda:transfer('GET',endpoint+'/content',range_file,range_header=f'bytes={start}-{end}'));assert count==1024*1024
 with source.open('rb') as original:original.seek(start);assert original.read(count)==range_file.read_bytes()
 assert next(v for k,v in range_headers.items() if k.lower()=='content-range')==f'bytes {start}-{end}/{size}'
 manifest=measured('signed_manifest',lambda:call('GET',endpoint+'/manifest'));assert manifest['payload']['root_id']==item['id'] and manifest['payload']['artifacts'][0]['size']==size
 manifest_path=root/'manifest.json';manifest_path.write_text(json.dumps(manifest));bundle=root/'approved-media.tar';generated.append(bundle)
 binary=os.environ['KEEL_ADMIN_BINARY'];public=os.environ['KEEL_MEDIA_MANIFEST_PUBLIC_KEY_FILE']
 def command(operation,args):
  result=subprocess.run([binary,operation,'--public-key-file',public,*args],env={'PATH':os.environ['PATH']},capture_output=True,text=True)
  assert result.returncode==0,f'{operation} failed'
  value=json.loads(result.stdout);assert value['artifact_bytes_verified'] is True;return value
 measured('bundle_create',lambda:command('media-bundle-create',['--manifest-file',str(manifest_path),'--artifact-directory',str(directory),'--output-file',str(bundle)]))
 measured('bundle_verify',lambda:command('media-bundle-verify',['--bundle-file',str(bundle)]))
 api_peak=None
 if os.environ.get('KEEL_ISOLATED_API_PID'):
  status=Path('/proc/'+os.environ['KEEL_ISOLATED_API_PID']+'/status').read_text().splitlines()
  api_peak=int(next(line.split()[1] for line in status if line.startswith('VmHWM:')))
 report={'status':'passed','artifact_bytes':size,'sha256':sha,'archive_bytes':bundle.stat().st_size,'operation_seconds':timings,'api_peak_rss_kib':api_peak,'scope':'one controlled synthetic artifact; real PostgreSQL/MinIO/API and offline CLI','hardware_verified':False,'scale_verified':False,'source_provenance_verified':False}
 (work/'large-artifact-report.json').write_text(json.dumps(report,indent=2)+'\n');(root/'report.json').write_text(json.dumps(report,indent=2)+'\n')
 print(json.dumps(report))
 print('PASS 500,000,000 actual bytes: registered digest -> streaming binding -> independent approval -> full and Range downloads -> signed receipt -> streamed offline archive and verification')
finally:
 for file in generated:file.unlink(missing_ok=True)
