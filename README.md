# ivoai

```text
██╗██╗   ██╗ ██████╗  █████╗ ██╗
██║██║   ██║██╔═══██╗██╔══██╗██║
██║██║   ██║██║   ██║███████║██║
██║╚██╗ ██╔╝██║   ██║██╔══██║██║
██║ ╚████╔╝ ╚██████╔╝██║  ██║██║
╚═╝  ╚═══╝   ╚═════╝ ╚═╝  ╚═╝╚═╝
```

**One host-first runtime for Codex, Claude, persistent AI memory, and private RAG.**

ivoai installs and coordinates Codex CLI, Claude Code, Headroom, ai-memory, and
Ruflo without requiring pay-as-you-go provider keys. The same Go binary can operate
an optional private server and expose its memory and context to desktop agents,
ChatGPT Web, and Claude Web.

## How it fits together

```text
Desktop / notebook                         Private Linux server

ivoai menu ── Headroom ── Codex/Claude       HTTPS gateway
    │             └────── direct fallback       ├── OAuth + Web MCP
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

Logins use the official Codex and Claude flows. ivoai never asks for or stores their
passwords, cookies, or provider OAuth tokens.

## Terminal experience

The menu adapts to terminal width and height, including resize events. Use arrow
keys or `j`/`k`, Enter to select, Esc to return, and `q` to exit. Long operations
show a spinner, elapsed time, or a download bar; success, warnings, and failures use
consistent semantic colors.

Non-interactive terminals receive a numbered fallback. `NO_COLOR=1` disables ANSI,
and `IVOAI_ASCII=1` selects ASCII-safe rendering. Progress goes to stderr, so stdout
and `ivoai doctor --json` remain suitable for automation.

## Server quick start

Ubuntu 22.04+, Ubuntu 24.04+, and Debian 12 are supported on amd64 and arm64:

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
[SKILL.md](skills/ivoai-memory-context/SKILL.md). The skill strongly prefers checking
ivoai for project-dependent answers, while final tool selection remains controlled
by the Web platform.

## Useful commands

```sh
ivoai status                 # concise client readiness
ivoai doctor                 # human-readable diagnostics
ivoai doctor --json          # stable automation output
ivoai update                 # explicit, configuration-preserving update
ivoai project init           # optional project-specific identity
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
