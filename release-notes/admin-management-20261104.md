# Administrative management and HTTPS proxy increment

Base: `d816a492`. Initial implementation: `d25b5369`. This increment implements
an administrative API over the existing application services and a production CLI
adapter; it does not add a scheduler, agent framework or conversational frontend.
The composed UI/browser proof and release integration are separate gates.

## Implemented contracts

The catalog contains 123 rows: 71 mutations, 34 reads, 11 native handoffs and seven
information/navigation/fixed-policy rows. It includes every one of the 118 public
TUI IDs plus profile preview/apply and Context ingest/search/get. The
[bounded operation map](evidence/admin-management-20261104/catalog.json) records
ID, category, typed input names, command templates, optional flags and existing
service/CLI binding. `all_operations_e2e_certified` is deliberately false; source
coverage is not proof of 123 real executions. Native login/chat handoffs never
count as executed web administrative operations.

- `GET /api/admin/v1/actions`: typed forms and capability availability.
- `GET /api/admin/v1/csrf`: in-memory browser CSRF token and read/manage authority.
- `POST /api/admin/v1/actions/{id}`: JSON input, `preview`/`execute`, one-use exact
  confirmation and caller request ID. The public API accepts no executable,
  arbitrary argv or shell command. Secrets enter existing services through stdin.
- Existing snapshot/session/event endpoints retain their routes and read scopes.
- New `admin init` creates missing private roots only. `admin access create --write`
  deliberately grants `admin:manage`; default and prior grants remain read-only.
- `admin serve --public-url https://host/prefix --trusted-proxies CIDR,...` keeps a
  loopback upstream. Public identity comes from configuration and actual socket
  peer, not forwarded headers. Prefix-aware OAuth, redirects, Secure/HttpOnly
  cookies and same-origin checks were exercised through a real local TLS proxy.

Every mutation is confirmed against its exact input, credential and config/state
fingerprint. Root paths, grant and management authority are rechecked. Durable
request claims precede dispatch and remain claimed after error/cancellation or a
server restart. A common kernel lock serializes console processes; conflicts are
returned instead of queued/retried. Project initialization requires an explicitly
matching console working directory. In-flight command contexts are cancelled on
revocation; already committed service changes cannot be magically rolled back.
Output is bounded/redacted; audit and durable receipts contain no parameters,
output, codes or credentials. Explicit newly issued one-time codes are returned
separately and must not enter browser history or logs.

## Context boundary

`server context ingest --connector NAME`, `search --query QUERY` and `get --id ID`
reuse the current backend and private server credential loader. Ingest-once and
the periodic daemon share the same nonblocking lock. The administrative receipt
verifies stored source IDs/content hashes and exact catalog recovery. A nonempty
verified corpus may be PARTIAL; empty/failed results remain BLOCKED. Source-
specific deployed search/get are still required before closing IVOAI-175.
Agent MCP remains read-only. No production corpus/service or personal credential
was changed here, and no Memory page is silently copied into Context.

## Actual local validation

- Full admin/CLI/servercmd focused suites passed, including existing cases.
- Race tests for these three packages passed; vet passed. These are package gates,
  not a claim that every provider or privileged operation ran in production.
- A production HTTP/CLI adapter test changed the real isolated App configuration,
  read its safe projection and rejected replay. Enumerated configuration choices
  passed the actual App validators; the catalog covers every public menu ID.
- Adverse tests exercised read scope, missing CSRF, changed inputs, stale policy,
  revoked tokens, live cancellation, duplicate/restart claims, secret redaction,
  option/control injection, symlink state and mismatched project scope.
- A real local TLS proxy exercised PKCE/login, mounted callbacks and cookie paths,
  Secure/SameSite/HttpOnly, origin rejection, API access, revocation/logout and
  traversal/forwarded-header rejection.
- Existing Context Service with an approved filesystem fixture, deterministic
  embedder/vector store and real catalog operations passed ingest, exact get,
  source-specific query, sensitive-file filtering and shared-lock rejection.
  This is not deployed Qdrant or provider inference.

## Remaining composition and certification

The UI must be composed with this backend and prove read/manage flows, uncertain
transport outcomes, revocation, one-time-code handling, a11y and prefix/TLS behavior
in a real browser. Root integration must certify all applicable administrative
operations by capability with controlled fixtures, including privileged service
lifecycle, backup/restore, connector activation, installations/updates, external
MCP/enrollment/auth and actual deployed Context retrieval. Existing CLI/service
tests provide relevant underlying coverage; this increment does not relabel those
as 105 new real HTTP executions. Provider authentication must stay in official
clients, with unavailable surfaces recorded rather than simulated as success.

Nginx/Caddy templates preserve the prefix and avoid proxy request logging. Their
exact deployment configuration still needs `nginx -t`/`caddy validate` and a real
proxy fixture or deployment proof. No production domain/certificate is claimed.
Root should regenerate the CLI reference and EN/PT-BR embedded site once after
composing all changes, and preserve prior release/benchmark histories.
