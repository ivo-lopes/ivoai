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
