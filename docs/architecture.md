# ivoai architecture

Status: implementation baseline for v0.4.6. Decisions and upstream data were
validated on 2026-08-23. Exact pins live in `manifest/components.yaml`; this
document explains why they exist and how the pieces fit together.

## Goals and boundaries

ivoai is one Go binary with client and server modes. A client is useful immediately
after setup, with no external connection and no pay-as-you-go API key. ChatGPT,
Claude, and an ivoai server are deliberate post-setup connections, all managed
through CLI commands.

Optional memory, context, and orchestration setup failures are isolated from local
agent launch. A failed Headroom preflight selects the direct agent. After a wrapper
process starts, its exit status is propagated rather than risking a duplicate
interactive session.

The core is organization-neutral. It contains no company domains, private addresses,
user paths, or preconfigured connectors. Git is optional; project mode is an explicit
override, not a prerequisite.

## System view

```text
CLIENT                                      SERVER

ivoai CLI/wizard                       one public HTTPS origin
  |                                      |
  +-- configuration + ownership          +-- /.well-known/ivoai
  +-- connection registry                +-- /health and /ready
  +-- official agent authentication      +-- enrollment/control API
  +-- fail-safe memory hooks              +-- context MCP (read-only)
  +-- session control plane               +-- ai-memory MCP
  |     +-- direct observability
  |     `-- safe Ruflo swarm + local MCP
  |                                      |
  +-- Headroom -- Codex/Claude            +-- Web MCP + OAuth 2.1
        `------ direct fallback           +-- context service
                                         |     +-- connectors/ingestion
                                         |     +-- local embeddings
                                         |     `-- Qdrant (internal)
                                         `-- ai-memory (internal)
```

## Automatic quota-aware control plane

`ivoai auto` adds a supervisor above the existing session control plane. The selected
official Codex or Claude TUI remains planner, primary, conversation owner, and result
consolidator. A standalone quota manager gates the initial primary and every worker;
Ruflo remains a provider-free ephemeral lifecycle coordinator. Codex quota comes
from the official app-server JSON-RPC method, while Claude quota comes from a
session-local structured statusline payload. Context-window pressure is modeled
separately from subscription quota.

The supervisor owns process identity and bounded failover. It persists only session
metadata, normalized quota snapshots, and secret-free checkpoints. On a confirmed
hard limit it reaps the primary process group, re-probes the alternate, reads a
bounded worktree summary without changing files, and opens the alternate official
TUI with the handoff. A two-failover ceiling prevents ping-pong loops. See
[Automatic orchestration](auto-orchestration.md) and
[Subscription quota routing](quota-routing.md).

Only the gateway is externally reachable. Qdrant, the embedding runtime, ai-memory
administration, and service management stay on a private Compose network or loopback
socket. The server does not expose a shell or an arbitrary-command endpoint.

## Client architecture

The public installer downloads a release archive and verifies its published checksum.
When invoked from an authenticated source checkout, it first uses a compatible system
Go toolchain. If Go is absent or older than the `go.mod` directive, it downloads the
reviewed linux/amd64 or linux/arm64 Go archive, verifies the pinned official SHA-256,
and builds with temporary module and build caches. The toolchain and caches are removed
when installation finishes; no global Go installation or package-manager mutation is
required.

### Files and ownership

The client follows the XDG Base Directory Specification:

- configuration: `$XDG_CONFIG_HOME/ivoai` or `~/.config/ivoai`, mode `0700`;
- persistent application data: `$XDG_DATA_HOME/ivoai` or `~/.local/share/ivoai`;
- operational state and ownership manifest: `$XDG_STATE_HOME/ivoai` or `~/.local/state/ivoai`;
- cache/downloads: `$XDG_CACHE_HOME/ivoai` or `~/.cache/ivoai`;
- secrets: `$XDG_CONFIG_HOME/ivoai/secrets.json` (or `~/.config/ivoai/secrets.json`), mode `0600`, inside the mode-`0700` config directory.

