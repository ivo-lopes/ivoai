# Matrix16 readiness — IVOAI-198 / IVOAI-138

Base: `49445919f745157e3c8aa1c717161e49cb5f50d5` (v0.13.0 main).
This delivery freezes a typed 16-case catalog, five synthetic projects,
executable offline oracles and four additional read-only harness cases.
**No Codex/Claude/Antigravity client or provider inference was executed. Global
economic acceptance remains INCOMPLETE.** Offline program correctness does not
establish client correctness, equivalent task quality, provider token savings or
whole-session economics. Historical failed gates and datasets are unchanged.

The repository's existing driver is `scripts/benchmark-economic-runtime.py`;
`scripts/benchmark-evidence.py` does not exist at this base. The existing
`TestLivePrimaryCodePrefetchBenchmark` and AB/BA process/resource driver are reused.
No scheduler, provider runner, agent authority, runtime defaults, configuration,
component dependency or paid API was added. The default scenario tuple remains
`api`, `implementation`, `multi_module_callers`.

## Catalog and fixture contracts

The labels preserve the 16-row matrix in
[economic-runtime-validation.md](economic-runtime-validation.md). The canonical
JSON is `internal/benchmarkfixtures/data/catalog.json`; Go and Python load typed
contracts from it. All unavailable cases carry explicit blockers, never a passing
oracle. A seed fixture for a planned lifecycle is not an implemented lifecycle.

| ID | Required scenario | Current contract / remaining blocker |
| --- | --- | --- |
| small_known_file_correction | Small known-file correction | UNSUPPORTED: runner is read-only; edit and post-edit executable oracle pending |
| implementation | Function identification | Legacy Go read-only contract; new client execution NOT_RUN |
| cross_module_refactor | Cross-module refactor | UNSUPPORTED: no edits/refactor lifecycle or executable post-edit comparison |
| dependencies | Dependencies | New Python imports/computation read-only contract; client execution NOT_RUN |
| multi_module_callers | Impact | Legacy direct-caller subset; broader semantic impact remains pending |
| api | APIs | Legacy strict Go signature contract; new client execution NOT_RUN |
| large_project | Large project / IVOAI | UNSUPPORTED: representative large fixture and boundary not frozen |
| monorepo | Monorepo | New TypeScript monorepo read-only contract; client execution NOT_RUN |
| unsupported_parser | Unsupported parser | New custom rules + shell fixture, plain-text fallback; client execution NOT_RUN |
| institutional_documentation | Institutional documentation | BLOCKED: Context ingestion/retrieval contract, IVOAI-175 |
| ai_memory_history | ai-memory history | BLOCKED: canonical history dataset/live retrieval not frozen |
| non_code | Non-code tasks | New milestone ordering/sum read-only contract; client execution NOT_RUN |
| multiple_workers | Multiple workers | UNSUPPORTED: primary-only harness has no worker lifecycle |
| reuse_between_turns | Reuse between turns | UNSUPPORTED: cold ephemeral turns have no persistent warm session |
| interrupted_continuity | Interrupted continuity | UNSUPPORTED: no interrupt/resume comparison |
| concurrent_distinct_projects | Concurrent distinct projects | UNSUPPORTED: AB/BA driver is sequential, no concurrent project lifecycle |

Seven read-only cases have schema/exact-answer contracts; seven cases are
UNSUPPORTED and two BLOCKED. The four additions are new predeclared prompts and
oracles, not retrospective scoring of the old 72 attempts. They are deliberately
small synthetic tasks, not representative certification of their entire category.

Five project fixtures cover Go, Python, TypeScript, shell/custom rules and
non-code JSON. The Go source bytes emitted for the three legacy cases are
unchanged. Stored `.fixture` suffixes keep Go modules/source out of package
discovery and are removed when materializing private test directories. Explicit
Python `__init__.py` is included with `all:data` embedding.

