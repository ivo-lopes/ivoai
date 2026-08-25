# ivoai

```text
██╗██╗   ██╗ ██████╗  █████╗ ██╗
██║██║   ██║██╔═══██╗██╔══██╗██║
██║██║   ██║██║   ██║███████║██║
██║╚██╗ ██╔╝██║   ██║██╔══██║██║
██║ ╚████╔╝ ╚██████╔╝██║  ██║██║
╚═╝  ╚═══╝   ╚═════╝ ╚═╝  ╚═╝╚═╝
```

[![Current release](https://img.shields.io/github/v/release/ivo-lopes/ivoai?label=version&color=8b5cf6)](https://github.com/ivo-lopes/ivoai/releases/latest)

**One host-first runtime for Codex and Claude Code, with persistent AI memory and private RAG.**

ivoai installs and coordinates Codex CLI, Claude Code, Headroom, ai-memory, and
Ruflo without requiring pay-as-you-go provider keys. The same Go binary can operate
an optional private server and expose its memory and context to desktop agents,
ChatGPT Web, and Claude Web.

## How it fits together

```text
Desktop / notebook                         Private Linux server

ivoai auto ── quota/capability gate ── Codex/Claude TUI
    │          ├── Headroom / direct fallback    │
    │          └── safe Ruflo swarm + workers    │
ivoai menu ── Session Control                   HTTPS gateway
    │                                            ├── OAuth + Web MCP
    │ HTTPS + scoped credential                 ├── context/RAG ── Qdrant
    └───────────────────────────────────────────└── ai-memory
```

Optional services fail independently: an unavailable server, memory service,
context index, Headroom, or Ruflo does not prevent the basic agents from opening.

## Client quick start

```sh
curl -fsSL https://raw.githubusercontent.com/ivo-lopes/ivoai/main/install.sh | sh
ivoai setup
```

Then run `ivoai` to open the responsive interactive menu, or connect and launch from
subcommands:

```sh
ivoai connect chatgpt
ivoai connect claude
ivoai connect server
ivoai codex
ivoai claude
```

Direct agent commands are unchanged. For observable or delegated work, use explicit
session modes:

```sh
ivoai session start --executor codex --mode direct
ivoai session start --executor codex --mode orchestrated
ivoai monitor --watch
```

For the quota-aware conversational mode, run:

```sh
ivoai auto                         # choose the primary; Codex is the default
ivoai auto --planner codex         # official Codex TUI
ivoai auto --planner claude        # official Claude Code TUI
```

The selected official client remains the conversation owner, planner, and primary.
Before opening it, ivoai reads subscription telemetry through Codex app-server and
Claude's session-local structured statusline, then routes around a hard exhausted
provider. The same gate runs before every worker. During a session, a hard limit can
trigger a bounded checkpoint/handoff to the other official TUI. Before Claude's
first response, missing rate-limit telemetry is `awaiting first response`; a field
observed missing later is `N/A / not exposed`, and an old value is labelled `stale`.
None of these states is treated as zero or invented.

Orchestrated mode proves a real safe Ruflo swarm before opening the official primary
client. Ruflo records only ephemeral lifecycle metadata; official Codex/Claude
clients perform inference with their existing subscription logins. No PAYG provider
key is passed to Ruflo or required by ivoai.

Logins use the official Codex and Claude flows. ivoai never asks for or stores their
passwords, cookies, or provider OAuth tokens.

## Terminal experience

The menu adapts to terminal width and height, including resize events. Use arrow
keys or `j`/`k`, Enter to select, Esc to return, and `q` to exit. Long operations
show a spinner, elapsed time, or a download bar; success, warnings, and failures use
consistent semantic colors.

Non-interactive terminals receive a numbered fallback. `NO_COLOR=1` disables ANSI,
and `IVOAI_ASCII=1` selects the plain-text `ivoai` fallback instead of alternate
lettering. Progress goes to stderr, so stdout
and `ivoai doctor --json` remain suitable for automation.

## Server quick start

Ubuntu 22.04+, Ubuntu 24.04+, and Debian 12 are supported on amd64 and arm64:

Install Docker Engine 28.0.0 or newer first from Docker's official repository.
Server setup validates the daemon and also requires Docker Compose 2.33.1 or newer;
it installs the project's pinned Compose plugin when needed.

```sh
curl -fsSL https://raw.githubusercontent.com/ivo-lopes/ivoai/main/install.sh | sudo sh
sudo ivoai setup --mode server
sudo ivoai server gateway configure --public-url https://ai.example.com
sudo ivoai server doctor
sudo ivoai server enrollment create
```

The gateway listens on loopback by default. Put it behind an HTTPS reverse proxy;
Qdrant, embeddings, and ai-memory are not exposed publicly. See the
[server guide](docs/server.md) for Nginx Proxy Manager and remote-proxy settings.

## ChatGPT Web and Claude Web

The server publishes a unified Streamable HTTP MCP at `https://ai.example.com/mcp`.
It exposes read-only context tools and scoped memory tools through OAuth 2.1 with
PKCE. Create a short-lived, one-time browser activation code:

```sh
sudo ivoai server web-access create --ttl 10m
```

Add the `/mcp` URL as a custom connector in ChatGPT Web or Claude Web, complete OAuth
in the browser, and enter the activation code when prompted. Access can be inspected
or revoked without revealing tokens:

```sh
sudo ivoai server web-access list
sudo ivoai server web-access revoke <id>
```

The server also advertises the `ivoai-memory-context` skill. A release contains
`ivoai-memory-context.zip` for direct import as a custom Claude skill; its source is
[SKILL.md](skills/ivoai-memory-context/SKILL.md). The skill uses the fixed research
order **memory → Context → web**. The Web platform still retains final tool-selection
control, so MCP initialization instructions, tool descriptions, and the imported
skill all repeat the same priority without weakening platform security policy.

## Useful commands

```sh
ivoai status                 # concise client readiness
ivoai doctor                 # human-readable diagnostics
ivoai doctor --json          # stable automation output
ivoai update                 # explicit, configuration-preserving update
ivoai project init           # optional project-specific identity
ivoai session list           # non-sensitive session metadata
ivoai monitor --watch        # primary, swarm, workers, services
ivoai auto --planner codex   # automatic quota-aware conversation
ivoai server status          # local server services
ivoai server backup          # authoritative data backup
```

## Security

Secrets are separated from TOML configuration and stored in `0700` directories with
`0600` files. Enrollment and Web activation codes are hashed, expire, and work once.
OAuth tokens are scoped and revocable. Logs redact authentication material. RAG
documents are untrusted data, and the gateway never offers arbitrary host-command
execution.

## Documentation

- [Architecture](docs/architecture.md)
- [Client guide](docs/client.md)
- [Direct and orchestrated sessions](docs/orchestration.md)
- [Automatic orchestration](docs/auto-orchestration.md)
- [Quota routing](docs/quota-routing.md)
- [Server and reverse proxy](docs/server.md)
- [Connections and Web MCP](docs/connections.md)
- [Security model](docs/security.md)
- [Development](docs/development.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Relatório executivo (PT-BR)](docs/relatorio-executivo.md)
- [Relatório técnico (PT-BR)](docs/relatorio-tecnico.md)

For a private source checkout, clone with authenticated GitHub access, run
`./install.sh`, and then run the appropriate setup command.

Licensed under the [MIT License](LICENSE).
