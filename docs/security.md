# Security

## Trust boundaries

- Official agent credentials belong to Codex CLI and Claude Code, not ivoai.
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
