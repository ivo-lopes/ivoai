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

## ADR: native model selection without provider login duplication

Status: accepted.

The managed OpenCode provider publishes a model catalogue derived from the IVOAI
executor registry. OpenCode's native model picker and variant support remain the
only selection UI. Opaque catalogue IDs resolve inside the loopback bridge to an
executor, its native model ID and a validated reasoning/effort value. `auto` keeps
the scheduler authoritative; an explicit entry is fail-closed and never silently
falls back to a different model.

Codex and Claude still execute through their official CLIs and continue to own
authentication. No access token, refresh token, cookie, or provider credential is
copied into OpenCode. The non-sensitive requested/effective selection is persisted
with the IVOAI↔OpenCode session mapping so a resumed conversation retains the
operator's choice.

An OpenAI-compatible provider remains appropriate here because it is a private UI
transport between two loopback processes, not a replacement for either executor's
agent contract. The bridge translates only the selected model, reasoning effort,
prompt stream, cancellation, and completion state into the existing executor
contracts.

## ADR: federated MCP reads preserve the tool-result envelope

Status: accepted.

Multi-server read federation is an IVOAI routing concern, but the response still
crosses an MCP `tools/call` boundary. Consequently the router must return a valid
`CallToolResult`; a JSON-RPC object containing a custom federation object directly
under `result` is not protocol-conformant. The router now returns one deterministic
text content item containing the bounded federation object, including provenance
for every source. It deliberately does not attach a foreign `structuredContent`
shape to an upstream tool whose declared output schema describes only one source.

This representation is validated with the official Go MCP SDK and works for both
single-source pass-through and multi-source reads. It keeps write routing unchanged:
federation of reads never implies broadcast writes.

Implementation history and detailed tradeoffs remain in the repository history and
the architecture document.
