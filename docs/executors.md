# Executors

## Codex

`ivoai codex` launches the official client and preserves its ChatGPT subscription login.

## Claude Code

`ivoai claude` launches the official client and preserves its native subscription login.

## OpenCode

`ivoai auto` and `ivoai opencode` launch the pinned OpenCode TUI as the managed IVOAI
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
`ivoai auto` or `ivoai opencode` session, not to an already running backend.
`/ivoai` displays the effective session permission mode. `full` pre-approves
OpenCode's configurable operations; direct reads of `.env` and `.env.*` remain
denied (`.env.example` is allowed). This is an approval policy, not a secret sandbox.
It does not change executor sandboxes, Unix permissions, Skill Gate, MCP policy,
write routing, or credential isolation. Executor-owned approvals remain independent.
Only the private managed overlay changes; personal and project OpenCode configuration
and authentication stores are not edited. No additional provider login is needed.

The managed provider is a local IVOAI bridge. It selects `CodexExecutor` or
`ClaudeExecutor` and runs the corresponding official CLI with its existing native
login. No Codex or Claude token is read, copied, converted, or placed in OpenCode.
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
copying its store into the managed frontend. This adapter alone does not make
OpenCode an eligible AUTO worker; the scheduler eligibility contract still applies.

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
Memory/Context, Skill Gate and executor policy remain in effect. This is not a
silent switch or a second execution. Fallback is refused after a request claim or
when another managed frontend holds the exclusive lease. Resolve the active
session first, then retry; do not delete its lock to force a second writer.
