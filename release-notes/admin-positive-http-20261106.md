# Positive administrative HTTP certification — 6 November 2026

Base: `f441b81abbbd9cd24f87651837a98f89a40e06ac`. This adds tests, a minimal
historical skill fixture and new evidence; it changes no production code, main,
tag, release workflow, existing matrix results or personal configuration.

Three previously unavailable administrative operations now have positive
HTTP-to-CLI-to-Core evidence. This reduces the identified certification gaps from
21 to **18**; it does not make IVOAI-189 complete or claim107 successful operations.
The original generic matrix still accurately reports unavailable preconditions
in its own fixtures.

## Executed operations

All three cases use an actual TCP loopback HTTP server and the existing admin
handler, real access store/PKCE authorization, management scope, CSRF, exact
preview confirmation, production CLI parser and actual Core services. No dispatch
or command runner returns a fabricated successful result. Every repeated request
returns409 with no additional dispatch, after both successful and failed actions.

- **`capability.rollback`**: installs the actual earlier Ponytail release-owned
  snapshot at revision `356918eba965ee1eac64bd3a7f0dd02108350de5`, updates through
  administrative HTTP to the current `9cc65d03aa2da1db7121b912d03596409ee340b8`, then
  rolls back through HTTP. The active immutable object and Registry must agree
  with the prior revision. Both installation and rollback use the existing
  supply-chain/skillupdate pipeline and NativeClassifier. There is no network or
  provider process. The MIT license and exact historical files are included;
  fixture provenance binds IVOAI commit `f68bc0ad4fba7a84547f861231e8e5c7c0dc38d6`,
  original catalog hash, selected overlay and file hashes. These are historical
  reviewed bytes, not invented revision identifiers.
- **`doctor.repair-hooks`**: verifies official ai-memory3.4.2 binary and archive
  hashes, installs their real hook assets and runs the official configuration
  renderer under private HOME/CODEX_HOME/CLAUDE_CONFIG_DIR. After deleting an
  owned wrapper, HTTP repair restores its exact bytes/permissions. A foreign
  registration remains byte-exact; modifying an owned wrapper is refused rather
  than overwritten. `IVOAI_TEST_MODE` is unset. No lifecycle hook or personal
  settings are executed/changed; ai-memory data is explicitly private too.
- **`update.dry-run`**: uses the original verified0.16.0 executable and the actual
  reviewed private0.17.0 candidate/archive supplied by the release coordinator.
  A loopback release-protocol mirror serves those exact bytes. Checksum,
  extraction, real candidate `_update-metadata` execution, compatibility and
  snapshot preflight run normally. Installed binary, config, state and ownership
  remain byte-exact; staged candidates are removed. A corrupted checksum is
  refused, preserving those same files.

The dry-run uses the **existing guarded TEST_MODE executable selector**, because
the test process itself is a Go test executable. It also selects an empty private
server root. No checksum, real command, compatibility or snapshot check is mocked.
The mirror checksum derives from an independently pinned private archive; this
is not GitHub availability, publisher signature or certification of the final
published tag assets. The candidate comes from source `7bce1090ea1eb971326f641460fea4fdee8e6b7b`.

## Results and reproduction

The [new validation index](evidence/admin-positive-http-20261106/validation.json)
records three positive cases/zero skips, controls PASS2.134s, final focused race
PASS18.068s and applicable vet PASS. Its source/runtime/evidence hashes preserve
the exact scope. The final tests are in `internal/admin/positive_operations_test.go`.

```sh
# Paths must identify the already reviewed binaries/archive recorded in the index.
export IVOAI_ADMIN_REAL_MEMORY_BINARY=/reviewed/memory-v3.4.2/x86_64/ai-memory
export IVOAI_ADMIN_REAL_V016_BINARY=/reviewed/original-v0.16.0/ivoai
export IVOAI_ADMIN_REAL_CANDIDATE=/reviewed/private-v0.17.0/ivoai
export IVOAI_ADMIN_REAL_CANDIDATE_ASSETS=/reviewed/private-v0.17.0/assets
GOMAXPROCS=2 go test -race ./internal/admin \
  -run '^TestAdministrativePositive' -count=1 -v
GOMAXPROCS=2 go vet ./internal/admin
```

The official Memory archive must be alongside the extracted architecture directory
as `ai-memory-linux-x86_64.tar.gz`. The two binary-dependent cases are explicit
opt-ins: missing paths skip, and wrong/missing configured artifacts fail. Those
skips are never runtime certification. The captured explicit run provided every
artifact and skipped no case. Capability rollback runs without external artifacts.

Earlier failures remain separately recorded: missing hook assets, a missing
private-bundle checksum file and rejection of the unpermitted test executable
selector. These were fixture/setup problems; no production guard or test threshold
was weakened. The final isolation hardening explicitly assigns a temporary Memory
data directory and repeats the focused race check. No historic proof is replaced.

Go1.27.3 used GOMAXPROCS2/GOMEMLIMIT768MiB with a private cache below the3GiB
budget. Root free space remained above12GiB (approximately25GB at completion).
No broad suite, npm build, external download, inference pipeline, systemd action, push, PR,
production mutation, Plane or memory update was performed.

## Remaining boundaries

Scope adoption/restoration still need an eligible independently authenticated
native conversation; this increment fabricates no account callback or provider
thread and does not modify frozen inference records. Positive Core rollback would
run full setup/runtime reconciliation and remains outside this bounded fixture.
The remaining systemd, remote-server and privileged service preconditions retain
their documented limits. IVOAI-189 stays In Progress until its remaining applicable
operational acceptance evidence is established.
