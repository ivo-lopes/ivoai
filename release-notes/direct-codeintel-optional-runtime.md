# Authorized builtin Direct without optional Graft/Node

This backend availability change is local, based on main26394c57; it is not part
of the immutable v0.15.0 tag. The tested source is
`0939edaca25d6ad6de801c8f0040bce0eab72da6`, tree
`d35bc20e19f266d7b0c2ae501ca157b0e44e44e5`. No economic improvement is asserted.
It addresses the existing IVOAI-190/198/191 scope without adding a provider,
database, orchestrator, proxy, package or operational service.

## Behavior and admission

Previously, App.CodeIntelligenceProvider required a verified managed Graft
installation and Node even when Provider.Inspect could answer with the existing
builtin literal, small-file or Go AST reader. Unversioned builds additionally
refused before that factory when no release-injected Graft catalog entry existed.

The host now returns an explicitly restricted Direct-only provider only after
normal context, repository Identity/Inventory and filesystem-policy checks and
one of these narrow absence proofs:

- The component state is absent/zero and no active Graft supply-chain object
  exists. A build without compiled Graft metadata still runs these same checks.
- Graft has passed the existing managed state/version/path/active provenance
  checks, but Node is genuinely absent. An ancestor symlink, unreadable path,
  non-executable file or final symlink is not treated as proven Node absence.

Malformed state, partial/claimed installations, an active object without matching
state, wrong version/revision/path, unsafe supply-chain paths and tampered payloads
remain errors. This change does not adopt an external installation, execute an
invalid runtime, silently repair state, install a dependency or write personal
configuration. Validated managed Graft retains its existing behavior.

The Direct-only mode is private and has no public setter. The host restriction
method returns a copy and clears executable paths and the structural runner.
Query refuses structural operations before queue/cache/index/result mutation,
even if executable paths or a runner are subsequently forged inside a test.
Inspect skips warm Graft results and marks Graft unavailable to the cost router.
Cost history is still read and can inform estimates; it cannot re-enable an
unavailable structural candidate.
Exact literal matches, bounded small files and unique valid Go declarations keep
their normal snapshot/fidelity/provenance behavior. Ambiguous or malformed
identity, unsupported exact declarations, oversized file skeletons and relational
questions fail when they require unavailable structure; literal matches never
become invented call graphs.

MCP tool names, schemas and Core method signatures remain compatible.
`code_inspect`, `graft_find_code` and `graft_file_api` already route through
Inspect and can use Direct where appropriate. Structural/grouped aliases retain
the existing bounded IsError response. Listing an alias does not assert an
installed Graft runtime. Provider.Probe remains the existing Graft/structural
health report (component.go:11-20), not Direct readiness or full-provider
introspection parity. The audited App factory/MCP/primary/worker paths do not
use Probe as their Direct admission gate; this increment does not change it. Unattempted structural refusals report unavailable with
no invented provider, cache hit, index freshness, CPU or memory measurement.

Session/task/worker admission, exact tool grants, Skill Gate and post-read refresh
are unchanged. Native endpoint projection is freshly host-owned; existing
configured endpoints are not adopted. Source bytes remain untrusted evidence,
not instructions. Exact ArtifactStore ownership and protected recovery contracts
are unchanged. Inventory still invokes the fixed Git enumeration process. Zero Node/Graft
starts or structural execution is not zero total local processes. No additional
feature flag is needed for this existing authorized
builtin read capability; Gateway/protected-creation flags remain independent.

## Cache compatibility and rollback

CodeIntel runtime identities and Gateway query digests now bind an explicit
`execution_mode` of `managed` or `direct_only`. Identical or empty runtime paths
cannot make full-mode evidence accessible as Direct. Tests prewarm a deliberately
forged/degraded managed Gateway entry and prove a Direct miss/source read or a
structural refusal, rather than reuse. A populated CodeIntel structural cache
and cost history favoring Graft cannot undo the Direct-only restriction.

The added identity discriminator invalidates older private structural receipts
semantically. A later permitted managed structural query may rebuild its own
private index through the existing cache logic; Direct-only does not trigger
that rebuild. Rollback is a cohesive code/test revert, without manual cache
removal, workspace edits, artifact deletion, schema migration or history replay.
Rollback may similarly require a later managed query to rebuild its private
index. Existing exact artifacts are not rewritten. No claim of atomic runtime
file/configuration freezing is added.

## Validation bound to source0939

The [receipt and manifest](evidence/direct-codeintel-optional-runtime/validation.json)
archive complete logs, source hashes and the explicit private/offline recipes.
All four final gates observed the same4,072-file freeze, unchanged afterward.

- Affected App and CodeIntel race, count1: exit0,187.400s wall; App174.962s and
  CodeIntel26.774s. Go reported595 passing events (273 top-level) and31 explicit
  opt-in skips. Skips and passing framework parents of skipped children are not
  official-client/Graft integration evidence. All eight new regression tests
  and their subcases passed with zero failure/skip.
- Affected vet: exit0,2.169s.
- CGO0 Linux amd64/v1 build: exit0,4.874s; ARM64 build: exit0,4.274s.
  Static Go build metadata binds source0939, clean VCS state and the requested
  architecture settings. Target execution, ARM execution and non-AVX runtime
  qualification were not run; these are compilation checks.

The existing wrong-revision and corrupted-payload assertion tail remains byte
identical; its setup was extracted into a shared fixture. Regression coverage
includes the actual public factory through primary evidence and Gateway exact
recovery, cross-owner refusal, typed structural denial without cache mutation,
mode-separated forged caches, unavailable Graft despite favorable history,
MCP aliases through an in-process handler without sockets, absent versus unsafe
Node/state, and the retained managed positive.

Adverse epochs are preserved. Overlaying the original main26394 factory on the
initial new public-factory assertion failed as expected in0.027s; it was not a
full original-base suite. The initial focused implementation had24 PASS events
and one failure because the public wrapper still rejected builds without
injected Graft metadata. After correcting that wrapper, its separate WIP epoch
passed25 events. Those results are not relabeled as final0939 proof; final race
also covers the later Gateway-public-factory and ancestor-symlink regressions.

No full repository suite or hosted CI is claimed for0939. No official client,
real provider/model, credentials, production service, actual Graft indexer,
installer or external network was invoked. Tests use disposable repositories,
synthetic fixtures and existing unit transports. No comparative turns are added;
global economics remains INCOMPLETE. Direct is existing MIT-licensed repository
code using the Go standard parser; no dependency/license, threshold, pin, module,
workflow, Core/policy/Skill Gate/ArtifactStore/Evidence engine or website/embed
input changed. Source docs for previously published releases remain historical.

The prior [activation-gate proposal](evidence/direct-codeintel-optional-runtime/prior-readonly-proposal.json)
is retained and deferred: this fix does not wire the benchmark, start a signed
turn, remove unactivated controls or change any of the126 historical rows.
