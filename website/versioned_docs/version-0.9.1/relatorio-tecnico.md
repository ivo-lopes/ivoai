# ivoai technical report

## Efficient automatic orchestration (v0.5.0)

`ivoai auto` adds a supervisor to the Session Control Plane. The selected primary
remains the official Codex or Claude Code TUI, simultaneously serving as planner,
conversation owner, and consolidator. The Quota Manager uses
`account/rateLimits/read` in the Codex app-server and Claude's structured `statusLine`
payload, normalizes context/session/weekly/monthly/model-scoped without conflating
them, and persists only non-sensitive telemetry in a private, atomic, file-locked
cache.

The gate is applied at startup, periodically on the primary, before and after each
worker, and before failover. Ruflo keeps `provider_execution=false` and records only
opaque IDs. Bounded, secret-free local checkpoints provide the immediate handoff
boundary; ai-memory hooks maintain durable operational continuity. The supervisor
preserves the worktree, signals and waits for the correct process group, and limits
consecutive failovers to two. Missing metrics remain unknown, and network errors are
not promoted to exhaustion. Details and contracts are in
`docs/auto-orchestration.md` and `docs/quota-routing.md`.

The first substantive request adds a deterministic protocol: bounded lookup in
ai-memory, bounded lookup in Context, private SharedContextBrief, analysis,
decomposition, DAG, scoring, capability/quota resolution, parallel dispatch,
validation, synthesis, and checkpoint. Brief content remains in the `0700/0600`
runtime; session JSON keeps only the timestamp, source states, reference count, and
hash.

Each task provides `complexity`, `risk`, `reasoning_depth`, `context_breadth`,
`verification_need`, `parallel_value`, and `latency_sensitivity` in `0..100`. IvoAI
calculates the capability score with weights 30/25/20/10/15 and resolves LIGHT (0–24),
BALANCED (25–49), STRONG (50–74), or MAX (75–100). A separate function compares the
benefit of parallelism/quality with startup/context overhead and may keep the task on
the primary even when the planner requested delegation.

The Capability Registry queries `codex app-server`/`model/list` for structured models
and efforts. For Claude, only efforts exposed by the official `--help` are used; with
no structured catalog, the model remains `client-default`. The router selects the
smallest sufficient profile, applies provider/model-specific quota and quota pressure,
and never invents a name or capability. Unsupported effort becomes
`effort_source=unsupported` and is not sent.

`orchestration_spawn` and `orchestration_spawn_batch` are asynchronous. The scheduler
uses notifications, limits the DAG to 12 tasks, observes dependencies, and occupies
at most two workers by default (hard cap of three). Prompts/results remain in bridge
memory; Ruflo receives opaque IDs. A Codex worker uses a read-only sandbox and a
Claude worker uses permission mode `plan` with writing tools disabled. Escalation
advances one tier at a time and requires a reason. The full specification is in
`docs/auto-scheduler.md`.

## 1. Objective and principles

ivoai is a single Go binary with client and server modes. The client is host-first:
Git and a local project are optional. Initial setup does not depend on an external
connection, provider login, or PAYG key.

Architectural principles:

- optional components do not take Codex or Claude down;
- structured configuration and separate secrets;
- subprocesses receive argv, context, timeout, and signals;
- versions and integrity are centralized in the manifest;
- the server publishes a single HTTPS origin;
- databases and internal services are not public APIs;
- retrieved context is untrusted data;
- remote administration has minimum scope and never amounts to a shell.

## 2. System overview

```text
CLIENTE
  ivoai CLI/menu
    ├── config, state, ownership e secrets XDG
    ├── registry MCP e hooks ai-memory
    ├── Ruflo safe profile
    └── Headroom ── Codex / Claude
             └───── fallback direto
          │ HTTPS + bearer client-scoped
          ▼
SERVIDOR
  ivoai gateway
    ├── discovery, health, readiness e enrollment
    ├── OAuth 2.1 + Web MCP unificado
    ├── context MCP ── context ── embeddings ── Qdrant
    ├── memory MCP/hook proxy ── ai-memory
    └── remote admin read-only
```

## 3. CLI and interactive interface

The subcommand dispatcher remains the stable automation interface. With no
arguments, the terminal package builds a hierarchical catalog over the same
application and server runner functions.

