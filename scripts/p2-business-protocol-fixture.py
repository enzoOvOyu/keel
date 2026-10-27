"""Private P2 test transport. Exercises device-origin software branches only.
Never used by runtime configuration or hardware acceptance. No real device I/O.
"""
import datetime, hashlib, importlib.util, json, re
from pathlib import Path
spec=importlib.util.spec_from_file_location('kea_contract',Path(__file__).with_name('kea-contract-fixture.py'))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
class P2BusinessFixture(module.KeaFixture):
 def __init__(self,work):
  super().__init__(work)
  self.recollections={}
  self.scope=work/'business-scope.json';self.scope.write_text('{}');fixture=self
  original=self.server.RequestHandlerClass
  class Handler(original):
   def sample(self,s):
    return {'version':'fabric-snapshot/v1','source':'switch','provider':'software-business-fixture-no-hardware','serial':s['switch_serial'],'nos':'cumulus-fixture','collected_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'capabilities':{'interfaces':True,'lldp':True},'ports':[{'name':'swp1','link_up':True,'lldp_readable':True,'speed_mbps':100000,'mtu':1500,'neighbor':{'serial':s['host_serial'],'chassis_id':s['host_mac'],'port_id':s['host_port_id'],'sysname':s['hostname'],'sysdesc':json.dumps({'sn':s['host_serial'],'rail':'rail-1','port':'1'},separators=(',',':'))}}],'fdb':[],'arp':[]}
   def do_GET(self):
    if '/recollections/' in self.path:
     receipt=fixture.recollections.get(self.path.rsplit('/',1)[-1]);self.answer(receipt or {},200 if receipt else 404);return
    if re.fullmatch(r'/v1/projects/[a-f0-9]{32}/snapshots',self.path):
     s=json.loads(fixture.scope.read_text());now=datetime.datetime.now(datetime.timezone.utc).isoformat()
     sample=self.sample(s)
     self.answer({'version':'fabric-collection/v1','project_id':s['project_id'],'evidence_source':'device','devices':[sample]});return
    super().do_GET()
   def answer(self,data,status=200):
    if isinstance(data,dict) and data.get('version')=='network-operation/v1':data['evidence_source']='device';data['provider']='software-business-fixture-no-hardware'
    super().answer(data,status)
   def do_POST(self):
    if re.fullmatch(r'/v1/projects/[a-f0-9]{32}/recollections',self.path):
     raw=self.rfile.read(int(self.headers['Content-Length']));r=json.loads(raw);s=json.loads(fixture.scope.read_text());assert r['project_id']==s['project_id']
     result={k:r[k] for k in ['version','project_id','execution_id','command_id','fencing_token','scope_hash','evidence_source']};result['devices']=[self.sample(s)]
     if r.get('port_diagnostic'):
      scope=r['port_diagnostic'];now=datetime.datetime.now(datetime.timezone.utc).isoformat();rawref='software-fixture://original/'+r['command_id'];link=s.get('diagnostic_link','up')
      capture={'version':'keel.port-diagnostic/v1','scope':scope,'binding':{k:r[k] for k in ['execution_id','attempt_id','command_id','fencing_token','scope_hash']},'started_at':now,'finished_at':now,'stopped':True,'lldp_readable':True,'host_peer_serial':s['switch_serial'],'host_peer_port':'swp1','switch_peer_serial':s['host_serial'],'switch_peer_mac':s['host_mac'],'switch_peer_interface':'eth0','observations':[{'key':'link.state','entity':'eth0','value':None,'state':link,'quality':'good','observed_at':now,'interface':'eth0','permanent_mac':s['host_mac'],'bdf':'0000:05:00.0'}],'raw_refs':[rawref]}
      result['devices']=[];result['port_diagnostic']=capture
     receipt={k:r[k] for k in ['project_id','execution_id','command_id','fencing_token','scope_hash','evidence_source']};receipt.update(version='fabric-recollect-status/v1',request_hash=hashlib.sha256(raw).hexdigest(),state='completed',finished_at=datetime.datetime.now(datetime.timezone.utc).isoformat(),result=result)
     fixture.recollections[r['command_id']]=receipt;self.answer(result);return
    if re.fullmatch(r'/v1/projects/[a-f0-9]{32}/ztp/manageability',self.path):
     raw=self.rfile.read(int(self.headers['Content-Length']));r=json.loads(raw);s=json.loads(fixture.scope.read_text())
     assert r['project_id']==s['project_id'] and r['switch_id']==s['switch_id'] and r['serial_number']==s['switch_serial']
     digest=hashlib.sha256(raw).hexdigest()
     result={k:r[k] for k in ['project_id','switch_id','serial_number','credential_batch_ref','hostname','management_ip','prefix_bits','gateway']}
     result.update(version='ztp-manageability/v1',request_sha256=digest,evidence_source='device',code='ready',at=datetime.datetime.now(datetime.timezone.utc).isoformat(),management_vrf='mgmt',host_key_sha256=digest,host_key_trust_ref='software-fixture-only:approved-host-key',ssh_authenticated=True,credential_purpose='ztp-management-read',config_imported=True,services_restarted=True,front_panel_isolated=True,dhcp_dora_verified=True,dhcp_client_mac=r['management_mac'],dhcp_lease_ip=r['management_ip'],dhcp_proof_sha256=digest,evidence_refs=[digest])
     self.answer(result);return
    super().do_POST()
  self.server.RequestHandlerClass=Handler
