# Gateway panic cleanup: separate flight release from artifact I/O

This test-only change strengthens the panic-flight oracle and separates it from filesystem latency. It is based on composition `b523055dcbcb833df1f1d903e0b823e1a0376b2c`; Gateway/LocalStore production, runtime deadlines, TTLs, authorization and cache policy are unchanged.

The original full Go run remains **FAIL**: `TestGatewayRecoveredProviderPanicReleasesFlight` failed after1.34s with `evidence source or authorization changed`;58 other packages passed and3 packages had no tests. Its immutable log digest and exact failure events are retained in this increment. The original one-second context covered the whole subsequent Retrieve, including LocalStore payload/metadata/directory syncs. That was an indirect assertion about cleanup of an earlier provider panic.

A flight waiter whose context expires returns `ctx.Err()` directly. The reported `ErrChanged` comes from packet admission checks after fetch, before or after Put. This fixture never changes its grant or generation. The report therefore does not demonstrate a poisoned flight. It does not reveal the exact check, scheduler stall or I/O latency in the original run; those were not instrumented, and no original site/cause is retrospectively attributed.

## Stronger, separate contracts

The panic test captures the admitted flight inside the provider. Immediately after recovering the synthetic panic, it synchronously requires its completion channel closed, its entry removed, an empty cache and zero artifact I/O. No elapsed-time success can substitute for these assertions. A missing cleanup defer fails before a second Retrieve can block. Only after that proof does a subsequent retrieval exercise real LocalStore persistence and exact recovery, with a controlled one-second virtual storage delay. That persistence phase is a correctness check, not a one-second filesystem latency SLO.

A separate test retains an actual `context.WithTimeout(..., time.Second)` from the standard library. Its post-Put hook proves the context is still live, delays exactly one virtual second, and verifies `context.DeadlineExceeded` before returning to Gateway's post-I/O check. The expected result is `ErrChanged`, no returned text/reference, no cache entry, and a removed flight with a closed completion channel. Already persisted but unissued artifacts remain under the existing ownership/TTL/GC rules; no cleanup policy is changed.

Both tests use `testing/synctest`, available in the already required Go1.27.3 standard library. Filesystem calls remain real; the controlled clock advances when bubble goroutines durably block. Host scheduling/fsync walltime cannot accidentally exhaust the flight oracle. No dependency, module pin, runtime clock, authorization or timeout threshold was added or relaxed. The new tests do not certify a bound on fsync walltime.

## Frozen controls and results

The [validation receipt](evidence/gateway-panic-flight-clock/validation.json) binds the final590 Go source files to freeze `1e424982dc7b1164f4587a0547527f02b808069d29d435da92849f97df7d1f91` and preserves all negative outcomes. Each control was executed once; there was no full-suite retry of the unchanged failing source.

| Control | Observed result | Interpretation |
|---|---|---|
| Original assertion + controlled post-Put virtual deadline | Expected FAIL,0.006s | Flight already released; Put completes, deadline expires, old assertion reports `ErrChanged` |
| New panic-release and post-Put-deadline tests | PASS,0.006s | Synchronous cleanup, subsequent exact storage/recovery, and fail-closed deadline contracts hold |
| Private overlay omitting only deferred flight cleanup | Expected FAIL,0.005s | Immediate completion-channel assertion rejects the leak before any second Retrieve |
| Private overlay omitting only post-Put admission recheck | Expected FAIL,0.006s | New deadline test rejects returned evidence with errnil |
| Evidence + WorkingContext race/count1 | PASS,134 passing events including subtests;3 opt-in skips | Local package coverage, not successful execution of skipped integrations |
| Vet on both packages | PASS | Static checks on the frozen final tests |

The two race packages took49.249s and66.523s respectively on the shared host. These times are validation observations, not performance or economic results. Expected mutant/legacy failures are rejection evidence and are never counted as ordinary passing tests. The candidate did not run a full Go suite; exact final-source hosted CI/composition is still required.

No native client, provider/model, status, authentication, installer, dependency download or external network operation was used. Benchmarks/historical cohorts, prior receipts, production Go, modules, pins, workflows and website/embed inputs are unchanged. The public release candidate and earlier composition artifacts remain separate. No global economics, lifecycle parity or release readiness is claimed.

## Reproducing without overwriting evidence

With Go1.27.3 and local dependencies, ordinary source-only validation is:

```bash
GOTOOLCHAIN=local GOENV=off GOPROXY=off GOSUMDB=off go test -count=1 ./internal/evidence \
  -run '^(TestGatewayRecoveredProviderPanicReleasesFlight|TestGatewayDeadlineAfterArtifactPutRemainsFailClosed)$'
GOTOOLCHAIN=local GOENV=off GOPROXY=off GOSUMDB=off go test -race -count=1 ./internal/evidence ./internal/workingcontext
```

The preserved overlay inputs have `.go.txt` extensions to avoid adding Go packages. For a separately authorized replay, create a new private output directory and a new Go overlay map using current absolute repository paths. Map only `internal/evidence/gateway.go` to the selected mutant `.go.txt`, and run only its matching test: panic cleanup for the no-cleanup mutant, deadline-after-Put for the no-recheck mutant. Both must exit1. The legacy overlay maps only `gateway_test.go` and reproduces the expected old assertion failure. Never run historical absolute-path overlay maps or replace their output files; preserve these epochs unchanged.

Rollback is a revert of this isolated test/docs commit. No production data, histories, credentials or client configuration should be removed or replayed.
## Independent test-only target

Root transplanted author commit `6411360f` without conflicts onto PR60 source
`8a956f7a`, producing `2b96397d`. This target imports no production context fix
from PR61. Its only Go change is `gateway_test.go`; all281 production Go files
match target base8a. Evidence and workingcontext sources match the authored
test epoch exactly. Earlier broader checks remain attributed to that epoch.

Two named tests passed with race/count1 on this target (package1.040s), zero
failures/skips; evidence vet passed. The target
[receipt](evidence/gateway-panic-flight-main/validation.json) and
[hashes](evidence/gateway-panic-flight-main/artifacts-sha256.json) retain those
checks separately. Fresh exact-head hosted CI is pending. No client, provider,
authentication, status, runtime timeout/TTL change or publication followed.