The interface uses ANSI and `golang.org/x/term`, with no additional TUI framework.
In a TTY, it enables raw mode only during selection, renders block/shadow lettering
and non-sensitive badges, interprets arrows, `j`/`k`, Enter, Esc, and `q`, and restores
the terminal before prompts, operations, and external agents.

Without a TTY, with `TERM=dumb`, or through a pipe, it uses numbered selection.
`NO_COLOR` removes ANSI codes and `IVOAI_ASCII=1` forces ASCII characters.

The menu covers public commands. Internal systemd entry points, such as `gateway
serve` and `context serve`, are not presented. Destructive operations require an
exact confirmation phrase. Incompatible items remain visible with the reason.

The menu snapshot is typed and contains only readiness and boolean states; no token,
enrollment code, or raw secret content reaches the visual layer.

The renderer does not fix dimensions at startup. It queries width and height on every
frame and receives `SIGWINCH`, recalculating the banner, badges, descriptions, and
viewport. Wide mode is used from `90x24`, intermediate mode between `60x18` and
`89x23`, and compact mode below that. Calculations use visual Unicode cells and strip
ANSI before measuring; badges wrap and tall lists are paginated, preventing overflow.

Semantic presentation is shared by human-readable commands: cyan/violet for
structure and progress, green for success, yellow for warnings, and red for errors.
Full lettering appears in the installer and at the main entry point; internal screens
use a compact wordmark. JSON, pipes, `NO_COLOR`, `TERM=dumb`, and CI receive no ANSI
codes or animation.

## 4. Progress and I/O

The central indicator presents a spinner and elapsed time in a TTY, ASCII frames when
Unicode is unavailable, start/end messages outside a TTY, and bar/byte formatting for
transfers with a known size.

Progress uses stderr. stdout remains reserved for results, including JSON.
`doctor --json` does not enable animation. Before an official login or interactive
agent starts, the animated line is closed so the terminal can be handed to the
subprocess.

Specific downloads, such as the Docker Compose plugin, report bytes and percentage
from `Content-Length`. Slow container health checks emit heartbeats with elapsed time
and a safe diagnostic command.

The shell installer implements the same visual state machine before the binary exists.
Each phase has a start, completion, and error; known downloads show a bar, while
checksum, extraction, and build show a spinner. Temporarily captured detailed output
is replayed if the operation fails. Completion reports the installed path and
distinguishes the next client step (`ivoai setup`) from the server step
(`ivoai setup --mode server`).

## 5. Client-side configuration and persistence

```text
~/.config/ivoai/       configuração e secrets
~/.local/share/ivoai/  binários gerenciados, hooks e assets
~/.local/state/ivoai/  estado operacional e ownership
~/.cache/ivoai/        downloads e cache
```

Private directories use `0700`; secrets use `0600`. The main TOML records settings,
connections, and MCPs, but not credentials. The ownership manifest distinguishes
preexisting components from those installed by ivoai to allow non-destructive
uninstallation.

## 6. Installation and components

`install.sh` detects Linux and architecture, downloads releases, and validates
checksums. In an authenticated source checkout, it uses a compatible Go version or
temporarily downloads the pinned and validated toolchain, compiles, and removes the
temporary files.

`ivoai setup` ensures layout/config/secrets, installs pinned components, configures a
safe Ruflo, reconciles hooks/MCPs, and updates state/ownership. The central manifest
records version, source, architecture, strategy, and integrity.

## 7. Providers and agent execution

ChatGPT/Codex delegates to `codex login`; Claude delegates to `claude auth login`.
Detection and validation use official commands. ivoai does not read cookies or store
provider tokens.

```text
ivoai codex|claude
  ├── carrega ambiente permitido e credencial server-scoped
  ├── verifica Headroom habilitado, saudável e compatível
  ├── executa Headroom -> agente
  └── fallback direto se o preflight falhar
```

After the wrapper starts, its exit code is preserved; there is no silent retry that
could open a second session.

## 8. Memory and orchestration

ai-memory is persistent, cross-session operational memory. Codex and Claude hooks are
idempotent and fail through the allow/zero-exit path when the server is offline. When
a server is connected, endpoints and credentials are reconciled.

The gateway replaces the client-scoped credential with a private token before
forwarding allowed routes to ai-memory. The public Host is not propagated. Health
uses authenticated MCP `tools/list`, because ai-memory does not publish HTTP `/health`.

