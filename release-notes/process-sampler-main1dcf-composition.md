# Sampler diagnostics composed with main1dcf

This epoch composes reviewed typed diagnostics `057c808c4e53b1c829cb5748a823de1c19d8a991`
with actual main `1dcf90400f67eb960a2446ccf3c987ecf977a9d8`, including the merged
PR51/52/53/55 source. The merge was automatic with no conflicts or manual
implementation/condition changes. Calibration and diagnostic scripts remain
byte-identical to057; ProcessSampler and the common driver remain byte-identical
to reviewed d7bd. All Go source inputs equal main. Incoming catalog/concurrency,
G1 and publication changes are inherited as reviewed, not new calibration logic.

**The original independent fixture failure remains INCONCLUSIVE and this work
remains DRAFT.** Neither successful composition nor earlier/future green CI
explains the original failure. All368 existing evidence files, including the
original343 proofs and typed diagnostic epoch, remain byte-identical. Original
32-row measurements, initial failure, read-only audit and follow-ups retain
separate source identities; none are rerun, backfilled or rescored.

[The source epoch](evidence/backend-first/process-sampler-main1dcf-composition-v1/source-epoch.json)
and [validation](evidence/backend-first/process-sampler-main1dcf-composition-v1/validation.json)
bind both parents, the merged source tree before these reports, script/source
hashes, exact inherited documentation markers and preservation checks. All
incoming main evidence files also match their source byte for byte.

Local composition gates passed:

- Full uncached Go suite:59 packages passed; three had no tests.
- Existing Python suite:79 total,78 passed and one explicitly NOT_RUN (15.988 s).
  The real memory/churn calibration opt-in was absent and is not counted as a
  successful real fixture. Deterministic typed diagnostics remain covered.
- Full vet passed.
- Mandatory offline legacy rollback passed with one named test, zero skips and
  source pin3be82. Old full/range reads rejected protected IDs; legacy put/read/GC
  continued and protected payload survived. Old inventory excludes protected
  disk, so complete old-version quota accounting is not claimed.

No Node build was needed: docs, website, embedded site and both build markers
are byte-identical to current main. No new calibration32, real fixture retry,
performance comparison, provider turn, authenticated client, release, dependency,
default or deadline change occurred. Ordinary existing unit fixtures remain
code-contract checks, not economic data or client parity. No new ARM/non-AVX,
browser or official-client certification is claimed; exact-head hosted CI remains
a gate before merging the draft.

The source composition does not introduce a correction factor, infer physical
RSS or complete lifecycle coverage, or close the economic gate. The126 count in
other reports remains scoped to six backend-first cohorts, with older PR30 and
projection work preserved separately. The economic gate stays INCOMPLETE and
CONTEXT_SYNC stays BLOCKED under IVOAI-175. No released v0.14.0 tag or asset was
changed. Rollback reverts only this source-composition merge; retained historical
proofs and the unresolved-failure record stay immutable.
