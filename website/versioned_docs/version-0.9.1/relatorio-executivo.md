# ivoai executive report

## Automatic orchestration

The `ivoai auto` command lets the user choose Codex or Claude Code to lead the
conversation in the familiar official interface. Codex is the default. ivoai tracks
the limits exposed by the subscriptions, avoids routing work to a provider with a
confirmed limit, and can transfer continuity to the other client. The switch
preserves work in progress and uses a safe summary of the last confirmed point; no
paid API key is requested or activated as an alternative.

Before starting substantial work, AUTO mode queries memory and private Context once,
prepares a shared brief, and divides the objective into tasks with dependencies.
IvoAI itself decides whether creating workers is worthwhile: trivial fixes remain
with the primary agent, while independent analyses can run in parallel. Each task
receives a LIGHT, BALANCED, STRONG, or MAX requirement, and the system chooses the
lightest profile that still meets the required quality while respecting subscription
limits.

Workers are read-only advisors. Codex and Claude run through their official clients,
share the same context brief, and return evidence to the primary agent, which remains
solely responsible for changing files and responding to the user. This lets the
product reduce repeated reading and latency without creating a memory silo or
sacrificing control.

A second window running `ivoai monitor --watch` shows who is leading the
conversation, workers, service health, weekly/monthly limits, and the source and
freshness of the data. When a platform does not expose a metric, the product reports
`N/A / not exposed` instead of inventing a percentage.

The monitor also presents the plan, dependencies, weight, tier, model/effort when
confirmed, concurrent tasks, and escalation reasons. When clients do not provide
official token metrics, ivoai reports that the data is unavailable instead of
estimating savings.

## Overview

ivoai is a personal platform for installing, connecting, and operating artificial
intelligence tools on Linux computers and servers. The product brings Codex CLI,
Claude Code, Headroom, ai-memory, and Ruflo into one experience without requiring
paid API keys for the primary path.

The user installs the client, runs setup, and can connect personal accounts or an
ivoai server later. The absence of those connections neither prevents installation
nor makes the environment defective.

The same private collection can also be queried from ChatGPT Web and Claude Web
through a protected MCP connector. Continuity is therefore not limited to the
terminal: operational history and indexed documents can accompany the user across
interfaces under revocable permissions.

## Problems the product solves

- Reduces the installation of multiple tools to one installer and one setup command.
- Avoids manual editing of TOML, JSON, hooks, MCPs, token files, and aliases.
- Keeps Codex and Claude usable when memory, context, Headroom, or Ruflo are
  temporarily unavailable.
- Centralizes diagnostics, updates, connections, execution, and administration.
- Provides persistent memory and contextual search without depending on OpenAI or
  Anthropic for embeddings.
- Separates personal information from the product: no specific company, account, or
  infrastructure is embedded in the code.

## Desktop experience

The `ivoai` command opens an interactive menu with custom lettering, a health summary,
and arrow-key navigation. The interface organizes operations into dashboard, setup,
connections, agents, memory, project, configuration, and server-side administration.

The layout adapts to the available width and height. In a large window, it presents
full lettering, descriptions, and indicators; over SSH or on small screens, it
reduces the header and uses a scrollable list without exceeding terminal bounds.
Resizing the window does not require restarting the application.

In basic terminals or automation, the menu automatically switches to numbered input.
All features remain available through subcommands for use in scripts.

Long-running operations show a spinner, elapsed time, or download progress. Automation
output remains separated: data goes to stdout and indicators to stderr. Colors have
consistent meaning: green confirms success, yellow indicates recoverable degradation,
red shows an error, and cyan accompanies work in progress. The installer uses the
same visual language and finishes by showing the next commands.

## Server experience

The same binary installs the server-side layer on supported Ubuntu and Debian systems.
It provides a single HTTPS gateway for client enrollment, operational memory,
read-only context/RAG, health checks, and limited remote administration.

Qdrant, embeddings, and ai-memory remain internal. There is no need to expose a
database, administration panel, or remote shell API.

## Use in ChatGPT Web and Claude Web

