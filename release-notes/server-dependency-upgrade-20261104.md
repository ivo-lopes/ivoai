# Server dependency upgrade checkpoint

Base: `c6ab79b1f`. This checkpoint implements a guarded dependency upgrade and
coordinated rollback path; runtime certification is still pending at this commit.
Do not interpret its unit fixtures or registry manifests as real migration tests.

Current reviewed targets: ai-memory 3.3.3 (MIT; official AMD64/ARM64 archives),
Qdrant 1.20.0 (Apache-2.0; AMD64/ARM64 OCI index), Docker Compose 5.7.0 (official
binary/checksum assets). TEI 1.9.4 CPU/ARM64 registry digests remain unchanged, and
the model's current main revision remains `614241f622f53c4eeff9890bdc4f31cfecc418b3`
(MIT). Docker Engine 29.9.3 was verified in official release notes and the private
validation daemon; existing operator-owned daemons are not silently replaced.

Ordinary setup refuses changes to runtime/model pins when dependency data exists.
`server upgrade` takes the existing stopped-service lifecycle, creates a Core
backup, preserves the Qdrant index separately, restores isolated copies for actual
candidate startup/authorization probes, and only then installs new managed assets.
A private operational receipt binds previous pins/units and index snapshot to the
exact Core backup. It does not expand Core Restore's executable-config whitelist.
`server upgrade --rollback-receipt PATH` explicitly restores data, index and old
runtime assets together; current forward index data is preserved for inspection.
A failed operation does not automatically start a partially changed runtime.
The existing Core scheduler/authentication boundaries are unchanged.

All mutations share a kernel lock before quiescing. Candidate validation uses the
exact pinned images with network none, non-root service identity, transient
private authentication, resource limits and a staged trusted Core probe; tokens
never enter command arguments or command output. Managed server configuration is
preserved rather than reset during upgrade. The panel exposes both upgrade and
explicit rollback as root-required, confirmed, non-replayable actions.

At this checkpoint, six focused unit tests passed for guard-before-write,
copy/original isolation, coordinated data/index/pin rollback, rejected candidate,
index tampering, stopped-service authority, concurrent lock and private/path-safe
receipts. Existing focused server/servercmd/components compilation tests passed.
These use fixture service controllers and are not production systemd or official
container startup certification. Real private Docker upgrade/rollback, native
AMD64/no-AVX checks and ARM execution limitations will be appended after execution.
