"""Loopback fault injection in front of disposable MinIO; never a runtime service.

Only the private harness owns this server/control file. Real storage requests are
forwarded with their original signed headers; a selected object's GET response
can have one byte flipped without modifying its stored immutable object version.
"""
import http.client,json,re,threading,urllib.parse
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
from pathlib import Path

class MediaIntegrityFaultProxy:
 def __init__(self,directory,endpoint):
  directory=Path(directory).resolve();assert directory.parent==Path('/tmp') and directory.name.startswith('keel-media-')
  upstream=urllib.parse.urlsplit(endpoint);assert upstream.scheme=='http' and upstream.hostname=='127.0.0.1' and upstream.port
  self.control=directory/'integrity-fault.json';self.lock=threading.Lock()
  owner=self
  class Handler(BaseHTTPRequestHandler):
   protocol_version='HTTP/1.1'
   def log_message(self,*args):pass
   def do_GET(self):self.forward()
   def do_HEAD(self):self.forward()
   def do_PUT(self):self.forward()
   def do_POST(self):self.forward()
   def do_DELETE(self):self.forward()
   def forward(self):
    connection=None
    try:
     length=int(self.headers.get('Content-Length','0'));assert 0<=length<=16<<20
     body=self.rfile.read(length) if length else None
     headers=dict(self.headers);headers.pop('Connection',None)
     connection=http.client.HTTPConnection(upstream.hostname,upstream.port,timeout=30)
     connection.request(self.command,self.path,body=body,headers=headers);response=connection.getresponse();data=response.read()
     fault=False
     with owner.lock:
      if self.command=='GET' and owner.control.exists():
       state=json.loads(owner.control.read_text());media=state.get('media_id','')
       if re.fullmatch('[a-f0-9]{32}',media) and self.path.startswith('/media/'+media+'/') and 'versionId=' in self.path and response.status==200:
        state['reads']=state.get('reads',0)+1
        if state.get('remaining',0)>0:
         state['remaining']-=1;fault=True
        owner.control.write_text(json.dumps(state))
     if fault and data:data=bytes([data[0]^1])+data[1:]
     self.send_response(response.status)
     for key,value in response.getheaders():
      if key.lower() not in ('connection','transfer-encoding','content-length') and not (fault and key.lower().startswith('x-amz-checksum-')):self.send_header(key,value)
     self.send_header('Content-Length',str(len(data) if self.command!='HEAD' else int(response.getheader('Content-Length','0'))));self.end_headers()
     if self.command!='HEAD':self.wfile.write(data)
    except (BrokenPipeError,ConnectionResetError):pass
    except Exception:
     self.close_connection=True
     try:self.send_error(502,'Disposable media fault proxy failed')
     except (BrokenPipeError,ConnectionResetError):pass
    finally:
     if connection is not None:connection.close()
  self.server=ThreadingHTTPServer(('127.0.0.1',0),Handler);self.thread=threading.Thread(target=self.server.serve_forever,daemon=True)
 def start(self):
  self.thread.start();return 'http://127.0.0.1:'+str(self.server.server_port)
 def close(self):
  self.server.shutdown();self.server.server_close();self.thread.join(timeout=5);self.control.unlink(missing_ok=True)
