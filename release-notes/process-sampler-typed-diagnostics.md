# Typed sampler fixture failure diagnostics

**The original independent failure remains INCONCLUSIVE and this increment
remains DRAFT.** Typed reasons improve the next failure report; they do not
identify or fix the earlier unclassified failure. No calibration workload or
32-row cohort was rerun, and no old outcome is rescored.

Rows gain two optional nullable fields while retaining schema 1 and all existing
status, generic error and measurement fields:

- `failure_reason`: `eof`, `deadline`, `oversize`, `invalid_json`, `shape`,
  `ready_mismatch`, `nonzero_exit`, `oracle_mismatch`, `os_error` or `value_error`.
- `failure_errno`: an integer 0..4095 only when supplied by a caught `OSError`;
  otherwise null. It is never inferred from exit status or signal.

Protocol errors subclass ValueError; protocol deadline errors subclass
TimeoutError. EOF, bounds, JSON decoding and shape checks still reject the same
inputs. The read loop keeps its predicates, order, one-byte reads and deadlines.
A known wait4 deadline sentinel 124 or nonzero exit takes diagnostic priority;
the reason is an observed outcome, not a causal explanation. Successful and
unexecuted rows keep the new fields null. Raw exceptions, stderr, input payload,
PIDs, argv, environment, paths and secrets are not added to rows.

The [source freeze and AST proof](evidence/backend-first/process-sampler-typed-diagnostics-v1/source-freeze.json)
precede tests and were verified afterward. Every existing function normalizes
identically after removing only the explicit diagnostic projection, typed raises
and the JSON error wrapper. All existing constants, workloads, oracles and
limits remain identical. ProcessSampler and the common wait4 driver are byte
identical; neither was modified. No deadline, success predicate, default,
dependency or project version was changed.

Deterministic negative tests use real bounded pipes and a simulated owned
process/wait outcome, with actual signal/reap forbidden. They assert reason,
phase and exit for EOF, oversize, malformed JSON, shape/READY mismatch, DONE
failure, nonzero exit, deadline and oracle mismatch. An injected OSError EMFILE
retains errno 24 while omitting its private message/filename; non-OSError spoofed
errno and invalid numeric values remain null. These are labeled fault controls,
not failed calibration attempts or evidence of the original cause.

The targeted suite passed five tests, with one explicit real-workload NOT_RUN.
The existing Python package suite passed 70 of 71 tests (10.452 s), with that same
single opt-in NOT_RUN. No skipped case is counted as a successful real fixture.
The two-million-term independent arithmetic unit oracle remains unchanged; no
calibration child workload was launched. No Go Core/runtime code changed, so no new
Go or client execution is claimed.

The prior [read-only audit](evidence/backend-first/process-sampler-typed-diagnostics-v1/read-only-audit/audit-report.md)
is archived unchanged with its original manifest, control scripts and all nine
helper-control outcomes. The archive verifies hashes, permits only flat
manifest-listed filenames, reads bounded regular files without following links
and writes exclusively to new files. The original reconstructed failure record,
32-row cohort and every prior evidence file remain byte-identical. No missing
original case, stage, exit, interpreter or errno is retrospectively invented.

[Validation](evidence/backend-first/process-sampler-typed-diagnostics-v1/validation.json)
records the precise source, tests, archive and compatibility limits. New source
identity prevents silently combining this diagnostic projection with the old
overhead cohort. There is no new timing comparison, correction factor, LLM
benefit, client/provider invocation, credential access or merge with newer main
in this increment. The economic gate remains INCOMPLETE and CONTEXT_SYNC remains
BLOCKED under IVOAI-175. Rollback removes only this reporting extension/tests;
existing sampler behavior and prior evidence remain intact.
