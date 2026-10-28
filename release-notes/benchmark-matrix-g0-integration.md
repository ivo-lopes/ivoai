# PR46 integration with protected recovery main064 — IVOAI-138 / IVOAI-198

This new source epoch combines PR46 `a137d693ebd7f48eb3701b1e019c734e6264425c`
with main `064edc3ad6972bf8bfd2d95f738c9732d1102396` after PR50. The merge is
automatic with no conflicts or manual runtime changes. All 94 incoming G0 paths
match main byte for byte, including the mandatory rollback steps in CI/Release.
Protected Evidence/Core recovery and its namespace/binding checks are inherited
from that approved implementation, not introduced as a benchmark optimization.

**All 193 previously tracked evidence files remain byte-identical**, including
the original 140 files, the preceding epoch's 22 indexed artifacts and their
manifest/imported main proofs. The six comparison arrays under
`release-notes/evidence/backend-first/` still retain 126 actual rows; no source
identity, outcome, failure or economic result was rescored. This count describes
only that lineage, not the repository's total historical runs. Older PR30
`economic-runtime` cohorts and the separate projection pilot are also preserved
but lie outside this tally; heterogeneous cohorts are not aggregated. The previous integration report stays unchanged. Its local checks
describe its own a137 source, not this newer epoch. No real client/provider turn,
new benchmark, calibration or optimization occurred here. The global economic
gate remains INCOMPLETE.

The [new source epoch](evidence/backend-first/matrix16-g0-integration-v1/source-epoch.json)
binds both parents, merged runtime input tree, 555 source inventory entries,
exact build hashes and rollback provenance. The
[historical inventory](evidence/backend-first/matrix16-g0-integration-v1/historical-artifacts-before.json)
and [validation](evidence/backend-first/matrix16-g0-integration-v1/validation.json)
record before/after preservation. Benchmark driver, common harness, sampler,
catalog and edit-oracle source bytes are unchanged. Protocol 3 retains mandatory
discovery even for default scenarios. No catalog status was promoted. Future
comparison arms must rebuild the identical final common harness and pass strict
discovery; old frozen binaries and C6/v0.12.1 receipts cannot certify this runtime.
Model/backend/cache/quota equivalence remains UNKNOWN.

All required local gates passed against the combined source:

- Full uncached Go suite: 59 packages passed, three had no tests.
- Full Python suite: 65 tests passed (18.658 s).
- Uncached race checks: 18 affected packages passed, including App, CodeIntel,
  benchmark fixtures, Evidence, WorkingContext, Orchestrator, workers, agents,
  external MCP, observations and the relevant host/CLI packages.
- Mandatory source-pinned legacy rollback: named test executed once, zero skips,
  validator PASS. The ancestral Core at
  `3be82befea6ed3875373afcb1bfaa450710310ac` rejected protected full/range reads;
  its legacy put/read/GC still worked and protected payload bytes remained intact.
  Its inventory excludes protected storage, so old Core disk/quota reporting is
  not claimed to include it. No network/download fallback was used.
- Explicit Bubblewrap Go/TypeScript edit-oracle gates both passed, zero provider
  attempts. This executes the opt-in separately from the generic suite.
- Full vet, read-only formatting and both build gates passed. AMD64 v1 ran
  `version`; ARM64 was cross-compiled, not executed. No non-AVX hardware execution
  or whole-toolchain attestation is implied.

The [rollback proof](evidence/backend-first/matrix16-g0-integration-v1/rollback-provenance.json)
records legacy source/probe/binary and module-validation hashes alongside the
named current test stream. Runtime checks used private HOME, pinned Go/Node
paths, absent provider/auth opt-ins, verified cached modules and disabled Go
network access. Synthetic protocol fixtures do not establish actual worker
counts, authenticated native parity or live G0 consumption. Check durations are
quality observations, not economic measurements.

`v0.14.0` remains the immutable original tag at main6d99, with G0 and this epoch
outside it. This integration does not publish or replace any release assets.
Official-client, browser/UI, native ARM/non-AVX and full exact-head hosted CI gates
are not silently substituted by these local checks; remote CI must pass before
merging PR46. PR51 and the sampler-calibration branch remain separate. The latter
is still LOCAL with an independent fixture failure under investigation; none of
its results are imported into this source epoch.

CONTEXT_SYNC remains BLOCKED under IVOAI-175. Rollback reverts this merge on the
benchmark branch; persistent protected objects follow the independently validated
G0 compatibility contract. No user credential, configuration or data migration
was changed by this integration.
