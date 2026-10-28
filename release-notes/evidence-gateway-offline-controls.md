# Evidence Gateway offline App controls — IVOAI-138 / IVOAI-198

**Fixture integrity correction:** the initial Git environment omitted
`GIT_CONFIG_PARAMETERS`; a controlled private post-checkout hook demonstrated
that inherited hooksPath could run during worktree creation. The original
artifacts remain unchanged. The [v2 correction and new receipts](evidence-gateway-offline-controls-env-fix.md)
record the adverse reproduction, explicit Git environments and executable
rejection tests. The original statement below about disabled hooks was incomplete and does not
establish isolation from hostile inherited configuration.


This test-only increment proves activated App/Gateway reuse and invalidation on
one small, executable offline fixture. It does not measure agent economics.
**No LLM/native-client turn, actual worker dispatch, or Graft execution occurred.
The economic gate remains INCOMPLETE; the 126 historical attempts and the matrix
catalog (9 READY / 5 UNSUPPORTED / 2 BLOCKED) are unchanged.**

Both arms run the same immutable App test binary and the existing repository and
session fixtures. OFF sets `IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY=0`; ON sets it to
`1`. Each arm keeps one App/session alive across the sequence, with its own
private cache, repository and valid owned Git worktree. The corpus bytes,
normalized skeleton query and 3072-byte retrieval budget match. Primary calls use
`preparePrimaryCodeEvidence`; already-admitted host worker metadata fixtures use
the shared `prefetchCodeEvidence` method. These fixtures are explicitly
`NOT_ACTUAL_DISPATCH`, with real workers UNKNOWN. No scheduler, public contract,
provider selection, feature default or production code changed.

CodeIntel retains its normal CacheRoot in both arms. This known small-file query
selects the actual direct CodeIntel implementation; it does not exercise the
Graft structural result cache. Per-operation snapshots count `code.inspect`
observations and `capability.resolve` / `evidence.code` observations separately.
Incomplete cache observations fail the control. A Gateway hit cannot be inferred
from the flag alone or miscounted as a CodeIntel cache hit.

| Operation | OFF CodeIntel calls | ON CodeIntel calls | ON Gateway |
| --- | ---: | ---: | --- |
| Primary cold | 1 | 1 | MISS |
| Primary warm | 1 | 0 | HIT |
| Worker fixture, same worktree | 1 | 0 | HIT, artifact reissued to worker owner |
| Worker fixture, distinct worktree, same bytes | 1 | 1 | MISS |
| Primary after source generation edit | 1 | 1 | MISS, updated exact bytes |
| Primary after grant revoke | 0 | 0 | no retrieval; denied in both arms |

Every positive call verifies the bounded, untrusted, exact-required envelope and
reads its complete issued artifact through **host ArtifactStore.Read with the
exact request owner**. Cross-owner Stat and Read are rejected. The original
low-level Gateway admission tests remain; their reusable assertions now also
check the stored bytes. Grant revocation proves admission blocks new primary
retrieval, not that an existing ArtifactStore archive reference is revoked.
The base is `16f4e42f3528a71522023ed61bc2406ca6ab8d2a`, which predates G0. This
control does not certify the protected public Core/MCP recovery boundary added
separately in PR50, and it does not duplicate that security work.

The exported receipt contains 12 bounded rows: 10 exact reads and two expected
admission denials. OFF observed five CodeIntel inspections; ON observed three,
five Gateway retrievals and two Gateway hits. CodeIntel cache hits were zero
in the reported successful direct-read events. **ON was slower for every positive
retrieval in this single exported run**, including cold (20.947 ms versus
10.243 ms) and warm (15.532 ms versus 9.780 ms). These timings include host checks
and artifact work. They are a shared-host OFF/ON diagnostic with no matched-load
claim, no ABBA repetitions and no causal performance estimate. Skipping an
inspection on this tiny source does not establish lower end-to-end cost. No
result was discarded, rerouted or tuned to obtain a favorable comparison.

[The receipt](evidence/backend-first/gateway-offline-controls-v1/receipt.json)
keeps only fixed enums, counts, durations, byte counts and content digests, never
source text, dynamic owner IDs, paths, command arguments, configuration or
credentials. Export is opt-in, bounded to 32 KiB, and requires a new 0600 file in
an explicit absolute private directory. Existing output, a nonprivate parent
and inherited Git redirection were rejected before a control arm started. Git
uses a private HOME and template directory, no global/system configuration and
fixed local operations with hooks/signing disabled; no network or installer is
needed. These safeguards affect only this offline test fixture.

The [validation](evidence/backend-first/gateway-offline-controls-v1/validation.json),
[pre-run freeze](evidence/backend-first/gateway-offline-controls-v1/freeze.json)
and [source inventory](evidence/backend-first/gateway-offline-controls-v1/source-inputs.sha256)
identify the base, changed test files, all Go inputs, adapter, go.mod/go.sum,
compiler/Git executables and exact executed AMD64 binary. Source and binary
hashes were checked again after export. This is not a whole-host, shared-library
or whole-toolchain attestation. The pre-run manifest's initially empty adverse
list is superseded by the validation's recorded slower ON observations.

Validation passed the five top-level Gateway tests with race detection (2.745 s),
the Evidence, WorkingContext and CodeIntel package race suites, App vet, the
frozen binary's explicit export, and three expected-rejection export/environment
controls. The generic Gateway run skips the export test as NOT_RUN; only the
separate explicit export is counted as receipt execution. Linux AMD64 v1 was
compiled and executed. Linux ARM64 was cross-compiled, not executed. Source and
binary hashes stayed frozen. No new benchmark, sampler calibration, model/cache/
quota equivalence, native TUI parity, release, public recovery certification,
Context ingestion or live-provider result is claimed. CONTEXT_SYNC remains
BLOCKED under IVOAI-175.

To run the default offline mechanism assertions with a reviewed Go toolchain:

```sh
go test -race ./internal/app -run 'Test.*Gateway' -count=1 -v
```

For a new exported receipt, create a new private output directory, build once
with `GOAMD64=v1 go test -c -trimpath -buildvcs=false -o /private/app.test
./internal/app`, freeze the binary and source hashes, then run only:

```sh
TMPDIR=/private/tmp IVOAI_GATEWAY_CONTROL_OUTPUT=/private/new-receipt.json /private/app.test -test.run '^TestExportEvidenceGatewayOfflineControls$' -test.count=1 -test.v
```

Do not reuse a historical receipt path. Timing repetitions or an actual pinned
Graft fixture need their own declared conditions; neither is silently combined
with this version. Sampler overhead calibration is a separate offline increment.
Rollback is a revert of this test/documentation commit, with no persistent or
personal configuration migration.
