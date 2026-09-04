# Architecture decisions

The current architecture is intentionally host-first and deep-module oriented:

1. Official executor clients own authentication and authoritative writes.
2. Server credentials are isolated by opaque stable server ID.
3. Session-local routers expose only selected knowledge sources.
4. Read federation is bounded; write routing is fail-closed when ambiguous.
5. Exact evidence is stored before any bounded projection or compression.
6. Caveman, Headroom, and Direct are mutually exclusive providers.
7. Remote Web MCP uses Streamable HTTP and OAuth 2.1 with PKCE.
8. OpenCode is the managed AUTO frontend, while IVOAI remains the session owner and
   routes work through official Codex/Claude CLI executor contracts.

## ADR: OpenCode-first AUTO without credential transfer

Status: accepted.

The evaluated integration choices were an OpenAI-compatible local provider bridge,
an attach-only compatibility backend, and an upstream TUI/server plugin. The selected
design combines supported OpenCode surfaces: `serve`/`attach`, an exact-version TUI
plugin, a small server plugin for session correlation, and a loopback
OpenAI-compatible provider implemented by IVOAI. The provider protocol is only the UI
transport; the actual executor contract remains the official Codex or Claude CLI.

This preserves streaming, cancellation, quota failover, tool access, Memory/Context,
WorkingContext and IVOAI single-writer policy without copying provider credentials or
placing a second scheduler in authority. Managed OpenCode uses isolated configuration,
turns off share and auto-update, ignores untrusted project config, and binds only to
loopback. A native OpenCode provider session remains available explicitly outside the
AUTO control plane.

Maintaining a downstream OpenCode fork was rejected. The IVOAI lettering, status
panel, commands and theme use official plugin and slot APIs, retaining upstream
license and attribution.

Implementation history and detailed tradeoffs remain in the repository history and
the architecture document.
