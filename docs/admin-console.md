---
sidebar_position: 17
---

# Administrative console

The administrative console manages the existing IVOAI App and services. It has
no conversational frontend, scheduler or separate configuration database. Native
Codex/Claude sessions remain in their official clients. Start the console as the
OS user whose IVOAI configuration and sessions you intend to administer.

```sh
ivoai admin init
ivoai admin serve --listen 127.0.0.1:7781
# In another terminal with the same OS user and HOME/XDG roots:
ivoai admin access create --ttl 10m
```

`admin init` creates missing private configuration/state roots without installing
clients or changing credentials, service configuration or existing directory
permissions. Existing public/foreign/symlink roots are rejected. The listener is
always a numeric loopback address. Access codes are shown once and entered in the
OAuth authorization form; they are not URL parameters.

## Management authority

Read-only is the default for existing and newly created grants. To explicitly
allow administrative changes, issue a separate grant:

```sh
ivoai admin access create --ttl 10m --write
ivoai admin access list
ivoai admin access revoke ACCESS_ID
```

The browser uses the existing OAuth PKCE flow and an HttpOnly SameSite cookie.
It never receives native-client subscription credentials or a refresh token.
`admin:manage` does not confer OS privileges, native tool grants or worker write
permission. A non-root console cannot become root through an HTTP request;
privileged server operations require a process already started by an authorized
server administrator. Grants apply only to their configuration/state instance.

The operation catalog covers the 118 public TUI IDs and adds profile preview/apply
and Context ingestion/search/exact-document retrieval. Every row is classified:

- **Read:** execute existing diagnostic, connection, session, capability and server
  views. Raw private TOML is replaced by the existing nonsecret management snapshot.
- **Mutation:** typed configuration, policy, profile, capability, MCP credential,
  server profile, lifecycle, connector, backup/restore, enrollment and update actions
  invoke the same application/CLI services and validators.
- **Native handoff:** login and conversational TUI launches provide the explicit
  native terminal command. They are not counted as executed web administration.
- **Information:** navigation and policies intentionally fixed by the TUI are
  explained without inventing a new setting or granting permission.

Mutations require a current management grant, same-origin JSON request, CSRF
header and an explicit preview/confirmation. A confirmation is short-lived,
one-use and bound to exact inputs, credential and the configuration revision.
Changing the input, policy or owner invalidates it. Configuration changes apply
with the same current/future-session semantics as the CLI.

Before dispatch, IVOAI durably claims the browser request ID. A failed connection,
cancellation, process restart or repeated confirmation cannot replay that ID.
All console processes sharing an instance use an OS lock. Failed operations are
not retried automatically, and the server does not add a background job scheduler.
An uncertain outcome requires inspection of state, audit and receipt before an
operator deliberately starts a new request. Revocation cancels in-flight command
contexts; it does not undo changes already committed by the underlying service.

Audit includes only action, verdict and hashed actor. Inputs, command output,
URLs, OAuth codes, cookies and secrets are excluded. Output is bounded and
redacted. Explicit enrollment/access creation can show its newly issued one-time
code separately; keep it private and close its result when finished. The private
operation receipts contain status/time/action only. Receipt capacity is bounded
at 10000 records; reaching it fails closed instead of deleting replay history.

## HTTPS reverse-proxy publication

Set the external URL and trusted *connection peer* CIDRs explicitly. An optional
path prefix is supported. Keep the upstream listener on loopback:

```sh
ivoai admin serve --listen 127.0.0.1:7781 \
  --public-url https://admin.example.com/ivoai \
  --trusted-proxies 127.0.0.1/32,::1/128
```

Configure the proxy to terminate HTTPS and preserve both Host and the entire
`/ivoai/` path. The console takes public identity from configuration, not
`Forwarded` or `X-Forwarded-*` headers. It checks the actual socket peer against the
configured CIDRs, rejects foreign Host/Origin and traversal/escaped separators,
and scopes redirects and cookies to the configured path. HTTPS cookies are
Secure and HttpOnly; responses disable caching and framing. The upstream remains
private; do not publish its socket directly.

Reviewed templates are in `deploy/admin/nginx.conf.example` and
`deploy/admin/Caddyfile.example`. The Nginx `proxy_pass` has no trailing URI; Caddy
uses `handle`, not prefix-stripping `handle_path`. The dedicated Caddy example
disables proxy logging, and Nginx disables access logs, to keep OAuth callback
queries out of logs. Preserve the Core's separate administrative audit. Adapt an
existing multi-site proxy's logging deliberately rather than copying global
settings over other services. Validate your final configuration with `nginx -t`
or `caddy validate` before reloading it.

The templates follow the official [Nginx proxy_pass documentation](https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_pass)
and [Caddy reverse_proxy](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy)
and [logging options](https://caddyserver.com/docs/caddyfile/options#log).
They are publication examples, not a claim that a production domain or TLS
certificate has been deployed by the development tests.

## Context administration

Existing approved filesystem/git connectors feed the current Context service;
there is no second memory system or automatic Memory-to-Context copy.

```sh
sudo ivoai server context ingest --connector approved-documents
sudo ivoai server context search --query 'distinctive phrase from an approved document'
sudo ivoai server context get --id DOCUMENT_ID
```

The same operations are in the management catalog. Ingest-once and the periodic
daemon share a nonblocking kernel lock. A concurrent ingestion is rejected and
must not be replayed automatically. The receipt records connector, document count,
source IDs, content hashes and catalog retrieval checks. Empty/unverified results
remain `CONTEXT_SYNC=BLOCKED`; identified documents with exact recovery may be
`PARTIAL`. A successful source-specific search and exact retrieval in the actual
deployment are still required before claiming synchronization (IVOAI-175).

Ingestion retains the existing pipeline's partial-failure semantics: vector and
catalog replacement are not a distributed transaction. After an error, inspect
and resolve its cause before explicitly authorizing another ingestion. Retrieved
content is untrusted data and never an execution instruction. Agent-facing MCP
Context tools stay read-only.

## Validation and rollout

The backend tests cover the real production CLI adapter/App configuration store,
OAuth read/manage separation, CSRF, exact and stale confirmations, durable replay
rejection across server instances, live revocation/cancellation, malformed input,
secret redaction, shared ingestion locking and existing Context ingestion/retrieval.
A real local TLS reverse proxy exercises PKCE, prefixed cookie paths, Secure,
callback, origin rejection and logout. Deterministic Context-store tests do not
claim a deployed Qdrant corpus, provider inference or production synchronization.
Browser evidence and the final operation-by-operation matrix are recorded with the
composed UI delivery. Catalog coverage alone is not execution certification.
