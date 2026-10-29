# Disconnected subscription preparation with actual PR56/63 main

Tested source `b7e616c6a1898fec91ecc618c16ff0777fa57193`, tree `559ac264fe87e6b738d25d4527de2949cb783862`, merges PR64 head `6327d0c6fcbf16d12865e2161acda6a6f8a1a3e5` with then-current main `d6111e50b607de2bec950fbb4185609699ccab3c`. The merge had no conflicts and required no Go/script edits. This is a local integration qualification of disconnected test-only preparation, not a release or real cohort.

## Partitioned source and frozen inputs

Relative to main, only the five subscription guard/transport/opt-in test files and controlled executable fixture `.go.txt` are added. All six are byte-identical to6327. The two servercmd paths from PR63 match both its904 head and actual main; all2607 other non-release-record paths, including other Go/scripts/modules/fixtures and documentation/embed, match main. No production API interplay or new dependency was introduced by the merge.

All4068 tracked paths were frozen before any gate, explicitly including the fake executable's literal `.go.txt` input. The same hashes were verified before/after every gate and before/after the later archive-only merges. The frozen set includes600 Go files, both root module manifests,61 script paths and41 literal `.go.txt` paths. Archived source text is not an executed fixture merely because it has that extension. Symlink entries, if present, are hashed as link text rather than followed.

The1455-file historical release-note union from the two source parents remains byte-identical. Previous adverse construction, fixture and economic results were retained. No comparison executable or old source projection was overwritten.

## Focused checks on b7

Each command ran once using reviewed Go1.27.3, an explicit offline environment, a fresh private HOME per gate, GOMAXPROCS2 and ext4 TMPDIR/GOTMPDIR under `/var/tmp`. Official-client opt-ins were absent.

| Gate | Result |
|---|---|
| App `-count=1 -json`, regex `^TestPrimaryPrefetch(DirectMCP.*|Subscription.*)$` |18 top-level tests/117 passing events including subtests;0 failures/0 skips;14.074s walltime |
| Servercmd and docsserver, `-count=1 -json` |32 top-level tests/52 passing events across2 packages;0 failures/0 skips;3.608s walltime |
| `go build -trimpath ./cmd/ivoai`, CGO0/Linux/amd64v1 |PASS;2.402s walltime; private binary not executed |

The App regex selects only hermetic direct-scope/route-projection/stdio controls. It excludes `TestLiveSubscriptionStdioNoAuthQualification`; there is no new official negative or authenticated positive here. Controlled stdio tests build/run only their owned fake Go executable. The server checks include the new connector restart-failure regression using a fake service controller; docsserver uses its own temporary local HTTP listener. No deployed service was contacted.

The private Core compile artifact has SHA256 `c1bfbcd0fb6c0a2e3903e84a2b76aaeef0b89418e76705bf6cc78455d09f3f12`,76110265 bytes. Read-only Go buildinfo reports CGO0/Linux/amd64/v1, revisionb7 and `vcs.modified=false`. This is an unexecuted local compilation, not a release payload, comparative binary pair, native ARM/no-AVX runtime test or reproducible-build attestation.

The [receipt](evidence/subscription-current-main-composition/validation.json), SHA256 `9b83ab215b49b4c44fcc9f99901887c6827d08c85796064f2e6f8aa212a22303`, binds the exact sources, hashes, command environments, test logs/counts and compile metadata. No full Go suite, whole-suite race, Python, Node build or extra vet run was repeated: source partitions are unchanged from their independently checked parents, and these focused checks address the composed paths. The2195-pass full Go result on c42 and all parent CI/race outcomes remain bound to their own sources, not relabeled as results for b7 or its descendants. Root owns any later exact-head CI decision.

## Archive-only merges after testing

After all three gates completed, two local non-fast-forward merges incorporated historical integration reports:

- `1a63afde1a7b2627bc352e0261c19dca6354959a`, producing merge `0547d0f75c8a54557b46c47a86148c22b9356534`.
- `ae61cbdd43cb7994a7d71c15bbb6a40a11f7751d`, producing merge `fd792fb170150abb80164e5b11481da7e8a8241a`, tree `50364c755bd3e8090e8b3270e759d5529b426d70`.

These merges added22 release-note/evidence files only. Every original b7 path remains byte-identical; Go/scripts/modules/fixtures/documentation/embed did not change. No command was rerun afterward, and test/compile results remain attached to b7.

Those merges also make the independently qualified source commits reachable, without cherry-picking or rewriting their history. Local Git confirms the original source/actual-main tree bindings:

| Qualified source | Actual main merge | Identical tree |
|---|---|---|
| `1f58745811b9a5f3ad7d483a06e41a1f25837bc1` |PR56 `740cfa63c787acfaeb8b32e206110f0ca4e1401b` |`af940419aed1615faeaacb5968fc26c1c96f0031` |
| `e80273d981632ec32ba60f8d534619ac1cf0bf18` |PR63 `d6111e50b607de2bec950fbb4185609699ccab3c` |`2ee4f51d76503aac13b0b44ba10cdaa55d6246fc` |

The older reports retain their original statuses, checker-scope failure and temporal wording as historical records. Neither their source-specific tests nor reported exact-head CI outcomes are reassigned to this composition. This final report/evidence addition likewise does not change tested source inputs.

## Limits and next review

The direct helper, subscription route projection and transport remain UNWIRED. Synthetic acceptance and `Connected=false` do not certify subscription subtype, billing, quota, models, later exec routing or permission to bypass institutional proxy policy. Root's prior ed9 official logged-out refusal remains a separate qualification and was not repeated. No official client, authentication/config/account RPC, model, real cohort, installer, ingestion or external service ran here; no personal credential/configuration was read/copied/hashed or modified.

The v0.15.0 tag/source8f is unchanged and excludes these post-tag PR56/63/64 changes. No release/asset operation, remote push, PR merge, deployment, Memory or Plane write occurred in this task; only the explicitly described local Git merges were made. Root is composing a later cohesive PR64 head and owns its source review and CI gates.

No new economic turn, token/cache/quota comparison or gain is recorded. Comparative both-arm builds/discovery/freeze and actual equivalent AB/BA execution remain separate prerequisites. Global economics and broad client/TUI/admin parity remain incomplete; `CONTEXT_SYNC=BLOCKED`/IVOAI-175 remains unresolved. A successful connector restart or a fake-controller regression is not evidence of document ingestion/synchronization. Rollback must preserve separately delivered main changes and must not replay uncertain mutations or delete user state.
