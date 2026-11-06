# Security

## Multi-server credential boundary

Every enrolled server has an opaque stable `server_id`; its credential is stored in
the private secret store under that ID, never under an untrusted alias. Aliases,
purposes and redundancy groups use a bounded safe character set. Base URLs reject
userinfo, queries and fragments, require HTTPS outside loopback, and discovered
service endpoints must remain same-origin.

Managed sessions receive only an ephemeral loopback router and a random local
capability. `IVOAI_KNOWLEDGE_SESSION_TOKEN` and the compatibility
`IVOAI_SERVER_TOKEN` contain that local capability—not an upstream bearer. The
router binds `127.0.0.1`, compares the capability in constant time, rejects
cross-origin redirects and revokes it when the session ends. Upstream token A is
looked up by opaque ID and attached only to source A; it is never rendered into
agent arguments, MCP files, session state, observability or logs.

Purpose isolation is fail-closed. An unfiltered session performs bounded reads from
all enabled sources; any `--knowledge-source` flag restricts that session to the
requested subset. A write with multiple purposes or multiple independent destinations is
rejected before contacting an upstream. Redundancy read failover stays within one
purpose/group; writes are not automatically replayed because the primary may already
have applied the side effect. Partial federated failures retain source attribution.

Enrollment first persists an unavailable marker, then the new scoped credential,
then atomically marks the profile connected. Runtime commit failures restore the
previous profile and credential. An interruption can therefore degrade the one
profile but cannot pair a new token with an old URL. Duplicate/tampered server IDs
are rejected before credential-bearing connect, test or disconnect operations.

## Trust boundaries

- Official agent credentials belong to Codex CLI and Claude Code, not ivoai.
- In AUTO, OpenCode is a loopback-only frontend. IVOAI invokes the official Codex and
  Claude CLIs and never reads, copies or converts their credentials into OpenCode.
- A connected ivoai server is authenticated but still treated as external.
- Connector content and retrieved RAG text are untrusted data, never installer or
  control-plane instructions.
- Third-party components are pinned in the central manifest.

## Secret handling

Secrets are never stored in main configuration or examples. Diagnostics and CLI log
output redact labeled bearer authorization, enrollment-code, access-token and common
API-key forms. Secret directories and files are created with `0700` and `0600`;
writes are atomic and refuse unsafe symlink targets.

Enrollment codes have high entropy, a TTL, one-time consumption, revocation, and
digest-only persistence. Client credentials have minimum scopes. Remote administration
offers named safe operations and never accepts host commands or executables.
Enrollment through `--code-stdin` or the no-echo prompt is preferred; the supported
`--enrollment-code` automation flag may be visible in process listings or shell
history.

Web connector authorization uses OAuth 2.1 Authorization Code with PKCE S256 and
exact redirect URI matching. The browser must also present a short-lived one-time
activation code created locally by `ivoai server web-access create`. Activation and
authorization codes, access tokens, and rotating refresh tokens are persisted only
as hashes. Access-token lifetime is one hour; refresh-token lifetime is 30 days;
revocation invalidates the associated token family.

Dynamic client registration does not waive redirect validation. Consent shows the
requested scopes, and every MCP tool checks its required scope. `memory_delete_page`
also requires confirmation bound to the normalized target path. Web credentials are
never interchangeable with native client enrollment credentials or backend service
tokens.

The OAuth `resource` parameter is bound to the canonical public `/mcp` URL throughout
authorization, code exchange, refresh rotation, and bearer-token validation. A token
issued for another audience is rejected before any MCP request is handled.

During enrollment the one-time code uses a dedicated `Authorization` scheme rather
than a proxy-parsed JSON field. Gateway audit never records that header. Legacy body
transport remains accepted only for rolling compatibility.

## Network controls

Clients use standard TLS validation and bounded HTTP timeouts. Cleartext HTTP is
accepted only on loopback for development or a same-host TLS reverse proxy. A
non-loopback plaintext listener requires an HTTPS public origin and explicit trusted
proxy CIDRs; the gateway validates the socket peer and forwarded HTTPS scheme. Direct
TLS remains available for non-loopback listeners. Discovery is non-sensitive.
The gateway is the only intended public endpoint. Qdrant, embeddings, and ai-memory
use independent generated bearer credentials even on loopback-only host mappings;
client credentials are never forwarded to them. After the pinned model is healthy,
systemd disconnects the embedding container from its download network and all
dependency containers from the transient network used to establish host bindings.

For direct TLS, ivoai copies the selected certificate and key into the gateway-owned
`/etc/ivoai/secrets/tls` directory as `0600` files. The gateway and context services
use distinct UIDs, hidden process views, and mutually exclusive filesystem deny
lists. After systemd loads the two backend credentials it requires, the context
service's sandbox blocks filesystem access to the entire secrets tree and to
enrollment and memory state. The gateway sandbox likewise blocks direct filesystem
access to memory, Qdrant, model, and corpus data it does not need.

