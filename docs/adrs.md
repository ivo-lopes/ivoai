# Architecture decisions

The current architecture is intentionally host-first and deep-module oriented:

1. Official executor clients own authentication and authoritative writes.
2. Server credentials are isolated by opaque stable server ID.
3. Session-local routers expose only selected knowledge sources.
4. Read federation is bounded; write routing is fail-closed when ambiguous.
5. Exact evidence is stored before any bounded projection or compression.
6. Caveman, Headroom, and Direct are mutually exclusive providers.
7. Remote Web MCP uses Streamable HTTP and OAuth 2.1 with PKCE.

Implementation history and detailed tradeoffs remain in the repository history and
the architecture document.
