# IVOΛI

```text
██╗██╗   ██╗ ██████╗   ███╗  ██╗
██║██║   ██║██╔═══██╗ ██╔██╗ ██║
██║██║   ██║██║   ██║██╔╝╚██╗██║
██║╚██╗ ██╔╝██║   ██║██║  ██║██║
██║ ╚████╔╝ ╚██████╔╝██║  ██║██║
╚═╝  ╚═══╝   ╚═════╝ ╚═╝  ╚═╝╚═╝
```

[![Current release](https://img.shields.io/github/v/release/ivo-lopes/ivoai?label=version&color=8b5cf6)](https://github.com/ivo-lopes/ivoai/releases/latest)

**One host-first runtime for Codex and Claude Code, with persistent AI memory and private RAG.**

ivoai installs and coordinates Codex CLI, Claude Code, Caveman compression,
ai-memory, and Ruflo without requiring pay-as-you-go provider keys. Headroom remains
available temporarily as a compatibility and rollback provider. The same Go binary can operate
an optional private server and expose its memory and context to desktop agents,
ChatGPT Web, and Claude Web.

The **v0.16.0 release line** delivers natural managed Codex turns, owned
conversation continuity, normal native shutdown, and same-turn escalation through
the existing Core scheduler. See the [release notes](release-notes/v0.16.0.md)
and [real official-client evidence](release-notes/adaptive-primary-natural-turns.md).
Adaptive mode remains opt-in on the exact reviewed Codex 0.162.1 Linux amd64 pair;
general client/model parity and global economic savings remain unproven.
Historical [v0.15.0 publication verification](release-notes/v0.15.0-published-validation.md)
remains bound to that release's immutable source and assets.

## How it fits together

`ivoai codex` presents the official native TUI while IVOAI Core admits managed
turns, selects capabilities, owns planning/workers and validates delivery. Simple
eligible adaptive turns need no DAG. Approved complex changes use the same Core
scheduler and worker isolation. Claude Code retains its supported direct and
readonly worker boundaries; this does not imply transparent TUI parity.

```text
Codex official TUI ── managed adapter ── IVOAI Core
                                           │
                                  orchestration / validation
                                           │
                                  scoped evidence and tools
                                   ├── Code Intelligence
                                   ├── protected artifacts
                                   ├── Context / private RAG
                                   └── ai-memory
```

Conversation continuity uses a stable IVOAI session ID. The native `/resume`
picker and `ivoai session resume <id>` retain their existing guarded mappings.
Adaptive direct-turn certification currently covers eligible new conversations
within the same owner process; cross-process resume and prior DAG histories are
not newly certified. See [continuity and recovery](docs/conversation-continuity.md).

Optional services fail independently: an unavailable server, memory service,
context index, compression provider, or Ruflo does not prevent the basic agents from opening.

## Client quick start

```sh
curl -fsSL https://raw.githubusercontent.com/ivo-lopes/ivoai/main/install.sh | sh
ivoai setup
```

Minimal Debian/Ubuntu images also need `libatomic1` for the managed Node 26
runtime (`sudo apt-get install curl ca-certificates libatomic1`).

Then run `ivoai` to open the responsive interactive menu, or connect and launch from
subcommands:

**Connections → IVOAI Servers** manages independent profiles: add, list, test,
enable/disable, edit, re-enroll and selectively remove. Adding another alias never
replaces an existing profile. The legacy `default` profile remains optional.

```sh
ivoai connect chatgpt
ivoai connect claude
ivoai connect server                    # legacy default profile
ivoai connect server add voicecorp --url https://voicecorp.example.invalid --code-stdin
ivoai connect server add mindsite --url https://mindsite.example.invalid --code-stdin
ivoai connect server list
ivoai codex
ivoai claude
ivoai auto                     # compatibility alias for managed Codex
```

Current managed development uses the official Codex frontend and one IVOAI Core.
`ivoai auto` aliases `ivoai codex`; `ivoai codex --direct` remains explicit.
Managed OpenCode installation, repair, discovery, execution and release assets
have been removed. Old `ivoai opencode`/`--legacy-opencode` entrypoints return
migration guidance. Existing user binaries, configuration, credentials, profiles
and history remain intact for explicit rollback; the original published v0.16
assets are preserved. See [executors](docs/executors.md) for native headless
clients and compatibility boundaries.

```sh
ivoai session start --executor codex --mode direct
ivoai session start --executor codex --mode orchestrated
ivoai codex --knowledge-source mindsite
ivoai claude --knowledge-source voicecorp
ivoai monitor --watch
```

Named knowledge servers remain connected simultaneously. Purpose routing selects
relevant sources; explicit `--knowledge-source` flags restrict the session to
those aliases or purposes. Memory writes are never broadcast. Upstream tokens
remain inside IVOAI's session-local loopback router, keeping simultaneous
organization/repository scopes separate without rewriting global client config.
See [multi-server knowledge sources](docs/multi-server.md).

IVOAI owns the logical session and dispatch policy. The adaptive Primary can
answer simple admitted requests directly and escalate complex work through the
existing plan approval, scheduler and isolated workers. Runtime-verified model,
effort and observed quota gates remain authoritative; missing provider telemetry
is unknown, never an invented zero or quota. Native client approvals, executor
sandboxes and Core permissions remain independent of strategy selection.

The same provider-free Ruflo lifecycle control plane records ephemeral metadata;
official clients perform inference using their existing authorized logins.
IVOAI does not request or copy provider passwords, cookies or OAuth tokens, and
does not silently switch a subscription session to a paid API.

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

On Debian 12, server setup can provision Docker Engine from Docker's official signed
APT repository when it is absent. Server setup validates Engine 28.0.0 or newer and
Docker Compose 2.33.1 or newer;
it installs the project's pinned Compose plugin when needed.

```sh
curl -fsSL https://raw.githubusercontent.com/ivo-lopes/ivoai/main/install.sh | sudo sh
sudo ivoai setup --mode server
sudo ivoai server gateway configure --public-url https://ai.example.com
sudo ivoai server doctor
sudo ivoai server enrollment create
```

The same setup installs an embedded production build of this Docusaurus portal as
`ivoai-docs.service`, listening on `0.0.0.0:7780` by default. Put that LAN listener
behind your external Nginx Proxy Manager and restrict the port at the firewall.

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
ivoai doctor --inventory --json # secret-free production inventory
ivoai update --dry-run       # verify candidate and migration compatibility
ivoai update                 # transactional binary + managed-state update
ivoai update --rollback      # restore the prior compatible transaction
ivoai project init           # optional project-specific identity
ivoai session list           # non-sensitive session metadata
ivoai monitor --watch        # primary, swarm, workers, services
ivoai auto                     # native Codex alias with shared Core admission
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
- [Automatic scheduler and model routing](docs/auto-scheduler.md)
- [Quota routing](docs/quota-routing.md)
- [Skill Control Plane foundation](docs/skill-control-plane.md)
- [Server and reverse proxy](docs/server.md)
- [Connections and Web MCP](docs/connections.md)
- [Security model](docs/security.md)
- [Production compatibility contract](docs/production-compatibility.md)
- [Read-only production inventory](docs/production-inventory.md)
- [Two-production canary rollout](docs/canary-rollout.md)
- [Development](docs/development.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Relatório executivo (PT-BR)](docs/relatorio-executivo.md)
- [Relatório técnico (PT-BR)](docs/relatorio-tecnico.md)

For a private source checkout, clone with authenticated GitHub access, run
`./install.sh`, and then run the appropriate setup command.

Licensed under the [MIT License](LICENSE).
