# Private Context ingestion certification — 2026-11-06

A real disposable stack now passes the existing administrative CLI flow: ingest a
curated filesystem document, retrieve it by semantic search, and recover its exact
UTF-8 bytes and SHA-256. The agent-facing Context MCP remains read-only.

## Product fixes

- `9d643eb74`: give only cold Qdrant collection creation a bounded 60-second
  budget. The original real create completed in 21.565553 seconds, after the
  previous 15-second client timeout. Ordinary requests retain their 15-second
  default; explicit clients, caller cancellation and earlier deadlines remain
  authoritative. No create retry was introduced.
- `1e95aa6c5`: put `wait=true` in the URL query for point upsert/deletion.
  Previously Qdrant received `/points/delete%3Fwait=true` and returned 404.
  Proxy base paths, including escaped paths, remain intact. Collection names are
  still validated and mutations are never retried automatically.

## Executed integration

The source fixture is `internal/servercmd/context_private_docker_test.go`.
`TestContextPrivateRealStack` invokes the actual compiled `ivoai server context`
CLI, without `IVOAI_TEST_MODE` or a server-root override. It requires an explicit
opt-in, a host-created identity marker and a separate network namespace before
using the normal fixed server paths. It uses real HTTP model inference and the
real Qdrant store, with no deterministic embedding provider.

The successful CLI/HTTP fixture completed in **0.398566740 seconds** after an
explicit private reset of the empty collection left by the failed attempt. This
is a functional integration duration, not a session performance benchmark.
Qdrant logs show a new 404 collection lookup, a successful create and synchronous
point mutations. The cold-create timeout failure and the subsequent 404 failure
remain recorded separately; neither was relabeled as a success.

Verified:

- One curated document, semantic search score 0.893796, and exact retrieval SHA-256
  `00aae2fa6b69bb95c324dbd14f3ef23962f0a81d145c09ac410e4c502f77a30b`.
- `.env` and a symlink outside the source are excluded; a second real Qdrant
  collection/catalog cannot enter the main source's retrieval.
- Gateway Context calls reject unauthenticated access with 401, insufficient scope
  with 403 and a revoked bearer with 401. Authorized exact retrieval succeeds;
  `context_ingest` is absent from the read-only MCP tools.
- A separate UID1000 CLI invocation is denied by the existing administrator
  requirement before mutation; the authoritative catalog hash remains unchanged.
  Its first harness attempt could not execute because the private binaries
  directory had mode0700. That exit126 is retained as an infrastructure failure;
  the corrected nonsecret binary mount allowed the actual authorization check.
- The private ingestion receipt reports `PARTIAL`, not synchronized shared memory.

## Runtime, security and limits

Qdrant1.20 uses digest
`sha256:9bfc89f9914fc14876a88c05270685921a55b8877b254a98bcc1cc2f0a424cd2`.
TEI CPU1.9.4 uses digest
`sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d`;
its `/info` and OCI metadata identify source revision
`e80ef225ed0e6cb1717ce632a6a84b6cf211bb67`.
The MIT-licensed multilingual E5-small model is pinned at
`614241f622f53c4eeff9890bdc4f31cfecc418b3`. Every downloaded model file was
verified against that revision's official Git blob or LFS digest. No installer
or global configuration was changed.

The real model backend was **Candle CPU**, using verified `model.safetensors`:
the optional ONNX file was not downloaded and the router visibly fell back from
ORT. This certifies that supported inference path, not ORT parity. Real embeddings
had 384 dimensions. The hardware exposed AVX/AVX2/SSE4.2; this is not a TEI/Qdrant
certification on CPUs lacking those features or on ARM.

The services shared one unique `--network none` namespace, with no published
ports, a read-only filesystem, dropped capabilities and no-new-privileges. They
ran as rootless mapped UID0, not production UID1000. The daemon had no usable
cgroup controller; no resource-limit enforcement is claimed. No personal
credentials or shared Mindsite Context were mounted or modified.

The TEI runtime applied the separately delivered product logging setting
`LOG_LEVEL=info,text_embeddings_router=warn`. Its raw Docker log was nonempty
(1755 bytes) and contained neither transient backend key **before sanitization**;
SHA-256 `63ae9193bfa7d52ae63576076e1e52b3972b2012950e8ea8190c8f03f95edade`.
Real embedding, missing-key rejection, `/health` and HTTP `/metrics` passed.
WARN/ERROR messages remained visible. A separate diagnostic exec in that same
container, using an empty local model directory, exited1 with a visible
`config.json not found` error and no key (86 raw bytes). This diagnostic is not a
Docker startup-log check.

Both owned containers and their transient env file were removed after the proof.
Only this task's exact TEI image was removed, after verifying no other container
referenced it. The original daemon and five historical images remained. The model
and receipts remain available; no global prune occurred. Root filesystem free
space returned to 15,051,927,552 bytes at cleanup.

## Verification and remaining work

- Context mutation RequestURI/auth/isolation/no-retry tests: pass.
- Cold-create delay beyond15 seconds, explicit-client deadline and caller
  cancellation tests: pass.
- Complete `go test -race ./internal/context -count=1`: pass,17.119s.
- `go vet ./internal/context`: pass.
- Actual real-stack readiness: pass,0.60s; actual CLI/HTTP fixture: pass,0.40s.
  The opt-in integration is not claimed to execute in ordinary offline CI.

The full-stack collection was cold, while model process and image layers were
already loaded. This is not a benchmark of deployment startup or token economy.
Prior pull timeouts, disk-pressure cancellation, NTFS/VFS extraction failure,
readiness-probe mistakes and product failures are retained in the private evidence
root and summarized in the receipts. The existing shared Mindsite ingestion
backlog **IVOAI-175** remains a separate operator action: **no shared
`CONTEXT_SYNC` advancement is asserted**. Parent integration and release CI are
separate gates.

Machine-readable results, original failed CLI outputs and the executed artifact
identities are in [evidence/context-private-20261106](evidence/context-private-20261106).
