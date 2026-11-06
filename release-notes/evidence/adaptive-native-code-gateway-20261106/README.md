# Native read-only Gateway evidence — 2026-11-06

These receipts certify the scoped opt-in behavior described in
[the release note](../../adaptive-native-code-gateway.md), not a global rollout
or economic improvement. No provider-fake test is counted as official inference.
No raw model prompts, answers, personal configuration or authentication material
are included. `sha256.json` seals the retained receipts and logs.

| Proof | Frozen source | Result |
| --- | --- | --- |
| Public production `ivoai codex`, native TUI, three RO turns, identical-query hit, explicit source edit invalidation, normal `/exit` | `3864613c46318efbd13a1b59d1e5198c4b076866` | PASS, 54.805s |
| Official managed bridge, live exact protected recovery on cache hit, owner receipt revocation, normal native completion, denied final response/recovery/duplicate | `c33d4afbe70b2e9571b62df7ecf3453b6958400e` | PASS, 30.916s |

Both proofs use the reviewed Linux amd64 official Codex 0.166.0 pair:

- executable SHA-256 `b8a11fedf24b2dfb59a70c9e52b030f9a508dcd49cd2dd0d41956f4c785156b8`;
- companion SHA-256 `657f4e902726e05867c937b583049d5997e057f8cd0ab40450633c9c9ef8d630`.

The public proof production IVOAI executable is
`cc3bc0571a0dd7e60e2f3084cb0d9e0ce428285359ad73b071eda9ef68177159`;
its test executable is
`56e9056e84b829d5e46f68d7fa927ce28d6b9b39291b1d7e15ddf36cfcd56ac2`.
The revocation test executable is
`43863c2f341d4fb441f0f484b16dfa0389d4914de321f23014637f22fb54a624`.
The production sources are identical between these two commits. Frozen
inventories include Go/module/test fixture files; both were checked again after
their real inference without mismatches. The test-only commit cannot retroactively
change the public proof's tested source.

Reproduction uses the existing opt-in integration tests and a newly built
production binary from the frozen source, with reviewed executable paths and
fresh private receipt paths. Keep the user's existing HOME/CODEX_HOME and
authentication; do not copy credentials or configure a paid API fallback.

```sh
IVOAI_ADAPTIVE_FULL_CLI_LIVE=1 \
IVOAI_ADAPTIVE_FULL_CLI_GATEWAY_LIVE=1 \
IVOAI_ADAPTIVE_FULL_CLI_BINARY=/absolute/reviewed/ivoai \
IVOAI_LIVE_CODEX_PATH=/absolute/reviewed/codex \
IVOAI_ADAPTIVE_FULL_CLI_REPORT=/private/new-public-receipt.json \
./app.test -test.run '^TestLiveAdaptivePrimaryFullCLI$' -test.count=1 -test.timeout=7m -test.v

IVOAI_ADAPTIVE_CODE_GATEWAY_REVOKE_LIVE=1 \
IVOAI_LIVE_CODEX_PATH=/absolute/reviewed/codex \
IVOAI_ADAPTIVE_CODE_GATEWAY_REVOKE_REPORT=/private/new-revocation-receipt.json \
./revoke-app.test -test.run '^TestLiveAdaptiveCodeGatewayCachedReceiptRevocation$' -test.count=1 -test.timeout=7m -test.v
```

Tests run from `internal/app`. The live tests perform existing fresh official
admission and use private fixture repositories/Core paths. The wrapper/TUI proof
is explicitly distinct from the real bridge revocation proof. The revoked
receipt test did not cancel the caller to trigger denial, and did not simulate
credential loss.

Offline validation on the same implementation:

- complete affected App/CodeIntel/Evidence packages: PASS (23.917s/19.149s/5.853s);
- focused race tests for the new App guards and native MCP dispatch, plus Evidence
  package: PASS (5.361s/1.101s/46.154s);
- exact native schema and typed payload equivalence, original-owner protected
  recovery, generation invalidation, in-flight revocation and concurrent loader
  deduplication are executable assertions, not provider usage observations;
- opt-in tests skipped in ordinary package checks are **NOT_RUN**, not additional
  real inference passes. Only the two explicit receipts above establish live
  results for this increment.

Retained negative iterations were necessary implementation diagnostics, and are
not hidden or attributed to provider failures: the initial fixture lacked the
real bridge SingleUserMessage observation; epoch2 lacked private Git metadata;
epoch3 used an absent relative DataDir; epoch4 used unsupported Gateway source
`code.mcp` (the concurrent fixture then timed out before a loader could enter);
epoch5 used an unsupported observation phase. Fixes use the existing observation,
private Git/DataDir, allowed source `code` plus a query digest domain, and existing
event contracts. Epoch6 and subsequent race/full checks passed. Compiler-only
logs do not establish live behavior.

Three public turns produced three Gateway resolves with one hit and two provider
inspections. Two revocation turns produced two resolves with one hit and one
inspection. This measures eliminated duplicate retrieval, not reduced model
context: every reply preserves the original typed payload. Official reported
input/output/cache tokens are retained per invocation, with no estimates for
missing values or inferred billing. No equivalent-condition OFF/ON comparison
is made here. Graft inference, cross-process cache persistence, worker sharing,
newer runtime certification and the seven outstanding economic matrix scenarios
remain separate. The historical 54-attempt OFF/ON result is unchanged; economic
targets are not claimed achieved.
