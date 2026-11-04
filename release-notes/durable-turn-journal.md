# Durable managed request claims and attempt audit

The previous managed claim callback retained only 256 frontend message IDs and
removed the oldest entry before executing a new request. After enough turns and
a process restart, that projection could no longer prove an old request had been
consumed. The 32-attempt projection also did not provide durable complete audit.
Neither limit is an acceptable execution authority for a long conversation.

## Execution authority

`SessionStore.ClaimFrontendRequest` now owns permanent replay protection. It
hashes the frontend, working directory, knowledge/organization scope and native
frontend ID into a private namespace, and hashes the request identity into a
record filename. Existing entries are terminal consumed claims; their contents
are never instructions or operations to execute. The 256-entry metadata map is
only a compatibility/UI projection. There is no 256-request execution limit for
new journal-backed conversations and no TTL on a committed claim.

The existing cross-process Store lock serializes scope migration and claims.
Records use exclusive private staging, file sync, atomic no-overwrite linking
and directory sync before dispatch. Directory descriptors, validated owner/hash
segments, private ownership/modes and NOFOLLOW reads prevent path/link escape.
Storage errors and cancellation deny admission. A failed later metadata update
can leave a conservatively consumed claim; it never justifies replaying work.
No database, broker, scheduler, authorization bypass or credential copy is added.

`AppendTurnAttempt` durably records the exact bounded callback bytes before its
32-entry metadata projection changes. `ReadTurnAttemptEvents` retrieves one
explicit owner's attempt events with integrity checks. These are audit records,
not provider results or recovered grants. Existing sessions import the attempts
still retained in their old projection; `legacy_attempt_audit_partial` honestly
marks histories that may already have been truncated. Lost historical records
are not fabricated.

Adaptive eligibility also retains a monotonic, owner-scoped denial marker.
Failed, cancelled, foreign or executor-substituted recorded attempts cannot
become eligible merely because 40 later clean turns evict them from the UI
projection. Recognized failed callbacks persist this marker before their audit
record; a crash between the two writes cannot clear it. Foreign callback inputs
are rejected before any owner state changes. Get/List/Bounded derive legacy
history denial in memory only; authorized mutations persist it. Updating metadata
cannot clear a durable denial or turn an already-partial legacy audit into proof.

## Migration and rollback

The first scoped claim imports retained legacy claims and audit under the same
lock. A scope marker becomes durable only after import succeeds. Matching older
logical owners are leased and migrated together when inactive. An active foreign
owner, corrupt namespace, missing proof or uncertain import is refused.

A legacy map already at 256 entries may have discarded unknown earlier claims.
That conversation is refused with `REQUEST_CLAIM_HISTORY_INCOMPLETE`; the original
records remain available for inspection. Starting a new managed conversation is
explicit; the product does not replay or silently fork an uncertain operation.
The resume entrypoint reports this condition before provider resolution.

Journal-backed records use additive `request_journal_version=1` metadata in the
private `.request-journal-sessions` namespace. New Get/List/Bounded/Watch/frontend
lookup/reopen paths include it. Earlier Cores cannot enumerate, resume or rewrite
these records, so rollback cannot discard the new authority and revive old IDs.
Metadata migration synchronizes both source and destination parent directories.
Reapplying a journal-aware Core resumes the same identity and committed claims.
Nonmigrated legacy records retain their original readable format. Finish/cancel
active work before downgrading; do not delete journals to force replay.

## Validation scope

Focused race tests passed for 300 unique claims followed by an oldest-ID
rejection after Store reconstruction, a real two-process contention with exactly
one claimant, logical-owner deduplication and scope separation, retained legacy
import, uncertain-history refusal, cancellation, symlink/storage denial, metadata
version downgrade refusal, namespace watch/reopen and exact audit recovery after
40 attempts exceed the 32-entry projection. A private helper's ordinary opt-in
skip is not counted as an independent integration success.

An explicit real **published v0.16.0** rollback reader, verified by executable
SHA-256 `a933d4d313930066409c77682a8cc26b574b5c259174e1cb68fac6b890c9d2ef`,
listed no journal-bound session and refused its explicit ID. It did not change
protected metadata; a reconstructed new Store still rejected the oldest ID.
The focused package run took 4.409s. These storage/reader proofs do not perform
model inference. Later focused race and complete Session/Core race runs also
passed; their immutable logs and source attribution are retained below.

## Official integration and retained failures

The real public production command with the official Codex 0.166.0 executable
passed three natural prompts across two processes on source `da0c86f5d` in
53.823s. The same Core session, frontend and native thread were retained, three
receipts closed, no DAG or worker was created, both native/backend and wrapper
exits were zero, and repository files remained intact. Existing HOME/CODEX_HOME
authentication was preserved and only private Core XDG locations were created.
The earlier journal-only source `9e13a2073` passed the same flow in 55.183s.

On the same frozen `da0c86f5d` source, approved changes C and D passed in 209.56s:
one real worker changed only `math.go`; two real workers changed only
`format.go` and `greeting.go`. Fixed executable tests failed before the changes
and passed afterward. Both scenarios then completed a read-only turn with the
full durable DAG history still recoverable. No alternate executor was used.
Official cancellation and native read-approval refusal passed in 16.32s; owner
receipt revocation during inference passed in 16.55s without using cancellation
as the revocation trigger. Duplicate requests returned 409 without another
attempt, fallback or worker. This is Core receipt revocation, not revocation of
subscription credentials.

A real public-CLI failure on intermediate source `adc6ffe44` is retained, not
counted as success: its strict history check mistook the host's deliberate
removal of older detailed traces from UI metadata for incomplete execution.
The second turn was denied direct admission and timed out in the guarded legacy
DAG path. `da0c86f5d` distinguishes an original callback (complete native trace
required) from a completed projection summary (explicit final response, native
ID, ended timestamp and zero exit code required). Exact original traces remain
in the journal; an earlier denial marker is never cleared. Tests cover 40 valid
trace-stripped summaries, 40 later clean events after a failure/cancellation/
foreign delivery/fallback, incomplete original terminal traces, restart,
forbidden taint downgrade, foreign-callback nonmutation and read-only inspection.

The final legacy-hardening source `7f01b39ca` additionally makes partial-audit
metadata monotonic, materializes its private denial marker during authorized
migration, and rejects exposed journal directory/file modes without silently
repairing them. Its complete Session race run passed in 7.637s, and the final
public CLI repeated all three turns with cross-process resume in 52.679s. Real C/D/F
receipts above belong to `da0c86f5d`; they are not relabeled as executions of a
later build. The final public-CLI confirmation and exact binary/source manifests
are recorded in
[`validation.json`](evidence/codex-durable-journal-20261104/validation.json).

These elapsed times are integration observations, not a controlled performance
comparison. Token records are official-client invocation reports, not billing
attestations; worker usage was unavailable and was not silently counted as zero.
No Gateway OFF/ON economic cohort is established by these runs. The global
economic gate remains **INCOMPLETE**. A combined single public-TUI approved-DAG
then process-exit/resume test remains **NOT_RUN**; approved-DAG continuation and
public-TUI cross-process continuation were separately exercised. Adaptive rollout
remains opt-in and retains the reviewed Linux amd64/runtime/ownership guards.
Retained runtime/account/root/model/thread identities must still match; an active
0.162.1 receipt is not silently grandfathered into a different 0.166.0 executable.
Cross-version active-receipt migration is not certified by this delivery.
These local proofs do not presume integrated main CI, merge or release results.
