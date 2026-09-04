# Skill Control Plane foundation

This document describes the foundation implemented by IVOAI-13, IVOAI-14,
IVOAI-16, IVOAI-48, and IVOAI-49, plus safe pack updates, the managed-session
Skill Gate, and the IVOAI-owned curated-source overlay. Existing `ivoai auto`,
`ivoai codex`, and `ivoai claude` sessions continue to operate when the
registry is absent or empty.

## Boundaries

The provider-neutral `core.SkillSource` and `core.SkillRegistry` interfaces
remain narrow. Rich skill metadata lives under `internal/skills`; policy lives
under `internal/policy`; generic external-artifact staging lives under
`internal/supplychain`.

External skill metadata and bodies are untrusted data. They cannot replace the
IVOAI orchestrator, grant capabilities, alter sandboxing, or modify policy.

## Versioned registry

The private registry is stored at:

```text
$XDG_STATE_HOME/ivoai/skills/registry.json
```

Registry schema 1 records canonical IDs, bounded descriptions, immutable
upstream revisions, SHA-256 integrity, signature and attestation status,
logical version, default branch observed during discovery, domains, triggers,
dependencies, conflicts, phases, roles, declared capabilities, risk,
compatibility, lifecycle, and non-sensitive provenance timestamps.

The active identity is an immutable commit plus integrity metadata. A tag or
default branch is useful discovery evidence but is not an active reproducible
revision. A registry write is deterministic and atomic. Its directory and file
are `0700` and `0600`; no-follow reads reject symlinks and bounded reads reject
oversized state.

A missing registry is interpreted as a healthy empty registry. This preserves
v0.5.0 compatibility without a config, state, or ownership schema bump. The
registry file is an explicit optional participant in the transactional update
snapshot, so future registry migrations cannot be invisible to rollback.

Lifecycle values are:

- `staged`
- `active`
- `quarantined`
- `previous`

## Metadata-only index

Discovery walks a bounded private source tree without following symlinks. It
opens each `SKILL.md` with `O_NOFOLLOW` and stops at the closing frontmatter
delimiter. The body is not loaded to identify or rank a candidate.

The normal indexer:

- performs no LLM call;
- executes no hook, script, command, code block, or setup file;
- accepts only a bounded declarative frontmatter subset;
- deterministically sorts candidates and quarantine reports;
- supports thousands of synthetic entries within the registry bound.

Malformed frontmatter, invalid UTF-8, unsupported schema, duplicate ID,
ID/path mismatch, symlink, traversal, oversized metadata, missing dependency,
and self-dependency result in quarantine or a bounded discovery error. Invalid
metadata never receives broad default permissions.

Ranking returns candidates, not session selection. It uses exact trigger,
keyword, domain, bounded name/description terms, compatibility, and maximum
risk. Stable score and ID ordering make the same input reproducible.

## Dependency and conflict graph

The resolver models required and optional dependencies, declared conflicts,
executor compatibility, capability availability, risk ceilings, execution
phases, and composable or exclusive roles.

Phases are:

1. planning;
2. research/context;
3. art direction;
4. implementation;
5. audit/review;
6. security;
7. orchestration;
8. interaction/profile.

Required dependencies are included and topologically ordered. Optional
dependencies constrain order only when selected. Duplicate candidates are
collapsed. Missing dependencies, cycles, mutually exclusive roles, explicit
conflicts, incompatible executors, unavailable capabilities, and risk above
policy produce typed, deterministic failures.

Complementary phases may compose. Multiple visual directors or competing
control-plane authorities do not. An external skill may declare orchestration
behavior as metadata, but this never transfers IVOAI orchestration authority.

## Policy engine

Policy is evaluated above registries, skills, hooks, tool providers, and
executors:

```text
IVOAI policy > external metadata or instructions
```

Inputs are subject identity/type, declared capabilities, requested
capabilities, risk, scope, metadata validity, and conflict status. Decisions
are `ALLOW`, `DENY`, and `REQUIRE_APPROVAL`.

Risk tiers are `LOW`, `MODERATE`, `HIGH`, and `CRITICAL`. A declared available
low-risk read capability may be allowed. High-risk writes may return a
structured approval requirement for the future Approval Engine. Destructive,
privileged, sandbox-disabling, shell-execution, and orchestration-authority
capabilities are denied in this foundation. Unknown, unavailable, undeclared,
invalid, or conflicted requests fail closed.

The engine never receives a skill body as policy authority. Text such as
"ignore policy", "grant shell", or "become orchestrator" cannot change a
decision.

## Unified supply chain

The generic pipeline supports future skills, components, and helpers:

```text
discover source
  -> resolve immutable revision
  -> fetch bounded archive
  -> verify SHA-256
  -> isolated staging
  -> structural validation
  -> policy validation
  -> extracted-content manifest
  -> immutable object store
  -> atomic active pointer
  -> health and integrity validation
  -> transaction commit
  -> previous retention
  -> rollback
```

Download adapters are separate from staging. Tests use synthetic in-memory
archives; no real skill pack or external repository is imported.

