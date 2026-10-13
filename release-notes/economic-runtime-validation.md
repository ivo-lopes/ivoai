# Economic runtime candidate — validation in progress

Baseline: v0.12.0, `db93c6eb90cbd89717208a73f8e85ee3c545ad96`.
The clean main checkout and latest published release were revalidated on
2026-10-12. PRs 28/29 are already incorporated. No production changes.

## Investigation before implementation

The preserved follow-up MCP experiment reports A/B input tokens 879174/1203629,
24/40 tool calls, and 202007/278003 ms. B made 19 native calls, only four
structural calls, returning 19404 source-result bytes. Real IVOAI navigation
alone contributed 169638 of the 324455 extra input tokens (52.3%) and 45411 of
75996 extra milliseconds (59.8%). The raw evidence does not include prompts or
per-call provider usage; it cannot establish how much came from schemas,
replayed context, planning, or cache pricing. Cached tokens are input tokens,
not an independently additive quantity. Correlation is not causal attribution.

Code inspection found seven overlapping native schemas exposed per primary and
worker by default, despite a facade supporting all local operations. Initial
tool discovery is distinct from execution admission: hiding every tool before
plan approval can strand clients that retain the initial inventory. Projection
must be fixed before client startup, with authorization still checked on calls.
Another finding: one observed sample displaced deterministic cost defaults;
persisted aggregate values were not validated when read for prediction.

## Predeclared experiment and release gates

First isolate schema projection using the existing deterministic repositories,
same official Codex executable, requested gpt-6.1-sol/low, identical prompts,
permissions, source fixtures, managed Graft, and cold private caches in both
arms. A exposes the seven v0.12.0 schemas; B uses production ProjectTools.
Alternate AB/BA by scenario and repetition; three repetitions. This is an
ablation of projection, **not** the complete published-binary A/B or full
orchestrator/frontend benchmark. Provider effective-model verification and
broader 16-scenario acceptance remain separate release requirements.

Pilot: implementation, dependencies, API signatures, exact string (24 turns).
Keep every failure. Existing read-only fact checks are smoke checks, not proof
of arbitrary semantic correctness. Subsequent release evaluation must use
independent structured or executable assertions for all required scenarios.

Before final runs, release gates are: zero correctness/security regressions;
at least 10% lower aggregate observed input tokens and 5% lower aggregate wall
time, with improvement in at least two of three repetitions; no new local
memory-limit violations; no additional structural processes for noncode work;
byte-identical personal configuration; all mandatory platform/runtime checks.
Per-scenario regressions above 20% require investigation even if aggregates pass.
Schema bytes are observed serialization size; byte/4 token counts are estimates,
not official token attribution. Missing model/resource metrics remain unknown.
Passing the pilot alone cannot authorize publication.

## Plane boundary

The available Mindsite list_projects schema has cursor/order_by/per_page but
no workspace_slug. A read still returns historical IVOAI UUID
`c8b183f7-f52c-4562-a0f6-9f2b3a560493`. The canonical project in Pessoal - Ivo
has not been verified. No tasks were written or duplicated. Local searches of
the available repository directories did not locate the Plane MCP server source.
The server fix must expose optional workspace_slug consistently, resolve only
authorized workspaces, reject ambiguous legacy calls rather than select the
first workspace, and check project membership before every operation. Required
tests: explicit selection, single-workspace legacy calls, ambiguous legacy
rejection, unauthorized selection, project/workspace mismatch, reads and writes.
Do not change IVOAI transport or the historical project to mask this issue.

IVOAI-188 remains pending real Claude subscription validation. Claude execution
is restricted to fixtures/contracts in this cycle.

## Implemented changes and boundaries

