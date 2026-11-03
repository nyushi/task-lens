"""One-way adapters -> configured transform -> validated task store."""
import copy, hashlib, importlib, json, time

def load_plugin(spec,options):
    module,name=spec.split(':',1)
    return getattr(importlib.import_module(module),name)(options)

def version_of(batch):
    def stable(value):
        if isinstance(value,dict):return {k:stable(v) for k,v in value.items() if k!='observedAt'}
        if isinstance(value,list):return [stable(v) for v in value]
        return value
    return hashlib.sha256(json.dumps(stable(batch),sort_keys=True).encode()).hexdigest()

def tick(adapter,transform,store):
    batch=adapter.read()
    version=version_of(batch)
    prior=store.public()
    unchanged=store.source_version==version and not store.error
    store.observe(version,batch.get('connected',False))
    if unchanged:return False
    result=transform.convert(copy.deepcopy(batch),prior)
    latest=adapter.read()
    latest_version=version_of(latest)
    store.observe(latest_version,latest.get('connected',False))
    if latest_version!=version:
        store.error='入力が更新されたため変換結果を破棄しました。次回更新を待っています。'
        return False
    return store.commit(prior['revision'],version,result)

def run(adapter,transform,store,stop,interval):
    while not stop.is_set():
        try:tick(adapter,transform,store)
        except Exception:
            with store.lock:store.error='入力または変換に失敗しました。前回の表示を維持しています。'
        stop.wait(interval)
