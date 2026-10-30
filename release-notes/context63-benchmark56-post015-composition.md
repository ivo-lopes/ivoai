# Post-v0.15.0 Context63 / benchmark56 source composition

This is a local integration qualification, not a release or a new economic cohort.
The immutable v0.15.0 tag/source `8f688e26f0c4570b6a2b30c7e052672f102703f3`
is unchanged and does not include PR63.

The isolated branch merges PR63 head `9049947a00e84c4442a4bb1ce8084d8b4630a732`
with the existing PR56/main-v0.15 composition `1f58745811b9a5f3ad7d483a06e41a1f25837bc1`.
The conflict-free source merge is `e80273d981632ec32ba60f8d534619ac1cf0bf18`,
tree `2ee4f51d76503aac13b0b44ba10cdaa55d6246fc`. No Go or script source was edited.

## Source and retained evidence

The pre-execution [freeze](evidence/context63-benchmark56-post015-composition/source-freeze.json)
binds all3,948 tracked paths. Git-object union checks cover692 Go/module/script
and literal `.go.txt` inputs, including archived source proofs. Only
`internal/servercmd/runner.go` and `connector_restart_test.go` differ from1f in
executable Go: both exactly match PR63. All remaining source inputs match1f.
All1,523 documentation/site inputs and embedded outputs match1f; there is no Node
build or dependency/toolchain/workflow modification in this increment. All35
literal `.go.txt` files are present with their exact source hashes; these include
archived evidence, not35 executed fixtures. No baseline projection is rewritten.
The1,243-file inherited evidence union from both parents is unchanged.

The initial private union checker incorrectly treated PR63's archived
`base-runner.go.txt` as an executable source that had to exist in1f. Its
[FAIL_CHECKER_SCOPE receipt](evidence/context63-benchmark56-post015-composition/initial-manifest-scope-failure.json)
is preserved. The checker was corrected to recognize the inherited evidence
union; no repository input was changed and no tests had run at that point.

## One focused execution

On the frozen source, one invocation ran:

```sh
go test -count=1 -timeout=120s -json ./cmd/ivoai ./internal/servercmd ./internal/docsserver
```

It exited0 in8.237s wall time. Servercmd passed in0.458s and docsserver in0.007s;
52 test/subtest PASS events were reported with no failed or skipped tests.
`cmd/ivoai` compiled and reported `[no test files]` (a package skip event); it is
not counted as a passing test. The complete JSON log and stderr, explicit recipe,
[receipt](evidence/context63-benchmark56-post015-composition/focused.result.json)
and manifest are archived together. Source hashes remained unchanged after the
execution. The process used a private HOME/temp directory and explicit offline
Go/module settings. No official client, auth/status/model/provider, production
service restart, ingestion or external network operation was invoked.

There was no full-suite rerun, additional race run or favorable retry. The PR63
16/16 CI result40814705813 belongs only to9049947a; PR56's reported16/16 belongs
only tofbba2412. Neither result is relabeled as CI for this merge. The existing
parent race/ARM/source-only validations remain bound to their original epochs.
Combined-source CI remains NOT_RUN here; root owns final merge/push decisions.

## Limits

This composition preserves the PR63 behavior of reporting activation failure
without undoing the saved connector registry. A successful restart return is not
proof of ingestion, retrieval or synchronization. IVOAI-175 and external
`CONTEXT_SYNC=BLOCKED` are not closed by these fixtures. PR56 benchmark preparation
remains unwired; no provider-route, concurrent-client, worker, economic or general
client parity acceptance is added. No tag, release, deployment, credentials or
personal configuration was changed.