| Area | v0.12.0 | Candidate |
|---|---|---|
| MCP discovery | Seven native schemas | One facade for ordinary AUTO/worker tasks; explicit native names retained; institutional-only AUTO skips the provider |
| Admission | Per-task/plan grants | Preserved independently of initial discovery; worker prefetch also respects explicit empty/alias-only lists |
| Known small file | Snapshot of all inventoried source | Inventory membership plus descriptor-safe snapshot of the target only |
| Exact Go declaration | Eight-line excerpt always marked truncated | Complete declaration when within budget; otherwise bounded excerpt |
| Prefetch classification | Some natural-language grammar treated as symbol names | Reject grammatical placeholders; identify explicit locate/signature/call targets |
| Cost history | A single observation replaced defaults; successful attempts only | Five observations minimum, bounded validated aggregates, at least 90% successful recorded attempts; attempted provider failures retained without private error text |
| Diagnostics | No discovery-size attribution | Actual presented/available counts and serialized schema bytes; provider token attribution explicitly unavailable |

The shared entry points remain `codeintel.Inspect`, `toolrouting.History`, the
existing MCP gateway, App turn/worker setup, and typed session observations.
There is no second orchestration system or Graft preference. Memory/Context
purpose routing, Skill Gate, Caveman, Headroom and ArtifactStore remain in place.
Exact evidence is not compressed. Existing source/result budgets, queue/process
locks, sandbox and managed component pins are unchanged.

This is **not yet a universal cost optimizer**. Native direct startup retains
full tool discovery because no task is available then. Ambiguous non-code text
can still receive the facade. Cross-worker result-reference reuse, global cache
retention across many repositories, fine-grained planning/validation timing and
all-provider cost attribution were not newly implemented. No claim of their
completion follows from these changes.

Modified source areas: `internal/{app,codeintel,toolrouting,externalmcp,
observability,opencodebridge}`. Documentation: `docs/native-graft.md`, its pt-BR
translation and generated embedded site. Benchmark harnesses and raw evidence
are versioned below. No component version, lockfile or approved checksum changed.

## Measured schema-projection ablation: negative aggregate

Raw data: [projection-pilot.json](evidence/economic-runtime/projection-pilot.json).
Three repetitions, alternating AB/BA, same requested model/effort, cold isolated
caches and Graft in both arms. A presents seven native schemas; B presents one.
All 24 turns passed their existing fact checks. These are narrow smoke oracles.

| Scenario (3 repetitions) | Input tokens A / B | Wall ms A / B | Tool calls A / B | Correct A / B |
|---|---:|---:|---:|---:|
| Implementation | 200866 / 201620 | 56428 / 55460 | 6 / 6 | 3 / 3 |
| Dependencies | 253380 / 252527 | 65599 / 51141 | 6 / 6 | 3 / 3 |
| API signatures | 280989 / 331423 | 65834 / 76316 | 8 / 8 | 3 / 3 |
| Exact string | 95461 / 95455 | 36228 / 35047 | 3 / 3 | 3 / 3 |
| Total | **830696 / 881025** | **224089 / 217964** | **23 / 23** | **12 / 12** |

Input tokens increased **6.06%**; time decreased **2.73%**. Per-repetition token
changes were -8.05%, -0.65%, +33.08%; time -18.97%, -2.63%, +20.79%.
This fails the predeclared release gate and shows substantial variance.
Structural calls remained 8/8; native calls were 10/9. Schema reduction alone
did not remove model exploration. A separate MCP contract test measured the
serialized tool array at **4585 → 1230 bytes** (7 → 1 schemas). These bytes are
not official model tokens. Later harness instrumentation adds actual discovery
counts and sanitized tool sequences; those fields are absent from this pilot
and were not reconstructed retrospectively.

## Production prefetch comparison against v0.12.0

Raw data: [runtime-comparison.json](evidence/economic-runtime/runtime-comparison.json).
Identical test harness compiled separately on detached `db93c6eb` and candidate
production code. Both use the actual App primary-prefetch path and retain Graft;
neither exposes MCP schemas to the model in this experiment. Three repetitions
per scenario, alternating AB/BA, 18 real Codex turns, strict structured oracles.

