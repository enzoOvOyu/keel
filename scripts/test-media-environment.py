#!/usr/bin/env python3
"""Private disposable MinIO for media integration; never uses runtime volumes."""
import argparse,json,os,secrets,subprocess,time,urllib.request,uuid
from pathlib import Path
os.umask(0o077)
p=argparse.ArgumentParser();p.add_argument('action',choices=['start','stop']);p.add_argument('--directory',required=True);args=p.parse_args()
work=Path(args.directory).resolve();assert work.parent==Path('/tmp') and work.name.startswith('keel-') and work.is_dir()
state=work/'media-container';config=work/'media.env';label='io.keel.test-environment'
def run(cmd):return subprocess.check_output(cmd,stderr=subprocess.DEVNULL,text=True).strip()
if args.action=='stop':
 if state.exists():
  name=state.read_text().strip();record=json.loads(run(['docker','inspect',name]))[0]
  assert record['Config']['Labels'].get(label)=='disposable' and name.startswith('keel-media-test-')
  run(['docker','rm','-f',name]);state.unlink()
 config.unlink(missing_ok=True);(work/'minio.env').unlink(missing_ok=True)
 print('Disposable MinIO and temporary credentials removed.');raise SystemExit
assert not state.exists() and not config.exists()
image='keel-minio:2025-10-15-9e49d5e'
if subprocess.run(['docker','image','inspect',image],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode:
 subprocess.run(['docker','build','-t',image,'infra/minio'],check=True,stdout=subprocess.DEVNULL)
access='keeltest'+secrets.token_hex(8);secret=secrets.token_urlsafe(36);name='keel-media-test-'+uuid.uuid4().hex[:12]
(work/'minio.env').write_text('MINIO_ROOT_USER='+access+'\nMINIO_ROOT_PASSWORD='+secret+'\n')
run(['docker','run','-d','--name',name,'--label',label+'=disposable','--env-file',str(work/'minio.env'),'-p','127.0.0.1::9000','--tmpfs','/data:rw,uid=65532,gid=65532,mode=700',image,'server','/data','--certs-dir','/tmp/certs']);state.write_text(name)
port=run(['docker','port',name,'9000/tcp']).split(':')[-1];endpoint='http://127.0.0.1:'+port
config.write_text('KEEL_MEDIA_ENDPOINT='+endpoint+'\nKEEL_TEST_MEDIA_ENDPOINT='+endpoint+'\nKEEL_MEDIA_ACCESS_KEY='+access+'\nKEEL_MEDIA_SECRET_KEY='+secret+'\nKEEL_MEDIA_BUCKET=media\n')
for _ in range(100):
 try:urllib.request.urlopen(endpoint+'/minio/health/ready',timeout=1);break
 except Exception:time.sleep(.1)
else:
 logs=run(['docker','logs',name]).replace(secret,'[redacted]').replace(access,'[redacted]');(work/'minio.log').write_text(logs)
 raise RuntimeError('Disposable MinIO did not become ready')
print('Disposable versioned media storage ready; credentials remain in private test files.')
