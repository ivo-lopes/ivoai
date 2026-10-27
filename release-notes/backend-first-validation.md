# Backend-first development validation

Date: 2026-10-27. Baseline: published v0.12.1, commit
`3ff6db8bb0174b1ae6f781f6754e2653a6dae5db`. This is incremental development
evidence, not completion of IVOAI-191 or authorization of an economic claim.
The [migration audit](../docs/backend-first-migration.md) records contracts,
dependencies, risks and rollback. No production state was migrated.

## Benchmark correctness (IVOAI-198 / IVOAI-138)

The old comparison driver could return success for an empty result array,
including after a nonzero subprocess exit. Missing tests, skipped tests, wrong
scenario/arm, duplicate fields and malformed measurements were not reliably
rejected. The new driver records a fixed-label failed attempt and continues
independent attempts. Successful execution requires one matching typed result,
provider input/cache/output measurements and a strict correctness verdict.
Only allowlisted scalar measurements are retained; subprocess stdout/stderr and
arbitrary error strings are discarded. Output is private and atomic. Timeouts
terminate the disposable process group.

Reports separate all attempts from pairs correct in both arms. Cached input is a
subset of input, never an additional token charge. Economic targets remain
predeclared at 10% input and 5% elapsed reduction, with improvement in two of three
repetitions. The global gate is always INCOMPLETE for this three-scenario harness;
a favorable subset cannot certify the full 16-scenario matrix.

## Real v0.12.1 control

[Sanitized data and hashes](evidence/backend-first/control-v0121/provenance.json)
preserve a three-repetition AB/BA control. Both arms use the **same compiled
v0.12.1 App test binary**, the same harness/fixtures, Codex 0.164.0, requested
gpt-6.1-sol/low, reviewed Node 26.11.1 and published Graft artifacts. There is no
candidate optimization in this control. It is not a full published-executable
frontend comparison. Official subscription authentication was used; no paid API
key, prompt, answer or credential is included in the dataset.

| Measurement | A | B |
|---|---:|---:|
| Correct attempts / total | 8 / 9 | 9 / 9 |
| Input tokens, all attempts | 133680 | 136236 |
| Cached input tokens, all attempts | 0 | 12288 |
| Output tokens, all attempts | 357 | 354 |
| Total elapsed ms, all attempts | 42455 | 42010 |
| Input tokens, 8 correctness-matched pairs | 118836 | 121394 |
| Elapsed ms, 8 correctness-matched pairs | 38092 | 37500 |
| Model tool calls | 0 | 0 |

The implementation/A/repetition-1 strict oracle failed; the runner exited **1**.
The failure is retained, not retried into a success. Matched input increased
**2.15%** and time decreased **1.55%** despite identical code. This establishes
observed variability, not a saving. Both subset and global economic acceptance
remain incomplete. Provider cache equivalence and independent backend model
identity were not established. Other host validation ran concurrently; these
measurements do not represent an idle host.

Graft's recorded structural CPU totals were 1375/1380 ms; maximum observed
structural RSS was 113471488/112152576 bytes. These exclude in-process direct
retrieval and cannot represent full session CPU/RAM. GNU time was unavailable
during this control; whole-command resource fields correctly remain null.
No monetary cost or quota consumption is inferred.

## Validation boundaries

Baseline `go test -p=2 -timeout=20m ./...` passed with Go 1.27.3. The real
`scripts/test-managed-graft.sh` passed against checksum-verified published
v0.12.1 artifacts and the repository-pinned Node archive, including sandbox,
operations, MCP, worktree/isolation, integrity and hostile-archive contracts.
The benchmark driver's 17 offline regression tests passed. Synthetic subprocess
fixtures in those tests verify the driver; they are not provider measurements.
The final driver also measures resources with Linux `wait4` when GNU time is
absent. This does not reconstruct the missing whole-command values in the
historical control above. All 23 Python tests in normal discovery passed.

The complete matrix, broader project/language mix, worker/continuity comparison,
Claude inference, Antigravity integration and whole-client economic certification
remain required. The canonical Plane project is now accessible in `pessoal-ivo`;
the requested memory pages could not be recovered. Context remains read-only to
agent sessions: `CONTEXT_SYNC=BLOCKED`, IVOAI-175.

Rollback of the benchmark changes is an ordinary code revert. No public runtime
contract, component pin, provider authentication or persisted session schema was
changed by this delivery. No new release is claimed by this report.