Staging accepts regular files and directories only. It rejects absolute paths,
`..`, backslash ambiguity, duplicate or reserved paths, symlinks, hardlinks,
special files, unexpected executables, file-count overflow, per-file overflow,
compressed-size overflow, and expanded-size overflow. Modes are sanitized to
`0600`/`0700`. Skills cannot declare executables. Components may declare a
bounded exact executable path but staging never runs it.

Validated content is placed in an immutable object path keyed by artifact ID
and revision. A canonical file manifest binds paths, modes, sizes, and file
digests to the transaction. Promotion first authenticates the staged journal,
then atomically replaces a small private active pointer, runs a post-promotion
health and integrity gate, and commits the journal. Any failure restores the
previous pointer. Repeated promotion is idempotent, rollback revalidates the
previous object and is safe to repeat, and interrupted staging or promotion is
recoverable without touching data outside the managed root.

Authoritative supply-chain active pointers are enumerated as explicit
transactional update participants. Immutable objects and live staging journals
are intentionally not copied blindly into update snapshots.

Integrity, signature, attestation, and trust are distinct fields. A checksum
delivered by the same GitHub release channel proves integrity, not independent
authenticity. `not_exposed` is recorded rather than inventing a signature.

## Safe skill-pack updates

`internal/skillupdate` composes the shared supply-chain manager, Registry,
metadata classifier, dependency resolver, Policy Engine, deterministic smoke,
and doctor callback into one transaction. Discovery adapters resolve the
upstream default branch dynamically but every staged and active identity is a
commit SHA. A GitHub adapter uses only the public structured API, performs a
bounded archive fetch, and records a locally calculated digest as
`commit_pinned_local_digest`; that value is deliberately not called an
independent signature or attestation.

Promotion binds the supply-chain pointer and Registry update. Validation or
doctor failure restores both. Rollback revalidates the previous immutable
object and restores both authorities; recovery reconciles interrupted
transactions. A no-change update verifies Registry/pointer consistency instead
of silently succeeding. Discovery, staging, classification, smoke, and tests do
not execute repository hooks, installers, Makefiles, package lifecycle hooks,
scripts, or binaries.

## Managed-session Skill Gate

Before the official Codex or Claude UI receives the first substantive managed
session instruction, the local gate performs:

```text
bounded session intent
  -> local Registry search
  -> metadata-only rank
  -> dependency/conflict resolution
  -> IVOAI Policy Engine
  -> select 0..N
  -> verify active pointer/provenance/content
  -> load only selected full documents
  -> bounded executor instruction
```

The gate performs no network request. An absent or empty Registry produces a
normal zero-skill selection. A corrupt Registry, invalid policy, missing active
object, pointer divergence, or content race activates no external skill and is
observable as degraded; an explicitly required skill fails its operation.
Policy decisions use only IVOAI-owned metadata. Content loaded after approval
is still labelled untrusted and cannot change capability, risk, policy, or
orchestration authority.

## Curated upstream overlay

`internal/skillcatalog/catalog.json` records a bounded pre-triage of the 13
named upstream sources. It keeps three layers separate:

1. upstream-provided name and description;
2. IVOAI-observed repository, default branch, license, commit, and digest;
3. IVOAI-owned domain, triggers, phase, role, conflicts, risk, requested
   capabilities, and executor compatibility.

The catalog does not vendor full third-party bodies. A classifier accepts only
the reviewed commit and selected file digest. Updating an upstream commit
therefore requires a reviewed catalog refresh before automatic promotion.
Visual-direction packs share an exclusive role so the graph rejects competing
directors. Ponytail remains an implementation skill, i-have-adhd an interaction
profile, and selected Superpowers/Caveman entries remain ordinary skills.
Shell-capable packs are denied by default and tool-using security packs require
approval; neither Codex Security ToolProvider nor Caveman compression is
introduced here.

Repository revisions and license observations in the catalog are point-in-time
provenance, not claims that an upstream will remain unchanged. Runtime discovery
must resolve the current default branch and immutable revision again before a
future refresh.

## Diagnostics and observability

`ivoai status` reports the registry as ready/empty or shows bounded lifecycle
counts. `ivoai doctor` and its JSON form add a `skill_control_plane` object with
registry readability/writability/schema, active/staged/quarantined counts,
provenance health, policy readiness, and supply-chain staging-root health.

The existing observability allowlist now supports registry discovery,
candidate ranking, quarantine, conflict resolution, policy decisions, source
resolution, staging, promotion, and rollback. Events may include canonical
skill/artifact IDs, immutable revision, risk, lifecycle, decision, trust, and
bounded reasons. They cannot contain skill bodies, prompts, scripts, README
content, raw external files, credentials, headers, or environments.

## Deferred work

This block intentionally does not:

- execute skill hooks or third-party setup;
- implement the complete approval UX;
- activate catalog entries before secure local materialization and policy;
- replace Headroom, Ruflo, Context, or an executor;
- implement Codex Security ToolProvider or Caveman CompressionProvider.

The security analysis and automated adversarial boundaries are documented in
[skill-control-plane-threat-model.md](skill-control-plane-threat-model.md).
