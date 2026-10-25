#!/usr/bin/env python3
"""Prepare loopback-only Keel development DB credentials; never prints secrets."""
import argparse
import os
from pathlib import Path
import re
import secrets
import subprocess
from urllib.parse import quote

root=Path(__file__).resolve().parents[1]
parser=argparse.ArgumentParser()
parser.add_argument('--grant',action='store_true',help='Grant application permissions after the reviewed migration is applied')
args=parser.parse_args()
os.chdir(root)
envpath=root/'.env'
if not envpath.is_file(): raise SystemExit('Create .env with development data credentials first.')
settings={}
for line in envpath.read_text().splitlines():
    if line and not line.lstrip().startswith('#') and '=' in line:
        key,value=line.split('=',1);settings[key]=value
owner=settings.get('POSTGRES_USER','keel'); db=settings.get('POSTGRES_DB','keel')
if not all(re.fullmatch('[a-zA-Z_][a-zA-Z0-9_]*',x) for x in (owner,db)): raise SystemExit('Unsupported local database identifier')
if not settings.get('POSTGRES_PASSWORD'): raise SystemExit('Database owner password is missing')

def sql(query):
    result=subprocess.run(['docker','compose','exec','-T','postgres','psql','-X','-U',owner,'-d',db,'-v','ON_ERROR_STOP=1','-At'],input=query,text=True,capture_output=True)
    if result.returncode: raise SystemExit('Development database configuration failed; inspect PostgreSQL locally without printing secrets.')
    return result.stdout.strip()

role_exists=sql("SELECT 1 FROM pg_roles WHERE rolname='keel_app';")=='1'
password=settings.get('POSTGRES_APP_PASSWORD')
if role_exists and not password: raise SystemExit('Existing keel_app has no saved credential; refusing to replace it.')
if not password: password=secrets.token_urlsafe(36)
if not re.fullmatch('[a-zA-Z0-9_-]{32,}',password): raise SystemExit('Application password must be a generated URL-safe value of at least32 characters')
if not role_exists:
    sql("CREATE ROLE keel_app LOGIN PASSWORD '"+password+"';")
if args.grant:
    sql('''GRANT CONNECT ON DATABASE "'''+db+'''" TO keel_app;
GRANT USAGE ON SCHEMA public TO keel_app;
GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO keel_app;
GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO keel_app;
DO $keel$ BEGIN
  IF to_regclass('public.audits') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON audits FROM keel_app; END IF;
  IF to_regclass('public.keel_schema_migrations') IS NOT NULL THEN REVOKE INSERT,UPDATE,DELETE,TRUNCATE ON keel_schema_migrations FROM keel_app; END IF;
  IF to_regclass('public.schema_migrations') IS NOT NULL THEN REVOKE INSERT,UPDATE,DELETE,TRUNCATE ON schema_migrations FROM keel_app; END IF;
  IF to_regclass('public.ssh_domain_keys') IS NOT NULL THEN REVOKE INSERT,UPDATE,DELETE,TRUNCATE ON ssh_domain_keys FROM keel_app; END IF;
  IF to_regclass('public.ssh_trust_requests') IS NOT NULL THEN REVOKE DELETE,TRUNCATE ON ssh_trust_requests FROM keel_app; END IF;
END $keel$;''')
origins=','.join('http://'+host+':'+port for port in ('3000','3001','5173') for host in ('localhost','127.0.0.1'))
app_url=lambda host: 'postgresql://keel_app:'+quote(password,safe='')+'@'+host+'/'+quote(db,safe='')+'?sslmode=disable'
updates={'POSTGRES_APP_PASSWORD':password,'KEEL_ENV':'development','KEEL_DATABASE_URL':app_url('postgres:5432'),'KEEL_PUBLIC_ORIGINS':origins,'KEEL_INSECURE_DEV_COOKIE':'true'}
lines=[line for line in envpath.read_text().splitlines() if line.partition('=')[0] not in updates]
lines.extend(k+'='+v for k,v in updates.items())
tmp=envpath.with_name('.env.dev.tmp');fd=os.open(tmp,os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o600)
with os.fdopen(fd,'w') as out:out.write('\n'.join(lines)+'\n')
tmp.chmod(0o600);tmp.replace(envpath)
config=Path(os.environ.get('XDG_CONFIG_HOME',str(Path.home()/'.config')))/'keel';config.mkdir(parents=True,exist_ok=True);config.chmod(0o700)
base='KEEL_ENV=development\nKEEL_INSECURE_DEV_COOKIE=true\nKEEL_PUBLIC_ORIGINS='+origins+'\n'
owner_url='postgresql://'+quote(owner,safe='')+':'+quote(settings['POSTGRES_PASSWORD'],safe='')+'@127.0.0.1:15432/'+quote(db,safe='')+'?sslmode=disable'
for name,url in [('development.env',app_url('127.0.0.1:15432')),('admin.env',owner_url)]:
    path=config/name;fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o600)
    with os.fdopen(fd,'w') as out:out.write(base+'KEEL_DATABASE_URL='+url+'\n')
    path.chmod(0o600)
print('Loopback development settings saved with mode600; application role prepared'+(' and grants applied.' if args.grant else '. Apply migrations before --grant.'))
