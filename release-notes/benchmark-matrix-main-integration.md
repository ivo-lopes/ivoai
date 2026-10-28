# PR46 integration with main6d99 — IVOAI-138 / IVOAI-198

The benchmark branch at `16f4e42f3528a71522023ed61bc2406ca6ab8d2a` now
includes main `6d99e320912f3f7325c3009209a10e9df86610d4`. The merge was automatic,
with no conflicts or manual runtime changes. Main brings the already developed
Claude readonly-worker boundary, administrative backend/UI, managed legacy
patch.3 and exact-Core release gates. This integration introduces no new
optimization or benchmark contract and does not modify the separate PR51
Gateway-control worktree.

**All 126 historical real comparative rows remain byte-identical.** They reside
in six existing runtime-comparison files with 18, 18, 18, 18, 42 and 12 rows.
Hashes of all 140 previously tracked evidence files were checked before and
after the merge and tests; none changed. The previous failures, diagnostics,
worker/model/cache/quota uncertainties and source identities remain historical.
No result was rescored or attributed to the newly combined runtime. There were
no new real client/provider turns, sampler calibration or economic measurements.
The global economic gate remains INCOMPLETE.

The driver, catalog, common harness, sampler and edit-oracle sources are unchanged
from the PR46 head. Protocol 3 still requires discovery before every default or
selected client scenario. Existing tests retain rejection of missing/wrong or
stale capabilities before a client can start. No catalog status was promoted.
Although these contracts are unchanged, their future runtime candidate now has
new production code: **both comparison arms must rebuild the identical final
common harness and pass a new strict discovery before any provider turn**.
Frozen C6/v0.12.1 binaries and their receipts cannot certify this combined source.

The [source epoch](evidence/backend-first/matrix16-main-integration-v1/source-epoch.json)
records both parents, the merged input tree before these new reports, 547
compiled-source inventory entries, unchanged protocol-file hashes, tool hashes
and exact AMD64/ARM64 build hashes. The
[historical inventory](evidence/backend-first/matrix16-main-integration-v1/historical-artifacts-before.json)
and [validation](evidence/backend-first/matrix16-main-integration-v1/validation.json)
retain the preservation and local-gate evidence. Tool/binary hashes are not a
whole-toolchain, shared-library or host attestation.

Local checks passed:

- Full Go suite: 59 packages passed; 3 packages had no tests.
- Full Python suite: 61 tests passed in 21.849 s.
- Uncached race checks across 11 affected packages: all passed, including App,
  benchmark fixtures, native decoder, observations, workers, session, admin,
  authorization, CLI and managed bridge/components.
- Explicit Bubblewrap edit gate: both Go correction and TypeScript refactor
  oracles passed, with zero provider attempts. This separately executes the
  oracle opt-in that the generic suite leaves unrequested.
- Full vet and read-only formatting checks passed.
- Linux AMD64 v1 built and ran `version`; Linux ARM64 cross-compiled and was not
  executed. This AMD64 host smoke does not establish non-AVX execution.

The PR contribution passes `git diff --check` against main6d99. Comparing the
incoming patch against the old branch separately reports seven space-prefixed
blank context lines in `patches/opencode/1.19.0-managed-local-plugins.patch`.
That unified patch remains byte-identical to main; its context syntax was
preserved. The [check record](evidence/backend-first/matrix16-main-integration-v1/diff-check.json)
retains this warning and the recording-time assertion that initially also found
trailing tabs emitted by `go version -m`. Those raw build-info bytes are stored
losslessly as JSON, rather than stripped or embedded in a recursive warning log.

Checks ran with a private HOME, pinned Go 1.27.3/Node 26.11.1 paths, absent
provider/auth opt-ins and Go downloads disabled. Existing content-addressed Go
caches were reused. Native-client opt-ins and live certification were not
silently counted as successful executions. Synthetic/fake process fixtures
remain local protocol tests, with no actual worker or quota claim. Durations
are quality-check observations, not performance comparisons.

This local integration does not repeat main's official Claude or managed-client
certification, browser/UI audit, online dependency audit, or native ARM/non-AVX
release jobs. Their imported evidence stays tied to its original source, and
GitHub Actions must pass before merging the PR. No release is published here.
CONTEXT_SYNC remains BLOCKED under IVOAI-175.

Rollback is to revert this merge commit on the benchmark branch; no persistent
migration or user configuration change occurred. PR51 remains stacked separately
and should be integrated only after review of this new PR46 source epoch.
