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

## Experimental Gateway (IVOAI-199 / IVOAI-148 / IVOAI-192)

The opt-in implementation composes existing prefetch and ArtifactStore. It adds
session/scope/generation keys, bounded concurrent deduplication, permission
rechecks, exact references, provenance/gaps and secret admission. The library's
sealed recovery API is tested separately; existing client artifact transports
still retain their prior session/ownership policy. General recovery-policy
integration, Memory/Context/tools adapters and task-specific sufficiency remain
pending. The experimental flag stays **off**.

All real attempts are retained in separate datasets:

| Experiment | Correct A / B | Matched pairs | Input change A→B | Elapsed change A→B |
|---|---:|---:|---:|---:|
| Initial Gateway | 9/9 / 6/9 | 6 | +5.84% | +7.90% |
| Legacy-compatible wire envelope | 7/9 / 7/9 | 6 | +2.86% | +1.56% |
| Explicit unqualified-symbol prompt | 9/9 / 9/9 | 9 | −5.43% | −0.12% |

Dataset directories are `candidate-gateway`, `candidate-gateway-v2` and
`candidate-gateway-v3` under `evidence/backend-first`. Every manifest preserves
binary, harness, official client, Node and distribution identities. These are
development test binaries, not a published candidate release. The intermediate
source trees were uncommitted; exact binary hashes must not be substituted for
a claim of reproducible published-executable comparison.

The first diagnostic retained only correctness flags: symbol differed from the
strict oracle; file, input and computed result matched. No answer was retained,
and the exact differing symbol is unknown. The old prompt did not specify
qualification. Both harnesses now explicitly request an unqualified declaration
identifier; the strict oracle still rejects `mathpkg.Scale`. This is a new
measurement contract, not a reinterpretation of earlier failures.

The final three-scenario comparison used 141424/133738 input tokens,
24576/12288 cached-input tokens, 356/348 output tokens and 45575/45520 ms.
All 18 attempts passed their strict oracles. The subset economic gate **failed**:
5.43% input and 0.12% time reductions fall below the 10%/5% targets. The global
gate remains INCOMPLETE. Cache equivalence is unproven, no model tool calls were
made in either arm, and no warm or cross-worker inference savings were measured.
Do not attribute the observed aggregate reduction causally to reuse.

Linux wait4 resource measurements are present in the candidate experiments.
They cover the command and its reaped descendants; maximum RSS is the maximum
individual process value, not simultaneous summed process-tree RAM. None of
these comparisons certifies the unchanged structural memory ceiling for the
whole Codex process. Quota and monetary consumption remain unknown.

The final quoted-credential/private-key admission hardening was added after the
last inference binary was built. Its security regressions and race checks are
recorded separately; synthetic benchmark sources do not exercise those patterns.
The reported binary identities remain the measured identities.

## Permissive intake (IVOAI-193)

The shared intake accepts bounded nonempty UTF-8 natural language without
requiring objective/deliverable/acceptance headings. It rejects malformed text,
unsafe controls and oversized input, with fixed diagnostic labels. LF, tabs and
CRLF remain valid. The Codex, bridge and App gates use the same assessment.
Approval, sandbox, MCP grants, task validation and final DAG acceptance were not
removed. Primary-only execution without a DAG is a separate pending change.

Normal package tests and focused race tests passed. Official Codex 0.164.0
against a local synthetic provider passed native App Server admission/resume and
PTY tests, including oversized input, informal text and an actual approval
exchange in the fixture. These are protocol tests, not real subscription
inference or proof of Claude/Antigravity parity. The real Codex inference above
uses the App prefetch harness, not that synthetic provider.

## Full scenario status

None of the 16 scenarios is fully certified across published binaries, clients,
model/resource provenance, cold/warm and security cases. Narrow evidence exists
as follows; unavailable work remains pending rather than counted as passing.

| Required scenario | Current evidence / remaining gap |
|---|---|
| Small known-file correction | Direct-read contracts; executable edit A/B pending |
| Function identification | Real repeated prefetch subset; full client flow pending |
| Cross-module refactor | Existing contracts; comparative executable refactor pending |
| Dependencies | Direct-caller subset; broader dependency recall pending |
| Impact | Provider contracts; real task A/B pending |
| APIs | Real repeated strict signature subset; full certification pending |
| Large project / IVOAI | Historical negative retained; current real comparison pending |
| Monorepo | Real Git worktree/subdirectory regression; inference A/B pending |
| Unsupported parser | Existing fallback contracts; real repeated comparison pending |
| Institutional documentation | Context retrieval unavailable; IVOAI-175 pending |
| ai-memory history | Requested canonical pages unavailable; real comparison pending |
| Non-code tasks | Intake contracts; real economy comparison pending |
| Multiple workers | Deterministic retrieval reuse/isolation; real inference A/B pending |
| Reuse between turns | Bounded cache contracts; persistent-client comparison pending |
| Interrupted continuity | Native synthetic protocol tests; real comparative flow pending |
| Concurrent distinct projects | Scope/concurrency regressions; real provider comparison pending |

## Final local checks for this development batch

- Full normal Go suite passed after the runtime/intake changes; live opt-in paths
  without their required inputs remain skipped, not counted as provider evidence.
- Final `internal/evidence` and `internal/app` race suites passed (58.128 s and
  112.208 s). The code adapter additionally passed immediate-plan, stopped-worker
  and real monorepo-worktree regressions.
- `go vet ./...` passed; pinned govulncheck 1.7.0 reported no vulnerabilities
  with the local Go 1.27.3 toolchain.
- All 24 Python tests in normal discovery, nine standalone smoke-assertion tests
  and 41 frontend Node tests passed. Synthetic tests are labeled as such.
- The EN/pt-BR production documentation build and translation parity passed.
  Native amd64 and ARM64 cross-builds were exercised locally; exact-head CI,
  distributed installation/update/rollback and no-AVX runtime certification are
  separate promotion gates. No new release or production deployment occurred.
