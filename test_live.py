"""Opt-in real hc integration, isolated session/config, synthetic data only."""
import json, os, subprocess, sys, tempfile, threading, time
from pathlib import Path
from app import RPC, Store, Collector

def main():
    hc=str(Path('.tools/bin/hc').resolve())
    with tempfile.TemporaryDirectory(prefix='hw-',dir='/tmp') as d:
        env={**os.environ,'HC_SESSION':'dashboard-test','HC_SOCKET':d+'/core.sock','XDG_STATE_HOME':d+'/state','XDG_CONFIG_HOME':d+'/config','XDG_RUNTIME_DIR':d+'/run'}
        os.mkdir(d+'/run',0o700)
        def run(*args): return subprocess.run([hc,*args],env=env,capture_output=True,text=True,check=True,timeout=30).stdout
        stop=threading.Event(); notices=[]
        try:
            run('server','start')
            schema=json.loads(run('schema'));Path('evidence/hc-0.1.17.openrpc.json').write_text(json.dumps(schema,indent=2)+'\n')
            methods={m['name']:m for m in schema['methods']}
            assert all(k in methods for k in ['terminal.list','terminal.read','events.subscribe'])
            script=Path(d)/'dummy.py'; script.write_text("import time\nprint('SYNTHETIC: Build demo search page', flush=True)\nprint('\\x1b]7501;state=working:app=demo\\x1b\\\\',end='',flush=True)\ntime.sleep(4)\nprint('SYNTHETIC: Review requested for sample button',flush=True)\nprint('\\x1b]7501;state=blocked:app=demo\\x1b\\\\',end='',flush=True)\ntime.sleep(8)\n")
            rpc=RPC(env['HC_SOCKET']); store=Store()
            def receive():
                try: rpc.events(notices.append)
                except Exception: pass
            threading.Thread(target=receive,daemon=True).start()
            run('run','--bg','--name','dashboard-dummy','--no-parent','--no-notify',sys.executable,str(script))
            terms=rpc.call('terminal.list',{'all':True})['terminals']; assert len(terms)==1
            address=terms[0]['address']; collector=Collector(rpc,store,[address],stop,poll=1)
            threading.Thread(target=collector.run,daemon=True).start()
            deadline=time.monotonic()+20
            while time.monotonic()<deadline:
                rows=store.public()['rows']
                if rows and 'Review requested' in rows[0]['source']: break
                time.sleep(.2)
            assert rows and 'Review requested' in rows[0]['source'],store.public()
            assert rows[0]['facts']['status']['state']=='blocked',rows
            assert any(n.get('method')=='event' and n['params'].get('type')=='status' for n in notices),notices
            result={'hc_version':run('--version').strip(),'isolated_session':True,'dummy_terminals':1,'read_how':rows[0]['read'],'status':'blocked','event_received':True,'collector_revision':rows[0]['revision'],'work_logs_used':False,'result':'PASS'}
            Path('evidence/live-herdr.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result,indent=2))
        finally:
            stop.set()
            run('server','stop')

if __name__=='__main__': main()
