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
model inference. Production public CLI and worker integration are being checked
separately before release; this record does not presume those results or CI.