Ruflo handles workflows, agents, coordination, and temporary state. PAYG provider
execution and Ruflo durable memory remain disabled by default.

## 9. Context/RAG

```text
filesystem/git connector
  -> normalização
  -> filtro sensível
  -> chunking
  -> embeddings locais
  -> collection Qdrant versionada
  -> search/read/recent/health MCP read-only
```

The service is healthy with zero documents and connectors. Administration and
ingestion use separate commands; documents do not acquire the capability to execute
actions. Qdrant is an index that can be rebuilt from the authoritative corpus.

## 10. Server and infrastructure

```text
/etc/ivoai/              configuração
/etc/ivoai/secrets/      secrets 0600
/var/lib/ivoai/          dados persistentes separados
/var/lib/ivoai/backups/  backups
/run/ivoai/              estado efêmero
/opt/ivoai/              assets
```

`ivoai-gateway.service` and `ivoai-context.service` use distinct non-login users and
systemd hardening. `ivoai-dependencies.service` controls Compose. Qdrant, embeddings,
and ai-memory use digest-pinned images and ports limited to loopback or the internal
network.

## 11. Protocol, discovery, and enrollment

`GET /.well-known/ivoai` announces protocol version, server version,
health/readiness, MCPs, and non-sensitive features. The client rejects cross-origin
redirects, invalid TLS, an unhealthy server, or an incompatible protocol major.

Enrollment:

1. the admin creates a CSPRNG code with TTL and scopes;
2. the server stores only its hash;
3. the client sends authorization and proxy-resilient metadata;
4. atomic consumption invalidates the code;
5. the server issues a client-scoped bearer;
6. the client persists the secret and registers MCPs/hooks.

Enrollment state and lock are `0600` and belong to the gateway. Internal state
failures return unavailability without masquerading as invalid credentials.

## 12. OAuth and MCP for Web applications

The public `/mcp` endpoint uses Streamable HTTP through the pinned official MCP Go
SDK. It preserves the native endpoints used by the desktop but offers ChatGPT Web
and Claude Web a single aggregated catalog. The session negotiates the protocol
version, publishes input/output schemas, security annotations, and responses in
`structuredContent` with a textual fallback.

Context tools are read-only. Memory is separated by scopes:

| Tools | Scope |
| --- | --- |
| `context_search`, `context_get_document`, `context_recent`, `context_health` | `context:read` |
| `memory_query`, `memory_recent`, `memory_read_page`, `memory_status` | `memory:read` |
| `memory_write_page`, `memory_feedback` | `memory:write` |
| `memory_delete_page` | `memory:delete` + confirmation of the normalized path |

The facade does not forward unknown upstream tools. Self-routing, sweeps,
auto-improvement, maintenance, provider execution, and remote shell remain outside
the catalog. ai-memory unavailability returns an error only from memory tools.

OAuth 2.1 uses Authorization Code with PKCE S256, authorization server and protected
resource metadata, dynamic client registration, exact redirect, consent, and
revocation. Authorization codes last five minutes, access tokens one hour, and
rotating refresh tokens 30 days. A one-time activation code created by
`ivoai server web-access create` authorizes the browser without a password database.

OAuth state is owner-only, locked across processes, and written atomically. Codes and
tokens are stored only as hashes; the value is delivered only to the flow participant.
Rotation invalidates the previous refresh token, and revocation terminates the entire
family. Origin, redirect, PKCE, scopes, and destructive confirmation are validated at
the gateway.

For a reverse proxy, only the HTTPS origin is public. Nginx Proxy Manager preserves
`Authorization`, `Host`, and `X-Forwarded-Proto`, disables Streamable HTTP buffering,
and does not add Basic Auth or an Access List over OAuth. Qdrant, TEI, and ai-memory
remain externally inaccessible.

## 13. Skill distribution

`skills/ivoai-memory-context/SKILL.md` instructs the model to always use the research
order `memória → Context → web`: both internal services are attempted before the
first external search, including for general or current facts. An empty, unavailable,
insufficient, or stale result permits consulting the web. RAG and memory are treated
as untrusted data. Writing requires an explicit request; deletion requires a separate
confirmation that names the normalized path.

