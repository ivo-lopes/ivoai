# Concepts

- **Executor:** a supported official native client, such as Codex, Claude Code or Antigravity that owns its login.
- **AUTO:** quota-aware planning and advisory-worker orchestration. The selected official client remains the authoritative writer.
- **Memory:** durable operational knowledge provided by ai-memory.
- **Context:** private indexed documents exposed through Context MCP.
- **Knowledge source:** one enabled `ivoai-server` profile with an isolated stable ID and credential.
- **Federation:** bounded read fan-out across selected sources. It never implies write replication.
- **WorkingContext:** transient bounded context; exact artifacts remain recoverable through `ResultRef`.
- **CompressionProvider:** one of Caveman, Headroom, or Direct. Providers are never chained.
