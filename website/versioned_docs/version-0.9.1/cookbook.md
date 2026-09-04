# Cookbook

## Install or update a client

```bash
curl -fsSL https://raw.githubusercontent.com/ivo-lopes/ivoai/main/install.sh | sh
ivoai setup
ivoai status
ivoai doctor

ivoai update --dry-run
ivoai update
```

Authentication stays in the official Codex, Claude Code, and OpenCode clients.
IVOAI never needs their provider tokens.

## Bootstrap a Debian 12 server, including LXC

```bash
curl -fsSL https://raw.githubusercontent.com/ivo-lopes/ivoai/main/install.sh | sudo sh
sudo ivoai setup --mode server
sudo ivoai server doctor
sudo ivoai server memory status
sudo ivoai server context status
sudo ivoai server docs status
```

On Debian 12, setup provisions a missing Engine from Docker's signed official APT
repository. If the diagnostic says Docker is unreachable inside LXC, enable nesting,
`keyctl`, and compatible cgroup/device access on the host, then rerun the same setup
command. Do not create `qdrant.env` or `memory.env` by hand.

## Enroll the first and second private server

On each server, create a different one-time code with `sudo ivoai server enrollment
create --ttl 10m`. On the client, pass each code through standard input:

## Add two servers and use all enabled sources

```bash
ivoai connect server add company-a --url https://ai-a.example.com --purpose company-a --code-stdin
ivoai connect server add company-b --url https://ai-b.example.com --purpose company-b --code-stdin
ivoai auto
```

Restrict a session with `--knowledge-source company-a`. Repeat the flag for an
intentional subset. Use `ivoai connect server test company-a` when one source is down.
Automatic mode keeps healthy sources and reports a degraded partial read. An
explicitly selected unavailable source fails instead of substituting another source.
New Memory writes are never broadcast; restrict the session to one destination when
the write target is ambiguous.

## Run executors and AUTO

```bash
ivoai codex
ivoai claude
ivoai opencode
ivoai auto
ivoai auto --planner codex
```

`ivoai auto` and `ivoai opencode` open the managed OpenCode frontend. IVOAI routes
turns to the official Codex or Claude Code CLI, so their existing subscription login
is reused without copying a provider token. For an unmodified standalone OpenCode
provider session, use `ivoai session start --executor opencode --mode direct`.

## Check Memory and Context

```bash
ivoai memory status
ivoai memory configure
sudo ivoai server memory status
sudo ivoai server context status
sudo ivoai server connector list
```

To add a reviewed local corpus:

```bash
sudo ivoai server connector add --name handbook --type filesystem --path /srv/handbook
sudo ivoai server context status
```

## Select compression safely

```bash
ivoai config set compression.provider caveman
ivoai config set compression.provider direct
ivoai doctor
```

Caveman is the requested default. IVOAI selects Direct when authoritative
Memory/Context or executor compatibility requires the exact path; it never chains
Caveman and Headroom.

## Add the remote MCP to ChatGPT Web or Claude Web

```bash
sudo ivoai server web-access create --ttl 10m
```

Configure `https://ai.example.com/mcp` in the Web product, complete OAuth, and enter
the one-time activation code only in IVOAI's authorization page. See [Remote MCP for
ChatGPT and Claude](mcp-web.md) for current plan/admin steps, revocation, secure
tunneling, and conformance diagnostics.

## Diagnose a partial server setup

Run `sudo ivoai server doctor`, address the reported `ROOT_CAUSE`, then rerun
`sudo ivoai setup --mode server`. Do not create backend `.env` files manually.

## Diagnose MCP HTTP 406

Confirm the connector uses the exact `/mcp` URL, update the Web/CLI client, and make
the reverse proxy preserve `Accept`, `Content-Type`, `Authorization`, and
`X-Forwarded-Proto`. A conforming POST accepts both `application/json` and
`text/event-stream`. Never paste a bearer value into a diagnostic command. See the
[troubleshooting runbook](troubleshooting.md#chatgpt-or-claude-web-cannot-connect-to-mcp).

## Host docs behind external Nginx Proxy Manager

The managed docs service listens on `0.0.0.0:7780`. In NPM create a Proxy Host with
scheme `http`, forward host set to the IVOAI server's private/LAN IP, and forward port
`7780`. Terminate public TLS at NPM. WebSockets are not required. Restrict port 7780
at the firewall to the NPM network/address when possible.

```text
Internet -> HTTPS/Nginx Proxy Manager -> HTTP/LAN -> ivoai-server:7780
```

See [MCP Web](mcp-web.md) for ChatGPT and Claude connectors and
[Troubleshooting](troubleshooting.md) for MCP HTTP 406 diagnostics.

## Back up, restore, and roll back

```bash
sudo ivoai server backup --output /var/lib/ivoai/backups/ivoai-backup.tar.gz
sudo ivoai server restore --input /var/lib/ivoai/backups/ivoai-backup.tar.gz
ivoai update --rollback
ivoai status
ivoai doctor
```

Backups exclude secrets and rebuildable indexes; protect secret administration
separately. Update rollback restores the previous binary and its compatible
IVOAI-owned mutable state transactionally.
