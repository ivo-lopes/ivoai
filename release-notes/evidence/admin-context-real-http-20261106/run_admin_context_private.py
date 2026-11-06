import hashlib,json,os,pathlib,secrets,subprocess,time
root=pathlib.Path(__file__).parent
oldroot=pathlib.Path('/var/tmp/ivoai-context-private-ingest-evidence-20261106')
binaries=pathlib.Path('/media/ivo/ARQUIVOS/ivoai-validation-private-20261106/context-admin-http-20261106/binaries')
os.umask(0o077)
epoch=root/('stack-'+secrets.token_hex(4));epoch.mkdir(mode=0o700)
droot=pathlib.Path('/var/tmp/ivoai-consolidation-tools-20261104/docker-private')
state=json.loads((root/'daemon-recovery.json').read_text())
env={'PATH':'/usr/bin:/bin','HOME':str(droot/'home'),'DOCKER_HOST':'unix://'+str(droot/'run/docker.sock'),'DOCKER_CONFIG':str(droot/'client')}
def identity():
 assert int(pathlib.Path(f"/proc/{state['pid']}/stat").read_text().split(') ',1)[1].split()[19])==int(state['start_ticks'])
def docker(args,timeout=180):
 identity()
 proc=subprocess.Popen(['/usr/bin/docker',*args],env=env,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
 begin=time.monotonic();reserve=12.5*(1<<30)
 while proc.poll() is None:
  usage=os.statvfs('/var/tmp');free=usage.f_bavail*usage.f_frsize
  if (args[0] in ('run','exec') and free<reserve) or time.monotonic()-begin>timeout:
   import signal
   fd=os.pidfd_open(proc.pid)
   try:signal.pidfd_send_signal(fd,signal.SIGTERM)
   finally:os.close(fd)
   stdout,stderr=proc.communicate(timeout=15)
   raise RuntimeError('owned Docker client stopped: '+('12.5GiB reserve' if free<reserve else 'bounded operation deadline'))
  time.sleep(.25)
 stdout,stderr=proc.communicate()
 return subprocess.CompletedProcess(['/usr/bin/docker',*args],proc.returncode,stdout,stderr)
r=docker(['info','--format','{{.DockerRootDir}}']);assert r.returncode==0 and r.stdout.strip()==str(droot/'data')
refs={'qdrant':'qdrant/qdrant@sha256:9bfc89f9914fc14876a88c05270685921a55b8877b254a98bcc1cc2f0a424cd2','tei':'ghcr.io/huggingface/text-embeddings-inference@sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d'}
for ref in refs.values():assert docker(['image','inspect',ref]).returncode==0
nonce=secrets.token_hex(12);label='ivoai.context.private-proof='+nonce
names={'qdrant':'ivoai-context-'+nonce+'-qdrant','tei':'ivoai-context-'+nonce+'-tei'}
keys=[secrets.token_hex(32),secrets.token_hex(32)]
credential=epoch/'backends.env';credential.write_text('QDRANT__SERVICE__API_KEY='+keys[0]+'\nAPI_KEY='+keys[1]+'\nQDRANT_INIT_FILE_PATH=/qdrant/init/.qdrant-initialized\n')
model=pathlib.Path(json.loads((oldroot/'model-location.json').read_text())['path'])
volumes={}
for target in ['/qdrant/storage','/qdrant/init','/qdrant/snapshots','/etc/ivoai','/etc/systemd/system','/var/lib/ivoai','/opt/ivoai']:
 path=epoch/'mounts'/target.lstrip('/');path.mkdir(mode=0o700,parents=True);volumes[target]=path
common=['--detach','--read-only','--user','0:0','--cap-drop','ALL','--security-opt','no-new-privileges:true','--label',label,'--env-file',str(credential),'--tmpfs','/tmp:rw,nosuid,mode=1777,size=256m']
marker=epoch/'boundary.json';marker.write_text(json.dumps({'nonce':nonce,'parent_network_namespace':os.readlink('/proc/self/ns/net'),'private_disposable_fixture':True})+'\n')
mounted=['--mount','type=bind,src='+str(marker)+',dst=/ivoai-private-context-boundary.json,readonly','--mount','type=bind,src='+str(binaries)+',dst=/proof,readonly']
report={'pass':False,'private_epoch':epoch.name,'image_refs':refs,'model_revision':'614241f622f53c4eeff9890bdc4f31cfecc418b3','service_network':'unique --network none namespace shared only by owned containers','service_uid':0,'service_uid_scope':'rootless mapped-root fixture, not production UID1000 certification','cgroup_limits_applied':False,'shared_context_accessed':False,'real_tei_inference':False}
created=[];start=time.monotonic()
def clean(value):
 for key in keys:value=value.replace(key,'[REDACTED_TRANSIENT]')
 return value
def record(name,result):
 (epoch/(name+'.log')).write_text(clean(result.stdout+result.stderr));report[name+'_exit']=result.returncode
try:
 args=['run','--name',names['qdrant'],'--network','none',*common,*mounted]
 for target in ['/qdrant/storage','/qdrant/init','/qdrant/snapshots']:args+=['--mount','type=bind,src='+str(volumes[target])+',dst='+target]
 args+=[refs['qdrant']];r=docker(args);record('qdrant-start',r);assert r.returncode==0;created.append(names['qdrant'])
 args=['run','--name',names['tei'],'--network','container:'+names['qdrant'],*common,*mounted,'--mount','type=bind,src='+str(model)+',dst=/model,readonly','--tmpfs','/run/ivoai:rw,nosuid,mode=0700','--tmpfs','/data:rw,nosuid,mode=0700,size=128m','--env','HF_HUB_OFFLINE=1','--env','TRANSFORMERS_OFFLINE=1','--env','OMP_NUM_THREADS=2','--env','MKL_NUM_THREADS=2','--env','RAYON_NUM_THREADS=2','--env','LOG_LEVEL=info,text_embeddings_router=warn']
 for target in ['/etc/ivoai','/etc/systemd/system','/var/lib/ivoai','/opt/ivoai']:args+=['--mount','type=bind,src='+str(volumes[target])+',dst='+target]
 args+=[refs['tei'],'--model-id','/model','--port','8080','--dtype','float32','--tokenization-workers','2'];r=docker(args);record('tei-start',r);assert r.returncode==0;created.append(names['tei'])
 testenv=['--env','IVOAI_PRIVATE_CONTEXT_NONCE='+nonce,'--env','IVOAI_ADMIN_CONTEXT_REAL_STACK=1','--env','HOME=/tmp/ivoai-operator','--env','XDG_CONFIG_HOME=/tmp/ivoai-operator/config','--env','XDG_DATA_HOME=/tmp/ivoai-operator/data','--env','XDG_STATE_HOME=/tmp/ivoai-operator/state','--env','XDG_CACHE_HOME=/tmp/ivoai-operator/cache']
 r=docker(['exec',*testenv,names['tei'],'/proof/admin-context-http.test','-test.run','^TestAdministrativeContextRealHTTP$','-test.v','-test.timeout','4m'],260);record('real-admin-http-context',r);assert r.returncode==0
 proof=json.loads((volumes['/var/lib/ivoai']/'admin-context-http-proof.json').read_text());report['context_http_proof']=proof;assert proof['pass']
 report['real_tei_inference']=True
 report['pass']=True
except Exception as e:
 report['failure']=str(e)[:500]
finally:
 hold=bool(os.environ.get('IVOAI_ADMIN_CONTEXT_HOLD_ON_FAILURE')) and not report['pass']
 report['held_for_diagnosis']=hold
 for name in reversed(list(names.values())):
  metadata=docker(['inspect','--format','{{json .Config.Labels}}',name])
  if metadata.returncode!=0:continue
  assert json.loads(metadata.stdout).get('ivoai.context.private-proof')==nonce
  r=docker(['logs',name]);rawlog=r.stdout+r.stderr
  if name==names['tei']:
   report['tei_raw_log_contains_private_key']=any(key in rawlog for key in keys)
   report['tei_raw_log_bytes']=len(rawlog.encode())
   report['tei_raw_log_sha256']=hashlib.sha256(rawlog.encode()).hexdigest()
   report['tei_runtime_info']='TEI1.9.4 pinned image, real embedding384 required by test'
   labels=docker(['image','inspect',refs['tei'],'--format','{{json .Config.Labels}}']);report['tei_oci_labels']=json.loads(labels.stdout)
  (epoch/(name+'-native.log')).write_text(clean(rawlog))
  if hold:continue
  r=docker(['stop','--time','5',name],20);record(name+'-stop',r)
  r=docker(['rm',name],20);record(name+'-rm',r)
 if not hold:
  credential.unlink(missing_ok=True)
  for file in (volumes['/etc/ivoai']/'secrets').glob('*.env'):file.unlink()
 if report.get('tei_raw_log_contains_private_key'):
  report['pass']=False;report['failure']='private TEI raw startup log exposed a transient key'
 report['seconds']=time.monotonic()-start;(epoch/'summary.json').write_text(json.dumps(report,indent=2)+'\n');print(str(epoch));print(json.dumps(report,indent=2))
raise SystemExit(0 if report['pass'] else 1)
