# Releases

## v0.15.0 — local candidate, not published

**Publication is conditional on exact final-head CI and the existing Release/distribution
gates; this candidate is not published.** A new restricted qualification on
`77d5fd6c` passed both named official-client/Core protocol tests and both CLI
subcases under race detection, with zero failures/skips. It used the reviewed
0.162.1 client/companion, fresh fixture homes and a local fake provider without
subscription authentication or real-model calls. The checks cover managed MCP
scope/receipt/replay behavior and a separate actual Core bootstrap; they do not
certify public TUI lifecycle, system/cloud isolation or general client/model
parity. See the [final-source qualification](https://github.com/ivo-lopes/ivoai/blob/release/v0.15.0-candidate/release-notes/v0.15.0-final-runtime-qualification.md).
Consult [GitHub releases](https://github.com/ivo-lopes/ivoai/releases) and the
[workflow status](https://github.com/ivo-lopes/ivoai/actions) for subsequent
publication/gate status; this record does not authorize a tag.

The following observations remain historical and bound to their own sources. A supported
`55ec7ab9` real turn passed its math oracle; its intentional SIGINT cleanup
persisted failed/fallback1, which is not a measured frontend process exit.
A separate public startup with only `/quit` + Enter completed naturally in
2.147s: Core Wait0, Session/frontend completed0 and no owned SID residue, with
zero question/attempt/worker and no forced signal. This qualifies normal idle
startup/quit for those exact inputs. A real turn followed by normal quit in one
session was not run. The original `0bab9b34` failure and UNKNOWN cause remain
preserved; none of these observations joins the 126 comparative turns.
See the [normal-exit report](https://github.com/ivo-lopes/ivoai/blob/release/v0.15.0-candidate/release-notes/native-normal-quit-validation.md).
No general TUI/client/model parity or economic result follows.

A separate scoped offline Core Code Mode proof now passed on `55ec7ab9`: one
actual Core tool call, matching non-error result, same read-only receipt,
closed final receipt, NoReplay and duplicate409. It used a fake provider and
proves no real-model, account, TUI or economic parity. Core child Wait0 was not
observed; lifecycle remains UNKNOWN. Earlier FAIL/NOT_MET receipts are unchanged.

This candidate composes actual main `bcdb3466` (PR60/62) and PR61 head
`83df882b`. Its Go/module bytes match that reviewed PR61 composition; no benchmark
PR54/56 or subscription-guard pilot is imported. PR61 merged as main `ccc48a05` after [CI40805624155](https://github.com/ivo-lopes/ivoai/actions/runs/40805624155)
passed16/16 at attempt1 on `83df882b`. That result retains its source identity;
this candidate still needs its own final-head CI and distribution gates.

Managed MCP inventory and execution now share captured cwd, environment, project
policy and explicit config/profile settings. This snapshot does not freeze config
files or cloud policy. Managed `-C`/`--cd` overrides and `--ignore-user-config` are
refused with a migration diagnostic: start IVOAI after shell `cd` and omit those
native flags. Existing sessions retain their recorded workspace; personal files
stay unchanged. Inventory is not skipped. See [setup](setup.md).

PR60 corrects a test process-state observer/PID publication; PR62 separates panic
flight cleanup from a storage deadline fixture. Production cleanup, TTL and
security thresholds are unchanged. Old PR57/56 CI failures, the prior Gateway
composition FAIL and causal/mutant controls remain historical evidence.

Native paths exclude OpenCode discovery/fallback and default client setup omits
it; explicit `ivoai setup --mode client --legacy-opencode`, deprecated commands
and retained histories/checkpoints remain. G0 code prefetch and protected Core/MCP
recovery remain experimental/default-OFF with a separate creation opt-in; G1
Memory stays unwired. Adaptive remains OFF: first eligible literal envelope,
no prompt classifier, existing Core DAG/approvals. PR55 is offline preflight,
not concurrent client execution; PR54/56 are excluded.

Global economics remains INCOMPLETE. General model/TUI/Claude/Antigravity/admin
parity is unproven, Context remains BLOCKED/IVOAI-175, and LOW docs deepmerge stays
needs_review. See the [draft notes](https://github.com/ivo-lopes/ivoai/blob/release/v0.15.0-candidate/release-notes/v0.15.0.md) and [source-bound report](https://github.com/ivo-lopes/ivoai/blob/release/v0.15.0-candidate/release-notes/v0.15.0-composition60-62.md)
for exact scopes, adverse evidence and migration without deleting or replaying data.

## v0.14.0

Published [v0.14.0](https://github.com/ivo-lopes/ivoai/releases/tag/v0.14.0) on
2026-10-28 at 19:10:11 UTC from `6d99e320`. All 12 public assets were downloaded
and matched GitHub sizes/digests; all 11 payload checksums passed. Core module,
VCS/dependency/component metadata and the skill matched the reviewed source.
The [post-publication report](https://github.com/ivo-lopes/ivoai/blob/main/release-notes/v0.14.0-publication-verification.md)
is a later documentation commit, not content added to the published tag/assets.

Adds the [local read-only administrative console](admin-console.md), managed
Codex native-worker controls, strict invocation usage reports and compression
policy corrections. Readonly Claude workers now expose only `Read,Glob,Grep`
and deny native `Agent`/`Task`; broader research uses approved Core/MCP
capabilities, with the write-worker contract unchanged. This is a delegation
boundary rather than native TUI or account parity. The private legacy frontend `.3` avoids runtime plugin SDK
installation and requires an explicit reviewed update from older builds.
Optional VERSION, RTK-OSS and Codebase readers remain narrow
pilots; the diagnostic native evidence runner still refuses production.

PRs [41](https://github.com/ivo-lopes/ivoai/pull/41),
[42](https://github.com/ivo-lopes/ivoai/pull/42),
[43](https://github.com/ivo-lopes/ivoai/pull/43) and
[44](https://github.com/ivo-lopes/ivoai/pull/44) each passed 15/15 checks.
PR [48](https://github.com/ivo-lopes/ivoai/pull/48) passed 15/15 at its reviewed
source ([run](https://github.com/ivo-lopes/ivoai/actions/runs/40729664838)). The
released main then passed [16/16 jobs](https://github.com/ivo-lopes/ivoai/actions/runs/40746711281),
and all five [Release jobs](https://github.com/ivo-lopes/ivoai/actions/runs/40746774761)
passed, including the official five-case Claude protocol proof and exact Core
version/help under private HOME/XDG on native ARM and amd64 Nehalem before
archiving. A separate local downloaded-Core smoke passed AMD64 version/help;
local ARM execution was not performed. No provider/model calls, local
installation or production deployment occurred in that post-download check.

The new seven-case comparison retained 42 correct real attempts: input +2.85%,
elapsed −1.44%; the 10%/5% targets failed. All 72 prior attempts remain preserved.
Another [12 edit turns](https://github.com/ivo-lopes/ivoai/blob/99fe07a93e242d2163116bf6dd8d265c3c8f9d8c/release-notes/benchmark-matrix16-edit-live-validation.md)
passed the declared oracles but missed the same targets: input −0.01257%, phase
time −2.28091%, process time −2.46501%. Both arms used direct inspection, with no
prefetch/Graft/Gateway. The 126 tally covers six backend-first cohorts only (18×4+42+12), not all
repository attempts; PR30 prefetch/schema pilots and earlier MCP research remain
separate. No grand total or heterogeneous causal saving is computed. The new edit harness is outside this release.
PRs [45](https://github.com/ivo-lopes/ivoai/pull/45),
[46](https://github.com/ivo-lopes/ivoai/pull/46),
[47](https://github.com/ivo-lopes/ivoai/pull/47),
[50](https://github.com/ivo-lopes/ivoai/pull/50) and
[51](https://github.com/ivo-lopes/ivoai/pull/51) remain excluded.
The global matrix is INCOMPLETE. These measurements predate the later changes
and do not establish their economic effect. OpenCode removal, general adaptive
execution, Claude equivalence, Antigravity and Context ingestion remain pending.
See the [tagged bilingual notes](https://github.com/ivo-lopes/ivoai/blob/v0.14.0/release-notes/v0.14.0.md)
for scope, compatibility and rollback limits. Their pre-publication candidate
language is preserved; the separate report above records actual publication.

The documentation follow-up audit passed the unchanged high-severity threshold
with one residual low advisory in transitive deepmerge 4.3.1. Scoped review found
a Node/SVGR build dependency, not a demonstrated Go HTTP/browser exploit path;
no zero-vulnerability or universal-safety claim follows. Details and the retained
low-threshold exit 1 are in the post-publication report above.

## v0.13.0

Incremental backend-first delivery: fail-closed benchmark evidence, an optional
Evidence Gateway disabled by default, informal-prompt intake, provider-neutral
executor-turn contracts and isolated dependency-profile updates for managed Ruflo.
OpenCode remains operational; primary execution still follows the existing DAG
completion policy. Claude authentication was not confirmed; Antigravity runtime
integration and Context ingestion remain unverified/blocked.

**Economic acceptance failed:** the final three-scenario comparison reduced input
by 5.43% and elapsed time by 0.12%, below the 10%/5% targets. All four datasets
retain 72 real attempts; the global 16-scenario gate remains INCOMPLETE. No general
savings, complete migration or new client parity is claimed.

[PR #31](https://github.com/ivo-lopes/ivoai/pull/31) and
[PR #32](https://github.com/ivo-lopes/ivoai/pull/32) merged as `c475d2a3` and
`7f5cf051`. Their runtime heads `d6f445e3` and `5949f20d` each passed 15/15
checks ([PR #31 run](https://github.com/ivo-lopes/ivoai/actions/runs/40594966702),
[PR #32 run](https://github.com/ivo-lopes/ivoai/actions/runs/40595049106)).
Publication requires passing metadata/documentation CI and the Release pipeline,
which rebuilds and validates distribution artifacts. This does not deploy to
production or establish artifact publication before that pipeline executes.

See the committed [backend validation](https://github.com/ivo-lopes/ivoai/blob/v0.13.0/release-notes/backend-first-validation.md),
[Core contract validation](https://github.com/ivo-lopes/ivoai/blob/v0.13.0/release-notes/backend-neutral-turn-contracts.md) and
[dependency/rollback report](https://github.com/ivo-lopes/ivoai/blob/v0.13.0/release-notes/dependency-remediation-validation.md). See the [bilingual release notes](https://github.com/ivo-lopes/ivoai/blob/v0.13.0/release-notes/v0.13.0.md).
The later local `VERSION` and RTK pilots are excluded.

The earlier PR #31/#32 pipelines passed their test/race/docs/audit/ShellCheck
checks but failed the Go 1.27.2 vulnerability gate for
[GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809). The increment now
requires Go 1.27.3, including both verified Linux bootstrap archives. The corrected
runtime heads passed the runs linked above; this does not retroactively make the
earlier partial passes a green promotion result.

## v0.12.1

Compatible corrections to exact/API retrieval, sanitized operation telemetry and
OpenCode catalog readiness. Published after explicit maintainer instruction with
**economic acceptance still failed** and the full 16-scenario comparison incomplete.
No aggregate token or latency savings are claimed. See the
[release notes](https://github.com/ivo-lopes/ivoai/blob/v0.12.1/release-notes/v0.12.1.md)
for validation, compatibility and rollback limits.

## v0.12.0

Consolidates the P1/P2 session, hook-recovery, telemetry and automation-profile
changes with managed [native Graft code intelligence](native-graft.md). The
runtime selects bounded direct or structural inspection, preserves institutional
Memory/Context routing, and confines indexing to private snapshots without network
or project-script execution. Claude remains supported by contract tests; real
Claude validation under IVOAI-188 is still deferred. See the
[complete notes](https://github.com/ivo-lopes/ivoai/blob/v0.12.0/release-notes/v0.12.0.md)
for compatibility, measured evidence and recovery limits.

## v0.11.0

Updates ai-memory to 2.6.3, Ponytail to 5.1.0, managed executors and reviewed locks;
fixes known Go/npm vulnerabilities and adds the governed Claude Red catalog.
Real Claude validation remains pending account access. Back up ai-memory 1.x data
with writes stopped before migration; binary rollback does not reverse database
migrations. See versions, compatibility limits, security and recovery in the
[complete notes](https://github.com/ivo-lopes/ivoai/blob/v0.11.0/release-notes/v0.11.0.md).

## v0.9.2

The `v0.9.2` stable patch keeps the OpenCode-first architecture while restoring
conformant federated Memory/Context results for Codex. It also exposes runtime model
and reasoning choices through OpenCode's native picker, persists requested and
effective selection metadata, classifies bridge failures without accepting partial
output, drains federated initialization deterministically, unifies the documentation
canvas, and adds complete English and Brazilian Portuguese locales. Complete notes
live in [`release-notes/v0.9.2.md`](https://github.com/ivo-lopes/ivoai/blob/v0.9.2/release-notes/v0.9.2.md).

## v0.9.0

The `v0.9.0` stable release makes the pinned OpenCode TUI the managed frontend for
AUTO while IVOAI remains the session, executor, policy, quota, and knowledge control
plane. Codex and Claude Code continue to own their subscription authentication; no
provider credential is copied into OpenCode. It also adds resumable frontend ↔
executor session mappings, persistent multi-server scope/health visibility, an IVOAI
theme and panel built with supported OpenCode plugin APIs, and an accessibility
hardening pass for the self-hosted documentation portal. Complete notes live in
[`release-notes/v0.9.0.md`](https://github.com/ivo-lopes/ivoai/blob/v0.9.0/release-notes/v0.9.0.md).

## v0.8.0

The `v0.8.0` stable release adds all-enabled multi-server read federation, hardened
Debian 12/LXC bootstrap, the embedded production documentation portal, and explicit
remote Web MCP conformance. Its complete public notes live in
[`release-notes/v0.8.0.md`](https://github.com/ivo-lopes/ivoai/blob/v0.8.0/release-notes/v0.8.0.md).

Stable releases are immutable tags built by GitHub Actions. Each release contains
Linux amd64/arm64 binaries, the Memory/Context skill archive, and SHA-256 checksums.

The documentation portal exposes `latest` from the installed binary and preserves a
versioned snapshot for each documented release. Release notes state migrations,
fallback behavior, known limitations, and rollback instructions.
