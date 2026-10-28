# Concurrent distinct projects: offline composition prerequisite

This increment declares a source-pinned benchmark composition and runs its two existing executable fixture oracles. It does **not** run clients concurrently. Matrix16 remains 9 supported, 5 UNSUPPORTED and 2 BLOCKED; `concurrent_distinct_projects` remains UNSUPPORTED, `client_execution=NOT_RUN` and `client_overlap=NOT_RUN`, even when `preflight_status=OFFLINE_ORACLES_VERIFIED`. Exit zero from the new command means only that both offline oracles passed. It is not a provider integration, economic result or release certification.

The frozen composition in `internal/benchmarkfixtures/data/concurrent-projects.json` admits exactly `implementation/go-core` and `dependencies/python-deps`, in that order, with read-only execution, two host-launched clients and mandatory client overlap. `max_clients` limits future host launches; it does not attest native worker/process totals. The typed Go parser and Python loader reject other children, duplicates, mutable execution, unknown fields and unbounded compositions. Existing catalog, fixture data, native runner and runtime invocation are unchanged.

`ConcurrentProjectContractSHA256` and `benchmark_concurrent_contract.contract_digest` share a domain-separated digest of the composition bytes, Go contract source, existing Go fixture/oracle contract, catalog and both project hashes. The preflight additionally records its Python source, existing Python oracle runner and host Go/Python executable hashes before and after execution. A changed pin blocks the preflight. This is a host-toolchain/cache trust boundary, not hermetic compiler attestation or an atomic filesystem snapshot.

Run only the offline preflight, with a reviewed Go executable and a new file in an existing private directory:

```sh
python3 scripts/benchmark_concurrent_contract.py --preflight-offline \
  --go /absolute/reviewed/go --output /absolute/private-0700/preflight.json
```

No Go executable yields a BLOCKED Go oracle while preserving the Python result. Failed, skipped, missing, reordered or mismatched oracle reports cannot verify the composition. Both named results remain visible. The existing `benchmark_catalog.verify_fixtures` has an optional validated selection argument; its default behavior is unchanged. The selection executes only the existing disposable Go and Python programs, using the existing offline environment, timeouts and exact fixture oracles. No installer, credentials, model or institutional service is used.

## Responsibilities required before an execution increment

| Owner | Required contract |
| --- | --- |
| Composition/catalog | Keep the exact two children and source hashes; advertise execution support only after the executable lifecycle exists. Old discovery binaries must not silently claim a new composition. |
| Existing child harness | Materialize independent private repositories, App sessions, local cache/state and evidence files. Keep read-only native CLI arguments, environment allowlist, complete-stream checks, exact structured answer oracle and before/after fixture checks. Pass executable fixture preflight before a provider attempt. |
| Composite driver | Validate both existing child rows independently through `read_evidence`; retain both successes/failures. Never accept one child, an aggregate answer, skipped tests or wrapper success as the pair. Run only one arm's two children together, bounded to two host launches, then use AB/BA across repetitions. |
| Client overlap observer | Bind each observation to the actual owned CLI executable/PID and process-start identity, child, arm, repetition and host-issued run identity. Observe both clients live on the same host monotonic clock; exclude wrappers, reused PIDs, already exited/zombie processes and setup-only overlap. Missing or unverifiable overlap fails the scenario. Synthetic process overlap remains a separate fixture proof. This establishes observed client lifecycle overlap, not simultaneous remote inference. |
| Resources/cleanup | Keep batch wall time separate from sums of child durations. Preserve per-child resources; do not sum maximum RSS into a simultaneous peak. Cancel/reap only the two owned disposable process groups, retain partial results and preserve current deadlines. No new scheduler, worker control or provider authentication client. |

The current `read_evidence` accepts one scenario/fixture/row and the driver executes sequentially. Neither is generalized in this prerequisite. The future composed row must contain two validated child rows plus a separately validated overlap receipt; it must not weaken the existing scalar/source/stream checks. It must also distinguish a verified-but-incorrect measurement from missing/invalid evidence when aggregating results.

`internal/nativecodex` remains diagnostic-only with its mandatory synthetic loopback provider guard. Existing negative evidence about native workers is not resolved here; no native-worker admission or quota equivalence is claimed. Persistent warm sessions, interruption/resume, large-project representativeness, live memory history and institutional Context remain separate work. This branch does not include the unmerged process-sampler calibration.

## Validation

The accompanying [receipt](evidence/concurrent-projects-contract/validation.json) binds this source epoch and lists executed checks and explicit skips. No real-provider cohort or client process was executed by the new preflight. Rollback removes the new contract/preflight files and the optional oracle-selection argument; persisted production state and public runtime contracts are unchanged.
