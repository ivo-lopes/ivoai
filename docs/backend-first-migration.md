# Backend-first migration: audited baseline and delivery gates

Audit date: 2026-10-27. Code baseline: published `v0.12.1`, commit
`3ff6db8bb0174b1ae6f781f6754e2653a6dae5db`, incorporating
[PR #30](https://github.com/ivo-lopes/ivoai/pull/30).
This document distinguishes that baseline from the approved migration target.
It does not certify implementations subsequently added on a development branch.

## Implemented increment after the audited baseline

The v0.13.0 preparation includes the bounded experimental code Evidence Gateway,
shared informal-prompt intake and Core-owned executor-turn contracts, together
with turn-local completion/resource ownership and dependency remediation. These
are partial deliveries on the migration below, not completion of IVOAI-191.
The Gateway stays disabled by default; ordinary execution still uses the existing
DAG acceptance policy. Memory/Context/tool adapters, recovery-policy integration,
adaptive dispatch, native-client certification and OpenCode retirement remain
pending. See [backend validation](https://github.com/ivo-lopes/ivoai/blob/5949f20d238acc1c4108515fc462b88e979537c3/release-notes/backend-first-validation.md),
[Core contracts](https://github.com/ivo-lopes/ivoai/blob/5949f20d238acc1c4108515fc462b88e979537c3/release-notes/backend-neutral-turn-contracts.md) and [dependency remediation](https://github.com/ivo-lopes/ivoai/blob/5949f20d238acc1c4108515fc462b88e979537c3/release-notes/dependency-remediation-validation.md).
The current global economic gate remains INCOMPLETE; the final measured subset
failed the 10% input and 5% elapsed targets. No later metadata fast-path experiment
is included in this prepared release.

## Sources and execution state

Git and reproducible runtime observations establish delivered behavior. Plane
records execution intent, acceptance requirements and task history; a task's
description or state alone does not prove that its implementation is present.

The canonical execution project was recovered in Plane workspace `pessoal-ivo`,
project `b99f4a34-0e48-455a-acc7-7d908ba00478`, identifier `IVOAI`.
The parent item is IVOAI-191. Its backend-first decisions and related work items
were retrieved; some later activity-history reads encountered rate limiting.
The historical `mindsite` project must not receive updates as a substitute.

The requested canonical memory pages could not be recovered during this audit:

- `mindsite/ivoai/arquitetura-evidence-gateway-economia-backend-first-2026-10-27`
- `mindsite/ivoai/decisao-backend-first-nativos-antigravity-remocao-opencode-2026-10-27`

Reads through the default `ivoai` scope returned no matching pages; the Mindsite
connector rejected retrieval. Their contents are therefore not represented as
independently verified historical evidence here. The current approved migration
requirements remain available in the maintainer's explicit instruction and Plane.
Context search returned no relevant indexed documents. Its agent-facing MCP is
read-only, so this audit did not ingest or synchronize documents:
`CONTEXT_SYNC=BLOCKED` (IVOAI-175). Writing a memory page would not change that
status or prove Context ingestion.

Remote inspection confirmed v0.12.1 as the latest published release. The initial
local checkout was an older v0.10.4 feature branch; implementation uses a separate
worktree based on the fetched main HEAD above, preserving that checkout.
Open PRs at audit time were dependency updates. PR #30 was already merged.
Recent Dependabot Docker update jobs included failures; these jobs must not be
reported as passing product certification. Recheck exact-head product CI and
release workflows before promotion.

## Existing subsystem boundaries

The executable remains one Go program with client and server modes. The table
covers the top-level `internal` packages at the audited commit; package grouping
does not imply a second runtime authority.

| Packages | Existing interfaces and responsibilities |
|---|---|
| `cli`, `app`, `terminalui` | CLI/TUI entry points, administrative menus and the application composition root. `App.OrchestratedWithKnowledge`, `SessionStartWithKnowledge`, launch and management methods bind existing services. `cmd/ivoai/main.go` is the executable entry point. |
| `core` | Provider-neutral `Component`, `Executor`, `MemoryBackend`, `ContextBackend`, `CompressionProvider`, `Orchestrator`, `SkillSource`, `SkillRegistry`, `ToolProvider` and `CodeIntelligenceProvider` contracts; runtime capabilities, health, compatibility and provenance. |
| `agents`, `codexresolver` | Official CLI launch/lifecycle, argument normalization, process observations, compatible executable selection and fingerprinting. OpenCode-specific executors still coexist. |
| `codexfrontend`, `opencodebridge` | Native Codex App Server facade and managed OpenCode transports. Shared turn, model, status and continuity types currently live in `opencodebridge`, creating an architectural dependency even for Codex. |
| `session` | Private lifecycle records, decisions, checkpoints, operational events, continuity mappings, PID/start-marker identity and bounded persistence. Provider-owned conversations remain separate. |
| `orchestrator`, `orchestration` | Existing session-local MCP and scheduler, DAG admission, dependencies, workers, recovery, worktree collection/integration, concurrency policy and lifecycle adapters. Native coordination already exists; Ruflo remains an explicit legacy lifecycle adapter. |
| `routing`, `toolrouting` | Executor/model/effort profiles, task tiers and capability-cost selection. Authorization, compatibility, freshness and quality precede economic selection; missing measurements are distinct from observed zero. |
| `workers` | Official executor adapters, task-scoped MCP access and bounded results. Advisory readers coexist with controlled implementation workers in owned worktrees. |
| `workingcontext` | Transient `ArtifactStore`, `ArtifactRef`, `ResultRef`, `WorkerResult`, findings and advisory `StateDelta`; exact payloads, hashes, TTL, ownership, quotas and bounded representations. |
| `codeintel`, `codesandbox` | Direct/Graft intent routing, repository inventory, snapshots, index/results cache, cost observations and managed structural helper isolation. `core.CodeIntelligenceProvider.Query` is the replaceable boundary. |
| `knowledgepolicy`, `serverpool`, `knowledgerouter` | Memory-first/Context-second research contract, purpose/redundancy selection and authenticated per-session loopback knowledge routing. Read federation differs from write destination selection. |
| `memory` | ai-memory lifecycle/configuration and hooks. Durable content operations remain behind its authenticated MCP boundary. |
| `context` | `LegacyQdrantContextBackend`, document catalog, embedding/vector-store interfaces, chunking, safe filesystem/Git connectors, ingestion and read-only agent MCP tools. |
| `compression`, `caveman`, `headroom`, `cavemaneval` | Fidelity classification, exclusive provider selection, Caveman default, optional Headroom compatibility, Direct fallback and bounded evaluation fixtures. |
| `externalmcp`, `connections` | MCP inventory, local projections, authenticated gateways, connection registry and tool/source grants. Discovery does not grant execution authority. |
| `skills`, `skillcatalog`, `skillgate`, `policy` | Skill registry/index/graph, source discovery, task selection, policy evaluation and activation admission. Registry presence is not permission. |
| `supplychain`, `skillupdate` | Reviewed source identity, integrity validation, non-executing staging, promotion and rollback for managed artifacts. |
| `quota`, `observability` | Official quota probes/cache and bounded typed operational events. Display reads existing state; execution retains live admission checks. |
| `config`, `secrets`, `project`, `platform` | Versioned configuration and paths, private credential storage, project detection, safe files/processes and redaction primitives. |
| `components`, `componentupdate` | Pinned managed component catalog, provenance, setup/update and executable ownership. |
| `migration`, `update` | Ordered reversible schema graph, bounded snapshots, verified candidate staging, atomic promotion, transaction recovery and rollback. |
| `server`, `servercmd` | Deployment assets, private backend credentials, service lifecycle, connector administration, ingestion runner, backup/restore and gateway composition. |
| `gateway`, `enrollment`, `webauth`, `webmcp` | Versioned HTTP discovery/control reads, scoped enrollment, Web OAuth and remote read/write MCP composition. These are not a complete administrative API. |
| `doctor`, `docsserver` | Runtime diagnostics/component matrix and embedded documentation serving. |
| `promptgate` | Bounded, non-executing intake assessment. At baseline it still requires observable acceptance criteria and rejects some otherwise actionable informal requests. |

Contract definitions are in [core/contracts.go](https://github.com/ivo-lopes/ivoai/blob/v0.12.1/internal/core/contracts.go)
and [core/codeintelligence.go](https://github.com/ivo-lopes/ivoai/blob/v0.12.1/internal/core/codeintelligence.go).
Runtime contracts are intentionally separate from persisted configuration and
ownership schemas; adding an adapter need not introduce a schema migration.

## Reusable evidence foundation and remaining gaps

`workingcontext.Projector` commits raw worker evidence before deriving its bounded
representation. `LocalStore` supplies opaque IDs, SHA-256 verification, private
files, size/count quotas, TTL and session/task/worker ownership. Full and range
reads are available through local orchestration MCP tools. Durable ai-memory,
document Context and transient artifacts serve different purposes and must remain
separate.

Code evidence is already prepared through
`app.prefetchCodeEvidence`, shared by primary and workers. Primary preparation
checks grants and Skill Gate, observes a bounded context budget and revalidates
admission after retrieval. `orchestrator.primaryEvidence` performs retrieval
outside the scheduler lock. These are composition points for the Evidence Gateway,
not reasons to add a scheduler.

Graft's current identity binds the physical worktree, host-selected knowledge
scope and permissions. Its cache validates source, adapter and runtime identity.
The provider uses private snapshots, a bounded cross-process queue, scoped locks
and a serialized structural process. The helper has restricted environment,
filesystem/network/syscall boundaries and a 512 MiB monitored process-memory
ceiling. The direct path avoids unnecessary structural queries for supported
literal and exact-declaration questions.

The baseline does not provide a generic Evidence Gateway. Missing capabilities
include a common evidence/provenance envelope, session-level reuse across primary
and workers, deduplication, sufficiency/gap reporting, explicit fact/inference
separation and budgeted selection across Memory, Context, code and tool results.
Existing code-result caching is not proof of these broader capabilities.

The migration must address these boundaries explicitly:

- `Ownership` records session/task/worker, not the full organization/repository/
  worktree/permission tuple. Bind these additional scopes in trusted host
  composition and recheck authorization on reuse and exact recovery.
- A code artifact preserves the selected result exactly. When that result is a
  truncated excerpt, its reference does not recover omitted source bytes. Report
  completeness honestly and retain a valid route to the required full source.
- `LocalStore` serializes one instance with a mutex. Do not assume that this
  supplies cross-process quota or deduplication guarantees for independent store
  instances. Add or reuse explicit admission when broadening concurrent access.
- Artifact sensitivity is supplied by the caller. The store rejects prohibited
  sensitivity classes; it does not automatically prove raw bytes contain no
  credentials. Content admission must prevent secret persistence before writing.
- A cache hit cannot replay a mutation, bypass current grants or convert retrieved
  content into execution instructions. No new durable database is required.

## Client coupling and compatibility

At baseline `ivoai codex` uses orchestration by default, with explicit `--direct`
available. Claude launch remains direct; its explicit legacy orchestrated path is
not equivalent to the native AUTO path. `OrchestratedWithKnowledge` accepts only
Codex and OpenCode frontends.

The current Codex frontend uses the official App Server interface, but its
`Options.Bridge` is an `opencodebridge.Bridge`. Application turn handling, worker
interfaces, operational events and continuity also reference OpenCode-namespaced
turn/result/catalog types. Removing the package would break shared Codex behavior.
First extract common turn and executor contracts without changing their behavior;
then move OpenCode-only presentation and transports behind their adapter.

No Antigravity runtime adapter, RTK adapter, Codebase Memory adapter or OpenViking
Context implementation was found in the baseline's own runtime packages. Their
acceptance requires upstream contract and license investigation plus real tests.
Headless execution alone must not be described as transparent native TUI control.
Official clients retain authentication ownership; do not copy subscription tokens
or enable an unrequested paid API fallback.

OpenCode deprecation must preserve configuration, credentials, executables and
history owned by the user. Keep explicit compatibility errors or deprecated
command routing during coexistence. Shared functions must reach the core before
managed integration removal.

## Dependencies and platform envelope

The historical audited v0.12.1 `go.mod` requires Go 1.27.2. The prepared v0.13.0
candidate raises both the module minimum and source-bootstrap pin to Go 1.27.3
after the earlier CI vulnerability gate failed on Go 1.27.2 with reachable
[GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809) in `crypto/x509`. The Linux
amd64/ARM64 bootstrap hashes, manifest and notice were updated together. New
exact-head CI on `5949f20d` remains pending. Direct Go modules in the audited baseline are
`github.com/coder/websocket` 1.8.15,
`github.com/modelcontextprotocol/go-sdk` 1.8.0,
`github.com/pelletier/go-toml/v2` 2.4.3,
`golang.org/x/sys` 0.49.0 and `golang.org/x/term` 0.47.0.
`go.sum` supplies module integrity; indirect versions remain defined by `go.mod`.
Earlier local checks already used Go 1.27.3, while CI selected 1.27.2 from the
then-current module directive; the local clean scan did not certify that CI
compiler. The new candidate pin aligns the required toolchain with 1.27.3. This
does not change historical benchmark executable identities or establish an
equivalent published-binary comparison.

Reviewed external pins and licenses are recorded in
[manifest/components.yaml](https://github.com/ivo-lopes/ivoai/blob/v0.12.1/manifest/components.yaml) and the compiled catalog:

| Components | Baseline pins |
|---|---|
| Official clients | Codex and Code Mode host 0.162.1; Claude Code 2.1.296 |
| Managed OpenCode | 1.19.0 with the repository's reviewed patch/build contract |
| Memory/code | ai-memory 2.6.3; Graft/Trail-Code 0.21.1-ivoai.1 |
| Compression/lifecycle | Caveman and Caveman MCP 2.1.0; Headroom 0.40.0; Ruflo 3.56.5 |
| Private runtimes | Node 26.11.1; uv 0.13.0; CPython 3.14.8 |
| Server | Qdrant 1.19.2; Docker 29.7.2; Compose 5.6.0; TEI 1.9.4 |
| Embeddings | `intfloat/multilingual-e5-small` revision `614241f622f53c4eeff9890bdc4f31cfecc418b3` |

These are repository pins, not claims that each dependency is installed or healthy
on the audit host. The manifest's IVOAI entry still says 0.12.0; the release tag
and commit establish the application baseline, not that descriptive field.

Preserve Linux amd64, including CPUs without AVX/AVX2, and Linux ARM64. Component
archive hashes, OCI digests, npm integrity/locks and hashed Python constraints must
remain enforced. New integrations need their own reviewed license, immutable
version/commit, integrity, resource, sandbox and rollback evidence. In particular,
RTK-OSS scope must be distinguished from BUSL components, and an optional
OpenViking provider requires AGPL distribution assessment before adoption.

## Economic baseline and provenance

[PR #30](https://github.com/ivo-lopes/ivoai/pull/30) delivered bounded retrieval,
schema projection, failure accounting and diagnostics. The inherited
[validation report](https://github.com/ivo-lopes/ivoai/blob/v0.12.1/release-notes/economic-runtime-validation.md) preserves
multiple experiments with different scopes and binary identities. Some sections
describe intermediate draft/release decisions; the actual merged commit and
[v0.12.1 notes](https://github.com/ivo-lopes/ivoai/blob/v0.12.1/release-notes/v0.12.1.md) establish final publication status.

The economic gate remained failed when v0.12.1 was published. The schema pilot
increased aggregate input by 6.06% and reduced time by 2.73%, missing both targets.
Positive bounded-prefetch subsets do not establish whole-session savings. The
required 16-scenario comparison remained incomplete. Preserve negative outcomes
and correctness failures instead of reporting only correctness-matched pairs.

Future comparisons must distinguish:

- Published release artifacts, locally compiled production binaries, injected test
  harnesses, schema-only ablations and local microbenchmarks. They answer different
  questions and are not interchangeable baselines.
- Requested model/effort, configuration reported by official client methods and
  independently observable backend inference identity. A model picker or thread
  configuration does not establish the last of these.
- Provider-reported input/output/cache usage and estimates such as byte/4.
  Cached input is a subset of input, not an extra additive token total.
- Cold local indices/artifacts and provider cache state. Clearing local caches does
  not establish equivalent provider-cache conditions.
- Child-process RSS/CPU, allocation bytes and whole-session resource consumption.
  Unavailable attribution is unknown, not zero.

Record immutable revisions, executable SHA-256 values, harness/fixture identities,
permissions, client versions and all retries. Use AB/BA ordering and multiple
repetitions with executable correctness oracles. Measure phase durations,
tool/repetition counts, workers/delegations, resource usage and available quota or
cost observations without storing prompts, private responses or secrets in reports.
Predeclare at least 10% aggregate input reduction and 5% elapsed-time reduction
with equivalent quality and no security regression; investigate scenario-level
regressions and variance. A successful single inference is a preflight, not a gate.

## Migration sequence, rollback and acceptance

Each delivery remains independently reviewable. A functional release may disclose
an unproven economic gate; it still requires passing quality, security, platform
and compatibility gates. The sequence below does not mark Plane items complete.

| Delivery and Plane items | Dependencies and bounded change | Acceptance and principal risk | Rollback |
|---|---|---|---|
| A: economic baseline, IVOAI-198/138 | Extend the existing reproducible benchmark and typed observations before optimization. Complete the documented 16 scenarios and representative project/language workloads. | Equivalent conditions, independent executable correctness, retained failures, actual provider usage versus estimates, resource/phase/worker counts. Prevent harness differences or skipped providers from becoming claimed savings. | Revert harness/instrumentation changes; retain immutable historical reports. |
| B: Evidence Gateway, IVOAI-199/148/192 | Compose existing retrieval, WorkingContext and ArtifactStore behind an experimental opt-in boundary. Add common scope/provenance, bounded selection, exact recovery and safe deduplication; leave scheduling in the existing engine. | Cross-scope and revoked-grant rejection, invalidation on source/runtime/policy changes, TTL/integrity handling, concurrent reuse, explicit incomplete evidence and no cached mutation replay. | Disable the experimental path and use existing retrieval; discard only identified rebuildable cache entries. No durable-memory cutover. |
| F1: adaptive intake/orchestration, IVOAI-193 | Use measured baseline and evidence availability. Accept actionable natural language without mandatory acceptance headings; route simple work through primary without unnecessary DAG/workers. | Preserve destructive/privileged/executor-change decisions; prove primary-only and complex dispatch paths, bounded cancellation and no authorization bypass. | Restore prior intake/dispatch policy through a reversible config/default change or code revert. |
| C/D: selective tool compression, IVOAI-200/201/46 | Audit pinned RTK-OSS and benchmark alternatives individually after B. Preserve originals in ArtifactStore and current Caveman/Headroom/Direct selection. | Exact errors/diffs/tests remain recoverable; no authority-bearing lossy input; measure session totals. Avoid stacked proxies and interpreting shorter logs as equal session savings. | Feature flag off; Direct or current mutually exclusive provider selection. Keep existing Headroom compatibility until evidence supports removal. |
| E: optional code adapter, IVOAI-190 | Audit Codebase Memory MCP and implement the existing provider contract after retrieval selection is measurable. Keep direct/Graft choices. | Real precision, cold/incremental index time, RSS/CPU/token cost, isolation and sandbox checks. No fixed Graft-then-Codebase chain or unnecessary indexing. | Disable adapter; retain Graft/direct and remove only its managed rebuildable index. |
| F2: native clients, IVOAI-194/195/196 | Extract shared turn contracts, then integrate only officially supported client capabilities against the same admission/scheduler. | Actual sessions, streaming, tools, models, quota and resume where supported. Certify Claude/Antigravity limits honestly; preserve auth and personal configuration byte-for-byte. | Select the prior supported adapter/version with continuity compatibility checks; explicit unsupported/degraded states where no equivalent exists. |
| OpenCode retirement, IVOAI-191 and client work | Finish shared-contract extraction, native-client acceptance and administrative migration first. | No operational dependency in supported paths; old commands explain migration; no user-owned data deletion. | Coexistence release retains prior managed adapter until native gates and observation window pass. |
| G: Context and memory, IVOAI-23/24/25/26/175 | Diagnose actual server connector/ingestion path independently of read-only agent MCP. Compare optional providers without replacing ai-memory. | Ingest allowed source, verify source/document identity and retrieval, record partial failure/retry. Optional provider needs license and isolation acceptance; no automatic OpenViking cutover. | Keep existing backend; rebuild index from authoritative corpus. Any durable-store migration requires its own explicit quiesced transactional participant. |
| H: administrative API/panel, IVOAI-189 | Expose current administrative services through a scoped backend API after core contracts stabilize; build UI on that API. | CLI/TUI capability inventory and parity tests, authentication, authorization, isolation, audit, secret-free views and mutation protections. No conversational scheduler or memory copy in the UI. | Disable panel/API routes and retain CLI/TUI; preserve backend state and existing credentials. |

Update and rollback must use the existing
[production compatibility contract](production-compatibility.md). Register new
persistent participants or keep experimental data as explicitly rebuildable caches.
Current update snapshots do not blindly include live Qdrant, memory, enrollment or
Web OAuth stores. Do not claim database rollback from a config/binary upgrade test.
Preserve unknown TOML fields, component ownership and official client auth stores.

For each increment: run affected unit/integration/security-boundary tests, race and
compatibility checks appropriate to the change; record exact-head CI and real
provider limitations; update Plane evidence and durable decisions; then commit/PR.
Never count an opt-in skipped integration as a passing real test. Use the existing
amd64/ARM64, non-AVX, install and upgrade/rollback checks before a release claiming
those platforms. No production migration is authorized by this document.

## Documentation and security reconciliation

The code audit is not a repository-wide vulnerability certification. It identifies
design obligations and documentation drift that must be reconciled while changing
the affected paths:

- [architecture.md](architecture.md) labels itself a v0.5.0 baseline while mixing
  later capabilities. Older limitation/compatibility text says native orchestration
  is future work, although `NativeOrchestrator` is implemented.
- [SECURITY.md](https://github.com/ivo-lopes/ivoai/blob/v0.12.1/SECURITY.md) describes automatic workers uniformly as advisory
  readers. The actual worker adapter also supports controlled writers using
  Codex `workspace-write` or restricted Claude tools in owned worktrees. Describe
  their host-approved write sets, isolation, validation and integration boundaries
  explicitly; do not remove those checks to match the older prose.
- The remote gateway currently exposes knowledge MCP, enrollment/OAuth and a small
  status/doctor/connector-read API. Administrative UI parity needs new scoped
  backend operations, not reuse of a broad agent token or shell endpoint.
- Checksums from the release channel detect corruption but are not independent
  publisher signatures. Preserve the existing authenticity boundary and do not
  describe it as signed provenance without implementing and validating that work.

The next implementation step is the reproducible economic baseline and additive
experimental Evidence Gateway. Broader migration completion remains contingent on
the real client, isolation, compatibility and operational evidence above.
