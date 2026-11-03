"""Local deterministic transforms. No model or external service calls."""
import copy

class FixtureTransform:
    def __init__(self,options):pass
    def convert(self,batch,previous):return copy.deepcopy(batch['fixture'])

class ObservationsOnly:
    """Conservative local fallback: no inferred task history or plans."""
    def __init__(self,options):pass
    def convert(self,batch,previous):
        tasks={};sources={}
        for obs in batch['observations']:
            source_id=obs['id'];sources[source_id]=obs
            task_id='observation:'+source_id
            tasks[task_id]={'id':task_id,'purpose':obs.get('label') or '未整理の入力','state':'unknown','attention':'unknown','stale':not batch['connected'],'updatedAt':obs.get('observedAt'), 'tree':[{'id':'observation','state':'unknown','attention':'unknown','current':False,'sourceRefs':[source_id],'content':{'kind':'text','text':'作業の解釈は未設定です。根拠に取得内容を表示します。'},'children':[]}]}
        return dict(tasks=tasks,roots=list(tasks),sources=sources)

class CommandTransform:
    """Trusted user-configured executable, JSON envelope via stdin, JSON stdout."""
    def __init__(self,options):
        from pathlib import Path
        self.command=options['command'];self.argv=options.get('argv',[])
        if not isinstance(self.command,str) or not isinstance(self.argv,list) or any(not isinstance(a,str) for a in self.argv):raise ValueError('command and argv required')
        self.prompt=Path(options['prompt_file']).read_text()
        self.timeout=float(options.get('timeout',30));self.max_output=int(options.get('max_output_bytes',2000000))
        if not 0<self.timeout<=300 or not 0<self.max_output<=8000000:raise ValueError('Invalid limits')
        self.last_stderr=''
    def convert(self,batch,previous):
        import json,subprocess,tempfile,os,signal
        request=json.dumps({'contractVersion':1,'prompt':self.prompt,'input':batch,'previous':previous},ensure_ascii=False)
        # Files prevent stdout/stderr from accumulating unbounded Python memory.
        with tempfile.TemporaryFile() as out,tempfile.TemporaryFile() as err:
            p=subprocess.Popen([self.command,*self.argv],stdin=subprocess.PIPE,stdout=out,stderr=err,text=True,shell=False,start_new_session=True)
            try:p.communicate(request,timeout=self.timeout)
            except subprocess.TimeoutExpired:
                os.killpg(p.pid,signal.SIGKILL);p.communicate();raise RuntimeError('Transform timed out')
            err.seek(0);self.last_stderr=err.read(8192).decode('utf-8',errors='replace')
            if p.returncode:raise RuntimeError('Transform failed with exit code '+str(p.returncode))
            out.seek(0);raw=out.read(self.max_output+1)
            if len(raw)>self.max_output:raise ValueError('Transform output limit exceeded')
            return json.loads(raw)
