# ivoai

ivoai is a host-first runtime that installs and coordinates Codex CLI, Claude Code,
Headroom, ai-memory, and Ruflo without requiring pay-as-you-go API keys. The same Go
binary can run an optional private server for durable memory and read-only RAG.

## Architecture

```text
ivoai client ── Headroom ── Codex / Claude
      │          `─ direct fallback
      │ HTTPS + scoped token
      ▼
ivoai gateway ── context / memory
                       │
               embeddings + Qdrant
```

Client setup is useful before any external account or server is connected. A failed
Headroom preflight selects the direct agent; once a wrapper starts, its exit status is
preserved instead of starting a duplicate agent session.

## Quick start

```sh
curl -fsSL https://raw.githubusercontent.com/ivo-lopes/ivoai/main/install.sh | sh
ivoai setup
```

For a private repository, clone with authenticated GitHub access and run
`./install.sh`, then `ivoai setup`. A compatible system Go is used when available;
otherwise the installer downloads a pinned, checksum-verified Go toolchain only for
the source build and removes it afterward.

Connect accounts later through official login flows managed by their own clients:

```sh
ivoai connect chatgpt
ivoai connect claude
ivoai connect server
```

ivoai never asks for or stores OpenAI or Anthropic login credentials.

## Usage

Run `ivoai` for the interactive menu, or use subcommands:

```sh
ivoai status
ivoai doctor
ivoai doctor --json
ivoai codex
ivoai claude
```

## Server

On Ubuntu 22.04+, Ubuntu 24.04+, or Debian 12+:

```sh
curl -fsSL https://raw.githubusercontent.com/ivo-lopes/ivoai/main/install.sh | sudo sh
sudo ivoai setup --mode server
sudo ivoai server gateway configure --public-url https://ai.example.com
sudo ivoai server doctor
sudo ivoai server enrollment create
```

The gateway remains on loopback by default, ready for a reverse proxy on the same
host. A proxy on another private host can be authorized with `--listen` and a narrow
`--trusted-proxy` CIDR. Direct TLS is also supported; see the server guide. Clients need only the
HTTPS base URL and a short-lived, one-time enrollment code. Dependency ports bind to
loopback and are not publicly exposed.

## Security

Client secrets are separate from TOML configuration and use mode `0600` inside
directories with mode `0700`. Enrollment codes are hashed, expire, and are consumed
once. Logs and diagnostics redact authorization material. RAG documents are treated
as untrusted data and context MCP tools are read-only.

## Documentation

- [Architecture](docs/architecture.md)
- [Client](docs/client.md)
- [Server](docs/server.md)
- [Connections](docs/connections.md)
- [Security](docs/security.md)
- [Development](docs/development.md)
- [Troubleshooting](docs/troubleshooting.md)

Licensed under the [MIT License](LICENSE).
