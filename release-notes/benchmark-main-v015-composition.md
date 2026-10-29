# PR56 composition with the source tagged v0.15.0

Local source `1f58745811b9a5f3ad7d483a06e41a1f25837bc1`, tree `af940419aed1615faeaacb5968fc26c1c96f0031`, merges original PR56 head `fbba2412e186379e6e8e9fc5087f01fb5b6202c5` with actual main `8f688e26f0c4570b6a2b30c7e052672f102703f3`. The merge had no conflicts and required no Go/script edits. This report and its evidence are local finishing additions; they were not pushed and are not part of the tested source tree above.

## Source bindings

All1082 non-documentation/non-release-record paths are byte-identical tofbba. This includes every Go/script/module/fixture input, workflows and the existing disconnected direct MCP helper. All1524 README/documentation/website/embed paths are byte-identical to8f. The worktree was a clean committed tree before the check, and all3928 existing paths were frozen and checked before and after the test command. Symlink entries, if present, are hashed as link text rather than followed.

All1322 unique historical release-note records from both parents remain byte-identical, including adverse economic/CI outcomes and previous source-specific validations. No new dependency, runtime contract, authorization path, benchmark scenario or provider routing behavior was introduced. PR63's connector correction and PR64's subscription guard/stdio preparation are absent from this composition.

The local annotated tag object `00b2c533eed1bf6be33aff3e27135b3c725a5dee` peels to8f. This benchmark composition is a different source, and PR56/63/64 are excluded from that tag. This task did not query release publication state, run the Release pipeline, download assets or qualify the distributed v0.15.0 payloads; those operations remain root-owned.

## Applicable new check

Only the embedded documentation changed relative tofbba. One invocation of `go test -count=1 -json ./internal/docsserver` passed the two existing tests with0 failures/0 skips,1.129s command walltime:

- `TestEmbeddedProductionSiteAndHealth`
- `TestNetworkForAddressPreservesConfiguredFamily`

The tests served their own temporary HTTP listener and checked embedded EN/PT pages, health, security headers and address-family behavior. They did not contact a deployed administrative service. Execution used the reviewed Go1.27.3 toolchain, explicit environment, fresh private HOME, offline module settings, GOMAXPROCS2 and ext4 TMPDIR/GOTMPDIR under `/var/tmp`; official-client opt-ins were absent.

The [validation receipt](evidence/benchmark-main-v015-composition/validation.json), SHA256 `1a62c572059562853c07472d80947006ef4e05ee3473b0d0d1bdafbaef0d7a78`, binds the source tree, equality checks, historical union, exact command and complete JSON log. No full Go/Python/vet/Node suite was repeated: Go/scripts/fixtures are unchanged fromfbba, and all documentation inputs/embed are unchanged from8f. Earlier tests remain tied to their own sources rather than counted as newly executed tests here.

## Exact-head CI and possible merge decision

Root reported that CI40812925389 passed all16 jobs on exact PR56 headfbba. This task did not independently query that workflow, rerun it or attribute its16 passes to1f. Root may evaluate a merge of the original PR head using that exact-head CI, the8f documentation binding and these new composition tests, after independently comparing the predicted merge tree to `af940419aed1615faeaacb5968fc26c1c96f0031` and verifying current remote heads and release gates. A changed base/head or differing predicted tree requires a new assessment.

Adding this local report does not invalidate or replace the original head's CI: root can retain the evidence as an audit record or archive it in a later reviewed increment. No push, PR update, merge, tag, release or deployment occurred in this task.

## Limits

The direct helper remains `USER_CONFIG_INCLUDED_SHARED_SCOPE`/`direct_mcp_helper_connected=false`. This declaration is not observed provider-route admission or equivalent arm configuration. No official client, authentication/config/account RPC, model, real cohort, ingestion, credential/configuration change, installer or external service ran. Local test compilation is not a comparative executable-pair rebuild.

There is no new token/cache/quota comparison, scenario promotion or economic gain. Global economic acceptance remains INCOMPLETE, and `CONTEXT_SYNC=BLOCKED`/IVOAI-175 remains unchanged. Guarded wiring, both-arm builds/discovery/freeze and actual equivalent AB/BA execution remain separate work. This composition does not establish general client/TUI/admin parity or authenticate a subscription route.

Discarding this local branch/report changes no external state. A future rollback of the benchmark increment must retain independently delivered main documentation/runtime changes and must not delete user sessions, credentials, configuration, history or uncertain mutations.