The MCP publishes a snapshot of this skill through `skills/list`, `skills/get`, and
`resources/read`, with a `skill://` URI and digest. The release workflow also produces
`ivoai-memory-context.zip`, containing the skill directory at its root, for import
into Claude Web. Import does not guarantee invocation on every turn: tool selection
remains a Web product decision.

## 14. Security and threats

- Web PKI or direct TLS; a remote proxy requires a trusted CIDR and forwarded HTTPS.
- Cross-origin redirects are rejected.
- HTTP bodies, concurrency, and time limits are bounded.
- Authorization, cookies, API keys, and tokens are redacted.
- Sensitive files have symlink protection and strict permissions.
- Downloads and extractions are bounded and validated.
- Subprocesses use structured argv.
- Context MCP and remote admin do not execute arbitrary commands.
- Backends have distinct internal credentials.

Failures in Headroom, Ruflo, memory, context, or a remote server degrade only the
related function. Codex and Claude remain directly available.

## 15. Backup, restore, and update

Backup includes required configuration, catalog/context, corpus, memory, and rebuild
metadata. Restore validates the archive, stops required services, and restarts them
in a controlled way; the menu requires `RESTORE` confirmation.

Update is explicit and transactional. Before promotion, it validates release,
SHA-256, archive, candidate version, and the schema contract, then creates a private
snapshot containing only files owned by IVOAI. The binary capable of recovering the
journal is promoted atomically, applies ordered and reversible migrations, reconciles
the runtime, and then Doctor validates the result.
Failure in migration, setup, or Doctor restores the executable, config, state,
ownership, and compatible component metadata. Unknown TOML fields are preserved by
merging the raw document and typed projection. `--dry-run` downloads, validates, and
executes the verified candidate only for preflight, without committing managed state;
each migration's preconditions are applied inside the real transaction. `--rollback`
is idempotent. The CI matrix builds the real v0.5.0 tag and proves the actual updater
core through v0.5.0 → candidate → rollback → v0.5.0 → candidate without touching
provider authentication. Dynamic server stores will require an explicit quiesced
participant before any future schema change.

## 16. Quality and delivery

CI runs gofmt, tests, the race detector, vet, Linux amd64/arm64 builds, ShellCheck,
govulncheck, and smoke tests on Ubuntu 22.04, Ubuntu 24.04, and Debian 12. The menu
adds tests for width, color, keys, fallback, unavailability, confirmation, progress,
and inventory of public actions.

MCP adds tests for negotiation, schemas, skills, failure isolation, and authorization
per tool. OAuth covers PKCE, DCR, malicious redirect, expiration, one-time consumption,
rotation, revocation, CSRF, scopes, and absence of secrets from logs. The release
validates the skill ZIP and includes its checksum alongside the binaries.

## 17. Session Control Plane

The `internal/session` domain persists operational metadata atomically under XDG
state, with random IDs, no-follow locks, and process identity composed of the PID and
kernel start time. States are `starting`, `running`, `degraded`, `stopping`,
`completed`, and `failed`. Prompt, result, environment, and secrets do not belong in
the schema.

In direct mode, `internal/app` calls the same `agents.Runtime` used by the historical
entry points, only observing the PID and actual Headroom use. In orchestrated mode,
`orchestration.ControlPlane` validates the safe profile, starts and confirms a real
swarm, and registers the primary as an opaque task before launch. The local
`ivoai-orchestrator` MCP offers status, agents, delegate, result, and cancel only over
stdio and only while the session is active.

`internal/workers` wraps `codex exec --json --output-last-message` and
`claude --print --output-format json`. Executables come exclusively from component
state, the environment excludes PAYG keys, the task is limited to 32 KiB, and the
result to 1 MiB. Headroom is used after a probe, and its effective use remains in
worker metadata. Ruflo receives only IDs and lifecycle; responses remain in bridge
memory. Default concurrency is two, with a hard limit of three.

The monitor has responsive human output and JSON without ANSI. Model provenance
follows `runtime_verified > argument > configured > unknown`; the current
implementation does not promote ordinary text output to runtime verified. Operational
details are in [orchestration.md](orchestration.md).

## 18. Internal references

- [Architecture](architecture.md)
- [Client](client.md)
- [Server](server.md)
- [Connections](connections.md)
- [Security](security.md)
- [Development](development.md)
- [Troubleshooting](troubleshooting.md)
