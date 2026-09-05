# Known limitations

- OpenCode is the managed AUTO frontend. OpenCode itself is not an AUTO worker;
  Codex and Claude Code remain the subscription-backed executor/worker contracts.
- The managed bridge displays executor text streaming and session state. Native
  provider-specific tool animation from the hidden executor CLI is not reproduced
  as a second nested TUI.
- OpenViking and NativeOrchestrator v2 are future work and are not defaults.
- Ruflo remains the bounded lifecycle orchestrator for current orchestrated modes.
- Headroom remains available for compatibility and rollback.
- Conversation Continuity and the complete monitor TUI remain planned.
- Remote Web MCP requires a publicly reachable HTTPS origin or the platform's supported secure tunnel.
