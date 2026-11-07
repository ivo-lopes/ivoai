import ast,hashlib,importlib.util,json,os,pathlib,resource,subprocess,sys,threading,time
OUT=pathlib.Path('/var/tmp/ivoai-sampler-readonly-audit-20261028')
SRC=pathlib.Path('/home/ivo/Downloads/PROJETOS/MINDSITE/ivoai-sampler-calibration')
sys.dont_write_bytecode=True;sys.path.insert(0,str(SRC/'scripts'))
def digest(path):return hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()
inputs={name:digest(SRC/name) for name in ['scripts/benchmark_sampler_calibration.py','scripts/benchmark_process_sampler.py','scripts/benchmark-economic-runtime.py','scripts/test_benchmark_sampler_calibration.py']}
planned=['complete_frame','fragmented_frame','two_buffered_messages','closed_pipe_eof','oversize_frame','empty_pipe_deadline','wait4_already_exited_unreaped','wait4_blocked_deadline']
freeze={'schema':1,'source_head':subprocess.check_output(['git','-C',str(SRC),'rev-parse','HEAD'],text=True).strip(),'source_sha256':inputs,'control_sha256':digest(__file__),'python_binary_sha256':digest(sys.executable),'plan':planned,'scope':'one deterministic IPC/reaping matrix, not real calibration workloads, not original failure reproduction','root_original_failure':'case/phase/interpreter/exit/stdout/stderr UNKNOWN; no retrospective inference','workload_deadlines_or_oracles_changed':False}
with (OUT/'freeze.json').open('x') as f:json.dump(freeze,f,indent=2);f.write('\n')
import benchmark_sampler_calibration as c
rows=[]
def record(name,fn):
 start=time.monotonic()
 try:detail=fn();row={'control':name,'result':'PASS_EXPECTED_BEHAVIOR','detail':detail}
 except BaseException as exc:row={'control':name,'result':'FAIL','error_class':type(exc).__name__}
 row['elapsed_ms']=(time.monotonic()-start)*1000;rows.append(row)
 with (OUT/(name+'.json')).open('x') as f:json.dump(row,f,indent=2);f.write('\n')
def pipe_case(body,expected_error=None,two=False):
 r,w=os.pipe()
 try:
  os.write(w,body)
  if expected_error is EOFError:os.close(w);w=None
  try:
   v=c.read_message(r,time.monotonic()+.2)
   if expected_error:raise AssertionError('expected rejection')
   assert v=={'kind':'ready'}
   if two:assert c.read_message(r,time.monotonic()+.2)=={'kind':'done'}
   return {'messages':2 if two else 1,'outcome':'exact JSON accepted'}
  except ValueError:
   assert expected_error in (ValueError,EOFError)
   return {'outcome':'expected bounded malformed/EOF rejection'}
 finally:
  os.close(r)
  if w is not None:os.close(w)
def fragmented():
 r,w=os.pipe();release=threading.Event()
 def writer():
  os.write(w,b'{"kind":');release.wait(1);os.write(w,b'"ready"}\n');os.close(w)
 t=threading.Thread(target=writer);t.start()
 # Predetermined fragmented payload; no workload or semantic condition changes.
 release.set()
 try:assert c.read_message(r,time.monotonic()+.5)=={'kind':'ready'}
 finally:t.join(1);os.close(r)
 assert not t.is_alive();return {'outcome':'fragmentation preserved, writer joined'}
def empty():
 r,w=os.pipe()
 try:
  try:c.read_message(r,time.monotonic()+.02);raise AssertionError('expected timeout')
  except TimeoutError:return {'outcome':'expected timeout; no workload attempt deadline changed'}
 finally:os.close(r);os.close(w)
def owned_child(blocked):
 process=subprocess.Popen([sys.executable,'-I','-S','-c','import time; time.sleep(2)' if blocked else 'pass'],stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True,env={'PATH':'/usr/bin:/bin','HOME':str(OUT/'home')})
 try:
  if not blocked:os.waitid(os.P_PID,process.pid,os.WEXITED|os.WNOWAIT)
  code,usage=c.waiter()(process,.02 if blocked else .2)
  assert code==(124 if blocked else 0) and process.returncode is not None
  try:os.wait4(process.pid,os.WNOHANG);raise AssertionError('owned child not reaped')
  except ChildProcessError:pass
  return {'outcome':'expected deadline kill and exact reap' if blocked else 'already-exited child reaped once','exit_code':code}
 finally:
  if process.returncode is None:
   os.killpg(process.pid,9);os.wait4(process.pid,0);process.returncode=-9
record('complete_frame',lambda:pipe_case(b'{"kind":"ready"}\n'))
record('fragmented_frame',fragmented)
record('two_buffered_messages',lambda:pipe_case(b'{"kind":"ready"}\n{"kind":"done"}\n',two=True))
record('closed_pipe_eof',lambda:pipe_case(b'',EOFError))
record('oversize_frame',lambda:pipe_case(b'x'*2049,ValueError))
record('empty_pipe_deadline',empty)
record('wait4_already_exited_unreaped',lambda:owned_child(False))
record('wait4_blocked_deadline',lambda:owned_child(True))
assert list(r['control'] for r in rows)==planned
assert inputs=={name:digest(SRC/name) for name in inputs}
old=ast.parse(subprocess.check_output(['git','-C',str(SRC),'show','8f513876:scripts/benchmark_sampler_calibration.py'],text=True));new=ast.parse((SRC/'scripts/benchmark_sampler_calibration.py').read_text())
fns=lambda tree:{x.name:ast.dump(x,include_attributes=False) for x in tree.body if isinstance(x,ast.FunctionDef)}
oldf,newf=fns(old),fns(new);stable=['worker','read_message','expected','plan','private_env','waiter','write_new'];assert all(oldf[k]==newf[k] for k in stable)
cohort=json.loads((SRC/'release-notes/evidence/backend-first/process-sampler-calibration-v1/freeze.json').read_text())
summary={'schema':1,'rows':rows,'source_unchanged_after':True,'unchanged_AST_since8f':stable,'all_controls_expected':all(r['result']=='PASS_EXPECTED_BEHAVIOR' for r in rows),'workloads_rerun':0,'provider_turns':0,'current_python_matches_original_cohort':digest(sys.executable)==cohort['python_binary_sha256'],'current_python_version':sys.version.split()[0],'original_root_failure_python_identity':'UNKNOWN','original_failure_cause':'INCONCLUSIVE; no original case/stage/exit or stderr to distinguish failure classes'}
with (OUT/'summary.json').open('x') as f:json.dump(summary,f,indent=2);f.write('\n')
print(json.dumps({k:v for k,v in summary.items() if k!='rows'}));sys.exit(0 if summary['all_controls_expected'] else 1)
