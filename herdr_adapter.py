"""Herdr watch: dependency-free, explicit-scope, read-only local dashboard."""
import argparse, copy, difflib, hashlib, json, secrets, socket, subprocess, tempfile, threading, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

FIELDS = ['purpose', 'phase', 'needed', 'user_action', 'next_action', 'evidence']
SCHEMA = {'type':'object', 'additionalProperties':False, 'properties':{**{k:{'type':'string','maxLength':600} for k in FIELDS},'attention':{'type':'string','enum':['decision','review','none','unknown']}}, 'required':FIELDS+['attention']}

def validate_summary(s):
    if not isinstance(s,dict) or set(s)!=set(SCHEMA['required']): raise ValueError('Invalid summary fields')
    if any(not isinstance(s[k],str) or len(s[k])>600 for k in FIELDS): raise ValueError('Invalid summary text')
    if s['attention'] not in SCHEMA['properties']['attention']['enum']: raise ValueError('Invalid attention')
    return s

class RPC:
    ALLOWED = {'rpc.discover','terminal.list','terminal.read','events.subscribe'}
    def __init__(self,path): self.path=path
    def connect(self):
        s=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM); s.settimeout(25); s.connect(self.path)
        return s
    def send(self,f,method,params):
        if method not in self.ALLOWED: raise ValueError('Write operations are prohibited')
        if method=='terminal.read' and params.get('lines')!=0: raise ValueError('Screen-only reading required')
        f.write((json.dumps({'jsonrpc':'2.0','id':1,'method':method,'params':params})+'\n').encode()); f.flush()
    def call(self,method,params):
        with self.connect() as s, s.makefile('rwb') as f:
            self.send(f,method,params); r=json.loads(f.readline(4*1024*1024))
            if 'error' in r: raise RuntimeError('Herdr RPC error: '+str(r['error'].get('code')))
            return r['result']
    def events(self,callback):
        with self.connect() as s, s.makefile('rwb') as f:
            self.send(f,'events.subscribe',{})
            first=json.loads(f.readline(4*1024*1024))
            if 'result' not in first: raise RuntimeError('Subscription failed')
            callback({'method':'snapshot','params':first['result']})
            while True:
                line=f.readline(4*1024*1024)
                if not line: raise ConnectionError('Herdr disconnected')
                callback(json.loads(line))

class Store:
    def __init__(self):
        self.lock=threading.RLock(); self.rows={}; self.connected=False; self.error=None; self.calls=0; self.usage={}; self.epoch=0
    def reset(self):
        with self.lock:
            self.epoch+=1
            for r in self.rows.values(): r['revision']+=1; r['stale']=True
    def update(self,t,read):
        a=t['address']; text=read['text'][-12000:]
        facts={k:t.get(k) for k in ['address','name','title','state','status','turn','created_ms']}
        digest=hashlib.sha256(json.dumps([facts,read,text],sort_keys=True).encode()).hexdigest()
        with self.lock:
            old=self.rows.get(a)
            if old and old['digest']==digest:
                old['observed_at']=time.time(); return
            row=old or {'revision':0,'summary':None,'summary_text':'','summary_revision':-1,'last_attempt':0}
            row.update(facts=facts,text=text,digest=digest,read={k:read.get(k) for k in ['complete','how','why']},revision=row['revision']+1,observed_at=time.time(),stale=True,error=None)
            self.rows[a]=row
    def commit(self,a,revision,epoch,summary,text):
        with self.lock:
            r=self.rows.get(a)
            if not r or r['revision']!=revision or self.epoch!=epoch: return False
            r.update(summary=validate_summary(summary),summary_text=text,summary_revision=revision,summary_at=time.time(),stale=False,error=None)
            return True
    def public(self):
        with self.lock:
            rows=[]
            for a,r in self.rows.items():
                v={k:copy.deepcopy(r.get(k)) for k in ['facts','read','revision','summary','summary_revision','summary_at','observed_at','stale','error']}
                v['address']=a; v['source']=r['text']; v['work_tree']=copy.deepcopy(r.get('work_tree')); v['tree_is_example']=r.get('tree_is_example',False); v['task_state']=r.get('task_state'); rows.append(v)
            return {'connected':self.connected,'error':self.error,'rows':rows,'calls':self.calls,'usage':self.usage}

