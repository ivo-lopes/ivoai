import hashlib,json,os,pathlib,signal,subprocess,time
os.umask(0o077)
root=pathlib.Path(__file__).parent
daemon=pathlib.Path('/var/tmp/ivoai-consolidation-tools-20261104/docker-private')
state=json.loads((root/'daemon-recovery.json').read_text())
assert int(pathlib.Path(f"/proc/{state['pid']}/stat").read_text().split(') ',1)[1].split()[19])==state['start_ticks']
env={'PATH':'/usr/sbin:/usr/bin:/sbin:/bin','HOME':str(daemon/'home'),'DOCKER_HOST':'unix://'+state['socket'],'DOCKER_CONFIG':str(daemon/'client')}
ref='ghcr.io/huggingface/text-embeddings-inference@sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d'
start=time.monotonic();samples=[];reason=None
free=lambda:os.statvfs('/var/tmp').f_bavail*os.statvfs('/var/tmp').f_frsize
assert free()>20*(1<<30),'insufficient initial private pull budget'
with (root/'reviewed-tei-pull.log').open('x') as log:
 p=subprocess.Popen(['/usr/bin/docker','pull',ref],env=env,stdin=subprocess.DEVNULL,stdout=log,stderr=subprocess.STDOUT)
 fd=os.pidfd_open(p.pid)
 try:
  while p.poll() is None:
   avail=free();elapsed=time.monotonic()-start;samples.append({'seconds':round(elapsed,3),'root_free_bytes':avail})
   if avail<16*(1<<30) or elapsed>600:
    reason='16GiB pull reserve' if avail<16*(1<<30) else '600s deadline'
    signal.pidfd_send_signal(fd,signal.SIGTERM);p.wait(timeout=15);break
   time.sleep(.5)
 finally:os.close(fd)
report={'exit_code':p.returncode,'seconds':time.monotonic()-start,'stop_reason':reason,'image_ref':ref,'minimum_free_bytes':min(x['root_free_bytes'] for x in samples),'samples':samples,'single_pull_window':True}
(root/'reviewed-tei-pull.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps({k:v for k,v in report.items() if k!='samples'}),flush=True)
raise SystemExit(0 if report['exit_code']==0 else 1)
