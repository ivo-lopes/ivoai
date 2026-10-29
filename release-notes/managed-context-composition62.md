# Composition of managed MCP context and Gateway clock fixture

Source `80c0814d19995bc3ad228a11ef917e14c75989ee` composes PR61 head `1fe1f6ace03e1e1b7c89fa6e73bff102f372a675` with PR62 head `5702381238b734c55ecf1942d9d35a3d711e4950` without conflicts or additional Go edits. One full Go run and one full vet run passed on that frozen source. The exact final report head still requires root review and hosted CI; this is not a release or global architecture/economic completion.

The only Go change relative to head61 is `internal/evidence/gateway_test.go`, byte-identical to the reviewed PR62 and original641 test. All282 production Go files remain byte-identical to `571006e953edbb48bd1577fb61fab92c75dc3d73`. The native cleanup fixture from8a and context implementation from571 are preserved. The original641 author metadata is preserved in its transplanted commit `2b96397db62275923ae68f14ad42cf658f40ecf8`;641 itself remains a separate provenance reference, not an asserted ancestor. Heads61/62,8a and571 are ancestors of this merge.

## Validation on the new source

The [receipt](evidence/managed-context-composition62/validation.json) and [source freeze](evidence/managed-context-composition62/source-frozen.json) bind590 Go files to SHA256 `b77d57026157ecfcd2d173c36af28ac1a828d716ace918658931538ee5ebf330`. An exclusive marker per command prevents a second run in this evidence directory. Source identities were verified before and after both gates.

| Gate | Result |
|---|---|
| `go test -count=1 -json ./...` | PASS:59 packages;3 packages without tests;2068 passing test events including subtests/1181 passing top-level tests;69 opt-in skips |
| Gateway panic-flight and post-Put deadline targets within that run | Both PASS |
| `go vet ./...` | PASS |

The full Go command took62.416s walltime and vet13.393s on the shared host. They ran concurrently; these are validation observations, not task performance/economic results. Build caches were reused; `-count=1` bypassed test-result caching. Skipped opt-ins are not integration passes. No separate race, official client, status/authentication, real provider/model, Node/docs-site build, installer or dependency-download run was performed in this composition epoch. Prior independent race/official fixture receipts remain source-specific inherited evidence.

## Preserved adverse results and source boundaries

The prior composition `b523055dcbcb833df1f1d903e0b823e1a0376b2c` remains **FAIL**: its Gateway panic test failed after1.34s, with58 other packages passing and3 packages without tests. Its gzip archive decompresses byte-exactly to the original2,142,187-byte log, SHA256 `16ded281de58f46f197aff5b553634ef16a907e26c073d0dea5716d080a5c5a0`. It was not retried unchanged or relabelled successful.

The new run changes the test condition through reviewed PR62: immediate cleanup assertions precede storage, and a separate standard-library virtual deadline control verifies the unchanged fail-closed runtime. Preserved legacy/mutant controls remain expected failures. The exact wait site/latency of the earlier failure was not instrumented; this composition does not invent that attribution or claim a production cleanup/timeout fix.

All758 unique release-note records from the two parent heads are byte-identical after composition. Six named artifact manifests/66 referenced payload entries were checked. All29 inherited gzip archives passed bounded streaming decompression/CRC checks (16MiB maximum inflated bytes per archive, no tar extraction or asset execution). Historical source freezes retain their original source identities and were not rescored against this new source. The new full Go log is archived separately as a158,855-byte gzip and was verified byte-exact on decompression.

Website/embed inputs, modules, pins, workflows, scripts and components are unchanged. No runtime contract, security/TTL threshold, benchmark row, scenario classification, historical126-cohort result, client configuration, credential or session history was modified. Global economics and broader client/system/cloud/lifecycle parity remain unqualified. Root owns the PR update, exact final-head CI, hosted merge and any later publication decision.

Rollback is a local code/merge revert to the reviewed parent state. Preserve old evidence, user data/configuration and session/checkpoint histories; do not automatically replay failed turns.