Offline oracles execute only these reviewed synthetic programs: Go independently
checks Scale/Entry/Process computations; Python parses imports and evaluates tax;
Node executes local TypeScript with native stripping and no package install;
shell/awk evaluates the rules file; Python verifies milestone order and total.
No external modules, installer, package-manager lifecycle or agent is involved.
All tool HOME/cwd/cache roots are temporary; results retain hashes/scalar verdicts
without stderr or arbitrary client text. Missing Go/Node is BLOCKED, not success.
The offline synthetic-program verifier collects stdout in memory and checks its
4 KiB acceptance limit **after process completion**; it is not a hard live output
cap. Inputs are the reviewed tiny fixtures, with a 60-second subprocess timeout.

## Discovery, equivalence and truthful accounting

`--matrix16`, `--scenarios ...` and `--preflight-only` opt into discovery.
The compiled App test binary exposes one **offline test**,
`TestPrimaryPrefetchBenchmarkCatalog`, which neither authenticates nor starts a
client. Each arm must report exactly the current catalog, fixture-contract,
harness-source and materialized-project SHA-256 values and supported IDs.
Binary SHA-256 values may differ; shared inputs/contracts must agree before any
inference. The harness embeds its own exact source to bind that contract to each
compiled binary. Matching a caller-supplied source file alone is insufficient.

New manifests report `host_mcp_tools_exposed=0`, `native_tools_exposed=null` and
`native_tools_inventory=UNVERIFIED`. Native Codex tools can remain available and
may perform local reads for the added scenarios. The old `tools_exposed=0` field
was not an attestation of an empty native catalog. Historical JSON is unchanged;
observing zero model tool calls is separate from advertising zero available tools.

Discovery uses the existing bounded process runner, a 10-second Go-test timeout,
15-second process-group timeout, private HOME/cwd, no inherited credentials/API
environment, suppressed stdout/stderr and a 32 KiB exact JSON allowlist. Missing
tests, skips, wrong exits, malformed/duplicate/unbounded output, wrong contracts
or unavailable runtime artifacts yield BLOCKED before client execution.
New-mode execution rows must repeat catalog/harness/fixture identities; a wrong
fixture is an invalid retained attempt. Both sides still use the same requested
model/effort, immutable fixture, permissions, budget and response schema.

READY means an executable read-only **contract was discovered**, not successful
client execution. Preflight records zero provider attempts. Unsupported/blocked
cases remain plan rows and never become fabricated successful attempts. A full
matrix run exits nonzero while any required case is unavailable, retains all
executed failures and leaves its subset diagnostic INCOMPLETE. The global gate
is always INCOMPLETE in this App-prefetch harness, even if seven cases pass.
Summary rows separate requested scenarios, actually measured cases and all
attempts from correctness-matched pairs. Legacy default selection is preserved.

Local caches remain cold/private per arm/turn. This does **not** establish equal
provider cache state; effective backend model identity, quota, money, warm reuse,
workers and whole-client resource attribution remain unverified. Fixture/output
bytes never stand in for provider tokens. Existing client-reported usage and
Linux resource accounting are reused without inventing missing observations.

## Checks performed and preserved failures

The [offline fixture report](evidence/benchmark-matrix16/offline-fixture-oracles.json)
records five verified synthetic program oracles, tool/source hashes and zero
provider attempts. The current host prefetch path was separately exercised
without a client for the four added cases: Python made one provider-factory call
and produced 669 evidence bytes; monorepo/rules/non-code made zero such calls and
produced zero prefetch bytes. They will rely on ordinary authorized local reads
in a future client turn. These are host-flow checks, not observed inference.

Focused Go fixture/App tests, focused race and vet passed. Python benchmark
regressions cover the 18 legacy tests plus eight new catalog/preflight cases,
including fake-binary AB/BA mechanics, unavailable cases and runtime identity
rejection. Synthetic process tests are not provider measurements.

The [binary discovery evidence](evidence/benchmark-matrix16/binary-discovery.json)
preserves the first failed and corrected discovery. The first build omitted
Python's underscore-prefixed `__init__.py`; the cross-language hash comparison
blocked both arms before inference. Corrected discovery matched both arms, but
the scenario preflight remained BLOCKED because no Codex executable input was
supplied (`/nonexistent`). No client or subscription was contacted.

Other retained development failures: the initial normal embed ignored the
nested Go fixture module; storing source/module files with `.fixture` suffixes
fixed materialization without changing emitted legacy bytes. A Python wait4
regression initially errored when `/proc/<pid>/stat` disappeared between open and
read. `ProcessLookupError` now joins `FileNotFoundError` as the exact expected
process-already-gone condition; kill/reap/assertions remain intact. The original
failure was not an economic observation and is not counted as a passing run.

