"""Codex-specific one-shot adapter. stdin envelope -> validated Task JSON stdout."""
import json,subprocess,sys,tempfile
from pathlib import Path
STATE={'type':'string','enum':['done','doing','todo','unknown']}
ATT={'type':'string','enum':['decision','review','none','unknown']}
def obj(properties):return {'type':'object','properties':properties,'required':list(properties),'additionalProperties':False}
SCHEMA=obj({'purpose':{'type':'string'},'state':STATE,'attention':ATT,'nodes':{'type':'array','items':obj({'id':{'type':'string'},'text':{'type':'string'},'state':STATE,'attention':ATT,'current':{'type':'boolean'}})}})
def main():
    envelope=json.load(sys.stdin)
    with tempfile.TemporaryDirectory(prefix='task-codex-dummy-') as tmp:
        schema=Path(tmp)/'schema.json';result=Path(tmp)/'result.json';schema.write_text(json.dumps(SCHEMA))
        prompt=envelope['prompt']+'\nUse only the supplied synthetic input. Do not use tools, read files, or access network resources. Return the schema JSON, with stable node IDs.\n'+json.dumps(envelope['input'],ensure_ascii=False)
        cmd=['codex','exec','--ignore-user-config','--ephemeral','--sandbox','read-only','--skip-git-repo-check','--cd',tmp,'--output-schema',str(schema),'--output-last-message',str(result),'--color','never','-']
        run=subprocess.run(cmd,input=prompt,text=True,capture_output=True,timeout=120)
        if run.returncode:
            sys.stderr.write(run.stderr[-8000:]);return run.returncode
        if not result.exists():raise ValueError('Codex produced no result')
        value=json.loads(result.read_text())
    source='synthetic-input';task_id='codex-dummy-task'
    task={'id':task_id,'purpose':value['purpose'],'state':value['state'],'attention':value['attention'],'tree':[{'id':n['id'],'state':n['state'],'attention':n['attention'],'current':n['current'],'content':{'kind':'text','text':n['text']},'sourceRefs':[source],'children':[]} for n in value['nodes']]}
    json.dump({'tasks':{task_id:task},'roots':[task_id],'sources':{source:{'id':source,'content':json.dumps(envelope['input'],ensure_ascii=False),'coverage':{'complete':True}}}},sys.stdout,ensure_ascii=False)
    return 0
if __name__=='__main__':sys.exit(main())
