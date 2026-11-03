import copy, json, os, socketserver, tempfile, threading, unittest
from pathlib import Path
from app import RPC, Store, Collector, demo, payload, validate_summary

class Tests(unittest.TestCase):
    def setUp(self): self.store=Store(); demo(self.store)
    def test_stale_summary_cannot_overwrite(self):
        r=copy.deepcopy(self.store.rows['t1']); self.store.update(r['facts'],{'text':'new','complete':True,'how':'screen'})
        self.assertFalse(self.store.commit('t1',r['revision'],0,r['summary'],r['text']))
        self.assertTrue(self.store.rows['t1']['stale'])
    def test_reconnect_invalidates_result(self):
        r=copy.deepcopy(self.store.rows['t1']); self.store.reset()
        self.assertFalse(self.store.commit('t1',r['revision'],0,r['summary'],r['text']))
    def test_duplicate_screen_no_revision(self):
        r=self.store.rows['t1']; revision=r['revision']
        # Match original demo input; stable content must not create extra requests.
        t={**r['facts']}; self.store.update(t,{'text':r['text'],'complete':True,'how':'screen'})
        self.assertEqual(revision,r['revision'])
    def test_event_invalidates_inflight(self):
        c=Collector(None,self.store,['t1'],threading.Event()); r=copy.deepcopy(self.store.rows['t1'])
        c.notice({'method':'event','params':{'terminal':'t1','type':'status'}})
        self.assertFalse(self.store.commit('t1',r['revision'],0,r['summary'],r['text']))
    def test_missing_event_reset(self):
        c=Collector(None,self.store,['t1'],threading.Event()); c.notice({'method':'missed','params':{'seq':0,'terminals':[]}})
        self.assertEqual(self.store.epoch,1)
    def test_delta_and_snapshot(self):
        r=copy.deepcopy(self.store.rows['t1']); r['summary_text']='\n'.join('line '+str(i) for i in range(100)); r['text']=r['summary_text']+'\nnew'
        self.assertEqual(payload(r)['mode'],'screen_diff'); r['text']='repaint'; self.assertEqual(payload(r)['mode'],'screen_snapshot')
    def test_invalid_summary(self):
        with self.assertRaises(ValueError): validate_summary({'purpose':'made up'})
    def test_protocol_allowlist(self):
        with self.assertRaises(ValueError): RPC('').send(None,'terminal.send',{})
        with self.assertRaises(ValueError): RPC('').send(None,'terminal.read',{'lines':100})
    def test_official_schema_requests(self):
        s=json.loads(Path('evidence/hc.openrpc.json').read_text()); methods={m['name']:m for m in s['methods']}
        for name,params in [('terminal.list',{'all':True}),('terminal.read',{'terminal':'t1','lines':0}),('events.subscribe',{})]:
            schema={p['name']:p for p in methods[name]['params']}
            self.assertTrue(set(params)<=set(schema)); self.assertTrue({k for k,v in schema.items() if v['required']}<=set(params))
    def test_unix_rpc_and_events(self):
        requests=[]
        class Handler(socketserver.StreamRequestHandler):
            def handle(inner):
                msg=json.loads(inner.rfile.readline()); requests.append(msg)
                result={'seq':7,'terminals':[]} if msg['method']=='events.subscribe' else {'text':'safe test','how':'screen','complete':True}
                inner.wfile.write((json.dumps({'jsonrpc':'2.0','id':msg['id'],'result':result})+'\n').encode());inner.wfile.flush()
                if msg['method']=='events.subscribe':
                    inner.wfile.write((json.dumps({'jsonrpc':'2.0','method':'missed','params':{'seq':8,'terminals':[]}})+'\n').encode());inner.wfile.flush()
        with tempfile.TemporaryDirectory() as d:
            path=d+'/core.sock'; server=socketserver.ThreadingUnixStreamServer(path,Handler)
            t=threading.Thread(target=server.serve_forever,daemon=True);t.start()
            try:
                rpc=RPC(path); self.assertEqual(rpc.call('terminal.read',{'terminal':'t1','lines':0})['text'],'safe test')
                messages=[]
                with self.assertRaises(ConnectionError): rpc.events(messages.append)
                self.assertEqual([m['method'] for m in messages],['snapshot','missed'])
                self.assertEqual(len(requests),2)
            finally: server.shutdown(); server.server_close()

if __name__=='__main__': unittest.main()