The administrator creates a temporary code with `ivoai server web-access create` and
registers the public URL ending in `/mcp` as a connector. The browser conducts OAuth
authorization and displays the requested permissions. The code works once and is not
reused as a permanent token.

The connector can search context, query memory, and, when authorized, write or delete
memory pages. Institutional context remains read-only. Deletions require a specific
permission and confirmation of the item. The administrator can list and revoke
access at any time.

The skill distributed with every release directs ChatGPT and Claude to always search
in the order memory → Context → web. Therefore, before searching an external source,
the model first tries operational history and then indexed private documents. If
those sources are empty, unavailable, insufficient, or stale, external research can
supplement the answer. The Web platform remains responsible for the final decision
to use a tool in each interaction.

## Main components

| Component | Role |
|---|---|
| Codex CLI | Official agent connected to a ChatGPT subscription |
| Claude Code | Official agent connected to a Claude subscription |
| Headroom | Optional optimization layer before the agent |
| ai-memory | Operational memory across sessions and agents |
| Ruflo | Safe orchestration, workflows, and coordination |
| ivoai gateway | Discovery, enrollment, authentication, and public APIs |
| Context service | Ingestion, chunking, and contextual search |
| Qdrant | Rebuildable vector index |
| Local embeddings | CPU-first vectorization without a paid external API |

## Security in plain language

- ChatGPT and Claude logins are handled by the official clients.
- ivoai does not capture cookies, passwords, or OAuth tokens from those providers.
- The server credential resides in a private `0600` file.
- Enrollment codes expire, work once, and are stored only as hashes.
- Web activation codes and OAuth tokens are also stored only as hashes.
- Web permissions separate context reads from memory reads, writes, and deletions.
- Logs and errors pass through centralized secret redaction.
- The gateway has no endpoint for executing arbitrary commands on the host.
- Ingested documents are treated as untrusted data.
- Optional failures are isolated and do not prevent basic agents from opening.

## Everyday operation

```sh
ivoai
ivoai status
ivoai doctor
ivoai codex
ivoai claude
```

Connections deliberately initiated by the user:

```sh
ivoai connect chatgpt
ivoai connect claude
ivoai connect server
```

On the server:

```sh
sudo ivoai setup --mode server
sudo ivoai server doctor
sudo ivoai server enrollment create
```

For Web access:

```sh
sudo ivoai server web-access create --ttl 10m
sudo ivoai server web-access list
sudo ivoai server web-access revoke <id>
```

## Continuity and recovery

Setup is idempotent. Updates preserve configuration and credentials and retain a
previous binary when rollback is possible. Server backups protect metadata, corpus,
and authoritative memory; vector indexes can be rebuilt.

## Status and limitations

The current baseline is Linux-first, with initial support for Ubuntu 22.04+,
Ubuntu 24.04+, and Debian 12+. Windows is outside the initial scope. macOS depends on
validation of all components.

Ruflo runs under the safe profile: direct PAYG provider execution and duplicate
durable memory remain disabled. External connectors such as Google Drive, S3, and
Notion are future extensions; the core works without them.

ChatGPT Web and Claude Web require the gateway to be published over valid HTTPS.
Availability of custom connectors and skills also depends on each provider's plan
and workspace policies.

## Observable and orchestrated sessions

Users can continue opening Codex and Claude in the simplest way, without any Ruflo
interference. When they need to monitor an activity, they choose a direct session;
when they need to delegate reviews or tests, they explicitly choose an orchestrated
session. The app confirms that Ruflo coordination is safe before opening the agent
and lets users monitor the executor, declared model, workers, and services through
`ivoai monitor --watch`.

This evolution does not create a new chat and does not require paid keys. Official
Codex and Claude continue to perform all intelligent work using the user's logins.
Ruflo retains only temporary organization; ai-memory remains responsible for durable
memory and Context remains responsible for retrievable knowledge.

## Related reading

- [Technical report](relatorio-tecnico.md)
- [Architecture](architecture.md)
- [Client](client.md)
- [Server](server.md)
- [Security](security.md)
