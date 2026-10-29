# Subscription stdio preparation composed with reviewed main61

Source `c42e12b15c8ece4f9fc9fe4cdfe97e5ccb59bf46` merges parent `7747ac86c748bbbbbddfe3457499fbef8f6f661c` (route projection, concrete test-only stdio transport and root's historical no-auth negative receipt) with benchmark PR56 head `fbba2412e186379e6e8e9fc5087f01fb5b6202c5`. The merge had no conflicts and required no Go/script edits. The existing main `ccc48a055ae3099a2027cfc8f31210c172baf247` is included through PR56. This is a local disconnected preparation, not a release or a live-cohort admission.

## Source identities and preservation

All282 existing main non-test Go files and both root module manifests are byte-identical to main. The three additional non-test Go benchmark-fixture files remain byte-identical to PR56 and are imported only by tests. No production contract, dependency, default, workflow, website/embed or existing benchmark script changed relative to PR56.

Relative to PR56, the only source additions are five `primary_prefetch_subscription*_test.go` files and `internal/app/testdata/subscription_stdio_fixture.go.txt`. The latter is controlled fake executable source, not an official client. The common direct helper/catalog paths are byte-identical to774. The two mapper/contract files are also exactb495; the three transport/opt-in files and fake fixture are exacted9. No mapper, constructor, RPC bound, cleanup oracle, diagnostic NativeRunner restriction or official recipe was weakened for this merge.

All1210 unique release-note records from both parents remain byte-identical, including construction failures, OLD controls and root's completed official negative. The source tree and file identities are recorded in the [source freeze](evidence/subscription-stdio-main61-composition/source-frozen.json); inherited record hashes are separate. This finishing step adds only this report and its new evidence directory.

## Local checks on this exact composition

The frozen661 Go/script/module paths were checked before and after each command. Commands ran once, using the existing Go1.27.3 toolchain, explicit environment, fresh private HOME per gate, offline module resolution, GOMAXPROCS2 and ext4 TMPDIR/GOTMPDIR under `/var/tmp`. All official-client opt-in variables were absent. The source was initially a clean committed merge tree.

The new fake executable's `.go.txt` source is outside the `.go` manifest filter. A separate hash check at04:09:14.675325Z verified it against the committed source, before the first controlled stdio test began at04:09:36.132337984Z, and checked it again after the gates. That supplemental hash is not presented as recorded before the entire Go invocation. Neither the fixture nor any Go/script source changed; no gate was repeated to relabel this distinction.

| Gate | Result |
|---|---|
| Full Go, `-count=1 -json ./...` |59 packages PASS;3 packages without tests;2195 passing test events including subtests,1203 top-level passes;72 explicit skips;127.500s command walltime |
| Python benchmark suites |71 tests PASS;29.351s command walltime |
| `go vet ./...` |PASS;11.067s command walltime |

The new `TestLiveSubscriptionStdioNoAuthQualification` was **SKIPPED**, as required with its opt-in absent. This is not a new successful official qualification. The other skipped integrations likewise do not count as passes. The concrete stdio unit tests compiled and ran only their owned fake Go executable to exercise framing, bounds, cancellation, process cleanup and refusal paths. A source-built test executable is not a frozen comparative client pair.

The [validation receipt](evidence/subscription-stdio-main61-composition/validation.json), SHA256 `1296724c07b21f92ca7526e5b8d4e4f5788bd1045f2684b401a14efeb75160e8`, binds the results and identities. Complete Go JSON is retained as gzip with its uncompressed digest. Existing source-only race/projection results remain their own epochs; no complete race suite, ARM execution or fresh official-client run was added here. Shared-host walltimes are not comparative latency gains.

## Official evidence and remaining admission limits

Root's one official negative remains bound to ed9 and its original reviewed628e/ec718 pair, synthetic HOME/etc/network namespace and receipt. It observed logged-out `AUTH_DENIED`, the two configured MCP declarations, zero MCP/API trap requests, no auth file and actual child Wait0/reaping. This composition preserved that record without rerunning it or attributing a new official result to c42. It certifies neither authenticated-positive routing nor that a normal account can complete a turn.

The route projection, transport and direct helper remain **UNWIRED**. `ACCEPTED_UNQUALIFIED`/`Connected=false` for synthetic inputs are not proof of subscription subtype, billing, quota, models, provider route or permission to alter institutional proxy policy. No user credentials or personal configuration were opened/copied/hashed, and no official help/status/auth/config/account call, model prompt, external provider, installer, service, new download or real cohort ran in this composition epoch.

A later equivalent experiment still needs separately reviewed ordinary authenticated read-only preflight, common safe wiring, both-arm build/discovery/freeze and actual bounded AB/BA execution. No new economic turn or token/quota saving is recorded. The126 historical backend-first turn count, matrix9 supported contracts/5 unsupported/2 blocked, adverse economic comparisons and `CONTEXT_SYNC=BLOCKED`/IVOAI-175 remain unchanged. This increment is excluded from release015.

## Proposed review stack

After root's independent review, a draft may target PR56's branch `feat/concurrent-projects-observed-driver` at the reviewedfbba base. Its scope is the six test-source inputs and their evidence, rather than duplicating PR56's lifecycle/metrics changes. No PR, push, merge, tag or release was performed by this task. Any later base/head movement requires source/merge-tree review and the appropriate exact-head hosted checks; the old failed PR56 CI is not rescored here.

Rollback before wiring is discarding/reverting the isolated test/docs increment against its reviewed base. There is no external state migration, credential/configuration change or mutation to undo, and nothing should be replayed or deleted from a user's sessions/history. Production main changes inherited through PR56 are not part of this disconnected feature's rollback.
