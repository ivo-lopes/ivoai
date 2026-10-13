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

Provider-reported cached input was 624384 / 677120 and output was 2293 / 2333.
The remaining input was 206312 / 203905. Most of the aggregate input increase
was therefore cached input; no monetary saving or cost is inferred without
verified pricing. Provider cache equivalence is not established by cold local
Graft caches.

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
- Repeated real smoke after the retrieval/security/panel corrections: **118.17 s**,
  same acceptance markers, zero repository/personal-config mutations. This is
  another functional smoke, not a controlled A/B latency comparison.
- Cross-builds and emulated version smoke for candidate Linux amd64/ARM64;
  Nehalem (no AVX/AVX2) smoke for candidate IVOAI, pinned OpenCode and Graft
  library/native-WASM grammars passed. QEMU was reused from an isolated cache;
  no system packages installed. Native ARM64 execution is covered by CI.
- Actual published v0.12.0 binary → candidate → v0.12.0 configuration rollback
  and reapply passed in 7.83 s, preserving two server profiles and secret
  isolation. This is config/binary compatibility, not database migration rollback.
- Full local race suite passed before the final test-fixture isolation change;
  final CI reruns the exact committed source. ShellCheck and generated CLI
  reference consistency passed.

The first full Go attempt overlapped replacement of generated embedded docs and
was invalidated by missing old asset paths. It was stopped; the stable rerun
passed. The first live smoke build omitted the release metadata URL and failed
before client launch; correcting the harness metadata enabled the successful
run. Neither failure was hidden by weakening product checks.

A later normal-suite rerun exposed an existing non-hermetic App fixture:
`managedSetupApp` marked synthetic binaries installed, but the supply-chain
installer correctly revalidated them and attempted a real Caveman download.
One download timed out after 286.13 s. The owned test process was stopped with
stack capture; this run is **failed**, not counted as passing. App now has a
private installer seam used only by that fixture, leaving Memory hooks and
diagnostics active. Production still calls the same real installer. Five
focused setup/hook/doctor/error-propagation tests passed in 24.887 s. Component
integrity, hostile archives and real installation remain tested separately;
no integrity check or critical test was disabled.

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

## Git and operational reconciliation

