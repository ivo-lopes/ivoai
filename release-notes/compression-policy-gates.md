# Compression policy and availability gates — IVOAI-201 / IVOAI-46

Source base: `813113fb6ed26548d4a00cf0ae5e5f6db9fd79d4`. This is a proposed
increment after published v0.13.0, not a claim that its fixes were in that release.
The audit first consulted current-project Memory; it confirmed the backend-first
limits and unchanged compression decision. Context returned no results. Source
code/runtime contracts and pinned upstream source then supplied the implementation
evidence. No personal configuration, credentials or production state were changed.

## Existing behavior and pins

`internal/config/config.go:23` requests Caveman by default. Explicit providers are
preserved; `internal/app/compression.go` selects one provider, without a Caveman →
Headroom chain. Direct remains the pre-launch fallback. Session-selected
authoritative Memory/Context bypasses either lossy proxy through
`internal/app/knowledge.go:44`. This is not a per-native-tool interception layer.

There are two separate compression paths:

1. `CompressionProvider` prepares a session proxy/wrapper. Its Core request has a
   session fidelity field, not a typed envelope for every native tool result.
   Caveman starts a private loopback proxy with private HOME/config, disables
   tool-schema stripping/cache breakpoint planning and refuses unsupported OpenCode
   subscription routing. A failure after the executor starts does not replay it.
2. WorkingContext stores exact output before deriving a context representation
   (`internal/workingcontext/projector.go`). Its optional `caveman-mcp` adapter
   runs a private ephemeral stdio helper, supports six declared content types and
   retains authoritative recovery through ArtifactStore/ResultRef. Headroom has
   no real `RepresentationCompressor` adapter here; a canary provider label alone
   never creates one.

| Component | Adopted source identity | License / integrity boundary |
| --- | --- | --- |
| Caveman proxy + MCP | Product 3.2.0 / `bin-v2.1.0`, `e20f07e8152a0c0360f58c09e79d30ac94329991` | Root, engine and MCP Apache-2.0; historical MIT and third-party notices retained |
| Headroom wheel | 0.40.0, tag commit `855390d6110e0bbb199d9b97025baac16c2f5cf3` | Own project Apache-2.0; dependencies/model assets separately licensed |

The adopted versions and checksums in `internal/components/components.go:98–112`
match publisher release metadata read in this audit:

| Asset | SHA-256 |
| --- | --- |
| Caveman proxy Linux amd64 | `44436d69705ad9968563c181cd11d396d6ae5eeccd8dbf228f6225d92dfadcb1` |
| Caveman MCP Linux amd64 | `24339e645da66fb3580e3d9410743499845748238f01d0dc8097330fcb0f9bde` |
| Headroom 0.40.0 Linux amd64 wheel | `49b6d8a01c8eceb5b9e9a675720ed5e4cb24e467282642653aac7f2fd8faf47f` |
| Headroom 0.40.0 Linux ARM64 wheel | `140811066bfb7afe888f471be4a587375779af9de8dd76bb9a8126b52b8e930f` |

