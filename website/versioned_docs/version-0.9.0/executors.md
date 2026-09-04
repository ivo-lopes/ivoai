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

The managed provider is a local IVOAI bridge. It selects `CodexExecutor` or
`ClaudeExecutor` and runs the corresponding official CLI with its existing native
login. No Codex or Claude token is read, copied, converted, or placed in OpenCode.
The bridge preserves streaming, cancellation, bounded quota failover, and an opaque
mapping between OpenCode and executor conversation IDs.

To intentionally use OpenCode's own providers outside this bridge, either run
`opencode` directly or use:

```bash
ivoai session start --executor opencode --mode direct -- <upstream-options>
```

That standalone path retains OpenCode-owned authentication and is distinct from the
OpenCode-first AUTO frontend.
