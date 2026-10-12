# Graft native runtime — validation record

This record separates implementation, provider execution, artifact validation and
publication. It is a candidate record until the release workflow and distributed
artifacts have been verified. No production deployment is included.

## Reconciliation and provenance

PR [#28](https://github.com/ivo-lopes/ivoai/pull/28) was merged as
`379ff2ead198b0b32158d76cbf5b19fe85d7cdb1`. The Graft branch descends from that
commit; it preserves the P1/P2 changes described in
[the previous validation record](backlog-p1-p2-validation.md). The previous stable
release is v0.11.0. PR [#29](https://github.com/ivo-lopes/ivoai/pull/29) contains
the Graft work.
Draft PR [#25](https://github.com/ivo-lopes/ivoai/pull/25) was closed as superseded,
with [the reconciliation rationale](https://github.com/ivo-lopes/ivoai/pull/25#issuecomment-6133662904):
PR #28 supplies receipt-owned hook diagnostics and transactional repair without
merging the older broad reconfiguration path. This is not a Plane Done update.

| Input | Immutable identity |
| --- | --- |
| Graft upstream | 0.21.1, `fe30ead39d5e6f0c921018d364da2bdbc9d4b3ad` |
| Managed distribution | 0.21.1-ivoai.1 |
| Upstream source archive SHA-256 | `ab27f9123eeb59b3987a342a24b7e357ad3412f41c556fde3a76ca9e250d2db1` |
| Original npm lock SHA-256 | `c05064ea18ed1b538e27cc788f1d6ea2cae00b2c00c79ef552fc1bb4c54fd5f5` |
| Node | 26.11.1, privately managed |
| License | MIT; additional dependency and WASM notices accompany the archive |

Pre-release CI run [38312535988](https://github.com/ivo-lopes/ivoai/actions/runs/38312535988)
produced an amd64 archive with SHA-256
`3f07763f1a04d1ac533bb5557702bfc9307feb2f85bf5e384b7f9831f1505d7b`.
This artifact was downloaded and exercised through the real sandbox, MCP SDK and
component promotion/rollback tests. It is **CI evidence**, not proof of a published
release. The final release hashes must be verified separately.
The successful later [CI run 38313736037](https://github.com/ivo-lopes/ivoai/actions/runs/38313736037)
rebuilt the same reviewed Graft inputs. Its downloaded amd64 archive is byte-for-byte
identical (7,689,744 bytes), with equal metadata and matching embedded provenance.
[`reproducibility.json`](evidence/graft-v0.12.0/reproducibility.json) records this
repeated-build evidence; it is not an independent cryptographic signature.

## Architecture and implementation map

| Area | Main paths |
| --- | --- |
| Provider contract, scoped source snapshots and bounded cache | `internal/core/codeintelligence.go`, `internal/codeintel/` |
| Economic eligibility and cost comparison | `internal/toolrouting/`, `internal/codeintel/routing.go` |
| Managed installation, provenance and rollback | `internal/components/managed_graft.go`, existing `internal/supplychain/` |
| Filesystem/network/process confinement | `internal/codesandbox/` |
| Session and worker capability projection | `internal/app/code_intelligence.go`, `internal/app/worker_access.go` |
| Native MCP contracts | `internal/codeintel/mcp.go` |
| Reviewed distribution and mandatory artifact tests | `scripts/build-managed-graft.sh`, `scripts/test-managed-graft.sh`, `patches/graft/` |
| Diagnostics and operational presentation | `internal/doctor/`, `internal/observability/`, `internal/opencodebridge/` |
| English and Portuguese documentation | `docs/native-graft.md`, `website/i18n/pt-BR/docusaurus-plugin-content-docs/current/native-graft.md` |

The provider supports build, ask, skeleton, callers, grouped literal grep, map,
blast, check and static viz. It runs reviewed libraries rather than upstream CLI
initialization, hooks, cloud synchronization, telemetry or update checks. Deep
LLM enrichment and LSP subprocesses are not admitted. Structural operation needs
no external API key. Memory and Context keep their existing responsibilities.

## Measured baseline and regression discovery

Raw sanitized measurements are under [evidence/graft-v0.12.0](evidence/graft-v0.12.0).
They contain metadata and counts, not prompts, tool output, credentials or model
reasoning. Null values mean unavailable, never zero.

The first extended real-provider run used official Codex 0.162.1, model
`gpt-6.1-sol`, effort `low`, equivalent fixture state and the same prompt per A/B
pair. All **24 executions across 12 scenarios** passed their correctness checks.
Four edit scenarios additionally used a closed-program acceptance guard followed
by offline Go tests. The real IVOAI navigation case used a detached read-only
checkout at `8193ecf6`.

| Observed total, first extended run | A: no structural tools | B: structural tools available |
| --- | ---: | ---: |
| Input tokens | 852,523 | 1,016,734 |
| Cached input tokens | 670,976 | 808,192 |
| Output tokens | 3,191 | 3,443 |
| Elapsed milliseconds, summed cases | 218,209 | 250,556 |
| Tool calls | 26 | 35 |
| Correct cases | 12/12 | 12/12 |

This is a **negative aggregate efficiency result**: B consumed approximately
19.3% more input tokens and 14.8% more elapsed time. It does not support a general
token-savings claim. One paired sample per case, provider caching and concurrent
host validation limit causal inference. The initial smaller pair favored B, but
does not override this broader result. These measurements motivated routing and
tool-contract refinements, which require separate follow-up evidence.

Tool-layer measurements separately showed useful relationships unavailable from
the bounded textual baseline, smaller API/declaration responses and cached
structural queries around 8–12 ms. Cold indexing of IVOAI under concurrent load
took 46,383 ms, with observed peak Node RSS 324,014,080 bytes. These timings are
not an idle-host benchmark. Direct text remained faster for some simple cases.
Result-byte reductions are not equivalent to measured provider-token savings.

A separate 121-file structural benchmark established a warm-query target of
100 ms from the observed 8–12-ms baseline, retaining the existing 90-second cold
timeout, 4,096-byte response budget and monitored 512-MiB RSS ceiling. Ten warm
samples all hit the cache; nearest-rank p95 was **9 ms**. Cold construction took
4,241 ms, an incremental source edit 628 ms, a rename 767 ms and deletion 744 ms.
The changed signature and source digest were checked, not just timing. Maximum
observed Node RSS was 109,203,456 bytes. These are local tool-layer measurements;
provider token counts remain unavailable. Raw samples are in
[`structural-cache-incremental.json`](evidence/graft-v0.12.0/structural-cache-incremental.json).

The extended harness initially caused Codex to create eight temporary fixture
trust entries in its personal configuration. Only those eight exact owned entries
were removed, preserving all unrelated values. The harness now uses the existing
process-local `ProjectTrustArgs` helper. A real write-task canary subsequently
passed with identical configuration hashes before and after. The initial run is
not represented as a configuration-preservation pass.

## Validation boundaries

- Unit/full Go tests passed before the final refinements. The initial full local
  race run was invalidated by concurrent source/document generation: compilation
  reported missing imports and embedded assets. It was not a successful race run.
  The repeated `go test -race -p 1 -timeout 20m ./...` in an immutable checkout of
  `330b7baf` **passed all 53 tested packages**, exit 0. App took 441.393 s,
  Caveman evaluation 354.377 s and orchestrator 106.483 s. Later primary-admission,
  explicit-grant, economic-MCP and Prompt Gate changes received focused race
  regressions; final-head full-suite CI remains a separate gate.
- Targeted race tests for code intelligence, component management and Codex
  resolution passed. `go vet`, `govulncheck` (no vulnerabilities found), shellcheck,
  adapter tests and documentation builds passed on the tested candidate.
- Actual reviewed-distribution tests passed for native operations, SDK MCP,
  worktree isolation and static visualization. Real archive installation,
  idempotence, malicious-candidate rejection, promotion and rollback passed.
- CI native amd64 and ARM64 artifact tests passed, including the amd64 baseline
  without AVX/AVX2. Cross-compilation alone is not used as native runtime evidence.
- Claude validation is contractual/simulated. No Claude authentication or paid
  fallback was attempted. IVOAI-188 remains deferred for real Claude validation.

Review refinements include exact native tool allowlists (an explicit empty list
denies access), completed native quota/checkpoint admission before prefetch,
standard-parser validation of exact Go declarations, and unknown index state for
direct reads. Tests reproduced the old failure before each fix. The public plugin
SDK version is pinned separately from OpenCode's managed executable version.

## Residual constraints and task governance

Indexes are regenerable caches. Memory is bounded per active process; the RSS
threshold is sampled, not a kernel hard limit. Cache generations are validated,
but there is no global disk-retention service. Source inventory deliberately
excludes oversized/generated/ignored/unsupported files and external roots.
Coverage diagnostics must accompany incomplete structural results.

The Plane connector still resolves only the historical Mindsite project and does
not expose canonical **Pessoal - Ivo** workspace selection. No tasks were created
or statuses changed in the historical workspace. Git evidence does not substitute
for lifecycle updates in the correct Plane project.

| Task | Preserved implementation / acceptance disposition |
| --- | --- |
| IVOAI-172 | Receipt-owned hook preflight and transactional recovery from merged PR #28 |
| IVOAI-138 | Typed correlated telemetry, bounded replay/SSE and explicit unavailable states |
| IVOAI-132 | Operational OpenCode console; current real frontend validation remains a separate gate |
| IVOAI-131 | Existing navigation, model/effort, diffs and operational presentation preserved |
| IVOAI-133 | Seven profiles, preview, effective configuration and bounded restoration |
| IVOAI-135 | Shared CLI/TUI administration and documented administrative exceptions |
| IVOAI-171 | Logical identity, scoped resume, explicit adoption and recovery preserved |
| IVOAI-136 | Complementary v0.11.0 installation/update/recovery homologation, without production migration |

**IVOAI-188:** deferred exclusively for real Claude Code validation; do not mark Done.
