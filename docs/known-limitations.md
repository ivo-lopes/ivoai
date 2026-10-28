# Known limitations

- OpenCode remains an explicit deprecated legacy frontend/executor. Native managed
  Codex sessions and Codex/Claude workers exclude it even when installed. Legacy
  release assets and updater rollback participants remain; global removal is pending.
  Personal custom provider/plugin/MCP configuration is not imported into controlled
  sessions. Native subagents and separate question dialogs are intentionally disabled.
- The managed bridge displays executor text streaming and session state. Native
  provider-specific tool animation from the hidden executor CLI is not reproduced
  as a second nested TUI.
- OpenViking and NativeOrchestrator v2 are future work and are not defaults.
- Ruflo remains available for explicit legacy orchestration; Codex/OpenCode use the native DAG core.
- Headroom remains available for compatibility and rollback.
- Conversation Continuity is available; native histories deleted by older ephemeral
  frontends cannot be reconstructed. Ambiguous writes require manual reconciliation.
  See [continuity limits](conversation-continuity.md). Native AUTO handoff to Claude
  and initial `ivoai codex --planner claude` fail closed until a managed native
  frontend contract is verified. Direct Claude remains available; no native Claude
  TUI parity is claimed. The legacy OpenCode console is not a native-client requirement.
- Remote Web MCP requires a publicly reachable HTTPS origin or the platform's supported secure tunnel.
