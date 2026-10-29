# Native cleanup fixture: coherent process observations

The original quality job failed `closed_writer` in 0.01s with “native writer
survived frontend cleanup before trigger.” Its source is the PR56 merge
`1e64d9b86588604d8e1f800d63a4bba1e03ffc85`, combining
`6cc9af6c31dc6e084b243fe9e1232bb83e0a403b` with main
`4368d5495a5be4ce425f4ab89849df0296fc6f9b`. The codexfrontend sources are identical
between those two parent commits. CI40795567122/job123191101273 remains a failed
historical run; its exact process snapshots were not logged.

The fixture predicate read `/proc/PID/stat` twice: once through
`session.ProcessMatches`, then again for process state. A known zombie could be
observed as dead, then be reaped after the next identity check. The second read
then returned ENOENT, which that predicate reported as alive. A deterministic
owned-child interleaving reproduces this false→true result while the actual child
has exited with Wait0. This proves a fixture bug consistent with the short CI
failure; it does not prove the exact unobserved CI interleaving.

The correction checks identity and state from one snapshot. Known missing or
replaced identities and terminal Z/X states retain their intended dead result.
Unknown read errors, missing identity and malformed state remain alive so the
assertion fails closed. The live control, release trigger, final liveness check
and 100×5ms polling budget remain. The PID file is also published through an
adjacent temporary file and rename, avoiding an independent empty-file readiness
race; that was not the assertion reported by the failed CI run.

No production file, permission, cancellation policy, public API, client pin,
module dependency or user configuration changes. The observed real canary,
normal public quit and earlier failures are separate immutable epochs.

## Evidence and validation

[Evidence manifest](evidence/native-close-writer-observer/validation.json) records
source and artifact digests. The complete original CI log remains private;
a small synthetic failure excerpt and full-log SHA256 are retained here.

- Original two-read observer: causal false-positive reproduced with an owned
  zero-exit zombie and a controlled reap between reads.
- New regression: FAIL before correction, PASS after correction. Before logs and
  both exact before-source files are preserved as `.go.txt`.
- Normal cleanup fixture: live-writer control and closed-writer assertion PASS;
  12 snapshot cases cover matching live/dead identities, replacement and unknown
  errors/data.
- Negative control: omitting only the fixture's `f.Close()` action leaves the
  writer alive and FAILS the unchanged assertion after 0.53s. This expected
  rejection is not counted as an ordinary passing test. Production is unchanged
  even in this negative control; existing defers close the owned fixture after
  its assertion fails.
- Focused source-only race: 36 passing events including subtests, 9 top-level
  tests, no failures/skips, package1.065s. `go vet` passes.
- Independent root epoch: whole codexfrontend race package, count1, 40 passing
  events/13 top-level/no failures/no skips, package8.038s. Its two official native
  opt-ins use pin0.162.1 with fresh private HOME and a fake provider/runner:
  AppServerAdmission0.92s and TUI6.03s PASS. No manual login/status/auth command
  or real model inference was requested by
  these mapped fake-provider/Core fixtures, and no new download was performed.
  This epoch used env-i/private HOME; it did not install a network namespace or
  replace `/etc`, trace all external traffic, or certify system/cloud MCP
  isolation. Root's two original artifacts are copied byte-for-byte.

These are local validation results. Fresh hosted CI and review are still required
before merge; no release or architecture/economic completion is claimed.

## Reproducing the controls

Use Go1.27.3 and source-only local dependencies. Do not set official/live opt-ins
for the following commands. The final regression and original fixture are:

```bash
GOTOOLCHAIN=local GOENV=off GOPROXY=off GOSUMDB=off go test ./internal/codexfrontend \
  -run '^(TestNativeCloseStopsBackgroundDirectoryWriters|TestNativeFixtureWriter)' -count=1 -v
```

The preserved before-source and negative source are Go overlay inputs, not new
Go packages. Map `internal/codexfrontend/facade_test.go` to
`before-fix-facade_test.go.txt` and `fixture_process_test.go` to
`before-fix-fixture_process_test.go.txt` for the expected regression FAIL.
For the negative control, map only `facade_test.go` to
`mutant-skip-close-facade_test.go.txt`, then run only
`TestNativeCloseStopsBackgroundDirectoryWriters/closed_writer`; expected exit1
and “native writer survived...” are rejection evidence, never a normal PASS.
No real model/provider/auth execution is involved in either control.
The exact first causal-overlay source is also preserved as `.go.txt` for review;
its historical private report path must be changed to a new owned directory
before any separately authorized replay, rather than overwriting old evidence.
