"""Offline command adapter fixture: no network or model invocation."""
import json,sys,time
if '--fail' in sys.argv:print('synthetic failure',file=sys.stderr);sys.exit(2)
if '--slow' in sys.argv:time.sleep(2)
if '--invalid' in sys.argv:print('{}');sys.exit(0)
request=json.load(sys.stdin)
assert request['contractVersion']==1 and request['prompt']
json.dump(request['input']['fixture'],sys.stdout,ensure_ascii=False)