def payload(r):
    old=r['summary_text']; new=r['text']
    # Screens can repaint or roll over. This is a screen diff, never a durable log cursor.
    diff='\n'.join(difflib.unified_diff(old.splitlines(),new.splitlines(),lineterm=''))
    reset=not old or len(diff)>len(new)
    return {'facts':r['facts'],'previous_summary':None if reset else r['summary'],'mode':'screen_snapshot' if reset else 'screen_diff','content':new if reset else diff,'coverage':r['read']}

class Collector:
    def __init__(self,rpc,store,addresses,stop,poll=15):
        self.rpc,self.store,self.addresses,self.stop,self.poll=rpc,store,set(addresses),stop,poll
        self.wake=threading.Event()
    def notice(self,msg):
        if msg.get('method') in ['snapshot','missed']:
            self.store.reset(); self.wake.set()
        elif msg.get('method')=='event':
            p=msg.get('params',{})
            if p.get('terminal') in self.addresses and p.get('type') not in ['size','foreground']:
                # Invalidate in-flight summaries immediately, before a new read completes.
                with self.store.lock:
                    r=self.store.rows.get(p['terminal'])
                    if r: r['revision']+=1; r['stale']=True
                self.wake.set()
    def subscribe(self):
        while not self.stop.is_set():
            try: self.rpc.events(self.notice)
            except Exception:
                with self.store.lock: self.store.connected=False; self.store.error='イベント接続を再試行中'
                self.store.reset(); self.wake.set(); self.stop.wait(3)
    def run(self):
        threading.Thread(target=self.subscribe,daemon=True).start()
        while not self.stop.is_set():
            try:
                terms=self.rpc.call('terminal.list',{'all':True})['terminals']
                selected={t['address']:t for t in terms if t['address'] in self.addresses}
                with self.store.lock:
                    for a in list(self.store.rows):
                        if a not in selected: del self.store.rows[a]
                for a,t in selected.items(): self.store.update(t,self.rpc.call('terminal.read',{'terminal':a,'lines':0}))
                with self.store.lock: self.store.connected=True; self.store.error=None
            except Exception:
                with self.store.lock: self.store.connected=False; self.store.error='Herdrへの接続・読み取りに失敗。表示は過去の情報です'
            self.wake.wait(self.poll); self.wake.clear()
            self.stop.wait(2) # debounce; cap burst reads

def demo(store):
    for i,(name,state,purpose,phase,need,attention) in enumerate([
        ('担当 A / 認証','blocked','Google認証を実装','設計確認','既存ユーザーのアカウント統合方針を決めてください','decision'),
        ('担当 B / API','done','検索APIのページング改善','テスト完了','差分とテスト結果のレビューをお願いします','review'),
        ('担当 C / UI','working','検索画面の応答性改善','実装中','現在は対応不要。次にキーボード操作を検証','none')],1):
        text=f'目的: {purpose}\n段階: {phase}\n{need}'
        t={'address':f't{i}','name':name,'state':'live','status':{'state':state},'turn':2}
        store.update(t,{'text':text,'complete':True,'how':'screen'})
        store.commit(f't{i}',1,0,dict(purpose=purpose,phase=phase,needed=need,user_action=need if attention!='none' else 'なし',next_action='返答後に作業継続' if attention=='decision' else '検証結果を整理',evidence=need,attention=attention),text)
    for address,name,status,attention in [('t4','担当 D / 表示修正','done','none'),('t5','担当 E / 調査',None,'unknown')]:
        t={'address':address,'name':name,'state':'live','status':{'state':status} if status else None}
        text='架空例：修正と確認が完了。追加対応なし。' if address=='t4' else '架空例：状態報告がなく、進捗は未確認。'
        store.update(t,{'text':text,'complete':True,'how':'screen'})
        summary=dict(purpose='一覧の表示崩れを修正' if address=='t4' else '検索の遅延原因を調査',phase='完了' if address=='t4' else '不明',needed='なし' if address=='t4' else '未確認',user_action='なし' if address=='t4' else '未確認',next_action='なし' if address=='t4' else '未確認',evidence=text,attention=attention)
        store.commit(address,1,0,summary,text)
        store.rows[address]['task_state']='completed' if address=='t4' else 'unknown'
    trees=json.loads(Path(__file__).with_name('demo-trees.json').read_text())
    for address,tree in trees.items():
        store.rows[address]['work_tree']=tree
        store.rows[address]['tree_is_example']=True
    store.connected=True
