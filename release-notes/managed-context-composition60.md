# Managed context composition with native fixture PR60

**DRAFT, local full-suite gate failed.** Source
`b523055dcbcb833df1f1d903e0b823e1a0376b2c` composes context fix
`571006e953edbb48bd1577fb61fab92c75dc3d73` and the reviewed test-only native
fixture `8a956f7a00632c2dfef757bc5b8eda62a7c19bfe`, without conflicts. The
individual [context](managed-codex-mcp-context.md) and
[fixture](native-cleanup-fixture-observation.md) checks retain their own source
epochs; none is reassigned as full-suite success for this composition.

One full Go invocation with count1 on this exact source passed58 packages,
failed1 and had3 packages without tests. `go vet ./...` passed. The failed
test was `internal/evidence/TestGatewayRecoveredProviderPanicReleasesFlight`,
1.34s: “recovered provider panic poisoned subsequent request: evidence source
or authorization changed.” The complete original JSON log is preserved as
[gzip](evidence/managed-context-composition60/full-go.jsonl.gz), decompressible
to its exact original bytes. The [receipt](evidence/managed-context-composition60/validation.json)
and [hashes](evidence/managed-context-composition60/artifacts-sha256.json) bind
the failed gate, rather than treating skipped optional cases as integration
successes. The unchanged source was not rerun to seek a green result.

The evidence package's code and test are byte-identical to main4368. Static
inspection shows that the fixture's authorization and generation never change.
The one-second context covers a second retrieval, including artifact storage;
`packet` maps failed revalidation/context checks to `ErrChanged`. A blocked
existing-flight wait instead returns `ctx.Err()` directly. This narrows the
investigation; it does not measure the exact failed check, storage latency,
scheduling cause or original interleaving.

A separate investigation is qualifying the intended panic/flight contract with
direct synchronous state/channel assertions and controlled deadline/storage
tests. No production timeout, TTL, authorization, security assertion or test
deadline is relaxed by this composition. No extra official client, status,
authentication, provider/model call or benchmark cohort ran in these checks.

Review and exact final-head CI remain required. PR60's hosted CI and the
original red PR56 run are separate evidence. No merge, tag, release, artifact
publication, personal configuration change or economic gain follows this draft.
