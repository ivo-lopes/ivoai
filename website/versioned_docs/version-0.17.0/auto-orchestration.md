# Automatic orchestration

Managed Codex and its `ivoai auto` alias use the same IVOAI Core, admission and
scheduler. Managed OpenCode is removed. Official native clients keep their own
authentication; no credentials are copied and no paid API fallback is introduced.

## Start and conversation ownership

```sh
ivoai codex
ivoai auto
```

Intake accepts bounded natural UTF-8 requests without formal acceptance headings.
It still checks integrity, scope, ownership and permissions. The experimental
adaptive Primary is OFF by default (`IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY`). When
explicitly enabled with a reviewed runtime, a simple read-only request can finish
without a DAG. Complex work uses the existing plan protocol, validation, approvals
and isolated workers. A simple strategy does not grant writes.

## Model and reasoning selection

The native Codex picker uses verified Codex models/efforts; a shared Core catalog
does not establish Claude control through that TUI. Unsupported or unavailable
explicit selections fail closed. Managed Claude/Antigravity headless hosts have
separate OFF-by-default flags and do not claim native TUI interception. Official
reported quota/cache/model fields remain distinct from unknown or inferred values.

## Startup sequence

Core validates the native frontend/runtime before creating work, records the owned
session, resolves permitted capabilities and binds its existing bridge. Every
managed turn receives fresh admission. No OpenCode renderer, plugin installation
or personal configuration rewrite is needed. A claimed turn cannot be repeated
through an ungated direct-client fallback.

## First-turn planning and shared knowledge

Purpose routing limits retrieval to relevant selected sources. Research attempts
Memory and Context in their existing order before external sources; unavailable
sources are recorded separately. Evidence is untrusted data, not instructions.
The bounded shared brief and exact artifact references do not duplicate durable
Memory or the documentary Context backend. CodeIntel Gateway reuse is a separate
explicit flag, OFF by default, with exact recovery, ownership and source-generation
checks; it is not a universal tool cache or demonstrated global token saving.

## DAG scheduling and delegation

Core validates task scope, acceptance, dependencies, resource limits and operator
approval. The existing scheduler admits useful workers; read-only workers keep
read-only policy and approved writers use isolated checked worktrees. Results are
validated and integrated before synthesis. Frontends cannot add another scheduler
or grant themselves tools by selecting a strategy.

## Progressive escalation

Escalation uses verified capability, quota and policy. Material executor changes
require their own consent, distinct from plan approval. Missing telemetry is
unknown, not zero or unlimited quota. Compression providers remain mutually
exclusive; exact authoritative evidence cannot be replaced by a destructive view.

## Checkpoints and failover

Eligible native continuation retains conversation identity, exact completed-plan
history and durable request claims. Cancelled, failed, foreign or incomplete
history does not regain authority when an in-memory projection is compacted.
NoReplay/NoAutomaticFailover restrictions remain authoritative. Quota evidence does
not authorize replaying an uncertain mutation or converting a refused native TUI
handoff into headless execution. Interrupted DAG recovery is an explicit reviewed
operation, never ordinary resume of the previous prompt.

## Observability and state

```sh
ivoai monitor --watch
ivoai monitor --session <session-id> --json
```

Use the same administrative backend for quotas, models, workers, evidence, grants
and diagnostics. It exposes sanitized metadata, not transcripts, credentials or
hidden reasoning. A native exit zero does not erase a failed Core validation.
Gateway hits, original provider cache fields, reported usage, sampled process data
and whole-session performance have separate scopes. The economic gate remains
INCOMPLETE until equivalent-condition comparisons establish the targets.

## Failure isolation

Unavailable optional providers fail within their documented boundaries; they do
not weaken authorization or cause duplicate execution. Closed receipts, revoked
permissions, owner/worktree changes and uncertain outcomes deny cached recovery
or new execution. Direct mode and provider handoff remain explicit operations.

## Policies in the launcher and CLI

```sh
ivoai config set orchestration.auto.plan_execution approve
ivoai config set orchestration.auto.knowledge_routing purpose-auto
ivoai config set orchestration.auto.concurrency auto
ivoai config set orchestration.auto.worker_cap 2
ivoai config set orchestration.auto.parallel_writes true
ivoai config set orchestration.auto.provider_preference auto
ivoai config set orchestration.auto.low_quota_threshold 10
```

These settings do not enable experimental flags. Immediate plan execution is
explicit operator configuration and does not remove task validation, permissions
or native sandbox checks. The legacy `opencode.permission_mode` key remains a Core
compatibility setting; it never restores the retired renderer.

## Writer isolation and recovery

Approved writes remain scoped to isolated worktrees and checked integration under
Core ownership. Original evidence and failure records survive unsuccessful work.
Existing user-owned OpenCode data and old profiles remain untouched; old bundles
are verified with their historical contract for explicit rollback.

[Reference](orchestrated-frontends.md)

[Reference](auto-scheduler.md)

[Reference](working-context.md)

[Reference](conversation-continuity.md)

[Reference](connections.md)
