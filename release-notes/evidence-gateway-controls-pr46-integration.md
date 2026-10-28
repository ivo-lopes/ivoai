# PR51 controls integrated with PR46/G0

Source merge `ab83f98a56e3c6b413de445d92b3608f04398fa6` combines the existing
controls at `10f00639591c03d1daba21806c92d87ef2368627` with PR46
`54eb54edb9a08e732c45ed7a996b538741294a87`. The public remote branch was checked
at that exact SHA before merging. PR46 subsequently merged to main
`a1ccf4ca33a1e1f748810add9e03fe61fb963831`; its tree was independently checked
locally equal to54eb, so no SHA-only merge is needed. The merge was automatic,
with no conflicts. The existing Git-runner callback stays byte-identical to10f.

The combined source inherits G0 and current CI/Release gates. This test-only
control now explicitly pins protected-evidence creation OFF and asserts its
legacy host-store namespace, preserving the previous mechanism even when the
caller has the G0 flag enabled. Dedicated G0 tests remain independent. The
control's exact reads are still host `ArtifactStore.Read` under the issued owner;
this is not a protected public Core/MCP or native-client consumption claim.
No production permission, scheduler, routing or storage code was altered beyond
incoming PR46.

**All 184 prior PR51 evidence files are byte-identical**, including v1/v2,
pilots, the controlled inherited-hook reproduction and rejection proofs. All
293 incoming PR46 evidence files also match that source byte for byte. Older
numeric outcomes keep their original source identities; none are relabeled as
measurements for this combined runtime. The original exported timing receipts
are unchanged. No export, timing cohort or actual client/provider turn was run.
The generic export test remains NOT_RUN; functional OFF/ON assertions discard
their per-operation timings. Metadata workers remain NOT_ACTUAL_DISPATCH.

The local gates passed on the combined Go source:

- Full uncached Go suite: 59 packages passed, three had no tests.
- Full Python suite: 65 tests passed (10.500 s).
- Uncached race checks: eight relevant packages passed, including App, fixtures,
  CodeIntel, Evidence, WorkingContext, Orchestrator and native observations.
- A functional test started with the caller G0 flag ON and verified that this
  host control still pins it OFF and creates no protected namespace.
- Full vet/formatting, AMD64 v1 build/version smoke and ARM64 cross-build passed.
  ARM64 was not executed, and no no-AVX hardware certification is implied.

The mandatory rollback gate first **failed at source-provenance before a probe
ran**: HEAD was still10f during an uncommitted merge, and the pinned ancestor
`3be82befea6ed3875373afcb1bfaa450710310ac` was not its ancestor. Object lookup
succeeded, old-HEAD ancestry returned1 and incoming54eb ancestry returned0.
That failed check is preserved separately. After the actual local merge commit,
the unchanged gate passed with one named test and zero skips. There was no fetch,
ancestry substitution, bypass or relabeling of the failed run. The old reader
rejected protected full/range reads; legacy put/read/GC worked and protected
bytes survived. Old inventory does not include protected disk usage.

[The source epoch](evidence/backend-first/gateway-controls-pr46-integration-v1/source-epoch.json),
[validation](evidence/backend-first/gateway-controls-pr46-integration-v1/validation.json)
and both rollback outcomes bind 556 source inventory entries, build identities,
parents, provenance and preservation checks. The source merge remains in history;
this follow-up adds only its reporting. Tests used private HOME, pinned tools,
no auth/provider opt-ins and disabled Go downloads with verified local caches.
Build-info stdout is retained losslessly in JSON. These hashes do not attest the
whole compiler installation, shared libraries or host kernel.

The driver, catalog, sampler and edit-oracle bytes match PR46, with protocol3
and mandatory discovery intact. The callback changes common-harness SHA relative
to PR46, as already recorded at10f. Future baseline/candidate comparisons require
identical rebuilt final harnesses and new strict discovery before any provider
turn. No old binary/receipt is silently reused. The126 count describes only the
six backend-first cohorts, not all repository history; older PR30/economic-runtime
and its separate projection pilot remain outside that tally, without aggregation.

Exact-head hosted CI is still required before merging PR51. This local source
integration does not recertify authenticated native clients, live G0 consumption,
browser UI, native ARM or non-AVX execution. The sampler draft remains separate
with its unresolved independent failure preserved; it was not changed here.
No release or tag is changed; public v0.14.0 remains at6d99. The economic gate
remains INCOMPLETE and CONTEXT_SYNC remains BLOCKED under IVOAI-175.
