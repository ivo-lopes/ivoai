#!/usr/bin/env bash
# No downloads: compile the actual legacy Core readers from an immutable local
# Git object, then test the new store against that old executable contract.
set -euo pipefail
umask 077
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
proof_root=$(mktemp -d "${TMPDIR:-/tmp}/ivoai-evidence-rollback.XXXXXXXX")
readonly repo_root proof_root
legacy_revision=3be82befea6ed3875373afcb1bfaa450710310ac
readonly legacy_revision
trap 'printf "Protected evidence rollback proof: %s\n" "$proof_root" >&2' EXIT
git -C "$repo_root" cat-file -e "${legacy_revision}^{commit}"
python3 - "$repo_root" "$proof_root" "$legacy_revision" <<'PY'
from pathlib import Path
import subprocess,sys
repo,root,revision=sys.argv[1:]
target=Path(root)/'source'
paths=subprocess.check_output(['git','-C',repo,'ls-tree','-r','--name-only',revision,'internal','go.mod','go.sum']).decode().splitlines()
for p in paths:
    if p.startswith('internal/docsserver/site/'):
        continue
    out=target/p
    out.parent.mkdir(parents=True,exist_ok=True)
    out.write_bytes(subprocess.check_output(['git','-C',repo,'show',revision+':'+p]))
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
(
  cd -- "$proof_root/source"
  GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -c -o "$proof_root/legacy-core.test" ./internal/orchestrator > "$proof_root/build.log" 2>&1
)
sha256sum "$proof_root/legacy-core.test"
cd -- "$repo_root"
IVOAI_LEGACY_ARTIFACT_PROBE="$proof_root/legacy-core.test" \
  go test ./internal/workingcontext -run '^TestLiveProtectedArtifactsDoNotPoisonLegacyCoreRollback$' -count=1 -v > "$proof_root/current.log" 2>&1
grep -qF -- '--- PASS: TestLiveProtectedArtifactsDoNotPoisonLegacyCoreRollback (' "$proof_root/current.log"
if grep -qF -- '--- SKIP:' "$proof_root/current.log"; then
  echo 'Required rollback proof skipped' >&2
  exit 1
fi
cat "$proof_root/current.log"
