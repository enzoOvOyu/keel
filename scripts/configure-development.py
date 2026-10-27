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
  IF to_regclass('public.network_address_plans') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON network_address_plans,network_address_plan_approvals FROM keel_app; END IF;
  IF to_regclass('public.asset_view_revisions') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON asset_view_revisions FROM keel_app; REVOKE DELETE,TRUNCATE ON asset_views FROM keel_app; END IF;
  IF to_regclass('public.pci_topology_snapshots') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON pci_topology_snapshots,topology_diffs,topology_issue_bindings FROM keel_app; END IF;
  IF to_regclass('public.validation_wiring_issue_retests') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON validation_wiring_issue_retests,validation_wiring_issue_escalations FROM keel_app; END IF;
  IF to_regclass('public.validation_wiring_confirmations') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON validation_wiring_confirmations,validation_wiring_confirmed_ports FROM keel_app; END IF;
  IF to_regclass('public.validation_wiring_issue_bindings') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON validation_wiring_issue_bindings,validation_wiring_issue_observations FROM keel_app; END IF;
  IF to_regclass('public.validation_wiring_nonstarts') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON validation_wiring_nonstarts FROM keel_app; END IF;
  IF to_regclass('public.validation_wiring_runs') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON validation_wiring_runs,validation_wiring_run_targets FROM keel_app; END IF;
  IF to_regclass('public.validation_wiring_host_proofs') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON validation_wiring_host_dispatches,validation_wiring_host_proofs FROM keel_app; END IF;
  IF to_regclass('public.validation_wiring_capture_records') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON validation_wiring_readback_queries,validation_wiring_capture_records FROM keel_app; END IF;
  IF to_regclass('public.validation_wiring_commands') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON validation_wiring_commands,validation_wiring_readback_windows,validation_wiring_readback_dispatches,validation_wiring_readback_results FROM keel_app; END IF;
  IF to_regclass('public.validation_wiring_batches') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON validation_wiring_batches,validation_wiring_batch_targets,validation_wiring_step_reports FROM keel_app; END IF;
  IF to_regclass('public.validation_wiring_handshakes') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON validation_wiring_handshakes FROM keel_app; END IF;
  IF to_regclass('public.topology_issue_retests') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON topology_issue_retests,topology_issue_escalations FROM keel_app; END IF;
  IF to_regclass('public.validation_precollect_requests') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON validation_precollect_requests,validation_precollect_records,validation_precollect_ports,validation_gate_evidence FROM keel_app; END IF;
  IF to_regclass('public.bmc_credential_verification_results') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON bmc_credential_verifications,bmc_credential_verification_targets,bmc_credential_verification_results FROM keel_app; END IF;
  IF to_regclass('public.bmc_policy_runs') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON bmc_policy_runs,bmc_inventory_events FROM keel_app; REVOKE DELETE,TRUNCATE ON bmc_collection_policies FROM keel_app; END IF;
  IF to_regclass('public.bmc_collection_dispatches') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON bmc_collection_dispatches FROM keel_app; END IF;
  IF to_regclass('public.device_credential_versions') IS NOT NULL THEN REVOKE INSERT,UPDATE,DELETE,TRUNCATE ON device_credential_versions,device_credential_heads,device_credential_revocations FROM keel_app; END IF;
  IF to_regclass('public.bmc_credential_verification_results') IS NOT NULL THEN GRANT UPDATE(verification_receipt_id) ON device_credential_heads TO keel_app; END IF;
  IF to_regclass('public.bmc_collection_requests') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON bmc_collection_requests,bmc_collection_records,bmc_collection_resources FROM keel_app; END IF;
  IF to_regclass('public.network_template_changes') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON network_template_changes,network_template_change_jobs FROM keel_app; END IF;
  IF to_regclass('public.issue_verification_dispatches') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON issue_verification_dispatches,issue_verification_queries FROM keel_app; END IF;
  IF to_regclass('public.issue_verification_requests') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON issue_verification_requests,issue_verification_evidence,issue_verification_results,issue_escalations FROM keel_app; END IF;
  IF to_regclass('public.fabric_diagnostic_records') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON fabric_diagnostic_records,fabric_port_issue_bindings,fabric_port_issue_observations FROM keel_app; END IF;
  IF to_regclass('public.fabric_issue_bindings') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON fabric_issue_bindings,fabric_issue_observations FROM keel_app; END IF;
  IF to_regclass('public.issue_events') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON issue_events FROM keel_app; REVOKE DELETE,TRUNCATE ON issues FROM keel_app; END IF;
  IF to_regclass('public.audits') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON audits FROM keel_app; END IF;
  IF to_regclass('public.keel_schema_migrations') IS NOT NULL THEN REVOKE INSERT,UPDATE,DELETE,TRUNCATE ON keel_schema_migrations FROM keel_app; END IF;
  IF to_regclass('public.schema_migrations') IS NOT NULL THEN REVOKE INSERT,UPDATE,DELETE,TRUNCATE ON schema_migrations FROM keel_app; END IF;
  IF to_regclass('public.notification_webhook_policies') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON notification_webhook_policies FROM keel_app; REVOKE DELETE,TRUNCATE ON notification_webhook_deliveries FROM keel_app; END IF;
  IF to_regclass('public.credential_master_state') IS NOT NULL THEN REVOKE INSERT,UPDATE,DELETE,TRUNCATE ON credential_master_state,credential_master_rewraps,credential_master_rewrap_entries FROM keel_app; END IF;
  IF to_regclass('public.provision_installation_rollouts') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON provision_installation_rollouts FROM keel_app; END IF;
  IF to_regclass('public.ssh_domain_keys') IS NOT NULL THEN REVOKE INSERT,UPDATE,DELETE,TRUNCATE ON ssh_domain_keys FROM keel_app; END IF;
  IF to_regclass('public.agent_enrollments') IS NOT NULL THEN
    REVOKE INSERT,UPDATE,DELETE,TRUNCATE ON agent_enrollments FROM keel_app;
    GRANT UPDATE(consumed_at,certificate_id) ON agent_enrollments TO keel_app;
  END IF;
  IF to_regclass('public.agent_credentials') IS NOT NULL THEN
    REVOKE UPDATE,DELETE,TRUNCATE ON agent_credentials FROM keel_app;
    GRANT UPDATE(state_version) ON agent_credentials TO keel_app;
  END IF;
  IF to_regclass('public.agent_connections') IS NOT NULL THEN REVOKE DELETE,TRUNCATE ON agent_connections FROM keel_app; END IF;
  IF to_regclass('public.agent_certificate_renewals') IS NOT NULL THEN REVOKE UPDATE,DELETE,TRUNCATE ON agent_certificate_renewals FROM keel_app; END IF;
  IF to_regclass('public.provision_key_repairs') IS NOT NULL THEN REVOKE DELETE,TRUNCATE ON provision_key_repairs FROM keel_app; END IF;
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