The main TOML contains connection status and non-secret settings only. The ownership
manifest records component executables as `managed` or `pre_existing`; all
ivoai-managed binaries, hooks, and component runtimes live below the ivoai XDG data
root. Uninstall removes the ivoai XDG roots and installer registration while
preserving pre-existing third-party executables and vendor authentication.
Integrations use each third-party client's supported configuration mechanism and
preserve unrelated settings.

The install catalog compiled into the binary mirrors the reviewed central manifest
and selects exact OS/architecture assets. Direct archives are verified against
reviewed SHA-256 values before bounded extraction. Headroom ships
architecture-specific hashed constraint locks and permits wheels only. Ruflo ships a
complete npm v3 lock and installs with `npm ci` and lifecycle scripts disabled; every
registry dependency has an integrity value. Both installers run with a minimal
environment that excludes user and provider credentials.

Managed launchers use atomic replacement. Downloads have size limits and timeouts.
`latest` is never a component install target. Updates are explicit through
`ivoai update`, preserve configuration and secrets, retain the previous managed
binary for `ivoai update --rollback`, and then run doctor.

Managed Codex includes the same-version `codex-code-mode-host` published as a
separate official release asset. Both architecture-specific archives are independently
checksum pinned. The host has no version command, so its compatibility identity is
the reviewed release version recorded in managed state; a missing or mismatched
companion fails setup/launch instead of silently removing the entire tool surface.

### Connections and official credentials

`ivoai connect chatgpt` delegates authentication to `codex login`;
`codex login status` is the supported status probe. OpenAI's official documentation
states that ChatGPT subscription login is supported and opens a browser. ivoai never
reads or stores the returned credential. Source: <https://learn.chatgpt.com/docs/auth>.

`ivoai connect claude` checks `claude auth status`, starts the official
`claude auth login` browser flow when needed, and validates status again. Anthropic
documents Pro, Max, Team, and Enterprise subscription access without an API key.
Source: <https://code.claude.com/docs/en/authentication>.

The exact managed Claude pin is 2.1.228 because Anthropic's `stable` registry channel
pointed there on the validation date while `latest` was 2.1.237. Anthropic describes
stable as delayed and regression-filtered. ivoai installs this reviewed asset and
changes its managed pin only during an explicit setup or update. Behavior implemented
inside the upstream Claude executable remains governed by Anthropic.

ivoai does not inspect `~/.codex/auth.json`, Claude cookies, or OAuth tokens. It does
not ask for, proxy, copy, or log credentials. A pre-existing official install and
login remain owned by the user.

### Agent launch and failure isolation

The launcher uses structured argv and signal forwarding. Interactive clients inherit
ivoai's foreground process group; creating a new group without `tcsetpgrp` would
suspend terminal reads with `SIGTTIN`. Terminal and shell job-control interrupt,
suspend, continue, and resize signals therefore reach the complete interactive stack, while
context cancellation uses a bounded TERM-to-KILL sequence. The normal decision tree is:

```text
requested agent
  -> ivoai-memory or ivoai-context MCP active?
       yes -> official agent [argv...]
       no  -> Headroom enabled, healthy and compatibility probe passed?
                yes -> headroom wrap <agent> [argv...]
                no  -> official agent [argv...]
```

Headroom 0.36.0 officially supplies `wrap codex` and `wrap claude`. It is installed
into an isolated tool environment using uv 0.12.5 and the exact managed CPython
3.13.15 runtime below the ivoai data root. An unavailable, unhealthy, or incompatible
preflight selects the direct agent. After a compatible wrapper process has started,
ivoai propagates its exit status; it does not silently retry a failed interactive
session. ivoai does not rewrite the user's aliases or replace third-party launchers.
Headroom 0.36.0 can apply lossy compression to Codex Code Mode
`custom_tool_call_output` items without reliably associating them with the originating
MCP tool name. IvoAI therefore bypasses Headroom for primaries and workers whenever
either shared-knowledge MCP is active. This preserves exact memory and Context
results; session telemetry still records that Headroom was requested but not used.
Sources: <https://headroomlabs-ai.github.io/headroom/cli/>,
<https://github.com/headroomlabs-ai/headroom/releases/tag/v0.36.0>,
<https://github.com/headroomlabs-ai/headroom/issues/940>,
<https://github.com/astral-sh/uv/releases/tag/0.12.5>, and
<https://www.python.org/downloads/release/python-31315/>.

