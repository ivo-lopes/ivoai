# Transient WorkingContext

WorkingContext is the transient evidence layer for an IVOAI-managed execution. It
preserves a worker's complete output without automatically injecting that output
into the primary's context.

```text
worker Codex/Claude
  -> saída bruta exata
  -> ArtifactStore privado
  -> ResultRef opaca
  -> WorkerResult bounded
       +-- summary
       +-- findings
       +-- StateDelta proposto
       `-- ResultRefs
  -> primary
```

Authorities do not overlap:

- ai-memory remains the shared durable operational memory;
- ContextBackend remains persistent Context/RAG that is read-only for agents;
- WorkingContext holds evidence and transient state for the current session;
- ArtifactStore temporarily stores exact bytes;
- the primary remains the sole authoritative writer and consolidator;
- WorkerResult and StateDelta are untrusted, advisory data only.

## Contracts and limits

`ArtifactRef` is a random opaque reference. It contains type, size, SHA-256,
normalized media type, creation, expiration, session/task/worker ownership,
sensitivity, and complete/truncated state. It contains neither a public path nor
content. `ResultRef` associates the reference with the evidence role. `WorkerResult`
contains status, a bounded summary, bounded findings, references, important errors,
and typed `StateDelta`. Long evidence is never stored inline.

Exact output is persisted before structured projection. Worker, test, build,
security, and cancellation failures remain explicit in the bounded result; complete
detail remains retrievable through the reference. Content classified as `secret`,
`credential`, or `raw_auth` is not accepted in the general store. `internal` and
`restricted` content is private and never enters observability as a body.

Each projection receives a provider-neutral class: `compressible`,
`exact_required`, `bypass`, or `unsupported`. Classification is deterministic and
fail-safe. Memory/Context responses, Skill Registry metadata, security evidence,
errors, stack traces, test/build failures, and payloads that influence policy or
authority are `exact_required`; unknown types also favor fidelity. Failure or
cancellation always overrides a compressible suggestion.

An optional bounded `association_id` links a ResultRef to the call/result that
originated the evidence. Distinct calls preserve distinct refs and are never merged
implicitly.

## Local ArtifactStore

The store resides in `$XDG_CACHE_HOME/ivoai/working-context` (or the equivalent XDG
cache). Directories use `0700`; payload and metadata use `0600`. IDs are not derived
from worker input. Writes use private staging, fsync, and atomic rename. Reads reject
symlinks and validate containment, ownership, TTL, size, and SHA-256.

Current defaults limit each artifact to 16 MiB, each session to 64 MiB/256 objects,
and the global store to 256 MiB/2048 objects. The default TTL is 24 hours, and the
maximum is seven days. GC is explicit and deterministic, uses no daemon, and removes
only expired objects within the managed root; corrupt or unknown entries are
preserved and reported, not deleted by assumption.

## Context budget and recovery

The storage budget preserves evidence; the context budget limits only the automatic
projection sent to the primary. Current budgets are 4 KiB (LIGHT), 8 KiB (BALANCED),
12 KiB (STRONG), and 16 KiB (MAX). The primary can use the read-only local tools
`orchestration_artifact_read` and `orchestration_artifact_read_range` to retrieve
exact content or a range of up to 1 MiB. A read revalidates session, integrity, TTL,
and limits; a reference never becomes arbitrary file read.

Session, checkpoint, SharedContextBrief, handoff, and automatic instructions persist
or carry only bounded metadata and ResultRefs. During a Codex/Claude handoff, the new
primary rehydrates the same references without duplicating raw output.

If the store is unavailable, IVOAI returns WorkingContext as `degraded` and does not
use raw output as a prompt fallback. This may lose the exact evidence from that
execution, but never causes silent, unbounded injection.

WorkingContext does not implement scheduling or replace evidence with compression.
When Caveman is selected, the managed `caveman-mcp` component may project smaller
representations only after ArtifactStore and without changing ResultRef/WorkerResult
as the source of exact evidence. The helper is the local BSL-1.1 stdio asset
`bin-v1.1.3`, pinned to the same runtime revision; it does not use `npx`, is not
registered as a primary MCP, and operates with a private ephemeral recovery store.
A helper failure, timeout, malformed/oversized response, or unavailability falls back
to the bounded deterministic projector; large raw output never becomes a prompt
fallback. JSON, logs, code, diffs, search results, and text are compressible types;
exact-required and bypass never invoke the helper.

Storage budget, context budget, and compression are separate controls. Caveman does
not change the 16 MiB per-artifact limit, ownership isolation, TTL/GC, or the 1 MiB
maximum read range.

A future orchestrator can consume the same contracts without changing the primary's
authority.
