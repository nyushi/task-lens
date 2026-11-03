"""Source-independent, bounded task graph with optimistic commits."""
import copy
import threading

STATES={'done','doing','todo','unknown'}
ATTENTION={'decision','review','none','unknown'}

def validate(tasks, roots, sources):
    if not isinstance(tasks,dict) or len(tasks)>200: raise ValueError('Invalid task map')
    if not isinstance(roots,list) or len(set(roots))!=len(roots): raise ValueError('Invalid roots')
    if any(r not in tasks for r in roots): raise ValueError('Missing root')
    edges={k:[] for k in tasks}; count=0
    for key,t in tasks.items():
        if t.get('id')!=key or not isinstance(key,str) or not key: raise ValueError('Task ID mismatch')
        if not isinstance(t.get('purpose'),str) or len(t['purpose'])>2000: raise ValueError('Invalid purpose')
        if t.get('state') not in STATES or t.get('attention') not in ATTENTION: raise ValueError('Invalid task status')
        ids=set()
        def walk(nodes,depth):
            nonlocal count
            if not isinstance(nodes,list) or depth>12: raise ValueError('Invalid tree depth')
            for n in nodes:
                count+=1
                if count>2000: raise ValueError('Too many nodes')
                if not isinstance(n,dict) or not isinstance(n.get('id'),str) or not n['id'] or n['id'] in ids: raise ValueError('Invalid node ID')
                ids.add(n['id'])
                if n.get('state') not in STATES or n.get('attention') not in ATTENTION or type(n.get('current')) is not bool: raise ValueError('Invalid node status')
                if not isinstance(n.get('sourceRefs'),list) or any(r not in sources for r in n['sourceRefs']): raise ValueError('Missing source reference')
                c=n.get('content',{})
                if c.get('kind')=='text':
                    if not isinstance(c.get('text'),str) or len(c['text'])>2000: raise ValueError('Invalid text')
                elif c.get('kind')=='task':
                    if c.get('taskId') not in tasks: raise ValueError('Missing embedded task')
                    edges[key].append(c['taskId'])
                else: raise ValueError('Invalid content kind')
                walk(n.get('children',[]),depth+1)
        walk(t.get('tree'),0)
    heights={}
    def visit(k,path):
        if k in path: raise ValueError('Task cycle')
        if k in heights:return heights[k]
        height=1+max((visit(child,path|{k}) for child in edges[k]),default=0)
        if height>12: raise ValueError('Task nesting too deep')
        heights[k]=height
        return height
    for k in tasks:visit(k,set())

class TaskStore:
    def __init__(self):
        self.lock=threading.RLock(); self.revision=0; self.source_version=None
        self.tasks={}; self.roots=[]; self.sources={}; self.error=None; self.connected=False
    def observe(self,version,connected):
        with self.lock:self.source_version=version;self.connected=connected
    def commit(self,base_revision,source_version,result):
        with self.lock:
            if self.revision!=base_revision or self.source_version!=source_version:return False
            tasks=copy.deepcopy(result['tasks']);roots=copy.deepcopy(result['roots']);sources=copy.deepcopy(result['sources'])
            validate(tasks,roots,sources)
            # Keep tasks if a source disappears; absence is not completion/deletion.
            for k,old in self.tasks.items():
                if k not in tasks:
                    tasks[k]=copy.deepcopy(old);tasks[k]['stale']=True
                    if k in self.roots and k not in roots:roots.append(k)
            sources={**self.sources,**sources};validate(tasks,roots,sources)
            self.tasks,self.roots,self.sources=tasks,roots,sources
            self.revision+=1;self.error=None;return True
    def public(self):
        with self.lock:return copy.deepcopy(dict(tasks=self.tasks,roots=self.roots,sources=self.sources,revision=self.revision,connected=self.connected,error=self.error))
