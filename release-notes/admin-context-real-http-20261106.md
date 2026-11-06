# Administrative Context HTTP integration — 2026-11-06

Three previously unavailable positive administrative operations now pass against
the real private Context stack: `server.context.ingest`, `server.context.search`
and `server.context.document`. This is one executed integration test covering
three action IDs, not three separate provider sessions or a global parity claim.

The fixture in `internal/admin/context_real_http_test.go` uses the actual loopback
HTTP handlers, real OAuth authorization store/PKCE token exchange, browser-cookie
authentication, Origin/CSRF validation and `cli.Run`. It registers the existing
`servercmd.New` dispatcher exactly as the executable entry point does. The same
Core services then call real TEI embeddings and Qdrant. There is no fake runner,
deterministic embedding model, fabricated successful callback or TEST_MODE/server
root override. This test does not navigate the browser UI or execute the published
Core release binary: it executes a compiled Go test containing the real handlers,
CLI dispatcher and Core, based on source `6179d2172` plus the new fixture.

## Observed results

- Real ingestion imported exactly one curated UTF-8 document, excluded `.env`,
  verified exact recovery and returned the existing `PARTIAL` ingestion receipt.
- Real semantic search found only that document with score `0.88827425`; exact
  retrieval preserved all bytes and SHA-256
  `ee6a750ee1d37e6f03c2d1461cea9213f12bb89d2d90345646b2010c2e862118`.
- The mutating ingest required preview/confirmation. Its duplicate returned HTTP 409
  without a second dispatch. Search/document are reads in the existing catalog:
  their previews require no mutation confirmation and do not dispatch. Authorized
  repeated reads correctly returned HTTP 200 with the same output. The resulting five
  dispatches are one ingest and two executions of each read.
- Missing administrative authentication returned HTTP 401, invalid CSRF returned HTTP 403,
  both before any CLI dispatch. Both real backends rejected a missing API key.
- TEI raw startup logs contained neither transient backend key before redaction:
  1,755 bytes, SHA-256
  `2b170838a09b0df84e3263957650fa362b462fec0b4590d45dcf086504876924`.

The full stack window took 105.801168487 seconds, including cold VFS container creation.
The integration after model readiness took 3.80194716 seconds. The single reviewed image
pull took 322.59949416 seconds. These are functional execution durations, not controlled
performance comparisons, token measurements or economic savings.

## Runtime identity and isolation

Qdrant1.20 uses the existing digest
`sha256:9bfc89f9914fc14876a88c05270685921a55b8877b254a98bcc1cc2f0a424cd2`.
TEI CPU 1.9.4 uses
`sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d`,
OCI revision `e80ef225ed0e6cb1717ce632a6a84b6cf211bb67` (Apache-2.0).
The existing MIT-licensed multilingual E5-small model remains pinned at
`614241f622f53c4eeff9890bdc4f31cfecc418b3`. Its eight source-file hashes were
reverified after the read-only runtime against the retained original provenance.
No model was downloaded again. The runtime used the previously reviewed Candle
CPU fallback with 384-dimensional embeddings; this does not certify ORT, ARM or
TEI on CPUs without AVX/AVX2.

The services shared one unique network-none namespace, without published ports,
with read-only root filesystems, dropped capabilities and no-new-privileges.
Before touching ordinary fixed server paths, the fixture requires an explicit
opt-in, a private 0600 nonce marker, a network namespace distinct from the parent,
mapped-root identity and dedicated mounts for `/etc/ivoai`, `/etc/systemd/system`,
`/var/lib/ivoai` and `/opt/ivoai`. The exact final test binary also refused a forced
host execution before layout writes. The ordinary offline invocation skips
explicitly; that skip is not counted as a real integration success.

Docker used the existing private rootless VFS data root. Its previous daemon was
already stopped after the interrupted session. Only that private daemon was
recovered for this test and then stopped by verified PID/start identity. Five
historical images remained intact, including verified configuration hashes.
The two owned containers, generated env file and all three copied secret files
were removed. Only the task's exact TEI image was removed after verifying no
container references. No global prune, production service, user credential,
personal configuration or shared Context was accessed. Root free space after
cleanup was 24,157,491,200 bytes. A 16 GiB pull watchdog and 12.5 GiB run reserve were
enforced; the pull minimum was 18,189,479,936 bytes. Go caches/temporary build files
were bounded external derived data, never a Docker data root on NTFS.

Mapped-root service identity does not certify production UID 1000, systemd or
cgroup resource enforcement; the private daemon has no usable cgroup controller.
Shared Mindsite `CONTEXT_SYNC` remains BLOCKED under IVOAI-175. This proof's
`PARTIAL` receipt does not synchronize shared memory or RAG.

## Validation and retained failures

Final-source `go vet ./internal/admin` passed. The opt-in compiled fixture passed
against the actual stack without retries. Default-offline skip and host-boundary
refusal were checked on the exact final binary. No live race-detector execution
is claimed by this report; ordinary CI and earlier product race gates retain
their separately recorded scopes.

Harness failures remain visible: an absent temporary build directory, the first
private-daemon clean PATH missing `sysctl`, and an inventory assertion using the
default image listing that hides digest-only images. None was a successful
runtime integration. Review corrected read-action expectations and registered the
real CLI server dispatcher before the actual stack execution. No production Go
behavior was modified.

Evidence and hashes: [evidence/admin-context-real-http-20261106](evidence/admin-context-real-http-20261106).
The exact compiled fixture SHA-256 is
`cfae24034be7b8cacb9a038cfcb2388bd85ff9f2874d69be5937767ee336bbf8`;
fixture source SHA-256 is
`06a2e4be3a69d9cd5c2a1f42db7544f7e247b4e6711f85ab9e943ed825ce94eb`.
These proofs reduce three administrative certification gaps when integrated;
IVOAI-189 remains In Progress until its remaining criteria are proved.
