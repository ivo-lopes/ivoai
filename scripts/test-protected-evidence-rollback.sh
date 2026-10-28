#!/usr/bin/env bash
# No fetch/download: workflows use full history and warm the pinned Go modules
# in their preceding full-Go gate. Missing history/cache is FAIL, never SKIP.
set -euo pipefail
umask 077
if (( $# != 0 )); then
  echo 'Usage: scripts/test-protected-evidence-rollback.sh (no arguments)' >&2
  exit 2
fi
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
proof_root=$(mktemp -d "${TMPDIR:-/tmp}/ivoai-evidence-rollback.XXXXXXXX")
readonly repo_root proof_root
legacy_revision=3be82befea6ed3875373afcb1bfaa450710310ac
readonly legacy_revision
stage=source-provenance
finish() {
  local status=$?
  trap - EXIT
  if (( status != 0 )); then
    printf 'PROTECTED_EVIDENCE_ROLLBACK_GATE=FAIL stage=%s exit=%d\n' "$stage" "$status" >&2
  fi
  printf 'Protected evidence rollback proof: %s\n' "$proof_root" >&2
  exit "$status"
}
trap finish EXIT
export GIT_NO_REPLACE_OBJECTS=1 GIT_NO_LAZY_FETCH=1
if [[ "$(git -C "$repo_root" rev-parse --verify "${legacy_revision}^{commit}")" != "$legacy_revision" ]] ||
  ! git -C "$repo_root" merge-base --is-ancestor "$legacy_revision" HEAD; then
  echo 'Pinned legacy ancestor unavailable: full local Git history is required; no fetch fallback.' >&2
  exit 1
fi
python3 - "$repo_root" "$proof_root" "$legacy_revision" <<'PY'
from pathlib import Path, PurePosixPath
import hashlib,json,subprocess,sys
repo,root,revision=sys.argv[1:]
target=Path(root)/'source'
entries=subprocess.check_output(['git','-C',repo,'ls-tree','-r','-z',revision,'internal','go.mod','go.sum']).split(b'\0')
manifest=[]
for entry in entries:
    if not entry:
        continue
    identity,name=entry.split(b'\t',1)
    mode,kind,blob=identity.decode().split()
    p=name.decode()
    parts=PurePosixPath(p)
    if kind!='blob' or mode not in ('100644','100755') or parts.is_absolute() or '..' in parts.parts:
        raise SystemExit('Pinned source contains an unsupported path or file type')
    if p.startswith('internal/docsserver/site/'):
        continue
    out=target/p
    out.parent.mkdir(parents=True,exist_ok=True)
    data=subprocess.check_output(['git','-C',repo,'cat-file','blob',blob])
    out.write_bytes(data)
    manifest.append({'path':p,'git_blob':blob,'sha256':hashlib.sha256(data).hexdigest()})
pins={
    'go.mod':'d1c486bcfb3a5e1127b4f47de7e62001c768ee744c42b6f96627f6895d026705',
    'go.sum':'44f145c4f9477395f29442a17757c7de7b24df282e19c7e6ffddb58662e2553c',
}
for name,digest in pins.items():
    if hashlib.sha256((target/name).read_bytes()).hexdigest()!=digest:
        raise SystemExit('Pinned legacy module manifest integrity mismatch: '+name)
(Path(root)/'SOURCE_MANIFEST.json').write_text(json.dumps({'revision':revision,'files':manifest},indent=2)+'\n')
(Path(root)/'SOURCE_REVISION').write_text(revision+'\n')
PY
cat > "$proof_root/source/internal/orchestrator/protected_namespace_probe_test.go" <<'GO'
package orchestrator
import (
 "context"
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "github.com/ivo-lopes/ivoai/internal/workingcontext"
)
func TestLegacyProtectedNamespaceReaderProbe(t *testing.T) {
 var f struct { Root,Session,Legacy,Protected string }
 raw,err:=os.ReadFile(os.Getenv("IVOAI_LEGACY_ARTIFACT_FIXTURE"));if err!=nil{t.Fatal(err)}
 if json.Unmarshal(raw,&f)!=nil{t.Fatal("invalid disposable fixture")}
 store,err:=workingcontext.NewLocalStore(f.Root,workingcontext.LocalOptions{});if err!=nil{t.Fatal(err)}
 s:=&Server{SessionID:f.Session,WorkingContext:store}
 if _,err:=s.artifactRead(context.Background(),toolRequest(map[string]any{"artifact_id":f.Protected}));err==nil{t.Fatal("old full reader accepted protected ID")}
 if _,err:=s.artifactReadRange(context.Background(),toolRequest(map[string]any{"artifact_id":f.Protected,"offset":0,"length":4}));err==nil{t.Fatal("old range reader accepted protected ID")}
 if _,err:=s.artifactRead(context.Background(),toolRequest(map[string]any{"artifact_id":f.Legacy}));err!=nil{t.Fatal("legacy reader poisoned",err)}
 owner:=workingcontext.Ownership{SessionID:f.Session}
 put:=workingcontext.PutRequest{Kind:workingcontext.ArtifactWorkerOutput,Owner:owner,MediaType:"text/plain",Sensitivity:workingcontext.SensitivityInternal}
 ref,err:=store.Put(context.Background(),put,strings.NewReader("old Core can still write"));if err!=nil{t.Fatal("old inventory poisoned",err)}
 reader,_,err:=store.Read(context.Background(),owner,ref.ID);if err!=nil{t.Fatal(err)};reader.Close()
 if _,err:=store.GC(context.Background());err!=nil{t.Fatal("old GC poisoned",err)}
 if _,err:=os.Stat(filepath.Join(f.Root,"objects",f.Protected,"payload"));err!=nil{t.Fatal("old Core removed protected evidence",err)}
}
GO
sha256sum "$proof_root/source/internal/orchestrator/protected_namespace_probe_test.go" > "$proof_root/PROBE_SOURCE.sha256"
offline_go=(env GOTOOLCHAIN=local GOENV=off GOWORK=off GOFLAGS=-mod=readonly
  GOPROXY=off GOSUMDB=off GONOPROXY=none GONOSUMDB=none 'GOPRIVATE=' 'GOVCS=*:off')
readonly -a offline_go
stage=offline-module-cache
for source_root in "$proof_root/source" "$repo_root"; do
  (
    cd -- "$source_root"
    "${offline_go[@]}" go mod verify
  ) >> "$proof_root/module-verify.log" 2>&1 || {
    echo 'Pinned Go module cache unavailable or altered; preceding full-Go gate must warm verified dependencies. No online fallback.' >&2
    exit 1
  }
done
stage=legacy-build
(
  cd -- "$proof_root/source"
  sha256sum go.mod go.sum > "$proof_root/legacy-modules.sha256"
  "${offline_go[@]}" go test -trimpath -c -o "$proof_root/legacy-core.test" ./internal/orchestrator > "$proof_root/build.log" 2>&1
  sha256sum --check "$proof_root/legacy-modules.sha256" > "$proof_root/module-integrity.log"
)
sha256sum "$proof_root/legacy-core.test"
cd -- "$repo_root"
stage=current-contract
sha256sum go.mod go.sum > "$proof_root/current-modules.sha256"
"${offline_go[@]}" IVOAI_LEGACY_ARTIFACT_PROBE="$proof_root/legacy-core.test" \
  go test ./internal/workingcontext -run '^TestLiveProtectedArtifactsDoNotPoisonLegacyCoreRollback$' -count=1 -json > "$proof_root/current.jsonl" 2>&1
sha256sum --check "$proof_root/current-modules.sha256" >> "$proof_root/module-integrity.log"
stage=required-proof-validation
python3 "$repo_root/scripts/validate-protected-evidence-rollback.py" "$proof_root/current.jsonl"
cat "$proof_root/current.jsonl"
