# Native Graft code intelligence

IVOAI owns local code inspection, its authorization and its resource budget. Graft
is a replaceable structural library behind `CodeIntelligenceProvider`; it does not
own the session, choose an executor or become a second knowledge server. The
institutional source order remains **Memory → Context**. Compiler and test results
remain the evidence for executable behavior.

This page describes the implemented provider, sandbox, gateway and build contract.
Application launch integration and the final native-client acceptance matrix are
still being finalized in this candidate. Benchmark results are pending; this page
makes no measured latency, memory or token-saving claim. Real Claude validation
remains pending under IVOAI-188 and is not implied by local fixtures or Codex tests.

## Managed implementation and lifecycle

The reviewed upstream is `trailhq/Graft` version **0.21.1**, commit
`fe30ead39d5e6f0c921018d364da2bdbc9d4b3ad`, under the MIT license. IVOAI's managed
identity is **0.21.1-ivoai.1**. Its source tar SHA-256 is
`ab27f9123eeb59b3987a342a24b7e357ad3412f41c556fde3a76ca9e250d2db1`;
the npm lock SHA-256 is
`c05064ea18ed1b538e27cc788f1d6ea2cae00b2c00c79ef552fc1bb4c54fd5f5`.

The release build installs locked dependencies with lifecycle scripts disabled,
compiles the ten native bindings explicitly with managed Node **26.11.1** headers,
and builds the viewer and grammar queries. The native compatibility patch changes two
`tree-sitter@0.21.1` C++17 flags to C++20 for those headers. Compilation uses a
network-isolated bubblewrap environment; runtime isolation is the Go sandbox
described below. CI selects GCC 12 on native Linux amd64 and arm64 runners. The
amd64 compiler flags exclude AVX/AVX2; QEMU Nehalem exercises the actual Node and
library, including native and WASM grammars.

Packaging refuses native bindings requiring more than **GLIBC 2.35**,
**GLIBCXX 3.4.30** or **CXXABI 1.3.13**. Compiler selection alone is not a passing
compatibility result. The archive contains the compiled library, runtime
dependencies, viewer/query assets, licenses, dependency integrity inventory,
per-file SHA-256 values, ABI evidence and build provenance. Release metadata binds
source, reviewed build inputs and the resulting archive digest to the immutable
component identity. Native architecture, ABI and no-AVX gates must pass in CI;
the local GCC 14 experiment was correctly rejected by the ABI gate.

The intended finished application path requires no separate Graft activation,
upstream CLI installation or manual project initialization. A release carrying the
reviewed metadata participates in normal `ivoai setup`, `ivoai update` and managed
component promotion/rollback. See [Update and rollback](update-rollback.md).
Development builds without that metadata report Graft unavailable instead of
installing an unreviewed substitute. An ambient `graft` executable is not adopted.
The packaged IVOAI `graft-runtime` accepts only `--version`; actual operations use
the private Go-owned adapter. Personal provider configurations and credentials are
not modified or imported.

## Choosing the smallest sufficient inspection

The host capability is `code_inspect`. It supports bounded symbol search, literal
occurrences, file API inspection, callers, repository maps, impact analysis,
freshness checks, explicit builds and private visualization exports. Native
`graft_find_code`, `graft_find_all`, `graft_trace_calls`, `graft_file_api`,
`graft_repo_map` and `graft_check_freshness` aliases remain scoped by the same gateway.
They do not expose an upstream MCP server or allow callers to choose another root,
organization or permissions scope.

Automatic inspection can answer a literal search directly. For API inspection of
an identified small file, it can return the complete file when it fits within half
the requested result budget. Relations and larger structural questions use Graft.
This routing avoids a structural subprocess when exact local evidence is already
sufficient. Cost inputs used for that choice are estimates, not observed savings.
A conservative task classifier can prefetch bounded code evidence; it neither
grants capabilities nor replaces institutional Memory/Context research.

