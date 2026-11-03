"""Read-only task overview: configured input, transform, store, web."""
import argparse,json,secrets,threading
from pathlib import Path
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
from task_model import TaskStore
from pipeline import load_plugin,run,tick
# Compatibility imports for existing adapter regression tests.
from herdr_adapter import RPC,Store,Collector,demo,payload,validate_summary

def serve(store,port,mode):
    token=secrets.token_urlsafe(24)
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            if self.headers.get('Host') not in [f'127.0.0.1:{port}',f'localhost:{port}']:self.send_error(403);return
            if self.path==f'/{token}/':body=Path(__file__).with_name('index.html').read_bytes();kind='text/html; charset=utf-8'
            elif self.path==f'/{token}/state':body=json.dumps({**store.public(),'mode':mode},ensure_ascii=False).encode();kind='application/json; charset=utf-8'
            else:self.send_error(404);return
            self.send_response(200)
            for k,v in {'Content-Type':kind,'Cache-Control':'no-store','X-Content-Type-Options':'nosniff','Referrer-Policy':'no-referrer','Content-Security-Policy':"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'"}.items():self.send_header(k,v)
            self.end_headers();self.wfile.write(body)
        def log_message(self,*args):pass
    server=ThreadingHTTPServer(('127.0.0.1',port),Handler)
    print(f'http://127.0.0.1:{port}/{token}/',flush=True)
    try:server.serve_forever()
    finally:server.server_close()

if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('--config',default='demo-config.json');parser.add_argument('--port',type=int,default=8765)
    args=parser.parse_args();config=json.loads(Path(args.config).read_text())
    adapter=load_plugin(config['input']['plugin'],config['input'].get('options',{}))
    transform=load_plugin(config['transform']['plugin'],config['transform'].get('options',{}))
    store=TaskStore();stop=threading.Event();tick(adapter,transform,store)
    threading.Thread(target=run,args=(adapter,transform,store,stop,max(15,config.get('interval',120))),daemon=True).start()
    try:serve(store,args.port,config.get('mode','読み取り専用'))
    except KeyboardInterrupt:pass
    finally:stop.set();adapter.close()
