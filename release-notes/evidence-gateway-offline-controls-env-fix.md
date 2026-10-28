# Offline Gateway control Git environment correction — IVOAI-138 / IVOAI-198

The initial `ad42d70d` test fixture inherited `GIT_CONFIG_PARAMETERS`. Clearing
config count/global/system configuration did not remove that channel. Its local
commit explicitly disabled hooks, but `git worktree add` did not. A safe
reproduction against the frozen original binary directed hooksPath to a private
post-checkout hook that appended one constant marker line per arm. The old test
exited 0 while the marker received two lines. **This was an adverse fixture
result, not a production vulnerability claim or valid hostile-environment
isolation proof.** The original v1 freeze, receipt and historical 126 provider
attempts remain unchanged; the reproduction has its own receipt.

The fixture now rejects inherited `GIT_CONFIG_PARAMETERS`, `GIT_CONFIG` and
`GIT_EXEC_PATH`, in addition to the existing repository/index/object-store
redirections, before creating a repository. All Git operations in this control,
including initialization through the shared corpus helper, use `/usr/bin/git`
with an explicit small environment and command overrides disabling hooks,
fsmonitor, untracked cache and commit signing. Caller config-parameter/key/value,
executable-path, editor, pager, SSH/askpass, external-diff and trace variables
never enter those subprocess environments. Commands are fixed local
init/add/commit/worktree operations; no editor, diff or network operation is
requested. Private HOME/templates, disabled global/system config and noninteractive
behavior remain. CodeIntel Inventory already used its own explicit environment
and needs no production change.

The shared corpus helper gained a small Git-runner callback so this control can
reuse the same repository bytes. Existing fixture callers retain their prior
behavior. The containing file is the embedded common harness, so its SHA changes.
Future comparisons must rebuild the same final harness on both runtime arms and
run discovery again; old binaries are not relabeled as compatible. No historical
harness identity or result is updated. This change does not certify every
pre-existing fixture, alter user configuration or establish a general host sandbox.

An executable Go regression test launches three bounded children of the test
binary, injecting only private nonexistent fixture paths. Each must reject
PARAMETERS, CONFIG or EXEC_PATH before the OFF subtest can start. Separately, the
final immutable binary rejected the original controlled-hook injection with
exit 1 before either arm; the two-line marker stayed unchanged. An earlier
post-fix pilot also ran real local Git with injected trace and program-selection
variables: no trace file or marker change appeared. That pilot did not request
or certify execution of unused editor/SSH/diff clients. Existing-output and
nonprivate-output rejection controls also passed.

The corrected condition is `offline-app-gateway-v2`; receipt schema remains 1.
[Final validation](evidence/backend-first/gateway-offline-controls-v2/validation.json),
[freeze](evidence/backend-first/gateway-offline-controls-v2/freeze.json),
[final receipt](evidence/backend-first/gateway-offline-controls-v2/receipt.json)
and [adverse reproduction](evidence/backend-first/gateway-offline-controls-v2/before-fix-validation.json)
are retained separately. A post-fix pilot freeze/receipts are preserved under
`pilot/`; the final source adds the executable rejection regression. No epoch
was overwritten, selected for favorable timing, rescored or aggregated.

The final clean OFF/ON receipt again observed five versus three CodeIntel direct
inspections, zero reported CodeIntel cache hits in both arms, and five Gateway
retrievals/two Gateway hits only in ON. All ten positive reads recovered exact
owner-bound ArtifactStore bytes; both revoked-primary calls were denied before
retrieval. The source and executed binary hashes were checked after export.
**ON was slower in all five final positive reads**, including cold 18.430 ms
versus 12.499 ms and warm 14.629 ms versus 12.211 ms. The clean pilot was slower
in four of five and the filtered-environment pilot in five of five. These are
shared-host development diagnostics, not ABBA repetitions or causal timing
estimates. No token, quota or end-to-end economy result is asserted.

The final Gateway race suite passed six top-level tests (2.662 s), including the
three rejection subcases. Its generic export test remains NOT_RUN; the frozen
binary's explicit export passed separately. The shared-fixture primary/prefetch
race regression passed eight top-level tests (1.329 s), with two unrelated opt-ins
explicitly NOT_RUN. All 42 Python catalog/driver tests passed (9.481 s); App vet
passed. Final Linux AMD64 v1 was compiled and
executed; final ARM64 was cross-compiled, not executed. No production dependency,
LLM/native-client/Graft invocation, actual worker, protected public Core/MCP G0
reader proof, READY promotion or sampler calibration is included. The economic
gate remains INCOMPLETE and CONTEXT_SYNC remains BLOCKED under IVOAI-175.

Run the corrected default controls with:

```sh
go test -race ./internal/app -run 'Test.*Gateway' -count=1 -v
```

Use the original report's explicit-export procedure with a **new private output
path and a newly frozen binary**. Do not replay inherited caller configuration
or reuse a v1/pilot receipt. Removing this test-only control and its helper is the
rollback; no persistent migration or personal configuration restoration is
required. Reverting only this correction would restore the known fixture gap.
