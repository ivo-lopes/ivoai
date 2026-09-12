# Orchestrated frontends

## Commands

| Command | Admission and execution |
| --- | --- |
| `ivoai codex` | Native Codex TUI; IVOAI admission; Codex primary preference |
| `ivoai opencode` | Managed OpenCode TUI; primary selected by IVOAI policy |
| `ivoai auto` | Deprecated alias for `ivoai opencode` throughout v0.10.x |
| `ivoai codex --direct` | Official Codex client, no prompt gate/DAG |
| `ivoai opencode --direct` | Standalone OpenCode, no prompt gate/DAG |
| `ivoai claude` | Existing optional direct Claude interface |

Put `--direct` before `--`; arguments after `--` belong to the official client.
The provider-neutral `session start --executor codex|opencode --mode direct`
surface remains available. `--mode orchestrated` enters the shared native core.

## Native Codex terminal

Enter the first prompt directly in the official Codex composer. Use its native
multiline controls and press Enter to send. For example:

```text
Read VERSION in the current directory and report its value.
Acceptance: return only the version; do not modify files.
```

The native model/reasoning picker selects a runtime-verified Codex primary;
workers retain independent routing. Plan and quota decisions use native question
dialogs, not `/approve` commands. Interrupt, navigation, scrolling, selection and
the composer remain upstream UI. There is no IVOAI pre-composer.

The [official App Server protocol](https://learn.chatgpt.com/docs/app-server)
connects that TUI to an authenticated, process-local IVOAI façade. The façade
admits `turn/start` before forwarding it to the official App Server. A private
Responses adapter returns the shared orchestration core's final answer; it never
returns native tool calls. Shell/exec, review, steering, configuration writes and
unknown execution methods cannot bypass admission. Use `--direct` for those
upstream operations outside orchestration.

Codex 0.153.4 and 0.154.0 are tested. App Server transport is upstream-experimental;
the supported local protocol is pinned and tested, not an invented TUI API.
The frontend uses an ephemeral Codex home without copying account credentials.
Worker authentication stays with the official executors. Native conversation
navigation works within the session; persistent upstream resume/fork and global
settings changes are not exposed by this façade. IVOAI keeps sanitized session
and turn metadata, not a second transcript store.

Prompt rejection starts no worker and no substantive executor turn. Acceptance
criteria are mandatory in both orchestrated frontends. Full OpenCode permissions
do not bypass plan approval. Immediate start remains an explicit shared policy.

## One control plane

Both frontends use the same admission endpoint and native scheduler: purpose-auto,
bounded Memory/Context, DAG, plan decision, economic routing, host-aware concurrency,
per-worker Skills/Ponytail and MCP allowlists, isolated worktrees, integration,
acceptance verification and primary synthesis. After approval the scheduler queues
delegated nodes automatically; the primary need not issue a spawn command.

Codex is the preferred strong primary in `ivoai codex`. Workers can still use
Claude when eligible. Primary replacement requires a separate routing decision.
At 10% quota the shared conservation policy proposes worker savings; it does not
silently replace the primary. Explicit unavailable executor/model overrides fail
closed. Claude authentication is optional, not a setup requirement.

Status distinguishes `frontend`, `orchestration_mode`, `primary_provider` and
per-worker executor/model/reasoning. Legacy `mode` and `primary_executor` fields
remain. Progress contains metadata, not worker transcripts or credential stores.

## Compatibility and recovery

No re-enrollment, MCP re-authentication or provider login is required on upgrade.
Existing profiles, secret references, permission mode, native pack, Ponytail and
orchestration settings are preserved. `auto.*` configuration is not renamed.
Forward-only Codex frontend records use a private namespace in the same session
store so older binaries ignore them during rollback; reapply restores access.

The alias warning is emitted once on stderr, never in stdout. If the managed
OpenCode frontend cannot start, IVOAI fails clearly instead of launching an ungated
direct TUI. Choose `ivoai codex` for controlled orchestration, or `--direct`
explicitly when orchestration is not wanted. Existing executor sandbox, Skill Gate,
MCP deny-by-default and purpose isolation still apply in orchestrated mode.
