# Server

Server administration is available both through `ivoai server ...` subcommands and
the **Server Administration** section of the interactive menu. Mutating local server
actions are shown as unavailable unless ivoai is running on a supported Linux host
with root privileges. Restore, stop, enrollment revocation, and connector removal
require explicit confirmation in the menu.

## Supported systems

Initial server support targets Ubuntu 22.04, Ubuntu 24.04, and Debian 12 on Linux
amd64 and arm64. Both architectures use their reviewed, immutable embedding-runtime
OCI digest. Run
`ivoai setup --mode server` as root. The operation is idempotent.

Setup installs `docker.io` from the operating-system repository when Docker is
absent. When that repository does not provide Compose v2, including Debian 12,
ivoai installs the architecture-specific official Docker Compose plugin at
`/usr/local/lib/docker/cli-plugins/docker-compose` after verifying its pinned
SHA-256 checksum. Progress is printed every 10 seconds and the bounded download
window is 30 minutes for slow links. An existing working Compose v2 installation
is preserved.

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

Rerunning setup changes ownership only on managed mount roots; it does not traverse
application-created content. This preserves legitimate Hugging Face cache symlinks.
Qdrant readiness uses its unauthenticated `/readyz` endpoint, while its data API
continues to require the generated internal credential.
Qdrant storage remains at `/var/lib/ivoai/qdrant`; its writable snapshot workspace
and initialization marker live at `/var/lib/ivoai/qdrant-snapshots` and
`/var/lib/ivoai/qdrant-init`. These separate mounts allow the pinned image to run as
the ivoai non-root container identity without making `/qdrant` writable.

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
ivoai server web-access create --ttl 10m
ivoai server web-access list
ivoai server web-access revoke <id>
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

If the HTTPS reverse proxy runs on another host or container, explicitly bind the
gateway to the server's private address and allow only the proxy's source address:

```sh
sudo ivoai server gateway configure \
  --public-url https://ai.example.com \
  --listen 192.0.2.10:7744 \
  --trusted-proxy 192.0.2.20/32
```

Use the real private address of the ivoai server and the real source IP/CIDR of the
proxy. Requests from other peers, and proxy requests without
`X-Forwarded-Proto: https`, are rejected. Do not use `0.0.0.0/0`.

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
The dependency containers join a non-internal network only while Docker establishes
the loopback bindings; systemd disconnects that transient network after startup.
After systemd loads the backend environment files, the context service cannot access
the managed secrets tree.

### Nginx Proxy Manager

Create one Proxy Host for the public hostname with scheme `http`, the ivoai server's
private address, and port `7744`. Enable a valid certificate and Force SSL. Do not
attach an NPM Access List, Basic Authentication, or another login challenge to the
host: Web connectors must reach the ivoai OAuth metadata and authorization flow.

When NPM runs on another host or container, configure the gateway listener and the
narrow source CIDR as shown above. NPM must preserve `Authorization`, `Host`, and the
original HTTPS scheme. The following Advanced configuration is suitable for the
Streamable HTTP MCP route:

```nginx
proxy_set_header Authorization $http_authorization;
proxy_set_header Host $host;
proxy_set_header X-Forwarded-Proto https;
proxy_buffering off;
proxy_request_buffering off;
proxy_read_timeout 3600s;
```

Do not use `0.0.0.0/0` as a trusted proxy and do not publish ports 6333, 6334, 8080,
or 49374. Only the HTTPS proxy origin should be internet-facing.

## Web MCP access

The public connector URL is the configured origin plus `/mcp`, for example
`https://ai.example.com/mcp`. Before connecting a Web client, create a one-time code:

```sh
sudo ivoai server web-access create --ttl 10m
```

The command prints the activation code once. During the connector's OAuth browser
flow, review the requested scopes and enter that code. The default grant contains
`context:read`, `memory:read`, `memory:write`, and `memory:delete`; select a narrower
set when mutation is not needed. `list` displays identifiers, scopes, expiry, and
revocation state without tokens. `revoke` invalidates the selected Web grant and its
refresh-token family.

The unified MCP exposes context search/read and bounded memory CRUD. Deleting memory
requires the `memory:delete` scope and an explicit confirmation for the normalized
page path. Context remains read-only. The gateway does not proxy administrative
ai-memory tools or arbitrary MCP calls.

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