## Content ingestion

Connectors constrain filesystem paths, reject traversal, and use OS-level no-follow
opens for the root and every path component. Git enumeration disables repository
hooks and fsmonitor programs. Filters exclude credential, cloud-state and key files;
quotas cap a document at 8 MiB, a connector at 10,000 documents/256 MiB, and an
ingestion at 250,000 chunks. Retrieved text is labeled as untrusted context. Context
MCP methods are read-only by default. Removing a connector purges its catalog and
vector entries.

The bundled skill reinforces this boundary: retrieved memory and RAG text are
evidence, not instructions. It permits memory writes only after an explicit user
request and deletion only after a separate path-specific confirmation. The Web MCP
does not expose ai-memory maintenance, self-improvement, provider execution, remote
shell, or unrestricted upstream tool forwarding.

Some upstream memory tools do not advertise MCP read-only annotations, so Codex
would otherwise ask for approval even for a lookup. IvoAI adds process-local approval
overrides only for the named read tools of registered `ivoai-memory` and
`ivoai-context` servers. It does not auto-approve memory writes, deletion, connector
administration, arbitrary MCP tools, or servers absent from the IvoAI registry.

## Supply chain and operations

The installer verifies release checksums before extraction. Component versions and
checksum strategies are pinned. Archives are extracted only after validating entry
paths. No update occurs silently; `ivoai update` reports the selected release and
preserves config and secrets. `ivoai server logs` redacts its rendered output;
journald should still be reviewed before logs are exported or shared.

The Skill Control Plane uses a shared non-executing staging pipeline for future
skills, components, and helpers. It requires immutable revisions and SHA-256,
separately represents signature/attestation status, rejects traversal, links,
duplicates, special files, decompression limits, and unexpected executables, and
promotes only by an atomic private pointer after structural and policy validation.
Discovery never executes a hook or command found in external content. Policy is
deny-by-default and external prose cannot grant capabilities or orchestration
authority. See [skill-control-plane.md](skill-control-plane.md).

Report vulnerabilities privately to the repository owner. Do not include tokens,
private documents, or production logs in reports.

## Session control plane

Session JSON contains only operational metadata and uses private XDG directories,
atomic `0600` writes, no-follow reads and an advisory lock opened with
`O_NOFOLLOW`. Random session/worker IDs, bounded metadata and validation of every
executor/model/state field reduce tampering and terminal-escape risks. Prompts,
answers, tokens and raw environments are never fields in this schema.

`ivoai-orchestrator` is a local stdio MCP attached only through per-process official
client configuration. Its startup requires an active session with a verified Swarm
ID, safe Ruflo status and provider execution disabled. The remote gateway has no
route to it. Delegation selects only trusted component-state paths whose basename is
`codex` or `claude`; it accepts no shell, executable path or working-directory input.
Tasks are capped at 32 KiB, results at 1 MiB, concurrent workers at the configured
limit and three absolutely.

Ruflo receives a clean environment, `RUFLO_PROVIDER=ivoai-disabled`, process-local
memory and opaque lifecycle IDs only. PAYG provider variables, prompts and results do
not cross that boundary. Official workers receive provider-key variables removed
while retaining their own supported subscription authentication stores.

Primary and worker process groups are signalled only when the recorded Linux kernel
start marker still matches the PID. Shutdown cancels workers, closes lifecycle tasks
and removes the private transient runtime. Stale sessions can be finalized without
killing a recycled PID.

## Managed OpenCode frontend

IVOAI starts the pinned OpenCode server on an ephemeral `127.0.0.1` port with an
independent random Basic-auth secret. Its private provider bridge uses a separate
ephemeral loopback listener and a random bearer capability compared in constant time.
Both credentials exist only in the session process environment and mode-`0600`
managed files below the private runtime directory; neither is persisted in session
JSON, normal config, logs, status or argv.

Managed mode uses isolated XDG roots, disables project OpenCode configuration,
disables OpenCode auto-update and sharing, and enables only the IVOAI provider. This
prevents a repository-local plugin or provider override from silently entering the
trusted control path. The user-owned global OpenCode configuration and authentication
store remain untouched, and direct `opencode` use retains upstream behavior.

The TUI plugin consumes only bounded status metadata through the authenticated local
bridge. Aliases, purposes, executor state and IDs are stripped of control, ANSI and
bidirectional-override characters before rendering. Health refresh is cached on a
five-second timer rather than probing on every frame. Frontend-to-executor session
mapping contains only safe bounded IDs; prompts and results are not stored there.

On cancel, failover or shutdown, IVOAI terminates the official executor process group,
closes both listeners and removes the private runtime through the normal session
lifecycle. The OpenCode backend is never bound to a LAN address. This differs from
the documentation server, whose intentional server-mode default remains
`0.0.0.0:<docs-port>` behind operator-managed firewall and reverse-proxy policy.
