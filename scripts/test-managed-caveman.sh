#!/usr/bin/env bash
# Exact rebuilt archive, vulnerability scan, baseline CPU and real installer gates.
set -euo pipefail
umask 077
archive=${1:?usage: test-managed-caveman.sh ARCHIVE METADATA NEW_RECEIPT}
metadata=${2:?usage: test-managed-caveman.sh ARCHIVE METADATA NEW_RECEIPT}
receipt=${3:?usage: test-managed-caveman.sh ARCHIVE METADATA NEW_RECEIPT}
[[ "$archive" = /* && "$metadata" = /* && "$receipt" = /* ]]
[[ ! -e "$receipt" && ! -L "$receipt" ]]
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d "${TMPDIR:-/var/tmp}/ivoai-caveman-qualification.XXXXXXXX")"
stage=archive_verification
finish() {
  status=$?
  python3 - "$receipt" "$stage" "$status" "$archive" "$test_root" <<'PY_RECEIPT'
import hashlib,json,os,sys
path,stage,status,archive,private=sys.argv[1:]
h=hashlib.sha256()
fd=os.open(archive,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
with os.fdopen(fd,'rb') as source:
 info=os.fstat(source.fileno())
 if info.st_size>512<<20: raise ValueError('archive report size limit')
 for block in iter(lambda:source.read(1<<20),b''): h.update(block)
row={'scope':'reviewed_local_caveman_rebuild_not_upstream_signature','outcome':'PASS' if status=='0' else 'FAIL',
     'last_stage':stage,'provider_inference':False,'archive_sha256':h.hexdigest()}
diagnostic=os.path.join(private,'installer.jsonl')
if status!='0' and os.path.isfile(diagnostic):
 outpath=path+'.installer-private.jsonl'
 source=os.open(diagnostic,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
 with os.fdopen(source,'rb') as stream:
  first=stream.read(32768);stream.seek(max(32768,os.fstat(stream.fileno()).st_size-32768))
  raw=first+b'\n[bounded diagnostic tail]\n'+stream.read(32768)
 outfd=os.open(outpath,os.O_CREAT|os.O_EXCL|os.O_WRONLY|os.O_NOFOLLOW,0o600)
 with os.fdopen(outfd,'wb') as out:out.write(raw)
 row['bounded_private_installer_diagnostic_retained']=True
fd=os.open(path,os.O_CREAT|os.O_EXCL|os.O_WRONLY|os.O_NOFOLLOW,0o600)
with os.fdopen(fd,'w') as out: json.dump(row,out,indent=2);out.write('\n')
PY_RECEIPT
  rm -rf -- "$test_root"
}
trap finish EXIT
python3 - "$repo_root" "$archive" "$metadata" "$test_root" <<'PY_ARCHIVE'
import importlib.util,json,pathlib,sys,os,stat
root,archive,metadata,private=map(pathlib.Path,sys.argv[1:])
spec=importlib.util.spec_from_file_location('release_gate',root/'scripts/verify-release-artifacts.py')
v=importlib.util.module_from_spec(spec);spec.loader.exec_module(v)
v.regular(metadata,65536)
raw=metadata.read_bytes()
doc=v.unique_json(raw.decode())
arch=doc['platform'].removeprefix('linux/')
v.require(arch in ('amd64','arm64'),'unreviewed Caveman platform')
v.require(archive.name==doc['archive'] and v.digest(archive)==doc['sha256'],'Caveman archive digest mismatch')
v.component_metadata(metadata.parent,'caveman',arch,{archive.name:v.digest(archive)},
                     {'caveman':'5f95e80d41d5744f16e09d534b1d56585fe746f9d5aed1e14c2586bbe953810d'})
payload=private/'sealed';payload.mkdir(mode=0o700)
fd=os.open(archive,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
with os.fdopen(fd,'rb') as source,(payload/archive.name).open('xb') as target:
    info=os.fstat(source.fileno())
    v.require(stat.S_ISREG(info.st_mode) and 0<info.st_size<=256<<20,'unsafe Caveman archive copy')
    remaining=info.st_size
    while remaining:
        block=source.read(min(1<<20,remaining))
        v.require(block,'Caveman archive truncated during sealing')
        target.write(block);remaining-=len(block)
    v.require(not source.read(1),'Caveman archive grew during sealing')
v.require(v.digest(payload/archive.name)==doc['sha256'],'sealed archive digest mismatch')
(payload/metadata.name).write_bytes(raw)
v.scan_caveman(payload/archive.name,arch)
PY_ARCHIVE
# The private verified copy is the only archive extracted or executed.
archive="$test_root/sealed/$(basename -- "$archive")"
tar -xzf "$archive" -C "$test_root"
stage=binary_vulnerability_scan
mkdir "$test_root/bin" "$test_root/home"
GOBIN="$test_root/bin" go install golang.org/x/vuln/cmd/govulncheck@v1.7.0
for binary in caveman-proxy caveman-mcp; do
  # Text mode has a failing exit status for findings; JSON mode does not.
  "$test_root/bin/govulncheck" -mode=binary "$test_root/$binary"
done
stage=baseline_cpu_startup
case "$(uname -m)" in
  x86_64) emulator=("$(command -v qemu-x86_64)" -cpu "Nehalem,-avx,-avx2") ;;
  aarch64) emulator=() ;;
  *) echo 'Native Linux amd64/arm64 required' >&2; exit 1 ;;
esac
for binary in caveman-proxy caveman-mcp; do
  env -i HOME="$test_root/home" PATH=/usr/bin:/bin \
    timeout 60 "${emulator[@]}" "$test_root/$binary" version --json > "$test_root/$binary-version.json"
  python3 - "$test_root/$binary-version.json" <<'PY_VERSION'
import json,sys
doc=json.load(open(sys.argv[1]))
assert doc['version']=='bin-v4.0.2-ivoai.1'
PY_VERSION
done
stage=real_installer_and_fidelity_canary
cd -- "$repo_root"
IVOAI_TEST_CAVEMAN_ARCHIVE="$archive" \
  go test -json ./internal/components -run '^TestLiveManagedCavemanInstallation$' -count=1 -timeout=3m > "$test_root/installer.jsonl"
python3 - "$test_root/installer.jsonl" <<'PY_INSTALLER'
import json,sys
events=[json.loads(line) for line in open(sys.argv[1])]
name='TestLiveManagedCavemanInstallation'
assert any(e.get('Test')==name and e['Action']=='run' for e in events)
assert sum(e.get('Test')==name and e['Action']=='pass' for e in events)==1
assert not any(e['Action'] in ('fail','skip') for e in events)
PY_INSTALLER
stage=completed