Primary immutable sources: [Caveman license boundary](https://github.com/JuliusBrussee/caveman/blob/e20f07e8152a0c0360f58c09e79d30ac94329991/LICENSING.md),
[Caveman engine license](https://github.com/JuliusBrussee/caveman/blob/e20f07e8152a0c0360f58c09e79d30ac94329991/engine/LICENSE),
[Caveman MCP binary license](https://github.com/JuliusBrussee/caveman/blob/e20f07e8152a0c0360f58c09e79d30ac94329991/mcp/BINARY_LICENSE.md),
[Headroom license](https://github.com/headroomlabs-ai/headroom/blob/855390d6110e0bbb199d9b97025baac16c2f5cf3/LICENSE),
[Headroom package metadata](https://github.com/headroomlabs-ai/headroom/blob/855390d6110e0bbb199d9b97025baac16c2f5cf3/pyproject.toml).
Caveman's annotated binary tag resolves to the adopted commit. Publisher digests
are provenance evidence, not a local reproducible-build or signature attestation.
Its third-party font/runtime notices still apply. This is not a legal opinion or
complete transitive-license audit. Upstream latest release metadata separately
showed Caveman v4.0.1 and Headroom v0.42.0; neither is adopted or benchmarked here.
The 17d38e5 Headroom research reference belongs to 0.42.0, not the adopted 0.40.0.

## Verified pre-existing gaps

**Headroom wrapper configuration mutation.** The adapter passed `wrap codex --`
or `wrap claude --` with the client environment. In pinned
[wrap.py](https://github.com/headroomlabs-ai/headroom/blob/855390d6110e0bbb199d9b97025baac16c2f5cf3/headroom/cli/wrap.py),
Codex preparation creates its durable home and registers MCP in client config
(6888–6908, invoked at 7018–7032); Claude registers its MCP at 5972–5975.
Both also set up code-memory tooling. The
[Codex registrar](https://github.com/headroomlabs-ai/headroom/blob/855390d6110e0bbb199d9b97025baac16c2f5cf3/headroom/mcp_registry/codex.py)
writes the client configuration. `--no-mcp` exists, but its help explicitly says
compression markers become unactionable (5719–5721 / 7071–7073). `--code-memory
none` / legacy `--no-serena` concerns a separate MCP and does not restore exact
retrieval. No safe configuration-free wrapper preserving official auth and CCR
was demonstrated. This affects the optional v0.13.0 path when selected; Caveman's
different default or Memory bypass does not certify the Headroom path as safe.
No real wrapper or personal configuration was exercised in this audit.

**Missing SPI fidelity guard.** A hermetic Go overlay reproduced Headroom choosing
the wrapper for both `exact_required` and `bypass` requests. App-level Memory
bypass exists, so this is a missing library defense, not proof that production
Memory was compressed. A second overlay proved an explicit unknown payload type
became `worker_output`/`compressible` and invoked the compressor once.

**Quality confused with optimization availability.** The original
`cavemaneval.Metrics.Passed()` tested only four mismatch counters. Offline overlays
observed all 22 fixture cases quality-passing with zero provider calls, and again
with 17 compression calls all failing, 17 degraded projections and zero evidence
mismatches. Safe fallback is desirable; it does not prove provider availability.
The old opt-in pinned-MCP test used that verdict alone. Named Headroom/Caveman
fake providers in hermetic tests remain synthetic evidence, not actual execution.

## Proposed corrective increment

- `HeadroomCompressionProvider.Prepare` refuses managed Codex/Claude wrapping
  before any binary probe or startup, returning deterministic `unavailable_by_policy`.
  Exact/bypass/unsupported requests remain Direct without invoking the wrapper.
  No installation, pin, user preference or global Headroom executable is removed.
  Runtime fallback launches the official executor once; a refused wrapper never
  starts. Status/doctor/component capability separate healthy/installed binaries
  from unavailable managed wrapping. Existing observability uses `policy_denied`
  and Direct, without adding a new persisted event enum.
- Explicit unknown content types and unknown explicit fidelity values cannot
  acquire lossy eligibility. The omitted legacy worker type remains compatible.
  Exact ArtifactStore bytes and association/ownership remain intact.
- `QualityPassed()` exposes the old evidence/fact verdict; `Passed()` remains
  its compatibility alias. `OptimizationVerified()` additionally requires a
  trusted live harness's reviewed runtime SHA-256, complete corpus coverage,
  accounted calls/bypasses, an applied transform and no provider mismatch,
  degradation or compression failures. The pinned MCP gate now requires both.
  The digest is a host-provided validated runtime receipt, not independent
  attestation inside the generic canary. Neither verdict is a session savings gate.
- Generic wrapper PTY, exit-propagation and process-cancellation tests still run
  through an explicitly injected fixture provider. They no longer masquerade as
  certification of the refused Headroom wrapper.

Regression fixtures simulate config mutation in private directories and assert
zero wrapper/probe invocations, unchanged fixture configuration and exactly one
Direct executor for both Codex/Claude. AUTO preserves the requested Headroom
setting, emits policy fallback and executes Direct once. No official client,
subscription, external compression runtime, installer or model was invoked.

Local validation passed the complete offline Go suite, focused race tests and
vet for the seven affected packages, English/Portuguese documentation parity,
both production documentation builds and embedded-site consistency. The first
full suite exposed doctor metadata selecting unrequested inactive Caveman after
Headroom refusal; fallback eligibility was corrected and the final suite passed.
These checks are not native-provider integration tests. Sanitized observations
and the initial failure are retained in
[offline findings](evidence/compression-policy-gates/offline-findings.json).

## Revalidation after the first PR gate

CI40613895059 at head5e852b84 failed the embedded documentation comparison:
13 jobs succeeded, quality failed and two dependent jobs were skipped. The
borrowed dependency tree had produced different CSS module identifiers and
asset hashes. Installing the frozen dependencies in this worktree's own website
and rebuilding produced the CSS filenames observed in CI. A subsequent complete
`scripts/build-docs.sh --check` passed without changing the comparison rules.
Corepack0.36.0, pnpm12.11.2 and Node26.11.1 were used.

Following a temporary-directory reset, Go1.27.3 and Node26.11.1 were restored from
official archives and checked against the previously audited hashes. Six focused
Go packages passed, while the App suite initially timed out in
`TestAutoMidSessionFailoverPreservesWorkingTreeAndHandoff` during concurrent
validation. That failure is retained; the unchanged case passed alone in1.28s,
then the full App package with `-p 1` and focused vet passed. No timeout or test
assertion was relaxed. This does not prove that concurrent-load flakiness has
been eliminated.

The pinned Caveman MCP binary was then downloaded into private scratch, verified
against SHA24339e645da66fb3580e3d9410743499845748238f01d0dc8097330fcb0f9bde,
and exercised with the existing opt-in canary under a fresh network/PID/mount
namespace. Only read-only system libraries and the test/helper binaries were
mounted; no host home, repository, credentials or personal configuration was
accessible. All22 scenarios passed quality and the stricter availability gate:
17 calls, one applied transform,16 passthroughs and five bypasses, with zero
degradation or mismatches. Harness wall time was2.37s. CPU/RSS collected through
the wrapper are not proven runtime aggregates and remain unknown.

The983089→28131 context-byte figures include the existing8KiB projection budget,
summaries and raw-reference bypass. They are neither a Caveman compression ratio
nor provider-token/session savings. This is one run without a matched Direct
arm or AB/BA repetitions. See the immutable input/log digests and limits in
[pinned canary evidence](evidence/compression-policy-gates/pinned-caveman-canary.json).
The prior offline-only report remains unchanged and describes its original scope.

## Recovery, cache and sensitivity limits

The pinned [Caveman engine](https://github.com/JuliusBrussee/caveman/blob/e20f07e8152a0c0360f58c09e79d30ac94329991/engine/engine.go)
selects structural compressors by content type, keeps originals when parsing or
size reduction fails and publishes lossy output only after CCR persistence.
`ExternalRecovery` is allowed only when the embedding host has already stored raw
bytes; it is not exposed by the current MCP adapter. Token figures are inferred.
Its [CCR contract](https://github.com/JuliusBrussee/caveman/blob/e20f07e8152a0c0360f58c09e79d30ac94329991/docs/technical/context-recovery.md)
states that handles are identifiers, not authorization tokens; default native
storage is SQLite, not encryption or cross-organization access control.

IVOAI's MCP helper forces an in-memory ephemeral store and closes after each
call. An auxiliary returned `ccr_` handle does not survive that process; exact
recovery is through the already-persisted ArtifactStore reference. The existing
handle-presence telemetry is not evidence that a CCR retrieval succeeded. This
patch does not add a second recovery store or client-native recovery tool.

Pinned [Headroom CCR documentation](https://github.com/headroomlabs-ai/headroom/blob/855390d6110e0bbb199d9b97025baac16c2f5cf3/docs/content/docs/ccr.mdx)
describes SQLite/memory storage, capacity eviction and a default 1,800-second TTL.
Its [store](https://github.com/headroomlabs-ai/headroom/blob/855390d6110e0bbb199d9b97025baac16c2f5cf3/headroom/cache/compression_store.py)
miss message recommends re-reading a file or re-running a command. This is unsafe
advice for mutating operations, **not proof that IVOAI automatically replays them**.
A future adapter must recover the authorized exact snapshot or report a gap;
it must never reexecute an original operation from cache. The low-level
[universal compressor](https://github.com/headroomlabs-ai/headroom/blob/855390d6110e0bbb199d9b97025baac16c2f5cf3/headroom/compression/universal.py)
can return compressed text while CCR store creation returns None, so a library
adapter must enforce the host's independent exact-reference contract.

WorkingContext's private Restricted raw artifacts are not universal secret
detection. Bounded summary redaction does not sanitize every raw artifact. A new
adapter still needs sensitivity admission, ownership/permission/generation
checks and original-byte recovery independent from provider caches. The current
MCP's small environment/private HOME is not kernel network/filesystem containment.
Headroom ML/tokenizer extras may need model assets; do not download them or enable
another memory/telemetry backend as an accidental benchmark dependency.

## Next justified measurement slice

Extend the existing `cavemaneval` harness with per-scenario AB/BA repetitions and
separate **quality, availability and output-only efficiency** verdicts. Reuse the
same raw snapshots and budgets; preserve all failures/passthroughs, assert exact
ArtifactStore recovery, and report process CPU/RSS/startup/cleanup separately.
Use frozen runtime hashes and independent oracles for tail errors, reordered
rows, JSON/Unicode, paths, diffs, repeated logs and high-entropy outputs.
Direct and already-reviewed Caveman MCP can be measured when the exact pinned
asset and containment are available. Keep Headroom BLOCKED until a small local
representation profile avoids personal configuration, downloads and redundant
stores while preserving the host raw reference. Compare each provider separately,
without chaining proxies. Tokens estimated from bytes or local tokenizers are
not whole-turn provider input/cache/output or monetary savings. No default or
Headroom-removal decision follows from this patch or a smaller log.

Rollback is an ordinary code revert, but would restore the identified unsafe
optional wrapper path. Prefer retaining Direct until a separately reviewed safe
Headroom integration is available. This report does not claim a release,
production fix, formal security audit or completed IVOAI-201/46.