## Reproduction and completing all 16

```sh
python3 scripts/benchmark_catalog.py --verify-fixtures --go /absolute/go --node /absolute/node --output /new/private/offline.json
python3 -m unittest discover -s scripts -p 'test_benchmark*.py'
go test ./internal/benchmarkfixtures ./internal/app -run 'TestMatrix|TestMaterialized|TestOracle|TestPrimaryPrefetchBenchmarkCatalog|TestPrimaryPrefetchMatrixReadOnlyFixtures|TestPrimaryPrefetchLiveExactQuality'
go vet ./internal/benchmarkfixtures ./internal/app
go test -c -o /new/private/candidate-app.test ./internal/app
python3 scripts/benchmark-economic-runtime.py --baseline /absolute/baseline-app.test --candidate /new/private/candidate-app.test --codex /absolute/codex --node /absolute/node --distribution /absolute/reviewed-graft --sandbox-binary /absolute/ivoai --output /new/private/preflight --matrix16 --preflight-only
```

For a historical baseline, compile **this frozen harness and the identical
`internal/benchmarkfixtures` package/data** on both runtime revisions; discovery
refuses the old three-case-only binary. Record the source commits and exact
binary SHA-256 values before any client execution. Do not substitute a later
binary identity for these observed test artifacts. Preflight outputs refuse
overwrite. Removing `--preflight-only` consumes the official subscription via
the existing runner; it was not done in this delivery.

Next, independently review this frozen contract; validate both baseline and
candidate host flows; run the seven-case AB/BA subset under equivalent recorded
conditions while retaining unfavorable rows. Complete the nine explicit gaps
with real edit/worker/session/resume/concurrency/institutional lifecycle contracts
and independent executable oracles. Freeze a representative large-project input
and broader project variants. Then evaluate the 16-scenario quality/security and
10% input / 5% time gates with at least three repetitions, not this catalog's
offline verdicts. No Plane item or economic gate is marked complete here.

Rollback is an ordinary revert of this harness/catalog increment; historical
datasets and runtime defaults remain unchanged. No release, push or merge is
part of this delivery.

## Environment boundary: separate post-catalog hardening

Checkpoint `9ed891e900c586a9522ce6ba0527ff45b98d6ac3` was exercised **offline
only**, before this environment hardening. Its binary/discovery/oracle reports
remain unchanged; it is not an approved inference checkpoint. Review found that
the old harness/driver inherited unrelated environment variables after removing
only two API-key names. Native executor reads in the added cases make that an
unnecessary secret-exposure boundary, even though no inference was performed.

The driver now builds an explicit environment, adds only its own benchmark paths,
forces GOMAXPROCS=2/private TMPDIR and admits the one known experimental Gateway
flag. The Codex child independently uses a strict allowlist: HOME, CODEX_HOME,
PATH, LANG, standard POSIX/glibc LC categories, TZ, TERM, TMPDIR, required XDG
directories and DBUS_SESSION_BUS_ADDRESS for official keyring authentication.
Unrecognized `LC_*` names are not a wildcard escape. Authentication stays with
the official client and its existing HOME/CODEX_HOME/keyring scope; no auth file,
token or credential is copied, read or rewritten by this filter or its tests.
This does not claim that keeping HOME makes its files unreachable to the client.

Arbitrary API keys, Git/provider tokens, AWS/Azure credentials, OTEL headers,
external MCP configuration, loader/Python injection and proxy/certificate
overrides are not inherited. Proxies and custom CA variables are absent by
default; the client uses system/default trust. No override is introduced without
a demonstrated requirement and separate host-controlled review. An environment
requiring such overrides may be unavailable for a future benchmark; it must not
silently restore broad environment inheritance.

Regressions inject fake secrets alongside a private fake HOME/CODEX_HOME scope,
verify both environment boundaries and preserve those authentication path values
without reading credentials. The fixed-source binary and provenance must be
rebuilt in a new private directory before any real comparison. Historical JSON,
failed observations and the pre-hardening checkpoint are not relabeled or erased.
