# Owned read-only continuation after an approved adaptive DAG

After a successful adaptive DAG, the same Codex Primary can answer the next
natural request in the same conversation without creating another DAG or worker.
The original DAG is archived before its operational pointers are retired; it is
not deleted, recompressed, replayed or converted into a new authorization grant.

## Admission and durable history

Eligibility requires the closed prior receipt and its successful matching native
attempt, the same owned account/thread/frontend/scope/configuration/runtime,
completed tasks, completed and reaped workers, resolved approvals and an integrated
checkpoint when present. Failed, interrupted, pending, foreign or unverifiable
history cannot acquire this adaptive capability. Older transcripts without the
required proof retain the existing guarded legacy DAG path.

The existing SessionStore stores an immutable private snapshot of the bounded
host journal, including original task/worker/decision/ResultRef metadata and exact
checkpoint source bytes. JSON base64 preserves the checkpoint byte array. The
small `completed_primary_plan` reference contains digest, size and receipt/plan
identity. Each snapshot retains its preceding reference; one owner-scoped read
returns one page, without truncating older plans or expanding active metadata.
This does not create a memory backend or use a transient ArtifactStore TTL as
indefinite history. Worker raw outputs retain their existing evidence references
and existing retention policies.

Snapshot promotion uses directory file descriptors, exclusive staging, file and
directory sync, a digest filename and no-overwrite linking. Idempotent retries
validate existing bytes. Nonregular files, symlinks, owner/permission mismatches,
foreign session references, tampering and oversized snapshots fail closed. Only
after the immutable file is durable can the host atomically replace active DAG
pointers with a new read-only receipt. A failed metadata write can retain an
unreferenced snapshot; it cannot justify replaying completed workers.

The additive `history_digest` receipt field pins the historical pointer to the
new turn. Late changes to the pointer, archive or retained checkpoint withhold
completion and preserve NoReplay/NoAutomaticFailover. Snapshot retrieval returns
untrusted historical evidence, never executable instructions or renewed grants.

## Actual validation

The [frozen source manifest](evidence/codex-postdag-20261104/postdag-frozen-source.json)
was verified before and after building immutable test/production binaries and
after real official inference. The [final receipt](evidence/codex-postdag-20261104/changes-postdag-frozen-epoch2.json)
records approved real Codex 0.166.0 execution:

- C: one isolated worker fixes only `math.go`, fixed tests fail before and pass
  after integration, then a same-conversation read-only turn finishes without a
  new DAG or worker. The archived plan retains that worker. 93.680 seconds.
- D: two isolated workers change only `greeting.go` and `format.go`, fixed tests
  fail before and pass after integration, then a same-conversation read-only turn
  finishes without a new DAG or worker. Both workers remain recoverable in the
  archived journal. 131.570 seconds.

The combined test wall time was 225.29 seconds. Account credentials remained in
place; no paid API or fake provider substituted for these worker results. Native
exit zero, exact task grants, explicit host approvals and repository isolation
were verified. Reported usage is primary-only official CLI usage; worker usage
is unavailable and is not added as zero.

A separate [public CLI proof](evidence/codex-postdag-20261104/full-cli-postdag-frozen.json)
passed on the same frozen production source: two read-only TUI turns, normal
exit, a new `session resume` process and a third related turn, one Core session
and native thread, no DAG/workers, normal exits and unchanged files (66.974s).
The combination of an approved DAG, public TUI exit and cross-process TUI resume
was not executed as one combined test; these separate proofs do not claim that
extra certification.

Focused App/Core/Session race tests, complete Core/Session race suites and
associated vet passed. Storage tests page 40 plans without truncation and cover
idempotence, exact checkpoint bytes, owner separation, non-following reads,
corruption and cancellation. Adverse host tests withhold finals after archive,
checkpoint or reference changes and reject duplicate execution. The initial
legacy compatibility failure and the earlier successful inference epoch remain
recorded; neither was reclassified as final-source certification.

## Limits and rollback

Adaptive mode remains controlled by `IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY=1`, with
only reviewed runtime pairs. This increment adds no other-client, ARM adaptive,
no-AVX execution or global OpenCode-removal certification. Session fields are
additive; original checkpoints and archive files remain untouched on downgrade.
An older Core does not consume the new history pointer, so finish/cancel active
turns before downgrade and retain the files for reapply; do not delete history
to obtain a replay. Snapshot failures deny the capability instead of claiming
successful continuation. **Global economic gate: INCOMPLETE.**

## Subsequent public-path verification

The later [public DAG and restart delivery](codex-public-dag-resume.md) records a
production trace-compaction regression and its fix, then a single real public
TUI DAG → read-only turn → normal exit → new-process resume → read-only turn.
It supersedes the earlier NOT_RUN limitation for that combined sequence without
rewriting these historical source-specific receipts or their original limits.
