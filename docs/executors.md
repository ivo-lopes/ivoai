# Executors

## Codex

`ivoai codex` opens the native Codex TUI through an IVOAI-owned App Server façade.
Prompt admission happens at the protocol boundary before execution, not through an
advisory instruction. Codex is the preferred strong primary; eligible Claude
workers remain available. `ivoai codex --direct` opens the official Codex TUI
without the prompt gate or automatic DAG. Both preserve official authentication.
See [Orchestrated frontends](orchestrated-frontends.md) for compatibility boundaries.

## Claude Code

`ivoai claude` launches the official client and preserves its native subscription login.

## OpenCode

`ivoai opencode` (and deprecated alias `ivoai auto`) launches the pinned OpenCode TUI as the managed IVOAI
frontend. The OpenCode backend listens only on an authenticated random loopback port.
Its isolated managed configuration disables project configuration, sharing, and
OpenCode auto-update; direct `opencode` use outside IVOAI remains untouched.

### Managed approval policy

New and existing configurations without an override use `interactive`: ordinary
file reads/searches are allowed; shell, edits, external-directory access and other
operations require OpenCode approval. Configure the policy once in IVOAI:

```bash
ivoai config set opencode.permission_mode full
ivoai status
ivoai config set opencode.permission_mode interactive
```

The last command restores interactive approvals. Changes apply to the next managed
`ivoai opencode` session, not to an already running backend.
`/ivoai` displays the effective session permission mode. `full` pre-approves
OpenCode's configurable operations; direct reads of `.env` and `.env.*` remain
denied (`.env.example` is allowed). This is an approval policy, not a secret sandbox.
It does not change executor sandboxes, Unix permissions, Skill Gate, MCP policy,
write routing, or credential isolation. Executor-owned approvals remain independent.
Only the private managed overlay changes; personal and project OpenCode configuration
and authentication stores are not edited. Codex/Claude bridge execution needs no
additional provider login; optional native OpenCode execution requires its own
official provider authentication.

The managed provider is a local IVOAI bridge. It selects `CodexExecutor`,
`ClaudeExecutor`, or an eligible native `OpenCodeExecutor`. Codex and Claude run
through their official CLIs with existing native login. No Codex or Claude token
is read, copied, converted, or placed in OpenCode.
The bridge preserves streaming, cancellation, bounded quota failover, and an opaque
mapping between OpenCode and executor conversation IDs.

The native OpenCode model picker contains one scheduler entry plus the runtime
catalog discovered from the official clients. Explicit entries carry only verified
reasoning variants. The selected model and effort are passed to `codex exec` or
`claude --print`; unsupported or stale selections are rejected before an executor is
launched. Returning to the automatic entry restores IVOAI quota-aware selection.

To intentionally use OpenCode's own providers outside this bridge, either run
`opencode` directly or use:

```bash
ivoai session start --executor opencode --mode direct -- <upstream-options>
```

That standalone path retains OpenCode-owned authentication and is distinct from the
OpenCode-first AUTO frontend.

### Controlled HTTP sessions

The internal `OpenCodeExecutor.OpenSession` contract reuses the authenticated,
loopback-only managed HTTP/SSE client. It supports create/get/list, synchronous and
asynchronous prompts, events, abort, status, diff, permissions, files, agents and
MCP status. Closing releases its backend and lease. Structured controlled
`StartSession` requests use this lifecycle; ordinary direct CLI requests still
launch the native TUI. Native execution uses OpenCode-owned authentication without
copying its store into the managed frontend. AUTO and advisory workers reuse this
same controlled lifecycle after the scheduler's eligibility checks.

### Native OpenCode eligibility in AUTO

Without a new preference, IVOAI preserves its existing Codex/Claude priority and
considers native OpenCode as a third candidate. To select it explicitly:

`ivoai opencode --planner opencode`

Explicit native selection fails closed if no authenticated, tool-capable native
model is available. The managed model picker also contains namespaced entries for
eligible native `provider/model` IDs and their discovered variants. No model,
reasoning level, subscription window, or unlimited quota is invented. Native quota
telemetry is **unknown**, separate from authentication and execution eligibility.

The native executor runs in a separate authenticated loopback backend, never
through the IVOAI provider recursively. Only the official OpenCode process accesses
its own authentication store. IVOAI consumes the official `auth list` metadata and
a bounded provider/model projection; credential-bearing upstream fields are
discarded before they reach the catalog, cache, diagnostics, or UI. Personal and
project configuration, plugins, and MCPs are not imported. Providers requiring
unprojected custom configuration are not advertised as available.

Native primary instructions include the approved Skill Gate instructions and the
IVOAI knowledge policy. Only session-local Memory/Context and the IVOAI orchestrator
are projected. Advisory workers receive the SharedContextBrief and can use only
read-only knowledge tools and read/search operations; `full` on the frontend does
not grant worker writes. Native subagents, unreviewed skill loading, and separate
native question dialogs are disabled: clarification belongs in the same frontend
conversation. Interactive native approvals are relayed to that frontend and grant
only the pending operation once; cancelling the dialog rejects the operation.

Cancellation aborts the native session and closes its backend. Verified rate-limit
signals permit bounded AUTO failover before final output, never against an explicit
model/executor selection or back to an executor already attempted in that turn.
Native rate limits trigger a short local cooldown, not an invented provider reset
time. Authentication continuity remains conservative: native resume IDs are not
reused without a proven identity/epoch.

Rollback keeps the v0.9.2 runtime readers usable: native quota telemetry is a
backward-compatible extension, and sessions containing native AUTO/worker
metadata live in a private session namespace ignored by older binaries. Reapply
reads that history again without changing its executor or conversation identity.
Older quota writers may discard unknown telemetry; discovery reconstructs it.

### Authentication continuity and resume

IVOAI reprobes the official executor before each managed turn. The current probes
do not expose a stable account identity, so the bridge conservatively starts a
fresh native executor conversation instead of reusing an unproven native resume ID.
The OpenCode conversation remains the frontend history. `/ivoai` reports this
resume policy. No credential file or secret hash is used to identify an account.
Where a trusted adapter supplies non-sensitive identity/epoch metadata, native
resume additionally requires an exact identity match. Logout or an unavailable
probe prevents dispatch; an identity change cannot reuse the previous native ID.

### Managed frontend unavailable

Before any request is dispatched, a failed backend startup/readiness or attach can
fall back to the selected executor's native TUI. IVOAI prints `AUTO_STATE=DEGRADED`
and identifies the native executor and loss of the OpenCode panel/model picker.
This recovery is available for Codex/Claude. A selected native OpenCode executor
fails closed if its managed frontend cannot start; IVOAI does not substitute an
unmanaged personal OpenCode TUI or another executor.
Memory/Context, Skill Gate and executor policy remain in effect. This is not a
silent switch or a second execution. Fallback is refused after a request claim or
when another managed frontend holds the exclusive lease. Resolve the active
session first, then retry; do not delete its lock to force a second writer.
