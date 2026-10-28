import hashlib,json,os,pathlib,sys,threading,time
from unittest import mock
OUT=pathlib.Path('/var/tmp/ivoai-sampler-readonly-audit-20261028');SRC=pathlib.Path('/home/ivo/Downloads/PROJETOS/MINDSITE/ivoai-sampler-calibration');sys.dont_write_bytecode=True;sys.path.insert(0,str(SRC/'scripts'));import benchmark_sampler_calibration as c
freeze={'control':'forced_between_reads_fragmentation','source_sha256':hashlib.sha256((SRC/'scripts/benchmark_sampler_calibration.py').read_bytes()).hexdigest(),'probe_sha256':hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),'note':'supplements initial two-write control by forcing the reader to consume prefix before suffix is sent; no calibration attempt'}
with (OUT/'forced-fragmentation-freeze.json').open('x') as f:json.dump(freeze,f,indent=2);f.write('\n')
r,w=os.pipe();release=threading.Event();prefix=b'{"kind":';seen=0;original=os.read;writer_errors=[]
def writer():
 try:
  os.write(w,prefix)
  if not release.wait(1):raise TimeoutError('control barrier')
  os.write(w,b'"ready"}\n')
 except BaseException as e:writer_errors.append(type(e).__name__)
 finally:os.close(w)
def observed_read(fd,n):
 global seen
 b=original(fd,n)
 if fd==r:
  seen+=len(b)
  if seen==len(prefix):release.set()
 return b
t=threading.Thread(target=writer);t.start();start=time.monotonic()
try:
 with mock.patch.object(os,'read',side_effect=observed_read):value=c.read_message(r,time.monotonic()+.5)
 assert value=={'kind':'ready'} and seen==len(b'{"kind":"ready"}\n')
finally:
 release.set();t.join(1);os.close(r)
assert not t.is_alive() and not writer_errors
with (OUT/'forced-fragmentation-result.json').open('x') as f:json.dump({'result':'PASS_EXPECTED_BEHAVIOR','prefix_consumed_before_suffix':True,'bytes':seen,'elapsed_ms':(time.monotonic()-start)*1000,'workloads_rerun':0,'provider_turns':0},f,indent=2);f.write('\n')
print('forced between-read fragmentation PASS; no workload rerun')
