# Production compatibility contract

This contract applies to every stable IVOAI release starting at `v0.5.0`.
Its purpose is to keep existing client and server installations upgradeable in
place. A clean host, deleted XDG state, or repeated provider login is not an
acceptable migration strategy.

## Guarantees

1. `v0.5.0` has a supported in-place path to the next stable release.
2. Every stable `N` release has a tested path to stable `N+1`.
3. When feasible, the current release accepts `v0.5.0` state and applies every
   intermediate migration in order.
4. New features do not require deleting IVOAI configuration, data, state, or
   cache roots.
5. Codex and Claude authentication remains owned by the official clients and
   is never migrated, copied, or cleared by an IVOAI update.
6. IVOAI-managed data and component metadata are migrated; unmanaged files and
   third-party installations are preserved.
7. An interrupted update leaves a private journal and is recovered before a
   subsequent update starts.
8. Completed steps are never silently replayed. Steps are ordered, validated,
   and either idempotent or protected by transaction state.
9. Every schema step declares source, target, precondition, apply, validation,
   and rollback behavior.
10. A candidate without an explicit reversible path is rejected before any
    managed file changes.
11. A failed migration, setup, or post-update Doctor restores the prior binary
    and the exact compatible managed-file snapshot.
12. Rollback is idempotent and followed by the Doctor appropriate to client or
    server mode.

## Transaction

`ivoai update` performs these phases:

```text
PREPARE
  release/checksum/archive validation
  candidate version + compatibility probe
  source/target schema validation
  path, permission, size and free-space preflight
  private exact-file snapshot
PROMOTE
  atomic promotion of the recovery-capable target binary
MIGRATE
  target candidate runs its ordered migration registry
SETUP
  target-owned managed component and runtime reconciliation
VERIFY
  version/load/setup/Doctor validation
COMMIT
  committed journal and bounded snapshot retention
```

Any failure before commit enters `rolling_back`, verifies every snapshot digest,
atomically restores files and modes, removes optional files that did not exist
before the update, restores the executable, and records `rolled_back`.

The target binary is promoted before migration so interruption recovery is
always performed by code that understands the target journal. The old binary is
already present in the exact snapshot and in the legacy rollback slot. Any
failure before commit restores it.

`ivoai update --dry-run` performs release download, checksum verification,
candidate/schema compatibility probing, and read-only path, permission, size and
free-space preflight. It stages and executes the checksum-verified candidate for
bounded `version`/metadata probes, but does not execute migration steps, create a
transaction journal, or commit managed-state changes. Treat it with the same
release-channel trust decision as an update. `ivoai update --rollback` restores
the current rollback checkpoint. It refuses to overwrite managed files changed
after commit unless the operator explicitly supplies `--force`. The legacy
`ivoai.previous` binary remains supported for an update originating from v0.5.0.

## Owned data boundary

The snapshot allowlist contains the IVOAI executable, main config, state,
ownership manifest, matching managed component files inside the IVOAI data root,
and explicit server configuration/service assets. It never includes an entire
home directory, `~/.codex`, Claude configuration/authentication, cookies, provider
OAuth tokens, raw environment, or externally managed component paths.

The update root and snapshot directories are `0700`; journals and regular-file
snapshots are `0600`. Paths must be absolute, contained by their declared managed
root, and pass non-symlink regular-file checks. Each snapshot has size, mode,
owner, group and SHA-256 metadata.
The aggregate snapshot is capped at 1 GiB by default, free space is checked with
a reserved margin, and the current checkpoint is the sole rollback target.
Durable live server stores (Qdrant, memory, enrollment and Web OAuth) are not
blindly copied during an update. A future schema migration for one of those stores
must add an explicit quiesced transactional participant or be rejected as not
rollback-safe.

## Schema and unknown fields

Config, state and ownership schemas are independently versioned. The current
foundation intentionally remains at schema 1, making v0.5.0 to this candidate a
no-op schema migration. Future releases add reversible ordered steps to the one
target-owned migration registry.

The published v0.5.0 updater predates the journal protocol: it validates and
promotes the candidate, then invokes plain `ivoai setup`. This foundation keeps
that entry path compatible, auto-detects an existing server marker, and can
consume v0.5.0's legacy rollback binary. Because this release does not bump a
schema, the client and server bridges are exercised hermetically. Any future
release that bumps a schema while still
accepting a direct jump from v0.5.0 must retain and test a transactional legacy
entry bridge; metadata understood only by the old binary cannot retroactively
protect that first promotion.

TOML saves use a typed projection merged into the existing raw document. Unknown
tables and fields survive round trips. Dynamic maps such as MCP servers and
component ownership are authoritative for known entries, so an explicit removal
does not resurrect an old entry. Loading a future unsupported schema fails closed.

## Progressive architecture changes

Future large changes follow coexistence, disabled/shadow/canary validation,
default promotion, an observation release, and only then legacy removal. This
foundation does not add OpenCode, OpenViking, Caveman, NativeOrchestrator, or
remove Headroom/Ruflo.
