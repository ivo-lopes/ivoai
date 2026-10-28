# Experimental observed concurrent-project driver

This increment implements an opt-in executable driver for the frozen `implementation/go-core` and `dependencies/python-deps` composition. It instruments the actual CLI command in the existing App benchmark harness and observes its owned process lifetime. It introduces no Core scheduler, worker executor, authentication client or new runtime grant. `concurrent_distinct_projects` deliberately remains UNSUPPORTED in matrix16 until independent review and real official-client evidence justify admission. The new mode has separate experimental probe admission; it does not promote the catalog.

The default sequential driver, its schema-3 discovery and ordinary `command.Run()` path remain available. Only `--concurrent-projects` selects the new path, mutually exclusive with `--matrix16` and `--scenarios`. `--preflight-only` starts no clients. Both baseline and candidate must be rebuilt with the identical current harness: this path requires schema-4 discovery and the exact lifecycle-observer source digest in addition to the existing catalog, fixture/oracle and harness pins. Missing source, unmatched discovery, failed offline Go/Python oracle or unavailable Linux pidfds blocks before clients.

The whole comparison binds one initial set of executable, observer/harness, contract/oracle/source, fixture, module-manifest and distribution-inventory hashes. Executable/source locations are represented by digests, not public paths. Checks before/after each pair and at finish compare against that initial set; pairs never establish a fresh scientific identity. An observed change preserves completed child results, fails the comparison and records both children of every remaining pair as NOT_RUN without retry. A separate final receipt can fail the cohort while preserving previously completed rows. These before/after checks depend on the host filesystem/toolchain trust boundary; they are not an atomic or adversarially hermetic filesystem snapshot.

The command uses the existing driver arguments, adding only the explicit mode:

```text
python3 scripts/benchmark-economic-runtime.py --concurrent-projects \
  --baseline <reviewed-baseline-App.test> --candidate <reviewed-candidate-App.test> \
  --codex <reviewed-official-client> --node <reviewed-node> \
  --distribution <reviewed-Graft-distribution> --sandbox-binary <reviewed-helper> \
  --oracle-go <reviewed-offline-Go> --output <new-private-output> \
  --preflight-only
```

Omitting `--preflight-only` consumes the selected native client's existing authentication/quotas. No authenticated invocation is part of this implementation's validation. Preflight verifies local prerequisites and source consistency; it does not establish a client's official provenance, subscription boundary, model/quota equivalence or actual provider support. Host-reviewed artifacts remain required.

For each repetition, A/B or B/A selects an arm. That arm launches exactly two existing read-only child benchmarks, concurrently, under two owned disposable process groups. Each child has its own fixture, App session/state/cache, CLI TMPDIR and output. There is no task queue or general-purpose job API. Existing CLI restrictions, output schema, complete-stream checks, exact answer oracle and whole-fixture before/after hashes remain unchanged. Both executable fixture oracles pass before experimental execution admission.

## Actual-client observation

The harness opt-in calls `benchmarkfixtures.RunObservedClient` in place of `command.Run()`. It binds the command's real Start/Wait to a host-generated attempt nonce, arm, child, fixture/harness/composition/client hashes and isolation digest. It pins the actual `/proc/<child>/exe` bytes, records Linux monotonic start/reap times and process-generation ticks, and reaps its exact child on observation failure. This is benchmark host correlation, not authorization.

The start timestamp is captured immediately after `command.Start()` returns, before ELF hashing or `/proc` verification. This is the host's post-Start observation, not an attestation of the kernel's exact fork/exec instant. ELF verification and observation overhead remain inside `ProviderMS`; no efficiency improvement is inferred from this instrumentation.

The start receipt is create-only, mode 0600, in a canonical owned 0700 host-control directory outside both the workspace and the CLI's writable TMPDIR. The host control variables and nonce are not passed through the CLI environment allowlist or prompt. Host IPC and CLI scratch directories are separate. This boundary depends on the existing official read-only sandbox; it does not claim protection from a compromised host/client or a sandbox escape.

The driver independently checks actual kernel state, executable inode, owned process group/session and generation. It opens pidfds for both clients, polls for lifetime termination before and after an A→B→A live-state bracket, excludes exited/zombie processes and validates common-clock bounds. Interval metadata or wrapper overlap alone cannot pass. Missing pidfd support has no weaker fallback. This brackets an observed local client lifetime; it does not attest simultaneous remote inference, continuous liveness outside the bracket, native-worker totals or an absolute identity guarantee from metadata alone.

Final child rows must match the exact start receipt and current attempt/source identity. Zero, stale, missing, reordered or substituted identities/intervals fail. The bracket must lie inside both actual start/reap intervals. Native PIDs/generation ticks remain transient private IPC, which is removed after parsing. Public rows retain a domain-bound process digest and scalar timing/source metadata; no argv, environment, auth paths, prompts or answers are added to telemetry.

## Results and cancellation

Both existing child rows pass through the existing strict evidence validator. A failed, missing or malformed child is retained alongside its counterpart; it cannot be dropped to make a pair succeed. A batch succeeds only when both children pass and an actual-client bracket is verified with stable executable pins. The catalog still remains UNSUPPORTED after experimental success.

Batch wall time includes per-pair pin checks, launch/observation/reap and result validation, separately from the sum of valid child durations. Shared discovery/oracle initialization and the cohort's final check are outside individual batch durations. Missing child timings produce an unknown sum. Per-child CPU/RSS observations remain available; maximum RSS values are never summed into a batch peak. Provider cache equivalence, native worker/retry totals and economics remain unverified. The global economic gate stays INCOMPLETE.

Cancellation signals the two owned attempts and reaps them before returning both results. Timeout/cancellation cleanup targets only their own disposable groups, retaining the existing 4-minute child-test and 250-second driver deadlines. No unrelated process is targeted. Existing GNU-time and native wait4 resource paths are reused. A cancellation before launch starts no clients.

## Validation limits

The [source-bound receipt](evidence/concurrent-projects-observed-driver/validation.json) distinguishes deterministic local process fixtures from official/native integration. Tests use synthetic Go helper processes and `/bin/sleep` clients, including the real driver entrypoint and actual kernel lifecycle checks. Admission stubs used in entrypoint fixtures are explicitly labeled; their token values are fabricated mechanics inputs and are not economic observations. Real offline Go/Python oracles and actual compiled App discovery are verified separately without client execution.

Historical receipts from the contract-only branch remain byte-for-byte unchanged. The native diagnostic runner's mandatory synthetic-provider guard, institutional services, production configuration and catalog are unchanged. No real provider/cohort, credentials, production ingestion, native parity, release or economic certification is claimed. Local functional validation must be followed by independent review and exact-head CI; official-client execution requires separately authorized integration proof.
