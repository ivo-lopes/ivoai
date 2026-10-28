# Adaptive primary pilot — integration with native provider and evidence controls

This is a local composition of the experimental IVOAI-192/193 pilot with the
reviewed native-provider snapshot, including actual main
`1dcf90400f67eb960a2446ccf3c987ecf977a9d8`. The frozen tested source is merge
`6fdd943ce447374568f76c698322c1b80f7b836e`, parents `6ff8ad58` and `edff3fc1`.
The [new receipt](evidence/adaptive-primary/current-main-integration/validation.json)
and [complete source manifest](evidence/adaptive-primary/current-main-integration/source-manifest.json)
bind this epoch. Earlier reports and failures remain unchanged. This report is
subsequent to the source commit; it is not part of the published v0.14.0 assets.

## Admission and evidence boundaries

`IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY` remains **off by default**. The preserved
source admits the first eligible literal user-message envelope in a fresh managed
Codex session using the reviewed Codex 0.162.1 Linux amd64 pair. There is no
prompt-content classifier and no restriction to a literal `VERSION` prompt.
The primary starts with the existing read-only capability and can request the
existing Core DAG protocol and approvals. The official executable test covers a
narrow synthetic metadata fixture; it does not establish quality on general
requests, real-model behavior or native TUI parity.

The four manual merge resolutions preserve scoped providers and catalogs,
pre-effect compatibility checks, the strict Memory reader and host tool policy,
and receipt/checkpoint writes serialized by the existing session lock. This is
not a rollback transaction across two files. A pure receipt check remains inside
that lock; a callback that rereads the Store must not recurse
there. Current and legacy checkpoint locations retain nonblocking, no-follow
reads. A native executor/checkpoint/FIFO denial now occurs before retiring a
closed prior receipt, preserving its operational pointer and recorded data.

Full primary artifact tools check both the active Core session and the captured
primary receipt before recovery, after recovery, and again before registered
response delivery. They cannot return bytes following callback-time receipt,
host-policy or Core revocation. The private worker projection remains a separate
constructor path with exactly two readers, no full Server Store or primary
identity, and no delegation tools. It accepts only protected IDs through its
exact-owner host recovery callback, which retains grant, scope and generation
checks before and after storage I/O. Its private flag is not model-selectable.
Legacy IDs remain readable by an authorized full server and unavailable in the
worker projection. No new scheduler, store, authorization service or dependency
was introduced.

## Validation

All gates below passed against the unchanged clean source epoch with Go 1.27.3:

- Full Go suite: 59 passing packages, one cached result, three packages without
  tests; `go vet ./...` passed. Normal opt-in live cases remain outside this suite.
- Race checks: App, orchestrator, Evidence Gateway, WorkingContext, knowledge
  router, Session, Core, OpenCode bridge, native Codex and external MCP: ten
  passing packages. These durations are test overhead, not a performance study.
- New primary reader regression: 16 RPC cases, including live positive reads,
  missing/stale/closed identity and revocation during recovery. Existing actual
  MCP worker projection, exact owner, foreign/legacy denial and grant revocation
  checks pass without a full Store. Four native gate cases preserve receipt and
  session/checkpoint bytes, including current and legacy FIFO entries.
- Reviewed Codex/Code Mode pair: exactly three explicit protocol tests passed,
  zero skips, plus both synthetic usage-schema branches. The real Core MCP child
  runs without the admission environment flag, exercises same-turn approved DAG
  escalation and read bootstrap, and denies closed/stale children. The official
  CLI uses loopback metadata responses, one request, zero workers and no DAG;
  duplicate submission returns 409. No subscription authentication or inference.
- Mandatory protected-evidence rollback: one named real old-Core test passed,
  zero skips. Python suite: 73 tests passed. Generated CLI reference passed.
- Linux amd64 `GOAMD64=v1` and ARM64 builds passed with `CGO_ENABLED=0` and clean
  source metadata. New amd64 `version` and `help` ran in a private empty HOME/XDG
  environment. ARM64 was not executed locally. QEMU is unavailable locally:
  actual no-AVX execution and native ARM gates remain hosted checks.

The authored docs, website inputs, embedded site and build/check scripts are
byte-identical to the reviewed incoming snapshot. All 394 documentation inputs
match their two embedded markers. No new local Node build or dependency audit is
claimed for this unchanged documentation; hosted CI will rebuild the final source.

Two isolated Go overlays demonstrate the new regressions' effect. Reusing only
incoming G0 artifact handlers/registrations without the new receipt composition
fails 14 denial cases while both valid reads pass. Moving receipt retirement
before compatibility checks fails all four preservation cases. These are
counterfactual composition controls, not historical findings against G0 before
receipts existed. Initial fixture/overlay construction failures are retained
separately and are not counted as product failures or successful controls.

The current synthetic CLI fixture reports 4,114 initial instruction UTF-8 bytes
and 8,323 legacy/DAG bytes. The original 8,313-byte epoch is preserved. These are
**bytes, not tokens, session savings or costs**. Global economic validation remains
FAIL/INCOMPLETE; no real-provider cohort or benchmark ran in this integration.

## Rollback and remaining gates

Unset the experimental flag for future launches. Finish or cancel admitted turns
before downgrading; keep receipts, request claims, attempts, checkpoints and
protected artifacts. Deleting them to force replay is not a rollback procedure.
`NoReplay` and `NoAutomaticFailover` remain enforced after admission, including
preparation/final-validation failure. Unsupported runtimes retain the existing
DAG path before admission.

No provider authentication, personal settings, credentials or history changed.
Claude/Antigravity activation, TUI parity, global OpenCode removal and Context
insertion are outside this delivery. Context ingestion remains dependent on
IVOAI-175. The published v0.14.0 tag and assets are unchanged. This local source
still requires parent review and fresh final-source hosted gates before merge;
no new release or economic certification is asserted.
