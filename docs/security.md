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

## Network controls

Clients use standard TLS validation and bounded HTTP timeouts. Cleartext HTTP is
accepted only on loopback for development or for a TLS-terminating reverse proxy;
a non-loopback gateway listener requires direct TLS. Discovery is non-sensitive.
The gateway is the only intended public endpoint. Qdrant, embeddings, and ai-memory
use independent generated bearer credentials even on loopback-only host mappings;
client credentials are never forwarded to them. After the pinned model is healthy,
systemd disconnects the embedding container from its egress network.

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

## Supply chain and operations

The installer verifies release checksums before extraction. Component versions and
checksum strategies are pinned. Archives are extracted only after validating entry
paths. No update occurs silently; `ivoai update` reports the selected release and
preserves config and secrets. `ivoai server logs` redacts its rendered output;
journald should still be reviewed before logs are exported or shared.

Report vulnerabilities privately to the repository owner. Do not include tokens,
private documents, or production logs in reports.
