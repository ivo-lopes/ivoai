# Reviewed Antigravity 1.5.2 — 2026-11-06

The native host now pins **Antigravity 1.5.2, Linux amd64** and requires its exact
ACP version at initialize, retaining the opt-in flag and all original Core
receipt, owner, scope, revision, permission and no-replay checks. It still uses
the same headless entrypoint and existing scheduler; native TUI interception,
cross-process resume and post-DAG continuation are not certified.

The installed executable had updated from 1.5.0 to 1.5.2. The first new epoch
correctly failed closed with `ANTIGRAVITY_RUNTIME_UNREVIEWED`, before a turn or
worker was admitted. The previous 1.5.0 proofs and their Go/PATH limitations
remain historical evidence; they are not relabeled as 1.5.2 certification.

Provenance was strengthened for this increment. The installer served from the
[official Google download channel](https://antigravity.google/download) was read,
**not executed**. It points to a versioned HTTPS release manifest and a Google
Storage archive. The archive's SHA-512 matched that manifest; its only regular
member, `antigravity`, matched the installed executable byte for byte. SHA-256:
`da1751e7658469fa80bf36eda2c60ee61216d8e42b735a184382ba31d1f6b88e`.
[Provenance receipt](evidence/native-clients-2026-11-06/antigravity-1.5.2/provenance.json)
records the URLs and digests. This verifies integrity against the publisher's
HTTPS channel; no independent publisher signature was exposed or claimed.
The reviewed artifact is retained only in a private test directory; IVOAI does
not redistribute it. Personal binaries, settings, histories and credentials were
not replaced or copied by the tests.

Actual authorized official-client inference passed:

- Public managed natural prompt plus same-session follow-up: 22.959s, two owned
  turns, normal exit0, no signal, no DAG/worker.
- Public Code Intelligence Direct: 20.831s, verified `calc.go:4`, normal exit0,
  no DAG/worker.
- Public protected-read request: safe refusal in 22.093s, unchanged canary,
  one completed attempt and no DAG/worker. This is a privacy/one-attempt proof,
  **not evidence that the permission-denial callback was exercised**. A stricter
  rerun requiring an unambiguous terminal state passed in 39.609s.
- Independent official ACP proof: four mandatory cases passed in 71.431s,
  covering two owned turns, scoped exact code recovery, and an actually denied
  mutation with `ANTIGRAVITY_PERMISSION_DENIED`, no replay and unchanged fixture.

See [public invocation log](evidence/native-clients-2026-11-06/antigravity-1.5.2/public-epoch17.log),
[stricter privacy proof](evidence/native-clients-2026-11-06/antigravity-1.5.2/public-refusal-strict-epoch20.log)
and [four-case ACP summary](evidence/native-clients-2026-11-06/antigravity-1.5.2/acp-epoch19-summary.json).
These times are measured whole invocations; the ACP suite covers four invocations.
Reported usage remains official-client reports, with missing counters marked
unavailable. No economic comparison or global token/time improvement is claimed.

The separate managed DAG fixture also passed the Core worker shell environment
validation detailed below. The first attempted public Go
harness also failed before scenarios because of a misspelled `-timeout14m` flag;
that failed log remains preserved separately. Default tests skip opt-in native
inference; skipped tests are not counted as certification.

## Real DAG and Go worker result

The separate epoch18 completed successfully in **562.350s**, measured from before
the public invocation through the independent host verification. That is a
scenario duration including external verification, not a pure CLI/model duration.
The same Primary escalated through the existing plan protocol, executed one
reviewed **Codex 0.166.0** worker in its isolated worktree and kept the dependent
review in the Primary because another worker had no expected benefit. Both tasks
completed, Core integration and checkpoint succeeded, the session/turn completed
normally with exit0 and no signal, and only `calc.go`/`calc_test.go` changed.

The worker's original ArtifactStore payload was recovered and its size/SHA-256
verified against the exact owner-bound ResultRef. Its official command event
records `/bin/bash -c 'GOCACHE=$(mktemp -d /tmp/calc-go-cache.XXXXXX) go test -v ./...'`
with **exit0**, including passing TestAdd, TestMultiply and TestDivide. The Go
127 failure in the historical epochs was absent. The worker used a fresh private
temporary build cache and received no new mount or filesystem/MCP grant. It also
encountered `rg: command not found` (exit127) and recovered using `find`; that
unfavorable diagnostic remains in the audit. The final independent host Go test
also exited0, separately from the worker evidence.

[Sanitized DAG/worker audit](evidence/native-clients-2026-11-06/antigravity-1.5.2/worker-dag-epoch18-audit.json)
retains the original artifact digest, exact successful command, failed lookup,
source/runtime identities, normal terminal trace, task routing and integrated
commit/file list. The private original remains recoverable with its owner-bound
reference. This proves functional compatibility of the worker shell settings;
it is not an isolated causal AB experiment because Antigravity also changed from
1.5.0 to1.5.2. No prior failure or historical benchmark was rewritten.
