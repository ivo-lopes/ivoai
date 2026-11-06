# Bounded descendant observation in the admission test

PR #76 CI [42277331096](https://github.com/ivo-lopes/ivoai/actions/runs/42277331096)
failed its race step in
`TestPrimaryPrefetchCommonResistantDescendantDeniesAdmission`, at the single
descendant `/proc/stat` observation after the leader had been reaped. Fifteen
jobs passed; two dependent deployment jobs were skipped, not successful. This
failure remains recorded and the PR was not merged.

The original diagnostic does not record whether the snapshot contained a live
state, an unknown identity, or a read error. Thirty local repetitions of the
original compiled test passed. Therefore the precise CI failure is **not
reproduced**; asynchronous observation is a source-based explanation to test,
not a proven root cause.

The revised test fixes one three-second monotonic deadline **before cancellation**
and shares it between leader cleanup and read-only descendant observation.
A known matching live state can be observed again until that same deadline.
Only matching `Z` or known absence (`ENOENT`/`ESRCH`) is accepted. Unknown I/O,
permission errors, identity changes, malformed data and unknown states fail
immediately. A descendant still executing at the deadline fails. There is no
new signal, cleanup retry, additional three-second allowance or claim that an
orphan was reaped. Admission still must be refused with zero model attempts.

The new focused controls cover a matching live-to-zombie transition, continued
execution, an exhausted deadline, identity changes and unknown observations.
Twenty repetitions of four focused groups under the race detector passed
(package time 20.801s, excluding compilation), without skips. `go vet
./internal/app` passed. An independent read-only review found no blocker or
weakening of these admission/identity/deadline oracles. Production Go code and
the published v0.17.0 binaries are unchanged.

Evidence and hashes are in
`release-notes/evidence/ci-descendant-observation-20261106/validation.json`.
The initial local harness launch used the repository directory instead of the
package directory and could not find its fixture source; that failure is
retained and is not counted as executed process controls. None of these tests
is official-provider inference or economic certification. CI must pass on the
new immutable PR head before merge.
