# Multi-server knowledge sources

One ivoai client can keep multiple independent ivoai servers enrolled at the
same time. Each `ServerProfile` has an opaque stable ID, a human alias, a
purpose, optional redundancy group, priority, discovery endpoints and bounded
feature metadata. Its scoped credential is stored separately under the opaque
ID; tokens never appear in TOML, status, Doctor, session state or agent
configuration.

Purpose and redundancy have different meanings:

- different purposes are independent knowledge domains and never fan out or
  receive each other's writes implicitly;
- members of one redundancy group represent equivalent sources for one purpose.
  Lower priority numbers are tried first for reads, with bounded health/circuit
  failover. Writes are primary-only and are never retried automatically after an
  uncertain failure.

## Voicecorp and Mindsite example

Use synthetic aliases and your own HTTPS origins and one-time enrollment codes:

```sh
printf '%s\n' "$VOICECORP_ENROLLMENT_CODE" | \
  ivoai connect server add voicecorp \
    --url https://voicecorp.example.invalid --purpose voicecorp --code-stdin

printf '%s\n' "$MINDSITE_ENROLLMENT_CODE" | \
  ivoai connect server add mindsite \
    --url https://mindsite.example.invalid --purpose mindsite --code-stdin

ivoai connect server list
ivoai connect server show mindsite
ivoai connect server test mindsite
ivoai doctor
```

Select one source without disconnecting the other:

```sh
ivoai codex --knowledge-source mindsite
ivoai claude --knowledge-source voicecorp
ivoai auto --planner codex --knowledge-source mindsite
ivoai session start --executor claude --mode orchestrated \
  --knowledge-source voicecorp
```

With no flag, all enabled connected profiles participate automatically in bounded
read federation. A newly enrolled enabled profile is included in future unfiltered
sessions without changing a special `default` alias:

```sh
ivoai auto
```

Supply the flag to restrict the session. Repeat it, or use a comma-separated value,
to select an exact subset:

```sh
ivoai codex \
  --knowledge-source mindsite \
  --knowledge-source voicecorp
```

Federated `tools/call` reads run concurrently with individual deadlines and a
bounded aggregate result. Each entry keeps `source_id`, alias, purpose and
redundancy metadata; identical document paths from different sources remain
distinct. A partial timeout or malformed source is visible instead of being
reported as total success. A write across multiple purposes, or across two
independent destinations with the same purpose, fails explicitly.

An unavailable source in automatic mode is reported as a partial/degraded result;
healthy sources still return. An unavailable explicitly selected source fails the
selection instead of silently substituting another purpose. Automatic federation is
read-only semantics: a new Memory write with multiple possible destinations fails
as `WRITE_DESTINATION=AMBIGUOUS` rather than broadcasting.

Disconnect is selective:

```sh
ivoai disconnect server mindsite
ivoai connect server list       # voicecorp remains
ivoai disconnect server --all   # explicit bulk operation
```

## Session isolation

Every selected session receives a private loopback router on `127.0.0.1` and a
random short-lived local capability. Codex and Claude see only the process-local
`ivoai-memory` and `ivoai-context` endpoints allowed for that session. The router
holds upstream credentials in memory and attaches each token only to its matching
opaque server ID. It rejects cross-origin redirects, caps requests at 4 MiB and
responses at 16 MiB, revokes the local capability on close, and never rewrites a
global agent MCP configuration to switch organizations.

ai-memory lifecycle hooks use the same session-local router. Concurrent Voicecorp
and Mindsite sessions therefore remain independent. AUTO keeps the same selection
through Codex/Claude failover, and advisory workers inherit the local endpoints,
not upstream tokens.

The router preserves authoritative single-source MCP responses. Federated results
add the source envelope needed for attribution. WorkingContext,
ArtifactStore and exact-required fidelity rules remain independent and unchanged.

## Redundancy example

```sh
ivoai connect server add mindsite-1 --url https://one.example.invalid \
  --purpose mindsite --redundancy-group mindsite-production --priority 10 \
  --code-stdin
ivoai connect server add mindsite-2 --url https://two.example.invalid \
  --purpose mindsite --redundancy-group mindsite-production --priority 20 \
  --code-stdin
ivoai codex --knowledge-source mindsite
```

This is deterministic primary/standby read failover, not quorum or replication.
No failover crosses a purpose boundary, no profile failure removes another profile,
and a failed write is not silently replayed.

## Legacy compatibility

The published single-server config remains readable. It normalizes to the stable
`default` profile and the old credential becomes the entry for
`srv_legacy_default`; no reenrollment is required. Config/state/ownership remain at
schema 1. The private secret store is independently versioned at schema 2, retains
the legacy `server` rollback mirror only for `default`, participates in the update
transaction snapshot, and has a reversible 1→2 migration.