Review: [PR #30](https://github.com/ivo-lopes/ivoai/pull/30), draft,
`fix/economic-runtime`, based on published `db93c6eb`. No merge, tag or release.
The initial CI run [38332814626](https://github.com/ivo-lopes/ivoai/actions/runs/38332814626)
passed all 12 platform/install jobs; its quality job was still running when the
fixture correction was prepared. Use the PR's current checks for the final head.
Classic branch protection returned “Branch not protected”; rulesets were empty.
The user's stricter technical release gates still apply.

Commits are separated by routing history, retrieval, exact worker grants,
MCP/metrics, experiments, bilingual docs, source-position correction, session
compatibility, failed-attempt accounting, narrow-panel correction and driver.
Complete history: `git log db93c6eb..fix/economic-runtime --oneline`.

Plane reconciliation for IVOAI-172, 171, 132, 131, 138, 133, 135 and 136 was
**not applied**: canonical workspace/project identity remains unavailable.
Their P1/P2 implementations remain inherited from v0.12.0; no task is marked
Done merely because code exists. No duplicate optimization task was created.
IVOAI-188 remains exclusively pending real Claude Code acceptance.

Local candidate binary SHA-256 (not published release artifacts):

- amd64: `627896b91bd197d8323d28727556e33c2a68ce2a13e0ffbd71411fb7860f085f`
- arm64: `f418aa59bd3f5598b6458e5e2ddd892c74bde5559e3f9f659e815b213257618e`

These binaries contain the runtime through `e6281e32`; later fixture/report
changes require final CI validation, not a claim that a release was distributed.

## API refinement — predeclared structured ablation (2026-10-13)

Continue the existing branch; do not replace historical measurements. The new
`api_schema_v2` experiment compares seven native schemas (A) with the targeted
API lookup contract (B), on the same updated production code in both arms.
This isolates discovery; **A is not the published v0.12.0 binary in this ablation**.
Three independent pairs alternate AB/BA, with cold local caches, requested
Codex gpt-6.1-sol/low, identical permissions, fixtures and JSON response schemas.
Acceptance independently checks symbol, actual package, ordered parameter types
and return type. Earlier substring-based API rows remain narrow smoke evidence.
The new oracle is versioned before this run, not retroactively applied to missing
historical answers. Keep all failures. The release's 10% input / 5% latency,
two-of-three and zero correctness-regression gates remain unchanged; this single
scenario cannot satisfy the required full matrix.

The official 0.162.1 App Server `model/list` and `thread/start` were exercised
in an isolated CODEX_HOME without credentials or inference. They report the
requested model and reasoning effort as client selection. They do not attest
which backend model served previous exec turns; no runtime_verified flag is used.

### Findings and implementation from the API investigation

The instrumented pre-fix reproduction recorded B doing non-exact `ask` through
Graft followed by file `skeleton` or shell inspection. A used direct file API
inspection or exact symbol lookup. Arguments, source and full model messages
were not persisted. This proves an avoidable retrieval sequence in these new
samples; the old 51,902-token outlier cannot be reconstructed retrospectively.
An independent fixture demonstrated a correctness gap in direct evidence: the
actual Go package name was absent, and a directory name cannot establish it.

`7cf9b6b8` supplies the parsed package with the bounded declaration and projects
the existing implementation/file-API contracts for single API tasks. Mixed
relations/impact retain the facade, and explicit grants remain authoritative.
Normalized operation names now appear as `code.<operation>` in the existing
capability event. Unrecognized operation text is omitted from telemetry.
No additional orchestration service, Graft switch, permissions or memory limit.
This is not a general host proof that every acceptance criterion is satisfied:
relationships and executable verification still need their own evidence.

The old and new API ablations are recorded separately under
[evidence/economic-runtime/api-refinement](evidence/economic-runtime/api-refinement).
Each row is three independent cold-local-cache pairs; A has seven schemas and
B has the then-current projection. These are discovery ablations on a shared
runtime, not published-v0.12.0 versus candidate full execution.

| API ablation | Input A / B | ms A / B | Calls A / B | Structural A / B | Correct A / B |
|---|---:|---:|---:|---:|---:|
| Instrumented before | 253588 / 281789 | 57446 / 68535 | 7 / 9 | 0 / 3 | 3/3 / 3/3 |
| First refinement | 202373 / 253055 | 48190 / 56421 | 6 / 7 | 0 / 0 | 3/3 / 3/3 |
| Refinement with item counters | 253068 / 227389 | 52100 / 53103 | 6 / 6 | 0 / 0 | 3/3 / 3/3 |

The sign of token improvement changes between repetitions/experiments. Removing
structural calls does not establish aggregate token or latency savings. The
item counters expose the same completed command/MCP/message counts with different
provider input totals; those counters are not an official per-inference request
trace. No hidden reasoning or backend routing is inferred from them.
`CommandResultBytes` in `api-events-after.json` is **invalid/unavailable** due to
a missing JSON field tag in that instrumentation revision; the zero values must
not be used. The tag is corrected before `api_schema_v2`. Historical files remain
unaltered. Byte measurements are never substituted for provider token accounting.

### Readiness reliability

`5796e7fd` preserves the 20-second ceiling while classifying loading (202),
unavailable status, empty/partial catalogs, invalid response, communication
failure and timeout. Authentication errors and redirects terminate immediately.
Responses carrying positive cache age are not accepted as fresh readiness.
Transient retries use bounded backoff instead of ten requests per second.
Diagnostics contain fixed state labels/counts, never response bodies, model
catalog contents, credentials or arbitrary network errors. Cancellation retains
its original error identity.

The old code repeated 401 nine times before a one-second fixture deadline. The
new tests prove one terminal attempt, recover from loading/empty responses, and
reject redirects/stale catalog responses. Race tests passed. The actual pinned
OpenCode interface passed nine launches (three repetitions at widths 60/100/160,
154.774 seconds total). This does not establish the cause of the historical
intermittent timeout, which remains unknown; its next occurrence is diagnosable.

### Local cache and concurrency measurements

Real reviewed Graft artifacts, synthetic data, race detector; no inference.
The 121-file cold build took 3860 ms. Ten warm requests all hit cache, p95 19 ms
(target 100 ms); source edit/rename/delete took 521/612/595 ms and invalidated
receipts correctly. Maximum measured process RSS was 108085248 bytes. These were
local tool-layer observations during other host validation, not an idle-host or
provider-token benchmark.

Eight concurrent requests across two distinct repositories and organization
scopes produced two structural processes in total, at most one active process,
six cache hits and eight correct isolated responses. Peak observed RSS was
106790912 bytes. This proves provider admission/deduplication/isolation for that
fixture; it is not a multiple-worker or frontend-session A/B benchmark.

### Current acceptance boundary

The full published-binary comparison across all 16 requested scenarios remains
unexecuted. Do not relabel the focused API ablation, previous App prefetch subset,
provider concurrency fixture or regression suites as that matrix. In particular,
new real comparisons for monorepos, unsupported parsers, institutional Memory /
Context, multiple workers, multi-turn reuse and interrupted frontend continuity
remain missing. The earlier matrix above continues to describe those gaps.
The gate remains failed; PR #30 stays draft and no release is authorized.

Full normal Go tests, vet and govulncheck passed after the runtime corrections;
the owning codeintel/observability/opencodebridge packages passed race tests.
The real managed OpenCode -> IVOAI -> Codex smoke passed in 143.95 seconds with
approved plan, integrated task and structural evidence, zero repository and
personal-configuration mutations. Documentation was regenerated in EN and pt-BR.
The final PR CI must validate the exact committed tree, including the final test
harness. Component pins and manifests remain unchanged.

The official model provenance probe used isolated CODEX_HOME and no inference:
model/list includes gpt-6.1-sol/low; thread/start reports that selection and the
openai provider. This is official client selection, not independent backend
attestation and not retroactive proof of historical exec inference identity.

A final directed search of accessible local repository directories found no
Plane MCP source. Explicit pessoal-ivo still returns WORKSPACE_REQUIRED because
the exposed schema lacks workspace_slug. No canonical project identity or writes
were obtained. IVOAI-188 remains pending real Claude; no Claude inference occurred.

### Structured API result and release decision

`api_schema_v2` ran at `0e363710` (runtime identical to `944e946e`) with test
binary SHA-256 `7c47c05d964607c743c3a89867c4e082139fe21d21ce5ba32a7801f1fe5d2879`.
All six rows and fixture hashes are retained. B passed 3/3; A passed 2/3. A
repetition 1 failed the strict acceptance check; the overall opt-in test **failed**.
No private answers were retained and no particular mismatched field is inferred.

Among the two pairs correct in both arms, input was **161560 -> 178798** (+10.67%)
and time **34270 -> 43788 ms** (+27.77%). Across all rows, including the A failure,
input was 280866 -> 245939 and time 59080 -> 62611 ms. The all-row token reduction
is not equivalent-quality savings. B made zero structural calls in all three
samples; eliminating those calls still did not satisfy the economic gate.
No further retries were used to select a favorable measurement.

The provider event stream does not expose per-inference input attribution, so
identical completed item counts cannot prove identical inference work or explain
all residual variance. No prompts or reasoning were captured to force attribution.
The full 16-scenario final acceptance, published A/B binaries, complete worker /
continuity coverage and aggregate savings remain unproven. This is a partial
technical delivery, not completion of the requested release mission.

Binary and dataset identities are in `api-refinement/manifest.json`. The published
v0.12.0 binary hash is recorded as provenance, explicitly **not** as an executable
used by the schema ablation. No release was published and no artifacts are claimed
as newly distributed. Final CI status belongs to the existing PR #30.
