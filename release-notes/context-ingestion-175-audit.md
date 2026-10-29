# IVOAI-175: ingestion exists; external synchronization remains unverified

This source audit uses main `ccc48a055ae3099a2027cfc8f31210c172baf247`. It did not access a live Context deployment, network service, personal configuration or credentials. No production connector, corpus or index changed. `CONTEXT_SYNC=BLOCKED`; IVOAI-175 is not complete.

## Existing implementation and operational boundary

- `core.ContextBackend` deliberately exposes initialize/search/get/recent/status, not ingestion (`internal/core/contracts.go:225`). The production adapter additionally exposes `Ingest`, forwarding to the existing service (`internal/context/backend.go:76`). No new backend or database is necessary.
- `Service.Ingest` enumerates the named connector, embeds and replaces document vectors, reconciles disappeared documents and replaces that connector's catalog (`internal/context/service.go:99`). It has an aggregate250000-chunk quota. `GetDocument` uses the durable catalog; Search uses the embedder/vector store (`:185`, `:222`). Vector replacement is not transactional: a failed upload requires another authorized ingestion, as already documented.
- The separate `server context serve` daemon initializes the existing local embeddings/Qdrant collection, loads configured connectors and ingests on startup and every five minutes (`internal/servercmd/serve.go:62`). The gateway loads the same catalog/backend for reads, not a second ingestion loop (`:95`). Backend credentials are loaded only on the server from private environment/secret files (`:32`); this audit did not open them.
- Local administrative commands persist connector definitions and restart context/gateway (`internal/servercmd/runner.go:845`). `server context` itself offers status/serve only (`:982`); there is no synchronous ingest-once CLI command. Remote administration offers only status/doctor/connector-list (`internal/servercmd/remote.go:17`). The gateway has no mutating Context route (`internal/gateway/gateway.go:94`).
- Agent MCP intentionally exposes only search/get/recent/health (`internal/context/mcp.go:32`, `:138`). A Memory page write does not enter this pipeline. Existing source documentation's reference to separate ingestion "API routes" should not be interpreted as a delivered mutating HTTP route.
- Zero connectors/documents may be healthy (`internal/context/service.go:225`, `docs/server.md:269`). Health alone cannot prove an ingestion, correct corpus/project identity or retrieval of a specific source. Empty searches alone do not identify the failing stage.

The current agent-facing read-only tools therefore cannot upload this roadmap or trigger an external ingestion. The deployed service version, connector registry, readable approved corpus, embedding/Qdrant health and document identity must be verified at the actual administrative target before claiming synchronization. That is an operational capability/identity gap, not proof that this repository lacks an ingestion implementation. No authenticated deployment was tested in this task.

## Small local correction

The old `connector add` saved the registry and then discarded the result of `systemctl restart` (`runner.go:888` in the audited base). It could return success while the new connector had not been activated. That static path is confirmed; it is not established as the cause of any external IVOAI-175 incident.

Local source `360a9de83e32414ffe894b38c60888dddca62d2c` uses the already-supplied `server.Manager.Controller.Restart` for the same two units. A failed or missing controller now returns a fixed error stating that the registry was saved, the services did not restart and ingestion is not confirmed. It recommends resolving the service failure and using the existing server restart command. It preserves the saved configuration, performs no automatic retry/replay and never echoes service output. The existing SystemdController supplies its ordinary caller-context/timeout behavior; no new service framework is introduced.

Only the test-mode dispatch for the connector command receives a nil controller, preserving its existing no-real-service behavior. A fake controller can exercise the complete private connector method in isolated test layouts. This does not add a new authorization bypass: the existing test-mode/root override and service-ownership rules are unchanged. Other command dispatches and connector removal are unchanged.

The correction does not make successful service restart equivalent to completed indexing. Ingestion remains asynchronous. It does not create a mutating agent MCP endpoint, copy Memory into Context, change credentials, adopt OpenViking/XERJ or change the read-only `ContextBackend` contract.

## Validation bound to the local source

The [receipt](evidence/context-connector-restart/validation.json), SHA256 `6534911042e69bfe0c88e99983cead4979f6e5075e2c004f7407856498bd712f`, records593 Go/module paths frozen before/after each gate.

| Gate | Result |
|---|---|
| Count1/race: servercmd, server, context packages |3 packages PASS,56 top-level tests/78 passing events,0 failures/0 skips;6.807s command walltime |
| Vet for those three packages |PASS;0.834s walltime |
| CGO0 Linux ARM64 servercmd test compilation |PASS;3.123s; binary not executed or distributed |

The new regression exercises successful and failed restart through a fake controller. It requires exactly one restart of the two existing units, retention of old/new connector definitions and0600 registry permissions, no private diagnostic output, and refusal of a duplicate add without another activation attempt. The existing public CLI idempotency test remains unchanged and passed, preserving its test-mode hermetic behavior. Existing Context tests cover the ingestion/search/read-only MCP pipeline, filtering, reconciliation and purge using synthetic stores/HTTP fixtures, not a deployed Qdrant corpus.

The new test against the old source failed in both subcases because the supplied controller was bypassed/ignored in test mode. That control is preserved; it is not presented as an external incident reproduction or as a real failed systemd call. Linux-only server setup rejection remains unchanged; no new non-Linux support or native ARM execution is claimed. No complete application suite, hosted CI, deployment, real server command, external ingestion or source-specific retrieval ran in this increment.

## Remaining acceptance and rollback

After separately reviewing this patch, identify the actual server/source version and existing scoped administrative path. Select an approved nonsensitive document set and connector root readable by the existing service identity; do not weaken `ProtectHome`, grant credential access or use unsafe links to reach a personal checkout. Validate registry activation, observe actual ingestion success/failure and counts, then use the existing Context search plus exact get-document to verify the intended source/path/content identity. Record partial failures and retry only the authorized ingestion after its cause is resolved. This work needs no new database or second memory system.

Keep `CONTEXT_SYNC=BLOCKED` until that path is available; use PARTIAL only when some identified documents have actually been ingested/retrieved. A successful local fake-controller test or Memory write is insufficient. Reverting the local code commit restores the prior diagnostic behavior without a schema migration or deleting connector definitions, documents or index state. No production rollback was executed.
