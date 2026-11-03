import copy,unittest
from task_model import TaskStore,validate
from adapters import DemoInput
from transforms import FixtureTransform
from pipeline import tick
class TaskTests(unittest.TestCase):
 def setUp(self):self.a=DemoInput({});self.t=FixtureTransform({});self.s=TaskStore();tick(self.a,self.t,self.s)
 def test_embedded_task(self):self.assertEqual(self.s.tasks['example-1']['tree'][1]['content']['taskId'],'account-policy')
 def test_no_duplicate_conversion(self):self.assertFalse(tick(self.a,self.t,self.s))
 def test_stale_commit(self):self.s.observe('new',True);self.assertFalse(self.s.commit(1,'old',self.a.batch['fixture']))
 def test_stale_revision(self):self.assertFalse(self.s.commit(0,self.s.source_version,self.a.batch['fixture']))
 def test_cycle(self):
  r=copy.deepcopy(self.a.batch['fixture']);r['tasks']['account-policy']['tree'][0]['content']={'kind':'task','taskId':'example-1'}
  with self.assertRaises(ValueError):validate(r['tasks'],r['roots'],r['sources'])
 def test_missing_reference(self):
  r=copy.deepcopy(self.a.batch['fixture']);r['tasks']['example-1']['tree'][0]['sourceRefs']=['missing']
  with self.assertRaises(ValueError):validate(r['tasks'],r['roots'],r['sources'])
 def test_duplicate_node(self):
  r=copy.deepcopy(self.a.batch['fixture']);r['tasks']['example-1']['tree'].append(r['tasks']['example-1']['tree'][0])
  with self.assertRaises(ValueError):validate(r['tasks'],r['roots'],r['sources'])
 def test_disappearance_retains_task(self):
  self.s.commit(1,self.s.source_version,dict(tasks={},roots=[],sources={}))
  self.assertTrue(self.s.tasks['example-1']['stale'])
 def test_invalid_commit_is_atomic(self):
  r=copy.deepcopy(self.a.batch['fixture']);r['tasks']['example-1']['state']='invalid'
  with self.assertRaises(ValueError):self.s.commit(1,self.s.source_version,r)
  self.assertEqual(self.s.revision,1)
if __name__=='__main__':unittest.main()

class CommandTests(unittest.TestCase):
 def transformer(self,args=[]):
  import sys
  from transforms import CommandTransform
  return CommandTransform(dict(command=sys.executable,argv=['fixtures/transform.py',*args],prompt_file='fixtures/prompt.txt',timeout=.1))
 def test_json_roundtrip(self):
  a=DemoInput({});s=TaskStore();self.assertTrue(tick(a,self.transformer(),s));self.assertIn('account-policy',s.tasks)
 def test_exit_and_stderr(self):
  t=self.transformer(['--fail'])
  with self.assertRaises(RuntimeError):t.convert(DemoInput({}).read(),{})
  self.assertIn('synthetic failure',t.last_stderr)
 def test_timeout(self):
  with self.assertRaises(RuntimeError):self.transformer(['--slow']).convert(DemoInput({}).read(),{})
 def test_invalid_preserves_previous(self):
  a=DemoInput({});s=TaskStore();tick(a,FixtureTransform({}),s);before=s.public();s.error='retry'
  with self.assertRaises(KeyError):tick(a,self.transformer(['--invalid']),s)
  self.assertEqual(before['tasks'],s.tasks);self.assertEqual(before['revision'],s.revision)

class PipelineRaceTests(unittest.TestCase):
 def test_changed_input_during_transform_rejected(self):
  a=DemoInput({});s=TaskStore()
  class Mutating:
   def convert(self,batch,previous):
    a.batch['fixture']['tasks']['example-1']['purpose']='new input'
    return batch['fixture']
  self.assertFalse(tick(a,Mutating(),s));self.assertEqual(s.revision,0)