### Session control plane

Session metadata is an explicit domain below the XDG state directory. Random IDs,
atomic private files and Linux PID start markers support lifecycle monitoring without
creating another conversation store. Prompts, results, tokens and raw environments
are excluded. Direct sessions call the same interactive runtime as `ivoai codex` and
`ivoai claude`; Ruflo is not touched.

Concurrent primaries do not share lifecycle state: each session has its own record,
process identity and runtime subtree, and every Ruflo command runs with that subtree
as both working directory and isolated `HOME`. Session and quota stores serialize
cross-process mutations with advisory locks and publish complete files by atomic
replacement. Shared `ai-memory` pages are the deliberate exception: they form the
common durable knowledge plane used by all official clients.

Orchestrated sessions have a strict gate: the safe profile must match its reviewed
tool allowlist, provider execution and durable Ruflo memory must be false, Ruflo must
pass a version/health command, a real hierarchical swarm must return a verifiable ID,
and an opaque primary task must be registered before the official client starts.
Ruflo gets a clean environment with `RUFLO_PROVIDER=ivoai-disabled` and
`CLAUDE_FLOW_MEMORY_BACKEND=memory`.

The local stdio `ivoai-orchestrator` MCP is injected only for the lifetime of that
primary. It maps delegation to trusted official Codex/Claude executables and maps
opaque lifecycle IDs to Ruflo task commands. Results are bounded to 1 MiB and kept in
the bridge process, never in session state. Worker concurrency defaults to two and is
hard-capped at three. The bridge is not registered in or routed by the public server
gateway. See [orchestration.md](orchestration.md).

ai-memory 1.29.0 is the durable operational memory layer. Its versioned native
binaries and hook bundle support Codex and Claude Code and publish per-platform
checksums. Hooks enqueue with bounded latency and treat network, authentication, and
service failures as non-fatal; hook failure results in the allow/zero-exit path
appropriate to the host. Connecting a server rewrites only ivoai-owned memory
endpoint metadata. The authoritative upstream release is
<https://github.com/akitaonrails/ai-memory/releases/tag/v1.29.0>; its changelog was
reviewed because this is newer than the 1.28.1 floor in the product requirements.
IvoAI installs lifecycle hooks with ai-memory's `repo-root` project strategy. Project
resolution therefore uses the main Git repository name across Linux/WSL path aliases,
subdirectories and linked worktrees instead of splitting observations by the client
process's spelling of the current directory. Explicit MCP queries still include the
global scope according to ai-memory's normal query semantics.