`deep`/LLM enrichment and LSP subprocesses are disabled until there is a safe,
reviewed integration through official clients. The adapter performs graph-only
builds, with no cloud wiring, repository hooks, provider configuration loading or
upstream CLI initialization. Missing structural evidence stays missing; a literal
match does not prove a call relationship or complete impact analysis.

## Scope, snapshots and coverage

The host supplies the canonical worktree root, organization/knowledge scope and
permission identity. All three bind the private cache key; absent trusted scope
is rejected. Repository text, tool arguments and organization-like directory names
cannot grant authority. Different worktrees or permission scopes do not share an
index merely because they contain the same Git revision.

Inventory uses Git's tracked and non-ignored untracked files, with hooks and
fsmonitor disabled and no global/system Git configuration. Nested repositories
remain separate. Only admitted source extensions and `go.mod` enter the inventory;
hidden paths, dependencies, build/output directories, generated hashed/minified
JavaScript bundles, non-regular files and oversized files are excluded. Unsupported
languages, excluded generated sources and large files are outside coverage. An
inventory count is not a claim that every file received structural extraction.

The host reads bounded exact bytes through `os.Root`, checks paths and file types,
and copies those bytes into a private snapshot. Graft receives that snapshot,
never a write grant to the original repository. Source digests, adapter identity
and managed version govern freshness and result reuse. Receipts also validate the
graph sidecars before reusing an index; invalid cache evidence causes rebuilding.
Query results are bounded to 64 cached entries per source generation. Repository
text and cached results remain untrusted data, not instructions or authorization.

## Runtime limits and failure behavior

| Boundary | Enforced limit |
| --- | --- |
| Source inventory | 20,000 files |
| Individual source | 1,000,000 bytes |
| Total source snapshot | 128 MiB |
| Result text | At most 32 KiB; a request can choose a smaller budget |
| V8 old-space heap | 256 MiB |
| Process RSS | 512 MiB threshold, monitored at 20 ms intervals |
| Structural request | 90 seconds, including admitted waiting time |
| Structural processes | One active process per shared cache root |
| Admission queue | Eight total admitted requests, including the active request |

The RSS threshold is monitored cancellation, not a promise of a byte-exact kernel
memory ceiling. Timeout, cancellation, memory excess and queue saturation produce
bounded failures. Missing metrics are not replaced with zero or invented values.

The disposable runtime helper applies Landlock and seccomp before Node starts.
It permits reads of the managed distribution, source snapshot and necessary shared
libraries, and writes only to the private index directory. Network access and new
child processes are denied; Node's required threads remain permitted. The child
has a minimal environment, no personal HOME/configuration grants, no provider
credentials and no inherited connected sockets. Unsupported platforms or missing
kernel protections fail closed; the adapter does not retry without isolation.
The runtime requires Linux amd64/arm64 and Landlock ABI 3 or newer.

The existing authenticated capability gateway owns session admission and tool
grants. Provider unavailability can select permitted direct inspection when it
answers the question, or an explicit unavailable result. It never broadens the
filesystem or authorization scope.

## Fidelity and operational evidence

Code observations use fidelity class **`exact_required`**. They record only
operational facts such as provider/routing reason, duration, result bytes,
truncation, cache hit, index freshness, source/index counts and measured CPU/peak
RSS when available. Cache hits do not inherit CPU/RSS measurements from an earlier
process. Queries, filenames, source text, source hashes, arbitrary errors and
exported artifact contents are excluded from observation events.

Token usage and savings remain unavailable without actual evidence. The adapter
removes upstream theoretical token-saving annotations; result-byte counts are not
token measurements. Console diagnostics preserve unknown/unavailable states.
A private visualization is an inspectable artifact, not proof of complete code
coverage or executable correctness.

The implemented fixtures cover scope identity, bounded snapshots, freshness,
corruption handling, routing, adapter calls, observations and sandbox denial.
Release builds additionally require native grammar/viewer smoke, ABI checks and
the no-AVX gate. Full integration, benchmark acceptance and real-client results
must be recorded by the candidate's final validation report; pending work must not
be inferred as complete from this architecture description.
