# Published v0.17.0 Core: official Codex vertical certification

The downloaded public **v0.17.0** AMD64 Core passed the existing real native
Codex integration in one attempt on 6 November 2026. The receipt records
**107.889 seconds**, three completed turns, three closed admission receipts,
one Core session and normal backend/wrapper exits. This is provider inference
through the official Codex 0.167.0 executable and native TUI, using its existing
authentication in place; it is not a synthetic provider or an API fallback.

The first request created a completed DAG and one successful archived worker
to repair `math.go`. The fixed baseline tests failed before the change and
passed afterwards; only `math.go` changed. A second read-only turn created no
new DAG or workers. The public command exited normally, then a new process
resumed the eligible session and completed a third turn with the same Core,
frontend and native-thread identity. Completed DAG history remained readable.
The fixture explicitly authorizes immediate execution only within its
disposable worktree; this run does not certify an interactive approval UI.

The receipt's final `dag=false` and `workers=0` fields describe the later
read-only state. Its `completed_DAG_before_readonly_and_resume=true` and
`archived_workers=1` fields establish the earlier worker execution.

## Exact artifacts and harness scope

- Published Core source: `f441b81abbbd9cd24f87651837a98f89a40e06ac`.
- Published AMD64 Core SHA256:
  `ecb8011b2ac215a7b6ac9166cd4fc032ef3bf0b2ce1a3dbbda756b93b467160c`.
- Existing compiled test harness source:
  `7bce1090ea1eb971326f641460fea4fdee8e6b7b`.
- Compiled harness SHA256:
  `06cd2785a824390d0014a120533e2a067d609cae418bf58441c5ed9a309bfd49`.
- Official Codex executable SHA256:
  `cdcb45aaa01d0f14e298985e09465aebccd47f64ecd298aea44814af7b62bbda`.

Before execution, `git diff` confirmed that all tracked Go sources and
`go.mod`/`go.sum` are identical between harness source and published source.
The harness was reused without rebuilding the product, relaxing runtime hashes,
changing admission guards or replacing callbacks. The actual production
executable was the final downloaded release artifact, rather than the earlier
private candidate `a133...`. Embedded documentation and release metadata remain
those of the published source; the harness is not misidentified as a build of
the final tag.

The existing test's isolated Go oracle compiles and executes the fixture tests
in its own temporary cache. The official client retains `HOME` and `CODEX_HOME`;
the agent did not inspect or copy authentication files. Only private Core XDG
paths are configured. No raw PTY, credentials or executable payloads enter the
committed evidence. A post-run scan found no remaining process using any of
these three exact executable paths.

## Evidence and limits

The exclusive epoch-1 receipt, Go test log, source/launch record, post-run checks
and hash manifest are in
[`evidence/published-codex-vertical-20261106/`](evidence/published-codex-vertical-20261106/).
Earlier unsuccessful private-candidate attempts and their receipts remain
unchanged in the existing certification history. No retry or automatic model
fallback was used for this public-artifact run.

Reported token values retain their `client_reported` / `cli_invocation` scope;
they are not summed into a claimed session bill or saving. This single
functional run is not an AB/BA comparison. **The global economic gate remains
INCOMPLETE.** Linux amd64 with the exact reviewed Codex pair is the certified
scope; no ARM inference, Claude inference, general TUI parity or global rollout
is inferred from this result.