| Scenario | Input tokens A / B | Total ms A / B | Model tool calls A / B | Correct A / B |
|---|---:|---:|---:|---:|
| API signature | 144459 / 48358 | 56289 / 26839 | 6 / 0 | 3/3 / 3/3 |
| Implementation | 112032 / 48336 | 45372 / 22473 | 4 / 0 | 2/3 / 3/3 |
| Direct callers | 131041 / 48412 | 47563 / 27152 | 7 / 0 | 2/3 / 3/3 |

The candidate passed **9/9**; baseline **7/9**. Keep the two baseline failures:
callers repetition 0 and implementation repetition 1 did not meet the strict
oracle. Do not present all 18 rows as equivalent-quality savings. Among the
seven pairs correct in both arms, input was 290188 → 112856 and total time
110386 → 56242 ms. The API-only three-pair comparison is entirely correct.
This supports bounded automatic retrieval for these tasks, not a global claim.

The measured mechanism is avoided follow-up model tool calls: B received
sufficient exact bounded evidence before its response, while A often had no
prefetch or inspected the wrong grammatical symbol. The Graft process is used
for caller relations; exact Go bodies/signatures can use direct source.
Observed structural CPU totals were 1403 / 1441 ms and peak child RSS
108265472 / 113254400 bytes. Direct-reader CPU/RSS attribution is unavailable,
not zero. No memory/process ceiling was increased.

Official Codex 0.162.1 was used with `gpt-6.1-sol` and `low` explicitly requested
in both arms. The exec event protocol did not independently expose the effective
model, so this is not a verified server-side model-identity measurement. API key
variables were removed. Personal Codex configuration hashes were guarded.
No prompts or full model responses are stored in the evidence.

Test binary SHA-256 values for this comparison:

- A: `b36a03b20460ce5a9866eedd45c6e1832fd34ee9fa5c4e5bdddaa85349388f7b`
- B: `36d5d5d840f046a51b7b4bdc674501604972f0eb926158f13e442955311eb00f`

The B binary predates the subsequent session-envelope compatibility correction,
source-position fix and failed-attempt accounting; the measured retrieval and
classification paths are unchanged for these fixtures. Re-run final acceptance
on the final release candidate; do not silently substitute binary identities.

Reproduction: compile `TestLivePrimaryCodePrefetchBenchmark` with the same
harness on both revisions (`go test -c ./internal/app`), then run
`python3 scripts/benchmark-economic-runtime.py --help` for explicit binary,
verified managed Node/Graft and official Codex inputs. The driver refuses to
overwrite an evidence directory, alternates arms, preserves failures, and exits
nonzero for failed correctness. It consumes the existing Codex subscription.

## Local known-file benchmark

Identical `BenchmarkKnownFileContext` on baseline/candidate: 201 source files,
about 5.8 MB of unrelated source, 20 iterations × 5 samples per arm, GOMAXPROCS=2.
Raw [A](evidence/economic-runtime/known-file-A.txt) and
[B](evidence/economic-runtime/known-file-B.txt) outputs are preserved.
Median ns/op: **19927155 → 2675911**; allocated bytes/op:
**13139761 → 85301**. No Node or inference. Arms ran B then A rather than
alternating, so this is a local diagnostic, not final acceptance. Allocation
bytes do not measure whole-process RSS or provider tokens.

## Security and compatibility review

Codex Security diff scan `3599871e-356d-4960-b2d3-86e0e23bdc15` reviewed the
immutable range through `7297b44b`. It found a low-severity parser-boundary
problem in the candidate: adjusted Go line directives were used to slice
physical source. A synthetic fixture reproduced a panic. Commit `4de9839b`
uses physical positions for both endpoints. Forward, backward, before-function
and misleading in-bounds directives in line/block forms now pass; ordinary
bounded declarations remain valid. Independent boundary and patch reviews
found no surviving issue in that correction. This is not a claim of a complete
repository-wide audit, nor does the pre-fix scan certify later commits.