Ruflo 3.38.12 is installed for workflows, coordination, and skills. ivoai registers a
least-privilege MCP profile containing only coordination tools, process-local
temporary memory, and a wrapper that strips supported PAYG-provider credential
variables before Ruflo starts. Provider execution and durable Ruflo memory remain
disabled; Codex and Claude themselves continue to run through their official
subscription-authenticated clients. Upstream issues
[#2356](https://github.com/ruvnet/ruflo/issues/2356) and
[#2962](https://github.com/ruvnet/ruflo/issues/2962) remained open and showed direct
execution selecting separately configured providers, so those paths are deliberately
excluded from the default profile.

### Identity and MCP registry

The connection registry models MCP entries uniformly by stable ID, transport,
URL/argv, scopes, ownership, and enabled status. Context and memory use the same
registry as user-added MCPs; agent-specific renderers are edge adapters, not separate
sources of truth.

The default ivoai client identity outside an explicitly initialized project is
`host:<normalized-hostname>`. It is independent of the current directory, so `/etc`,
`/opt`, and `/var/lib` do not become accidental projects. `ivoai project init`
creates a deterministic ID from the absolute Git root and a local `.ivoai.toml`
marker that overrides host identity. Merely entering a Git repository does not
silently change ivoai identity. ai-memory lifecycle scoping is a separate concern and
uses the stable main-repository strategy described above.

## Server architecture

### Runtime and persistence

Supported initial hosts are Ubuntu 22.04+, Ubuntu 24.04+, and Debian 12 on amd64 or
arm64. The ivoai gateway and context processes are systemd services running as
distinct non-login `ivoai-gateway` and `ivoai-context` users in the shared `ivoai`
group. Units use `Restart=on-failure`, `NoNewPrivileges=yes`, a restrictive
`UMask`, capability removal, hidden process views, private temporary directories,
and write allowlists limited to their own data paths. Backend containers use the
separate non-login `ivoai` identity.

Layout:

```text
/etc/ivoai/                 non-secret server configuration
/etc/ivoai/secrets/         0700 directory, 0600 secret files and managed TLS copies
/var/lib/ivoai/context/     metadata, normalized corpus and ingestion state
/var/lib/ivoai/memory/      ai-memory authoritative data
/var/lib/ivoai/qdrant/      rebuildable vector index
/var/lib/ivoai/qdrant-snapshots/ Qdrant snapshot workspace
/var/lib/ivoai/qdrant-init/ Qdrant non-secret initialization marker
/var/lib/ivoai/models/      pinned embedding model snapshot
/var/lib/ivoai/enrollment/  hashed enrollment/client credential records
/var/lib/ivoai/backups/     versioned backup archives
/run/ivoai/                 sockets/PIDs and other ephemeral state
```

Service stdout and stderr go to journald by default. CLI-rendered server logs and
diagnostics pass through ivoai's redactor for labeled authorization, enrollment-code,
and common API-key forms. Authentication handlers do not log request bodies or
credential values; operators must still review journald output before sharing it.

Docker Compose is reserved for Qdrant, Text Embeddings Inference, and ai-memory. Every
image is referenced by immutable digest, is attached to an internal network, and has
no host port unless a loopback-only compatibility port is unavoidable. ivoai manages
the Compose project and volumes idempotently. ivoai's own Go services remain ordinary
systemd executables and require no private ivoai image. On supported apt-based
hosts, Docker Engine comes from the distribution. Where Compose v2 is not packaged,
ivoai installs the reviewed Docker Compose 5.5.0 CLI plugin for amd64 or arm64 from
its official release, verifies the manifest SHA-256, and never selects a floating
`latest` artifact. Direct-TLS certificate and key
copies are service-owned `0600` files under `/etc/ivoai/secrets/tls`. After systemd
loads its Qdrant and embedding environment files, the context service's sandbox makes
the complete secrets tree, enrollment state, and memory state inaccessible. The
gateway has a separate denial list for memory, Qdrant, model, and
corpus filesystem data outside its control-plane responsibilities.

### Protocol version 1

The public discovery response is non-sensitive. The gateway applies
`Cache-Control: no-store` consistently to discovery and API responses:

```json
{
  "protocol_version": 1,
  "server_version": "0.1.0",
  "public_base_url": "https://ai.example.com",
  "health_endpoint": "/health",
  "ready_endpoint": "/ready",
  "enrollment_endpoint": "/v1/enroll",
  "context_mcp_endpoint": "/v1/mcp/context",
  "memory_mcp_endpoint": "/v1/memory/mcp",
  "memory_hooks_endpoint": "/v1/memory",
  "web_mcp_endpoint": "/mcp",
  "oauth_authorization_server_metadata": "/.well-known/oauth-authorization-server",
  "features": {"context": true, "memory": true, "memory_hooks": true, "web_mcp": true, "oauth_pkce": true, "remote_admin_read_only": true}
}
```

The gateway exposes this response at `GET /.well-known/ivoai`. `/health` is process
liveness and does not depend on optional connectors. `/ready` requires a healthy
context service; an unavailable ai-memory dependency is reported as `ready_degraded`
so context remains usable.

Before consuming an enrollment code, the client requires valid Web PKI TLS, a
healthy or degraded-ready server, and protocol major 1. After enrollment, the
one-time credential and MCP registry are persisted before context and memory probes.
Probe failures become explicit degradation warnings, so a consumed code never
strands the issued credential. Redirects to another origin are rejected during
discovery and enrollment. Plaintext is accepted only on loopback for development or
behind a same-host TLS-terminating reverse proxy. A remote reverse proxy requires an
explicit source CIDR and forwarded HTTPS enforcement; other non-loopback listeners
require direct TLS configuration.

Endpoints use bounded JSON bodies and server-level request, read, write, and idle
timeouts. MCP endpoints accept JSON-RPC over HTTP and require bearer authentication.
Protocol changes that preserve version 1 are additive; removing or changing a field
or tool requires a new protocol major. Database and Qdrant collection migrations
carry their own monotonically increasing schema versions.

### Enrollment and authorization

`ivoai server enrollment create` generates 256 secret bits from the operating-system
CSPRNG and displays the base64url one-time code once. Because these high-entropy
random codes resist offline guessing, the server stores only a SHA-256 hash, together
with the ID, creation time, expiry, scopes, and consumed/revoked timestamps. The
default TTL is 10 minutes.

Codes are compared in constant time. Mutations take an exclusive OS file lock, and a
successful exchange atomically replaces the state file after marking the code as
consumed. This prevents independent CLI and gateway processes from consuming or
overwriting the same state concurrently. Codes and returned credentials are never
placed in argv examples, URLs, or logs; standard input and an interactive no-echo
prompt are supported.

The exchange returns a random client-scoped bearer credential once. Only its hash and
metadata are stored server-side in the owner-only local enrollment backend at
`/var/lib/ivoai/enrollment/state.json`; the client stores the value in a `0600` secret
file. Initial scopes are `context:read`, `memory:read`, `memory:write`, `status:read`,
`doctor:read`, and `connector:read`. Administrative mutation is never implied by
enrollment. Revocation is immediate. Invalid, expired, consumed, and revoked codes
share one uniform external error, so code state is not disclosed.

### Web MCP and OAuth

ChatGPT Web and Claude Web use a separate public integration boundary. `/mcp` is a
Streamable HTTP MCP endpoint built on the pinned official MCP Go SDK. It aggregates
context and memory behind one connector URL while preserving the existing native
client MCP routes. `initialize` advertises server instructions, tool capabilities,
and the bounded skills extension; tools return typed `structuredContent` plus a text
representation for compatibility.

The Web tool surface is intentionally narrower than the backing services:

- `context_search`, `context_get_document`, `context_recent`, and `context_health`
  are read-only and label retrieved documents as untrusted data;
- `memory_query`, `memory_recent`, `memory_read_page`, and `memory_status` require
  `memory:read`;
- `memory_write_page` and `memory_feedback` require `memory:write`;
- `memory_delete_page` requires `memory:delete`, a normalized page path, and explicit
  confirmation in the call.

Maintenance, self-improvement, handoff orchestration, arbitrary upstream tools, and
host commands are not published by this facade. Loss of ai-memory degrades memory
tools without making context tools or gateway liveness unavailable.

OAuth follows Authorization Code with PKCE S256. Protected-resource and authorization
server metadata support Web connector discovery; dynamic client registration accepts
only validated HTTPS redirect URIs (plus the explicitly supported loopback development
case). Authorization codes live for five minutes, access tokens for one hour, and
rotating refresh tokens for 30 days. The browser consent flow additionally requires a
short-lived one-time activation code created by the local administrator, avoiding a
new password database.

Server-side OAuth persistence stores only hashes of activation codes, authorization
codes, access tokens, and refresh tokens. Mutations are locked and atomic. Scopes are
checked again at the tool boundary. Origin validation, PKCE verification, `state`
round-tripping, exact redirect matching, token rotation, and revocation limit token
substitution and cross-site authorization attacks.

The RFC 8707 `resource` value is the canonical public `/mcp` URL. It is preserved in
authorization codes, access tokens, rotating refresh tokens, and every authenticated
MCP request so credentials cannot be replayed against a different audience.

### MCP skill delivery

The repository's `skills/ivoai-memory-context/SKILL.md` is also available through
`skills/list`, `skills/get`, and `resources/read`. The advertised resource uses a
`skill://` URI and digest so compatible clients can import a fixed snapshot. A release
also publishes the same directory as `ivoai-memory-context.zip` for Claude custom
Skill import. Importing instructions does not guarantee that a Web model will invoke
a tool on every turn; the platform retains final tool-selection control.

Remote administration is an explicit allowlist of typed operations such as status,
doctor summary, and connector list. There is no host-command parameter,
executable-path parameter, shell, file-write primitive, or generic proxy endpoint.

## Context and RAG

The context core is healthy with zero connectors and zero documents. A connector
implements discovery and streaming reads into a normalized document interface; it
cannot write the server filesystem outside its configured source. v0.1 adapters are:

- `filesystem`: a configured local root; directory traversal skips unsafe names and symlink entries;
- `git`: an existing local checkout enumerated with bounded `git ls-files` and then filtered by the same document rules.

Future Drive, S3, Notion, HTTP, GitHub, and generic MCP adapters implement the same
interface without changing chunking, storage, or agent tools. Connector credentials
are separate scoped secrets.

Pipeline:

```text
connector -> canonical document -> sensitive/binary filter -> deterministic chunks
          -> local embedding -> versioned Qdrant collection -> read-only MCP tools
```

Normalization records a stable document ID, source, relative path, timestamps, and
connector metadata. The filter excludes secret, credential, cloud-state, and key
files; binary content; non-regular files; hidden VCS internals; and files above 8 MiB.
Git enumeration disables repository-controlled hooks and fsmonitor programs.

Ingestion opens the connector root and every path component with OS-level no-follow
semantics, rejects traversal and special files, reads through the verified descriptor,
and enforces aggregate document, byte, and chunk quotas. Catalog replacement is
batched. Re-ingestion reconciles disappeared documents, while connector removal
purges that source's catalog entries and Qdrant vectors before deleting its registry
entry.

Chunk IDs derive from document ID, chunk index, and chunk text. The default policy
uses up to 1,200 Unicode code points with approximately 150-code-point overlap and
prefers newline or word boundaries. Re-ingestion is deterministic, but Qdrant
currently deletes a document's old points before uploading its replacement. A failed
upload therefore requires another ingestion run.

The Qdrant collection is `ivoai-context-v1-d384`; Qdrant 1.19.0 uses the pinned
upstream unprivileged multi-architecture image digest. Its host mapping is
loopback-only (`127.0.0.1:6333`) and requires a generated API key. Embeddings and
ai-memory use separate generated credentials. Docker uses a transient publication
network to establish the loopback bindings and a separate model-download network with
explicit highest gateway priority. After the embedding model is healthy, systemd
disconnects the download route. The loopback-publication network remains attached so
the host mappings stay valid, but disables IP masquerading and therefore provides no
backend egress. The index is a cache; source documents,
normalized metadata, and connector definitions are authoritative. A collection can
be deleted and rebuilt deterministically.

Agent tools are `context_search`, `context_get_document`, `context_recent`, and
`context_health`. They are read-only and return source metadata. Search and recent
counts are bounded to 100; `context_recent` omits document bodies; and
`context_get_document` returns at most one document, subject to the 8 MiB ingestion
limit. Ingestion and connector administration use separate authenticated commands and
API routes.

Document text is untrusted data. Tool descriptions and results label it as such and
never route document-provided commands into installers, shells, connector
configuration, or authorization decisions.

### Local embedding decision

Text Embeddings Inference (TEI) 1.9.3 was chosen over a cloud API because it is
Apache-2.0, maintained by Hugging Face, exposes a small HTTP API, supports CPU
containers, and documents both x86_64 and aarch64. The amd64 image and the upstream
`cpu-arm64-sha-30507cb` arm64 image are each pinned by immutable OCI index digest;
neither architecture uses a mutable tag at runtime. Sources:
<https://github.com/huggingface/text-embeddings-inference/releases/tag/v1.9.3> and
<https://github.com/huggingface/text-embeddings-inference>.

The default model is `intfloat/multilingual-e5-small` at immutable revision
`614241f622f53c4eeff9890bdc4f31cfecc418b3`, with its safetensors hash recorded in the
manifest. It is MIT-licensed, 384-dimensional, supports 94 languages, has a 512-token
limit, and is suitable for Portuguese/English CPU-first retrieval at moderate
footprint. E5's required `query:` and `passage:` prefixes are applied centrally, and
the Qdrant collection name includes the dimensional/schema version. Model source:
<https://huggingface.co/intfloat/multilingual-e5-small/tree/614241f622f53c4eeff9890bdc4f31cfecc418b3>.

TEI was preferred to embedding directly inside the Go gateway because process
isolation bounds native-model crashes and permits independent health, restart, and
resource limits. FastEmbed 0.8.0 is a credible lower-footprint future alternative,
but it would add a Python runtime inside the context process or another bespoke
service without improving the current HTTP boundary.

## Backup and restore

A backup writes a versioned manifest and includes server configuration with secret
values excluded, context metadata and the normalized authoritative corpus, connector
definitions without credentials, ai-memory persistent data, and index rebuild
metadata. Enrollment and client credential state are excluded. Qdrant data is
rebuildable and is never the only recoverable source.

Archives are created under a mode-`0700` backup directory using a temporary name.
They reject symlinks and special files and are atomically finalized. The CLI stops
the managed gateway, context, and dependency services before backup or restore and
starts them again afterward.

Restore stages and validates the format, bounded entry sizes, and safe paths; rejects
links and traversal; and then atomically writes individual files without restoring
secrets. It merges into managed roots and does not remove stale files. Restore accepts
only an explicit local absolute archive path and is not exposed through the remote
API.

## Security invariants

- Secrets never enter the main TOML, URLs, diagnostics, or logs. ivoai does not inject
  stored secrets into subprocess argv; enrollment uses standard input or a no-echo
  prompt by default.
- Installer and probe subprocesses use structured argv, bounded contexts, and minimal
  environments. Interactive agent subprocesses use structured argv and signal
  forwarding while retaining the compatible user environment required by the vendor
  clients.
- HTTPS validation is on by default; no insecure-skip-verify production switch is persisted.
- All optional layers have bounded timeouts or circuit breakers. They fail open only
  for launching the official local agent, never for authorization.
- Connector and RAG data are untrusted and cannot invoke tools or mutate configuration.
- Qdrant and internal admin surfaces are not public.
- Enrollment is one-time, short-lived, cross-process locked, and atomically persisted.
  Client credentials are hashed server-side, scoped, and revocable.
- Atomic writes reject unsafe ownership, modes and symlink targets.
- Doctor redacts values and reports permissions, versions, reachability, TLS and protocol compatibility only.

## Upstream decisions and known uncertainty

| Component | Pin | Decision and uncertainty on 2026-08-23 |
|---|---:|---|
| Codex CLI + code-mode host | 0.148.0 | Official same-release assets; `codex login` supports ChatGPT subscriptions and the separately checksummed host preserves the managed tool/MCP surface. |
| Claude Code | 2.1.228 | Official stable channel; latest was 2.1.237. Proprietary external binary subject to Anthropic terms. |
| Headroom | 0.36.0 | Current PyPI/GitHub release with amd64/arm64 wheels; fast-moving integration requires a setup smoke probe and direct fallback. |
| uv / CPython | 0.12.5 / 3.13.15 | Exact private installer/runtime pair for Headroom with embedded architecture-specific hashed constraints. |
| ai-memory | 1.29.0 | Newly released on the validation date, with changelog and per-arch hashes reviewed. Limited burn-in is mitigated by exact pin, hook timeout and failure isolation. |
| Ruflo | 3.38.12 | Exact npm integrity pin. Direct inference/provider routing remains unsuitable for the no-PAYG default and is disabled. Package/repository naming has moved historically, so updates require provenance review. |
| Qdrant | 1.19.0 | Current Apache-2.0 release and multi-arch OCI digest; internal only. |
| TEI | 1.9.3 | Mature CPU HTTP service; amd64 and arm64 upstream images are pinned independently by OCI index digest. |
| multilingual-e5-small | `614241f…` | Immutable MIT model revision; 384 dimensions/94 languages. Retrieval quality must be measured on the user's corpus before changing models. |

No external account, private server, or real owner credential was used during this
discovery. Compatibility with future upstream releases is not assumed. An ivoai
release that changes a pin must revise this table and the manifest only after
automated install, auth-status, wrapper, and failure-isolation tests pass.
