"""Version-1 TLS Kea/boot/collector/repository fixture; no device or external network access."""
import http.server,ssl,threading,hashlib,json,subprocess,socket,struct,datetime,re
from pathlib import Path
class KeaFixture:
 def __init__(self,work):
  self.work=work;self.fault=work/'fault';self.log=work/'commands.log';self.fault.write_text('');self.log.write_text('')
  self.cert=work/'tls.pem';self.key=work/'tls.key'
  subprocess.run(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-keyout',str(self.key),'-out',str(self.cert),'-days','1','-subj','/CN=127.0.0.1','-addext','subjectAltName=IP:127.0.0.1','-addext','basicConstraints=critical,CA:TRUE','-addext','extendedKeyUsage=serverAuth,clientAuth'],check=True,capture_output=True)
  self.config={'Dhcp4':{'valid-lifetime':600,'lease-database':{'type':'memfile','password':'fixture-only-secret'},'subnet4':[{'id':1,'subnet':'192.0.2.0/24','pools':[{'pool':'192.0.2.2 - 192.0.2.254'}]}]}}
  self.network_running={};self.network_receipts={};self.artifact=b'approved-fixture-canary';fixture=self
  self.udp=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);self.udp.bind(('127.0.0.1',0));self.udp.settimeout(.2);self.udp_stop=threading.Event()
  local=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);local.bind(('127.0.0.1',0));self.relay_listen=local.getsockname();local.close()
  def dora():
   while not fixture.udp_stop.is_set():
    try:request,peer=fixture.udp.recvfrom(4096)
    except socket.timeout:continue
    except OSError:return
    if len(request)<240:continue
    options={};i=240
    while i<len(request):
     code=request[i];i+=1
     if code==255:break
     if code==0:continue
     size=request[i];i+=1;options[code]=request[i:i+size];i+=size
    mac=':'.join(f'{b:02x}' for b in request[28:34])
    reservations=fixture.config['Dhcp4']['subnet4'][0].get('reservations',[])
    record=next((v for v in reservations if v['hw-address']==mac),None)
    if record is None:continue
    with fixture.log.open('a') as f:f.write('dhcp-discover\n' if options[53]==b'\x01' else 'dhcp-request\n')
    response=bytearray(request[:240]);response[0]=2;response[16:20]=socket.inet_aton(record['ip-address'])
    kind=2 if options[53]==b'\x01' else 5
    url=bytes.fromhex(record['option-data'][0]['data'])
    if fixture.fault.read_text()=='bad-dora':url=b'https://foreign.example.invalid/'
    response.extend(bytes([53,1,kind,54,4])+socket.inet_aton('127.0.0.1')+bytes([51,4])+struct.pack('!I',600)+bytes([239,len(url)])+url)
    redirect=next((bytes.fromhex(v['data']) for v in record['option-data'] if v['code']==143),None)
    if redirect is not None:response.extend(bytes([143,len(redirect)])+redirect)
    response.extend(bytes([255]))
    fixture.udp.sendto(response,peer)
  self.udp_thread=threading.Thread(target=dora,daemon=True);self.udp_thread.start()
  class Handler(http.server.BaseHTTPRequestHandler):
   def log_message(self,*args):pass
   def answer(self,data,status=200):
    self.send_response(status);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(data if isinstance(data,bytes) else json.dumps(data).encode())
   def do_GET(self):
    if re.fullmatch(r'/v1/projects/[a-f0-9]{32}/snapshots',self.path):
     now=datetime.datetime.now(datetime.timezone.utc)
     if fixture.fault.read_text()=='fabric-stale':now-=datetime.timedelta(hours=1)
     devices=[]
     for index,other in [(2,3),(3,2)]:
      port='swp99' if fixture.fault.read_text()=='fabric-wrong-port' and index==2 else 'swp1'
      devices.append({'version':'fabric-snapshot/v1','source':'switch','provider':'protocol-fixture/v1','serial':'FIXTURE-SW-'+str(index),'nos':'cumulus-fixture','collected_at':now.isoformat(),'capabilities':{'lldp':True,'interfaces':True,'ddm':True,'bgp':True,'ptp':True},'bgp_up':True,'ptp_locked':True,'ports':[{'name':'swp1','link_up':True,'lldp_readable':True,'neighbor':{'serial':'FIXTURE-SW-'+str(other),'chassis_id':'02:00:00:00:01:'+str(other).zfill(2),'port_id':port,'sysname':'fixture-'+str(other),'sysdesc':'fixture'},'speed_mbps':100000,'mtu':9216,'crc':0,'flaps':0,'optics':{'vendor':'fixture','model':'fixture-optic','serial':'OPTIC-'+str(index),'media_type':'optical','rx_dbm':-4,'tx_dbm':-3,'temperature_c':42,'voltage_v':3.3,'bias_ma':5,'supported':True},'metrics':{'pfc_tx_pause_frames':0}}],'fdb':[],'arp':[]})
     if fixture.fault.read_text().startswith('fabric-lossless'):
      for device in devices:
       device['capabilities'].update(lossless_metrics=True,nvue=True)
       intended={'no_drop_priorities':[6],'dscp_map':{'17':6},'trust_mode':'dscp','ecn':{'enabled_tc':[6],'min_threshold_bytes':20,'max_threshold_bytes':100,'probability':0.05},'traffic_pool':{'lossless_ratio_percent':60},'roce_profile':'lossless','pool':'shared-lossless','queue':'6'}
       applied={**intended,'roce_operational_profile':'lossless'}
       if fixture.fault.read_text()=='fabric-lossless-qos':applied['trust_mode']='pcp'
       device['applied_lossless']={'swp1':applied}
       points=[]
       definitions=[('pfc_rx_pause_frames','frames','port_priority','ethtool',60,9007199254740992),('pfc_tx_pause_frames','frames','port_priority','ethtool',60,9007199254740992),('pfc_xoff_duration_ms','ms','port_priority','events',60,0),('ecn_marked_packets','packets','port','ethtool',60,0),('tx_packets_ecn_enabled_tc','packets','port','ethtool',60,1000000),('queue_depth_watermark_bytes','bytes','port_queue','nvue',10,0),('buffer_pool_occupancy_ratio','ratio','device_pool','nvue',10,0),('wjh_drop_events','events','port_priority','wjh',60,0),('interface_no_drop_packets','packets','port_priority','ethtool',60,0),('ecn_max_threshold_bytes','bytes','device_pool','nvue',900,100),('xoff_buffer_bytes','bytes','device_pool','nvue',900,1000),('pool_size_bytes','bytes','device_pool','nvue',900,160000000),('lossless_ratio_percent','percent','device_pool','nvue',900,60)]
       for key,unit,axis,source,period,value in definitions:
        for offset in range(-3600,1,period):
         if fixture.fault.read_text()=='fabric-lossless-gap' and key=='ecn_marked_packets' and -1200<offset<-600:continue
         v=value
         if key=='tx_packets_ecn_enabled_tc':v=value+(3600+offset)*100
         if key=='pfc_tx_pause_frames' and offset==0 and fixture.fault.read_text()=='fabric-lossless-pfc':v+=1
         if key=='pfc_xoff_duration_ms' and offset==0 and fixture.fault.read_text()=='fabric-lossless-storm':v=90000
         point={'key':key,'unit':unit,'source':source,'at':(now+datetime.timedelta(seconds=offset)).isoformat(),'value':v,'port':''}
         if axis.startswith('port'):point['port']='swp1'
         if axis=='port_priority':point['priority']=6
         if axis=='port_queue':point['queue']='6'
         if axis=='device_pool':point['pool']='shared-lossless'
         points.append(point)
       if device['serial']=='FIXTURE-SW-2':
        device['applied_lossless']['swp2']=dict(applied)
        device['ports'].append(dict(device['ports'][0],name='swp2',neighbor=None))
        points.extend([dict(point,port='swp2') for point in list(points) if point['port']=='swp1'])
       device['metric_points']=points
       device['capabilities']['logs']=True
       device['log_window_start']=(now-datetime.timedelta(hours=48)).isoformat()
       device['logs']=[{'at':(now-datetime.timedelta(minutes=minute)).isoformat(),'level':'error','source':'syslog','process':'bgpd','message':'BGP peer down','raw':'BGP peer down on swp1','object_ref':'swp1'} for minute in [2,1]]+[{'at':now.isoformat(),'level':'error','source':'snmp_trap','process':'asic','message':'unrecognized fault 42','raw':'unrecognized fault 42','object_ref':''}]


     if fixture.fault.read_text().startswith('fabric-diag'):
      fault=fixture.fault.read_text()
      for device in devices:
       device['capabilities'].update(interface_counters=True,physical_link=True,nvue=True)
       if fault=='fabric-diag-baseline':device['collected_at']=(now-datetime.timedelta(seconds=60)).isoformat()
       port=device['ports'][0];port.update(counter_epoch='fixture-boot1-reset1',duplex='full',media_class='optical',crc=9007199254740992,flaps=0)
       device['applied_interfaces']={'swp1':{'speed_mbps':100000,'mtu':9216,'vlans':[],'pfc_priorities':[6],'autoneg':True}}
       if fault in ['fabric-diag-current','fabric-diag-mismatch','fabric-diag-reset','fabric-diag-source']:port['crc']+=1
       if fault=='fabric-diag-reset':port['counter_epoch']='fixture-boot2-reset1'
       if fault=='fabric-diag-source':device['provider']='changed-protocol-fixture/v1'
       if fault=='fabric-diag-mismatch':
        if device['serial']=='FIXTURE-SW-2':device['applied_interfaces']['swp1']['mtu']=1500;port['optics']['temperature_c']=90
        else:port['speed_mbps']=25000;port['duplex']='half';port['media_class']='copper'
       if device['serial']=='FIXTURE-SW-2':
        unused={**port,'name':'swp99','neighbor':None,'crc':9999999999999999,'optics':{**port['optics'],'serial':'UNUSED-OPTIC','rx_dbm':100}}
        device['ports'].append(unused)
     if fixture.fault.read_text()=='fabric-foreign-device':devices[0]['serial']='FOREIGN-DEVICE'
     if fixture.fault.read_text()=='fabric-no-data':devices=[]
     self.answer({'version':'fabric-collection/v1','project_id':self.path.split('/')[3],'evidence_source':'protocol_fixture','devices':devices})
    elif self.path=='/sztp/readyz':self.answer({'service':'keel-ztp','status':'ready','capabilities':['sztp143']})
    elif self.path=='/boot/readyz':self.answer({'service':'keel-ztp','status':'ready','capabilities':['ztp239']})
    elif self.path=='/collector/readyz':self.answer({'service':'keel-fabric-collector','status':'ready','capabilities':['lldp','interfaces','ddm']})
    elif self.path=='/canary':self.answer(fixture.artifact)
    else:self.answer({},404)
   def do_POST(self):
    raw=self.rfile.read(int(self.headers['Content-Length']));body=json.loads(raw)
    if re.fullmatch(r'/v1/projects/[a-f0-9]{32}/network-delivery/review',self.path):
     fault=fixture.fault.read_text();now=datetime.datetime.now(datetime.timezone.utc)
     result={'version':'network-delivery-review/v1','project_id':body['project_id'],'scope_hash':body['scope_hash'],'request_sha256':hashlib.sha256(raw).hexdigest(),'observed_at':now.isoformat(),'review':{'acceptance':None,'waivers':[],'code':'fixture_no_cmdb_acceptance'}}
     if fault=='delivery-review-bad-scope':result['scope_hash']='0'*64
     if fault=='delivery-review-stale':result['observed_at']=(now-datetime.timedelta(minutes=1)).isoformat()
     if fault=='delivery-review-fixture':result['review']['acceptance']={'id':'fixture-acceptance','scope_hash':body['scope_hash'],'approval_ref':'fixture:approval','cmdb_snapshot_ref':'fixture:snapshot','accepted_at':now.isoformat(),'evidence_source':'protocol_fixture'}
     self.answer(result);return
    if re.fullmatch(r'/v1/projects/[a-f0-9]{32}/network-operations',self.path):
     op=body;phase=op['phase'];key=(op['project_id'],op['device_id']);fault=fixture.fault.read_text()
     with fixture.log.open('a') as f:f.write('network-'+phase+':'+op['serial']+'\n')
     if op['operation_id'] in fixture.network_receipts:self.answer(fixture.network_receipts[op['operation_id']]);return
     intended=op['intended'];hashof=lambda value:hashlib.sha256(json.dumps(value,sort_keys=True,separators=(',',':'),ensure_ascii=False).encode()).hexdigest()
     running=fixture.network_running.setdefault(key,{**intended,'asn':65001,'description':'before'})
     gates=['syntax','semantic','consistency','rehearsal','golden'] if phase=='template-test' else ['syntax','semantic','consistency','reachability','credentials','system','ports','neighbors','configuration','services','batfish','assertions','rehearsal'] if phase=='prepare' else ['applied'] if phase=='apply' else ['restored'] if phase=='rollback' else ['connectivity','bgp','nvue','hardware','lossless']
     result={'version':'network-operation/v1','operation_id':op['operation_id'],'project_id':op['project_id'],'device_id':op['device_id'],'serial':op['serial'],'input_sha256':hashlib.sha256(raw).hexdigest(),'fencing_token':op['fencing_token'],'state':'completed','code':'','evidence_source':'protocol_fixture','provider':'protocol-fixture/v1','observed_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'gates':[{'id':gate,'state':'PASS','location':gate,'code':'fixture','evidence_sha256':hashof([gate,intended])} for gate in gates],'exit_code':0,'duration_ms':1,'snapshot_ref':'fixture:snapshot:'+hashof(running)}
     if op.get('addresses') and phase in ('template-test','prepare'):
      # This fixture's normalized JSON profile places allocations in this field.
      # Other NOS adapters must validate their own mapping of the same approved
      # addresses; unknown support must not return a fixed PASS.
      matches=intended.get('network_addresses')==op['addresses'] and fault!='network-address-plan-mismatch'
      result['gates'].append({'id':'address-planning','state':'PASS' if matches else 'FAIL','location':'network_addresses','code':'fixture_address_binding' if matches else 'fixture_address_binding_mismatch','evidence_sha256':hashof([op['address_plan_id'],op['addresses'],intended.get('network_addresses')])})
     if phase=='template-test':running=intended
     if phase=='prepare' and fault=='network-precheck-fail':result['gates'][0]['state']='FAIL'
     if phase=='apply':
      if fault=='network-drift-before-apply':running={**running,'description':'external-change'};fixture.network_running[key]=running
      assert op['fencing_token']>0 and op['credential_batch_ref'] is not None
      if op.get('expected_running_hash')!=hashof(running):result.update(state='rejected',exit_code=1,code='running_changed')
      else:fixture.network_running[key]=intended;running=intended
      if fault=='network-drop-apply':self.answer({},503);return
     if phase=='rollback':fixture.network_running[key]=intended;running=intended
     if phase=='verify' and fault=='network-verify-fail':result['gates'][0]['state']='FAIL'
     result['running']=running;result['running_hash']=hashof(running)
     fixture.network_receipts[op['operation_id']]=result;self.answer(result);return
    cmd=body['command']
    with fixture.log.open('a') as f:f.write(cmd+'\n')
    active=lambda:hashlib.sha256(json.dumps(fixture.config,sort_keys=True,separators=(',',':')).encode()).hexdigest().upper()
    args={}
    if cmd=='config-hash-get':args={'hash':active()}
    elif cmd=='config-get':args={**fixture.config,'hash':active()}
    elif cmd=='config-test':assert 'Dhcp4' in body['arguments']
    elif cmd=='config-set':
     fixture.config=body['arguments'];args={'hash':active()}
     if fixture.fault.read_text()=='drop-set':self.answer({},503);return
    elif cmd=='config-write':assert 'arguments' not in body;args={'size':1024,'filename':'/fixture/startup.conf'}
    else:self.answer([{'result':1}]);return
    self.answer([{'result':0,'arguments':args}])
  self.server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler)
  context=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);context.minimum_version=ssl.TLSVersion.TLSv1_2;context.load_cert_chain(self.cert,self.key);context.verify_mode=ssl.CERT_REQUIRED;context.load_verify_locations(self.cert)
  self.server.socket=context.wrap_socket(self.server.socket,server_side=True)
  self.thread=threading.Thread(target=self.server.serve_forever,daemon=True);self.thread.start()
  origin='https://127.0.0.1:'+str(self.server.server_port)
  targets={}
  for name,path in {'kea':'/','boot':'/boot/readyz','sztp':'/sztp/readyz','collector':'/collector/readyz','repository':'/canary','network':'/network/readyz'}.items():
   targets[name]={'url':origin+path,'ca_file':str(self.cert),'certificate_file':str(self.cert),'key_file':str(self.key)}
  targets['network']['allow_config_write']=True;targets['kea']['allow_config_write']=True;targets['kea']['through_agent']=True;targets['repository']['sha256']=hashlib.sha256(self.artifact).hexdigest()
  self.configuration=work/'probes.json';self.configuration.write_text(json.dumps({'version':'fabric-probes/v1','targets':targets,'dhcp':{'server':'127.0.0.1:'+str(self.udp.getsockname()[1]),'listen':'127.0.0.1:'+str(self.relay_listen[1]),'relay_ip':'127.0.0.1','mac':'02:00:00:00:ff:01','address':'192.0.2.250','timeout_seconds':2}}));self.configuration.chmod(0o600)
 def close(self):
  self.udp_stop.set();self.udp.close();self.udp_thread.join(timeout=2)
  self.server.shutdown();self.server.server_close();self.thread.join(timeout=2)
