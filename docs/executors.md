# Executors

## Codex

`ivoai codex` opens the official native Codex TUI through the IVOAI App Server
façade. The same Core admits natural prompts, controls permissions and approves
plans. The adaptive Primary is experimental and off by default. During controlled
certification, `IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY=1` permits eligible read-only
requests without a DAG on reviewed runtime pairs/platforms; complex work uses the
existing scheduler and workers. `ivoai auto`
is a compatibility alias for this managed Codex path. `ivoai codex --direct`
starts the official client without managed turn orchestration. See
[frontends](orchestrated-frontends.md).

Authentication remains owned by the official client. IVOAI does not copy
subscription credentials or silently substitute a paid API. Reviewed runtime
versions/platforms and feature-flag rollout remain explicit; consult the release
notes for actual inference and native TUI certification.

## Claude Code and Antigravity

`ivoai claude` starts the official direct Claude TUI. Managed headless execution
uses `ivoai claude run --prompt ...` with `IVOAI_EXPERIMENTAL_CLAUDE_HEADLESS=1`.
The flag is off by default; the current reviewed host is Claude Code 2.1.318 on Linux
amd64. Official inference certification remains pending an authorized user
account; offline protocol checks do not provide it.

`ivoai antigravity run --prompt ...` requires `IVOAI_EXPERIMENTAL_ANTIGRAVITY=1`
and the reviewed `agy` 1.5.2 binary on PATH, currently limited to Linux amd64.
This experimental ACP host validates the exact executable identity; a matching
version banner alone is insufficient. The existing official authentication is
used without copying subscription credentials or adding a paid-API fallback.

These headless hosts share Core admission, ownership, permissions and the existing
planning protocol. They do not control the native TUIs or claim universal parity.
Follow-up turns supported within a live host are distinct from cross-process
resume; unsupported planner/handoff or resume paths remain explicit refusals.

## Managed OpenCode removed

`ivoai opencode`, its `--direct` mode, OpenCode session starts, planner selection,
frontend switches and `ivoai setup --legacy-opencode` now return explicit migration
errors before dispatch or installation. Default and injected installer catalogs
cannot reinstall the component. Doctor displays retained state without probing
or executing the old binary. Administrative launch actions explain the removal.

Use `ivoai codex` for new managed conversations. Retained OpenCode sessions and
checkpoints remain readable as history; this version does not replay or silently
reroute their saved tasks. A new native session can use explicitly reviewed
context. The existing compatibility types/config keys remain readable for
rollback; their names do not grant a legacy execution capability.

The legacy `opencode.permission_mode` configuration key remains the existing Core
tool-approval setting for compatibility. It does not restore OpenCode, change
executor sandboxes, grant worker writes or bypass native approval requirements.
No personal OpenCode configuration, authentication, executable, profile or history
is deleted, rewritten or adopted. Users may run their independently installed
upstream OpenCode themselves, outside IVOAI's managed runtime.

For rollback, use the transactionally retained previous Core/profile and its
original verified release assets. Do not rebuild old tags, substitute a current
asset set into an old release, remove history namespaces or delete lock files.
New request journals and protected evidence retain their existing conservative
rollback boundaries. See [update and rollback](update-rollback.md).
