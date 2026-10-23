"""Version-1 TLS Kea/boot/collector/repository fixture; no device or external network access."""
import http.server,ssl,threading,hashlib,json,subprocess,socket,struct,datetime,re
from pathlib import Path
class KeaFixture:
 def __init__(self,work):
  self.work=work;self.fault=work/'fault';self.log=work/'commands.log';self.fault.write_text('');self.log.write_text('')
  self.cert=work/'tls.pem';self.key=work/'tls.key'
  subprocess.run(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-keyout',str(self.key),'-out',str(self.cert),'-days','1','-subj','/CN=127.0.0.1','-addext','subjectAltName=IP:127.0.0.1','-addext','basicConstraints=critical,CA:TRUE','-addext','extendedKeyUsage=serverAuth,clientAuth'],check=True,capture_output=True)
  self.config={'Dhcp4':{'valid-lifetime':600,'lease-database':{'type':'memfile','password':'fixture-only-secret'},'subnet4':[{'id':1,'subnet':'192.0.2.0/24','pools':[{'pool':'192.0.2.2 - 192.0.2.254'}]}]}}
  self.artifact=b'approved-fixture-canary';fixture=self
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
    response.extend(bytes([53,1,kind,54,4])+socket.inet_aton('127.0.0.1')+bytes([51,4])+struct.pack('!I',600)+bytes([239,len(url)])+url+bytes([255]))
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
     if fixture.fault.read_text()=='fabric-foreign-device':devices[0]['serial']='FOREIGN-DEVICE'
     if fixture.fault.read_text()=='fabric-no-data':devices=[]
     self.answer({'version':'fabric-collection/v1','project_id':self.path.split('/')[3],'evidence_source':'protocol_fixture','devices':devices})
    elif self.path=='/boot/readyz':self.answer({'service':'keel-ztp','status':'ready','capabilities':['ztp239']})
    elif self.path=='/collector/readyz':self.answer({'service':'keel-fabric-collector','status':'ready','capabilities':['lldp','interfaces','ddm']})
    elif self.path=='/canary':self.answer(fixture.artifact)
    else:self.answer({},404)
   def do_POST(self):
    body=json.loads(self.rfile.read(int(self.headers['Content-Length'])));cmd=body['command']
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
  for name,path in {'kea':'/','boot':'/boot/readyz','collector':'/collector/readyz','repository':'/canary'}.items():
   targets[name]={'url':origin+path,'ca_file':str(self.cert),'certificate_file':str(self.cert),'key_file':str(self.key)}
  targets['kea']['allow_config_write']=True;targets['kea']['through_agent']=True;targets['repository']['sha256']=hashlib.sha256(self.artifact).hexdigest()
  self.configuration=work/'probes.json';self.configuration.write_text(json.dumps({'version':'fabric-probes/v1','targets':targets,'dhcp':{'server':'127.0.0.1:'+str(self.udp.getsockname()[1]),'listen':'127.0.0.1:'+str(self.relay_listen[1]),'relay_ip':'127.0.0.1','mac':'02:00:00:00:ff:01','address':'192.0.2.250','timeout_seconds':2}}));self.configuration.chmod(0o600)
 def close(self):
  self.udp_stop.set();self.udp.close();self.udp_thread.join(timeout=2)
  self.server.shutdown();self.server.server_close();self.thread.join(timeout=2)
