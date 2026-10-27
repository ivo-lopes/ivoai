# Experimental Evidence Gateway

Status: initial, disabled-by-default implementation for IVOAI-199/148/192.
This is a transient retrieval layer inside the existing Go backend. It does not
replace the scheduler, Memory, Context, the code providers or ArtifactStore.
The complete migration and certification remain pending.

## Enable and rollback

Set `IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY=1` only on the IVOAI host process for an
isolated experiment. Other values keep the existing prefetch implementation.
Remove the variable to roll back. No personal client configuration, component
pin, persisted configuration schema or authentication store is changed.
This flag is not a production rollout recommendation: the initial real economic
comparison failed its quality/economy criteria.

The first adapter handles already-admitted code prefetch for primary and workers.
Existing intent classification and direct/Graft selection remain responsible for
choosing evidence. Memory/Context/tool-output adapters, compression filters and
general multi-source sufficiency are not implemented by this slice. No source
fan-out or new public MCP endpoint is introduced.

## Retrieval and isolation

`internal/evidence` binds each request to a session and host-derived digests for
organization, repository, physical worktree and permissions. The code adapter
binds the knowledge scope and canonical filesystem root; it conservatively treats
different working roots as different repositories. Equivalent normalized queries
can share one retrieval only within the same scope, source generation and budget.
Host callbacks repeat authorization and source-generation checks before serving
a cache hit and after retrieval. Source snapshots detect edits, additions,
deletions and renames; changing grants or Skills invalidates admission.

The in-memory cache is process-local, limited to 64 entries / 4 MiB / five minutes,
with at most eight distinct active retrievals. It is not a cross-process cache.
Concurrent equivalent requests wait for the same retrieval and independently
revalidate admission. Cancellation is bounded; a failed or panicking retrieval
releases its slot. Mutating requests are rejected before their callback runs.

Each recipient receives a separate exact artifact under its existing task/worker
ownership. Sharing retrieval does not grant a worker the primary's artifact or
another worker's authority. Active worker records must match the task and owned
worktree; valid subdirectories are supported without allowing an escape.
Primary read admission preserves the existing immediate-plan policy and rejects
an explicit pending/rejected decision. It does not create approval authority.

Raw content is admitted before persistence: prohibited sensitivity classes,
recognized credential patterns, unsafe media metadata, invalid UTF-8, NUL and
oversized payloads are rejected. Credential-pattern checks are defense in depth,
not proof that arbitrary source text cannot contain a secret. Host source policy
and the existing inventory exclusions remain required.

## Fidelity and provenance

The library records `extracted` versus `inferred`, the source generation, exact
SHA-256 artifact reference and whether the retrieved source is complete. References
are sealed to prevent relabeling an artifact into a different scope. Content is
always untrusted data and never changes instructions or execution grants.

If a payload exceeds its context budget, the generic packet contains only its
reference; it does not cut a diff, stack trace or authoritative result. Full exact
bytes remain in ArtifactStore. A stored excerpt is exactly recoverable as an
excerpt: its reference does not recover source bytes that were never retrieved.
`source_incomplete` and `inference_requires_verification` are explicit gaps.
`requires_task_validation` is the initial sufficiency state; complete source is
not proof of task completion.

The code adapter preserves the established text/artifact/fidelity/untrusted wire
envelope. Internal cache/provenance metadata stays outside the model context;
reuse is visible through the existing metadata-only `capability.resolve` event
with `capability=evidence.code`. The existing ArtifactRef reports truncation.

The library's sealed `Recover` API repeats current scope/generation/admission
checks. It is not yet a new client protocol. Client artifact tools retain their
existing session/ownership policy and are not certified as that stricter recovery
API. Do not use this initial adapter to claim general cross-organization recovery
or Memory/Context sharing. Integrating a common recovery policy into every client
and administrative transport is a remaining gate.

## Validation and performance boundary

Tests cover scope separation, grant revocation, content generation, cancellation,
concurrent deduplication, TTL, queue/cache bounds, secret admission, exact recovery,
reference tampering, primary-to-worker reuse, immediate mode and a real Git
worktree with a monorepo subdirectory. They do not prove real client/worker parity.

The initial cold-prefetch comparison reported 9/9 correct baseline attempts and
6/9 candidate attempts. Six correctness-matched pairs increased input by 5.84%
and elapsed time by 7.90%. All failures are retained in the validation evidence.
A diagnostic identified a symbol-field oracle mismatch without storing answers.
Subsequent runs and refinements must remain separate datasets; no negative sample
may be replaced by a favorable retry.

Generation checks currently snapshot the authorized source inventory repeatedly.
This can be expensive on large repositories. The small synthetic benchmark does
not quantify that overhead at the 128 MiB inventory bound, warm reuse between
turns or real multiple workers. Keep the flag off until broader objective quality,
security and economic acceptance is demonstrated. See the
[validation record](https://github.com/ivo-lopes/ivoai/blob/feat/backend-first-evidence/release-notes/backend-first-validation.md) and the
[migration audit](backend-first-migration.md).
