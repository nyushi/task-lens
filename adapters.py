"""Input plugins: synthetic data and read-only Herdr; no task rendering rules."""
import copy, json, threading, time
from pathlib import Path
from herdr_adapter import RPC, Store, Collector

class HerdrInput:
    def __init__(self,options):
        if not options.get('socket') or not options.get('terminals'):raise ValueError('Explicit socket and terminal allowlist required')
        self.store=Store();self.stop=threading.Event()
        self.collector=Collector(RPC(options['socket']),self.store,options['terminals'],self.stop)
        threading.Thread(target=self.collector.run,daemon=True).start()
    def read(self):
        state=self.store.public();observations=[]
        for r in state['rows']:
            observations.append({'id':'herdr:'+r['address'],'label':r['facts'].get('name'), 'revision':r['revision'],'epoch':self.store.epoch,'observedAt':r['observed_at'],'content':r['source'],'coverage':r['read'],'metadata':r['facts']})
        return {'connected':state['connected'],'observations':observations}
    def close(self):self.stop.set()

class DemoInput:
    def __init__(self,options):
        trees=json.loads(Path(__file__).with_name('demo-trees.json').read_text())
        purposes=['Google認証を実装','検索APIのページング改善','検索画面の応答性改善','一覧の表示崩れを修正','検索の遅延原因を調査']
        tasks={};sources={'example':{'id':'example','content':'架空の作業例です。実際の仕事の履歴ではありません。','coverage':{'complete':True}}}
        def nodes(items):
            return [dict(id=n['id'],state=n['state'],attention=n.get('attention','none'),current=n.get('current',False),content={'kind':'text','text':n['title']},detail=n.get('detail',''),sourceRefs=['example'],children=nodes(n.get('children',[]))) for n in items]
        for i,(key,tree) in enumerate(trees.items()):
            tid='example-'+str(i+1)
            tasks[tid]=dict(id=tid,purpose=purposes[i],state=['doing','doing','doing','done','unknown'][i],attention=['decision','review','none','none','unknown'][i],tree=nodes(tree),updatedAt=time.time(),stale=False)
        # A real Task reference embedded into a node, with its own ordered tree.
        owner=tasks['example-1']['tree'][1]
        tasks['account-policy']=dict(id='account-policy',purpose=owner['content']['text'],state='doing',attention='decision',tree=owner['children'],updatedAt=time.time(),stale=False)
        owner['content']={'kind':'task','taskId':'account-policy'};owner['children']=[]
        self.batch={'connected':True,'fixture':dict(tasks=tasks,roots=['example-'+str(i) for i in range(1,6)],sources=sources)}
    def read(self):return copy.deepcopy(self.batch)
    def close(self):pass

class RecordedTaskInput:
    """Replay an already-produced Task fixture without invoking a model."""
    def __init__(self,options):self.path=Path(options['path'])
    def read(self):return {'connected':True,'fixture':json.loads(self.path.read_text())}
    def close(self):pass
