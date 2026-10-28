# Native-provider isolation: reviewed Core integration

PR45 retains its original `2c467521` history and receipts. The local merge
`5d4092d04a2c0d8f6831739f9cba6f3119033f21` incorporates reviewed snapshot
`c182a954accbc69ca3dee25356af7a6b14ccb568`, including protected Evidence recovery,
strict Memory response snapshots, the classification fix and publication docs.
This remains an incremental native-provider filter/default-setup change with
explicit deprecated legacy paths. It does not complete IVOAI-194/195/196.

The [merge proof](evidence/native-provider-independence/integration-c182-v1/go-merge-proof.json)
checks 25 native-only and 43 incoming-only Go files byte-for-byte. The two
overlaps, App session assembly and the orchestrator server, match an independent
three-way merge. Both native-provider restrictions and protected recovery guards
remain. No authored source conflict required manual changes. Generated-site and
marker conflicts were resolved by rebuilding EN/PT with this worktree's own
frozen modules. The reviewed `.3` patch bytes were preserved, including literal
blank context lines reported by a broad historical whitespace check; the actual
PR45 delta against the incoming snapshot passes `git diff --check`.

## Validation epochs

The [new receipt](evidence/native-provider-independence/integration-c182-v1/validation.json)
keeps two distinct epochs:

- **Merge `5d4092d0`:** full Go 59 packages passed, three no-test packages recorded
  separately; race passed 13 affected packages; full vet and 65 Python tests
  passed. Own EN/PT build/check and CLI-reference checks passed. Real protected
  legacy/current rollback passed its one named test with zero skips. Linux
  AMD64v1 and ARM64v8 cross-builds passed with CGO0 and clean exact-source VCS
  metadata. Cross-compilation is not native/no-AVX execution.
- **Boundary `73f678f8`:** after all preceding checks completed, a regression test
  and four authored documentation files clarified the presentation contract.
  Product Go code remained unchanged. The complete façade package passed race,
  including the new test, followed by its vet, own EN/PT rebuild/check, CLI and
  formatting checks. The earlier full-Go/cross results retain their `5d` scope;
  the final test/docs/embed composition requires fresh hosted CI.

Opt-in skips are listed and are not successful live integrations. No real
provider/model authentication or inference, personal configuration/history
mutation, or production operation was performed. Earlier failure/adverse data
and the original `2c` report/receipt remain unchanged.

## Retained presentation and Core boundaries

Initial `ivoai codex --planner claude` and native AUTO session handoff to Claude
are refused before opening a different presentation or lease. The shared Core
retains official Claude headless/worker contracts and existing grant/routing
decisions. Its native Codex façade resolves its default to a strong Codex model,
exposes Codex models and rejects a forged Claude selection even when Claude is
present in the shared catalog. The new regression includes that shared-catalog
control and a valid Codex admission control. Internal alternate-route callbacks
do not certify a reachable Claude-primary path through native Codex presentation.
No native Claude TUI or Codex-presentation/Claude-primary parity is asserted.

The nil-quota compatibility behavior is unchanged. The sole production full
orchestrator-server constructor assigns a nonnil scoped manager and no OpenCode
adapter for native sessions. The other nil-quota constructor is a protected
artifact-reader projection with no delegate tools or executor adapter. This
inspection found no public native/nil-quota/live-OpenCode dispatch path; it does
not introduce a deny-all rule into trusted host/fixture contracts.

Updater snapshot/rollback/Doctor and release assets still retain legacy
OpenCode. No global removal, new client activation, Antigravity integration,
Context ingestion or measured economic saving is delivered here. The published
v0.14.0 tag/assets/body remain unchanged. PR51 must merge, and the final PR45
source still requires review and its fresh 16 hosted checks before merge. A
later main merge can first compare its tree with the incorporated snapshot.
