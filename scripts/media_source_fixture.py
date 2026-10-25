"""Disposable loopback HTTP source fixtures; credentials are never accepted."""
import json,re,threading
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
from pathlib import Path
class MediaSourceFixture:
 def __init__(self,work):
  work=Path(work).resolve();assert work.parent==Path('/tmp') and work.name.startswith('keel-media-')
  self.files=work/'source-fixtures';self.files.mkdir(mode=0o700);owner=self;self.lock=threading.Lock();self.log=work/'source-requests.jsonl'
  class Handler(BaseHTTPRequestHandler):
   def log_message(self,*args):pass
   def do_GET(self):
    with owner.lock:
     with owner.log.open('a') as log:log.write(json.dumps({'path':self.path,'credentials_received':any(self.headers.get(name) for name in ['Cookie','Authorization','X-CSRF-Token'])})+'\n')
    match=re.fullmatch('/(artifact|redirect|escape)/([a-f0-9]{32})',self.path)
    if not match:self.send_error(404);return
    mode,identifier=match.groups()
    if mode in ['redirect','escape']:
     self.send_response(302);self.send_header('Location',('/artifact/'+identifier) if mode=='redirect' else 'http://127.0.0.1:1/forbidden');self.end_headers();return
    path=owner.files/(identifier+'.bin')
    if not path.is_file():self.send_error(404);return
    data=path.read_bytes();assert len(data)<=1<<20
    self.send_response(200);self.send_header('Content-Length',str(len(data)));self.send_header('Content-Type','application/octet-stream');self.end_headers();self.wfile.write(data)
  self.server=ThreadingHTTPServer(('127.0.0.1',0),Handler);self.thread=threading.Thread(target=self.server.serve_forever,daemon=True)
 def start(self):self.thread.start();return 'http://127.0.0.1:'+str(self.server.server_port)
 def close(self):self.server.shutdown();self.server.server_close();self.thread.join(timeout=5)
