# Matrix16 edit contracts — IVOAI-198 / IVOAI-138

Follow-up integrity correction: the initial snapshot recorded permission bits
but omitted setuid/setgid/sticky flags. Six static mutations reproduced this
acceptance gap without executing altered source. Snapshots now reject all three
flags on regular files and directories, including their initial state. Evidence
is appended separately in [special-mode validation](evidence/backend-first/matrix16-special-modes-v3/validation.json);
the original v2 artifacts remain unchanged. The corrected common harness was
rebuilt over both runtime revisions before a new offline discovery. The catalog
and driver schema remain version 2; “v3” identifies this evidence generation.

This increment adds executable workspace-edit contracts for the existing
`small_known_file_correction` and `cross_module_refactor` IDs. The v2 catalog has
nine supported contracts, five UNSUPPORTED cases and two BLOCKED cases. **No real
provider inference or subscription authentication was performed. The global
economic gate remains INCOMPLETE.** The 114 historical comparative attempts and
their unfavorable results are unchanged.

The Go task repairs a pure Clamp function in one declared file. The TypeScript
task renames one exported identifier across three files in a small monorepo.
Answer JSON alone cannot pass: the host requires all declared files to change,
preserves all other bytes (including Git metadata), file permissions and
directories, checks a restricted Go AST or identifier-only TS token change, and
executes an independent oracle. These narrow synthetic contracts do not certify
general bug fixing or semantic refactoring.

The executable oracle receives an immutable bounded copy of the validated
snapshot. Bubblewrap creates private mount/PID/network/IPC/user namespaces,
read-only source and tool inputs, private HOME/tmp/cache, dropped capabilities
and fixed commands under CPU/address-space/file/descriptor limits. Go package
downloads are disabled; Node uses native TypeScript stripping. There is no
installer or new production dependency. Missing tools or a failed containment
probe BLOCK an edit before the client launches. The Go oracle exhaustively
checks the declared small input grid; the TS oracle checks the export, numeric
results and both consumers. A wrong but syntactically admitted Go result fails
the executable test.

The existing AB/BA driver and Core prefetch path are reused. Both runtime builds
must expose identical embedded catalog, fixture, contract-source and harness
hashes. The v2 contract includes the new oracle source and executable identities
for Go/Node, bwrap and prlimit. A source-only comment changed after an earlier
binary build; the subsequent discovery correctly blocked both stale binaries
before any client attempt. Fresh baseline/candidate binaries were rebuilt from
the final shared source. Their exact identities and negative findings are in
[validation.json](evidence/backend-first/matrix16-edit-v2/validation.json).

Both arms append the same host-controlled native-worker restrictions at the
final trusted `Runner.Run` command. They are a newly recorded experimental
condition, not a retrospective property of the old 114 attempts. Public
`autoBridgeArgs` and production authorization are unchanged. Native worker events
are not yet instrumented: new summaries leave worker/delegation counts unknown
and identify the separate Core primary-only scope. No version-general worker
certification, effective model, backend equivalence, quota or cache equivalence
is inferred from flags or synthetic events.

Validation covers the real local sandbox/toolchains and actual compiled host
paths, plus an explicitly FAKE process fixture. The fake fixture applies fixed
reviewed patches and emits fabricated usage; it tests AB/BA mechanics and oracle
receipts only. Answer-without-patch and Git-metadata tampering are rejected in
both arms. None of their token values or elapsed comparisons are economic
results. Earlier candidate-versus-itself controls and the distinct v0.12.1 versus
candidate controls are retained separately. The full 16-case preflight has zero
client/provider attempts; READY means a discovered executable contract, not an
available authenticated client or a successful agent task. Codex/Graft inputs in
these offline controls are placeholders, explicitly not a native-client proof.

Two development failures are retained: a Go AST rejection could be reset by a
later valid unary node (fixed with monotonic rejection), and a probe accepted an
oversized response consisting of JSON plus whitespace (fixed with a live read
bound). The unsafe AST was tested statically and was never executed. A later
stale-contract discovery failure is also retained; no failed run is rescored.

To reproduce the focused offline gates with existing reviewed absolute paths:

```sh
python3 -m unittest discover -s scripts -p 'test_benchmark*.py'
go test -race ./internal/benchmarkfixtures ./internal/app -run 'TestMatrixCoverageAndStructuredOracles|TestMaterializedBytesAndNoncodeMutation|TestOracleRejectsWrongNumericTypesAndOrder|TestEditContractsRejectUnauthorisedChanges|TestPureEditShapeRefusesExecutableInjection|TestEditSnapshotAndOutputAreBounded|TestPrimaryPrefetchLiveExactQuality'
IVOAI_BENCHMARK_EDIT_ORACLES=1 IVOAI_BENCHMARK_ORACLE_GO=/absolute/go IVOAI_GRAFT_TEST_NODE=/absolute/node IVOAI_BENCHMARK_BWRAP=/usr/bin/bwrap go test -race ./internal/benchmarkfixtures -run '^TestBenchmarkSandboxedEditOracles$' -count=1 -v
```

Build this identical harness/package/data over each frozen runtime revision,
then use `--matrix16 --preflight-only --oracle-go /absolute/go` with the existing
driver arguments. Each output directory must be new. Removing preflight-only
would permit real inference; that was not done with real clients in this
increment. Keep the benchmark's controlled environment and existing client-owned
authentication boundary. The schema change is versioned as 2; v1 historical
reports remain historical and are not silently reinterpreted.

Remaining cases require a representative large-project fixture, a live scoped
Memory dataset, Context ingestion/retrieval (IVOAI-175), real worker/reuse,
warm-turn, interruption/resume and concurrent-project lifecycles. The edit
snapshot limit stays 1 MiB/128 files and is not relaxed to pretend a large-project
case is covered. Oracle executable hashes do not authenticate every system
library or the entire Go toolchain tree; installation provenance must be
reviewed before future real comparisons. Concurrent malicious same-user host
mutation is outside this synthetic snapshot boundary. ARM64 is cross-compiled,
not executed on this AMD64 host. No release, production default or personal
configuration changes are included. Rollback is a revert of the harness/catalog
increment; no persistent migration is introduced.
