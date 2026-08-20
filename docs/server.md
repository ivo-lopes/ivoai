# Server

## Supported systems

Initial server support targets Ubuntu 22.04, Ubuntu 24.04, and Debian 12 on Linux
amd64 and arm64. Both architectures use their reviewed, immutable embedding-runtime
OCI digest. Run
`ivoai setup --mode server` as root. The operation is idempotent.

## Layout

| Purpose | Path |
| --- | --- |
| Configuration | `/etc/ivoai` |
| Secrets | `/etc/ivoai/secrets` |
| Persistent authoritative data | `/var/lib/ivoai` |
| Backups | `/var/lib/ivoai/backups` |
| Application assets | `/opt/ivoai` |
| Logs | journald |

The gateway and context services run as distinct unprivileged accounts
(`ivoai-gateway` and `ivoai-context`) in a shared read-only group. Their systemd
units hide unrelated processes, deny each service the other's private state, use
`Restart=on-failure`, `NoNewPrivileges=yes`, and narrow write allowlists. Qdrant's
unprivileged image, the embedding runtime, and ai-memory use the non-login `ivoai`
container identity. Dependency ports bind only to loopback, require separate generated
internal credentials, and are not public. After TEI downloads the pinned model and
passes health checks, its download network is disconnected.

## Operations

```sh
ivoai server setup
ivoai server status
ivoai server doctor
ivoai server start
ivoai server stop
ivoai server restart
ivoai server logs
ivoai server gateway configure --public-url https://ai.example.com
ivoai server backup [--output <path>]
ivoai server restore --input <backup>
ivoai server remote status
ivoai server remote doctor
ivoai server remote connector list
```

The gateway exposes liveness at `/health`, readiness at `/ready`, and non-sensitive
protocol discovery at `/.well-known/ivoai`. Protocol version 1 is checked before a
client persists connection state.

## Public HTTPS gateway

Setup listens on `127.0.0.1:7744` without TLS. For the usual deployment, keep that
loopback listener behind an administrator-managed HTTPS reverse proxy and record its
public origin without editing a file:

```sh
sudo ivoai server gateway configure --public-url https://ai.example.com
```

Alternatively, let ivoai serve TLS directly. The certificate and key are copied into
`/etc/ivoai/secrets/tls` as service-owned `0600` files, and a non-loopback listener
is accepted only when both are supplied:

```sh
sudo ivoai server gateway configure \
  --public-url https://ai.example.com \
  --listen 0.0.0.0:7744 \
  --tls-cert /absolute/path/fullchain.pem \
  --tls-key /absolute/path/privkey.pem
```

Certificate issuance and renewal remain operator responsibilities. Re-run the
configure command to refresh managed certificate copies. Qdrant, embeddings, and
ai-memory stay on loopback mappings; only the gateway or reverse proxy is public.
After systemd loads the backend environment files, the context service cannot access
the managed secrets tree.

## Enrollment

```sh
ivoai server enrollment create --ttl 10m
ivoai server enrollment list
ivoai server enrollment revoke <id>
```

Only the create command displays the one-time code. The server persists a
cryptographic digest, expiry, and state, never the original code. Consumption issues
a scoped client credential; replay is rejected. The v0.1 authentication backend is
the owner-only, cross-process-locked local record store at
`/var/lib/ivoai/enrollment/state.json`; it contains hashes and metadata, not plaintext
codes or issued bearer tokens.

## Context connectors

Filesystem and Git connectors normalize text, reject sensitive and unsafe paths,
chunk documents, produce local embeddings, and upsert a versioned Qdrant collection.
Connectors are managed with explicit commands such as
`ivoai server connector add --name docs --type filesystem --path /srv/docs`,
`ivoai server connector list`, and `ivoai server connector remove docs`. The core
remains healthy with zero connectors and zero documents. Connector definitions are
loaded when the context service starts. Removal purges the connector's catalog and
vector entries before removing its registry definition, then restarts the context
service so its active configuration matches the registry.

Agent-facing context tools are read-only: `context_search`, `context_get_document`,
`context_recent`, and `context_health`. Ingestion and connector administration are
separate authenticated operations.

## Backup and restore

Backups include configuration without unnecessary plaintext secrets, connector and
corpus metadata, context metadata, ai-memory persistent data, and index rebuild
metadata. Original corpus and memory data are authoritative; vector indexes are
rebuildable. Restore validates bounded entries, rejects links and traversal, excludes
secrets, and writes regular files atomically. The CLI automatically stops the managed
gateway, context, and dependency services around each backup/restore operation and
starts them again afterward. Restore merges validated files into the managed roots
and does not delete stale files that are absent from the archive.
