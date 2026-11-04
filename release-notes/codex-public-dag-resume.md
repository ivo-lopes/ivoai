# Public Codex DAG, read-only continuation and process resume

The public production `ivoai codex` command now completes an approved change,
continues read-only in the same native TUI, exits normally, and resumes the same
owned conversation in another `ivoai session resume` process. The existing
Primary, plan protocol, scheduler and workers remain the execution authority.
The experimental adaptive flag is still required; no global rollout is enabled.

The complete official Codex 0.166.0 Linux amd64 sequence passed in **116.045s** on
source `e3b43138d`. One actual worker fixed only `math.go`; a host AST check allowed
only pure addition and executable tests failed before/passed after the change.
A second natural turn archived the completed DAG and answered read-only. After
normal `/exit`, another public process resumed the same Core session, frontend
and native thread, completed a third read-only turn, retrieved the original DAG
history and exited normally. Three receipts closed; final active DAG/workers
were empty and repository files retained the authorized change. Backend/native
and public wrapper exits were zero, without SIGINT as a completion mechanism.

The disposable fixture used an explicitly authorized private immediate-plan
policy. The actual Primary retained its read-only permissions. Existing official
authentication locations were preserved, Core XDG directories were private, and
no fake model/executor, injected admission callback, new scheduler or paid API
fallback was used. The [immutable receipt, source manifest and validation logs](evidence/codex-public-dag-resume-20261104/validation.json)
identify the exact production binary and distinguish local proof from integrated
main CI and release publication.

## Regressions found and corrected

Review found that production removes detailed traces from older attempt UI
metadata, while the earlier real-worker fixtures retained them. Earlier C/D
runs exercised official inference, but could not certify the public continuation
path. Production and both fixtures now share `persistManagedTurnAttempt`,
including the same immutable audit, trace compaction, bounded metadata and event
transitions. Historical receipts are preserved with their original limits.

Before archiving a completed DAG, the Core reads its exact original terminal
attempt from the existing durable journal. It verifies owner/content integrity
and compares every nontrace field to the current summary. Missing, changed,
foreign or multiple terminal events are denied. The archive retains the exact
original bytes while the active Session keeps its compact projection. No trace
or execution success is reconstructed from an output summary.

A second issue affected process restart: runtime cleanup reserialized an already
durable checkpoint and changed its update timestamp. That invalidated the exact
bytes bound to archived evidence. Cleanup now leaves a validated durable file
intact and promotes a legacy runtime checkpoint byte-for-byte. Tests cover both
paths, audit tampering and the production compaction transition. The original
reviewer reproduction now reports that its candidate is no longer reproduced;
that expected negative result is retained separately from passing suites.

Focused Session/App race checks passed in 1.054s/4.162s; the complete affected
Session/App/Core/bridge packages passed in 1.442s/20.633s/0.002s/4.814s, and vet
passed. Build and earlier contract failures remain recorded, with no weakening
of acceptance tests. No token, time or quota saving is inferred from these
functional observations; the global economic gate remains **INCOMPLETE**.

ARM64/no-AVX inference and cross-version active-receipt migration remain outside
this certification. Existing account/runtime/model/root/thread identity and
ownership checks continue to apply, including exact reviewed binary pairs.