Discovery remains distinct from authorization; tests cover pre-plan refusal,
exact grants, empty worker lists, JSON/SSE filtering, and callbacks outside the
gateway mutex. New metrics persist scalar counts only. They use the existing
`capability.resolve` envelope with `capability=tool.projection`; an unchanged
compatibility test passed on both candidate and v0.12.0 validators. No migration
of session data or personal agent configuration is required.

## Validation status and remaining release gates

Executed successfully in this cycle:

- `go test -p=1 -timeout=20m ./...` after the embedded documentation build stabilized.
- `go vet -p=1 ./...`; `govulncheck ./...`: no vulnerabilities reported.
- Runtime npm audit and documentation production dependency audit.
- Official documentation build; frontend Node tests: **41 passed**.
- Python script unit discovery: **6 passed**.
- Focused source-position correction and owning `internal/codeintel` package.
- Official native Codex frontend contract smoke (no inference): passed.
- Managed Graft archive contracts: operations, incremental indexing, cache,
  worktrees, cross-organization/permission isolation, corruption recovery,
  hostile archives and healthcheck passed against published v0.12.0 bytes.
- Managed OpenCode HTTP, approval, cancellation, keyboard/resize and native
  executor contracts passed. The initial attach test split `index=fresh` at
  60 columns; commit `38126447` separates diagnostic fields into compact rows.
  Actual attach passed at 60/160 columns. One subsequent 100-column startup
  hit the existing provider-catalog readiness timeout; three targeted repetitions
  passed (23.81, 23.16, 22.05 s). That intermittent readiness failure remains
  recorded and unresolved; passing retries do not establish its cause.
- Real `TestLiveGraftManagedOpenCode`: **166.06 s**, approved plan, integrated task,
  official Codex native tool use and structural evidence, operational panel,
  **0 repository mutations, 0 personal Codex configuration mutations**.

The first full Go attempt overlapped replacement of generated embedded docs and
was invalidated by missing old asset paths. It was stopped; the stable rerun
passed. The first live smoke build omitted the release metadata URL and failed
before client launch; correcting the harness metadata enabled the successful
run. Neither failure was hidden by weakening product checks.

Full race, final post-fix regression, architecture/platform, install/update and
rollback validation are tracked separately; ordinary opt-in live tests skip
without their explicit artifact inputs. A successful unit suite does not imply
those skipped real-provider paths passed. Claude remains contract-only.

| Required final scenario | New repeated real A/B evidence |
|---|---|
| Small known-file correction | Local read microbenchmark only; executable edit A/B pending |
| Function identification | 3 App pairs; baseline 1 strict failure |
| Cross-module refactor | Pending |
| Dependencies | 3 schema-ablation pairs; not full App A/B |
| Impact | Pending |
| APIs | 3 correct App pairs plus schema ablation |
| Large project / IVOAI | Historical negative preserved; new final A/B pending |
| Monorepo | Pending |
| Unsupported parser | Existing contracts; repeated real A/B pending |
| Institutional documentation | No-code-provider contract; real A/B pending |
| ai-memory history | Existing purpose routing; real A/B pending |
| Non-code tasks | Conservative facade fallback; real A/B pending |
| Multiple workers | Exact-grant regression; real reuse A/B pending |
| Reuse between turns | Existing cache tests; repeated real A/B pending |
| Interrupted continuity | Existing regression suite; new real comparative flow pending |
| Concurrent distinct projects | Existing isolation tests; repeated real A/B pending |

**Release decision: not authorized by current evidence.** The schema pilot
missed the gate; the positive prefetch subset does not replace the full
16-scenario comparison, independently verified model identity, warm/cold and
concurrency coverage. No new release/tag or production deployment is claimed.
Keep the candidate reviewable and retain all negative results. The next cycle
must first expand the comparison and resolve these acceptance gaps; no generic
token-saving percentage should be advertised.
