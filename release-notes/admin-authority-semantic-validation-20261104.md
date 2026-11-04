# Administrative authority and semantic execution follow-up

This follows the initial [administrative increment](admin-management-20261104.md)
and preserves its original evidence. The current catalog still contains 123 rows,
now classified as **73 mutations, 32 reads, 11 native handoffs and seven information
rows**. `mcp.test` persists a probe result and `doctor` persists discovery/quota
observations, so both require management scope, exact preview confirmation and a
one-use request claim. `doctor.inventory` remains a read-only support inventory.
Native login/chat handoffs remain terminal operations and never count as web
administrative execution.

## Functional/security fixes

- Read-only OAuth grants cannot refresh MCP/configuration or doctor caches.
  Legitimate management MCP probes still use the existing registry and persist
  health/tool observations; replay is rejected before a second invocation.
- The Context ingestion lock validates the opened regular inode, owner, group,
  link count and mode, and explicitly sets 0640 after creation. A restrictive
  umask no longer leaves a root-created lock unreadable by the service identity.
  A real different-UID subprocess succeeded in a mapped Linux user namespace;
  this is not a production-root or deployed-daemon certification.
- A truncated result is visibly identified as incomplete in the browser. The
  warning states that the complete result was not retained and instructs users
  to inspect state/available records without repeating the mutation. Full
  sanitized output archival/download is **not implemented** in this increment.
- Observed `antigravity` session identity is displayed. No quota is invented and
  unsupported experimental executors are not added to global routing choices.

## Actual semantic HTTP execution matrix

`TestAdministrativeOperationCatalogThroughActualCLIAndIsolatedServices` traverses
all **105 applicable** catalog actions through real OAuth admission, CSRF,
management preview, HTTP dispatch and the actual CLI parser/App services with
independent temporary state. Configuration actions verify the specific persisted
setting; MCP, server profiles, secrets, skills policies, project markers, session
stopping, connectors, gateway settings, backup and access registries verify their
isolated effects where available. Read actions must not change protected stores.
Every advertised field also has required-input, enum, flag/control-injection and
secret-to-stdin contract checks. These are additional gates, not a relabeling of
fixtures as provider inference.

| Run | Completed isolated service operations | Existing OS privilege denial | Dependency/precondition unavailable | Authorized uninstall invalidated authority |
| --- | ---: | ---: | ---: | ---: |
| Ordinary UID | 76 | 16 | 12 | 1 |
| UID 0 in disposable user namespace | 85 | 0 | 19 | 1 |

The 105 records include every applicable ID. Unavailable operations retain their
actual failure result and an explicit non-certification scope; newly unexpected
failures fail the test instead of being accepted generically. Uninstall removes
its isolated authority, withholds its result, and subsequent dispatch is denied.
These counts are **not 105 successful operations**, provider certification,
production systemd administration, or proof that IVOAI-189 is complete.

The namespace's 19 unavailable operations are: capability rollback without a prior
revision; hook repair without a managed installation; three legacy remote reads
without an enrolled legacy server; binary rollback without a prior binary; three
Context reads/ingestion without live backend dependencies; service logs/start/stop/
restart without systemd; memory status without ai-memory; server restore without
the service account; two scope migration actions without an eligible managed
conversation; and update/check without external release access. The ordinary-UID
run additionally verifies existing privilege denials instead of elevating via HTTP.

Native process calls use a fixture runner, and external MCP uses an isolated
JSON-RPC server. Real provider inference, actual install/update artifacts,
production lifecycle/restore and deployed Context retrieval remain separate gates.
No personal credentials, host services, production corpus or configuration changed.

## Reproduction and retained evidence

Run the bounded matrix and catalog checks with:

```sh
GOMAXPROCS=2 go test ./internal/admin \
  -run 'TestAdministrativeOperationCatalogThroughActualCLIAndIsolatedServices|TestEveryAdministrativeForm' -count=1
GOMAXPROCS=2 go test -race ./internal/admin ./internal/cli ./internal/servercmd -count=1
```

Set `IVOAI_ADMIN_OPERATION_MATRIX_OUTPUT` to a private JSON destination to retain
per-ID status/changed-store names. No command input, secret values, output payloads
or one-time activation codes enter the matrix. The mapped-root run compiles the
same test and executes it with `unshare --user --map-auto --map-root-user`; this
requires user namespaces and does not imply host root access. Security and
semantic evidence, including failed fixture development runs, is retained in the
managed `ivoai-admin-hardening-20261104` scan artifact set rather than replacing
prior reports. Integration/release CI and root-owned real service certification
remain the release coordinator's responsibility.
