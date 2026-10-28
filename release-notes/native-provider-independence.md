# Native client provider independence

Partial **IVOAI-195** and **IVOAI-192**. Claude frontend limits below remain
**IVOAI-194** work; Antigravity IVOAI-196 is outside this delivery. This increment
follows main `2bf9f94358aa54c22e0cc8d839e3a108774811c6`, including the managed
Codex worker-handler controls and existing administrative console. The admin
surface is retained. This increment is not included in the immutable v0.13.0 tag.

## Runtime behavior

`ivoai codex` already launches the official Codex TUI with its official App
Server and a process-local IVOAI Responses facade. The facade forwards admitted
turns into the shared Core and presents its final answer, approvals and progress.
It does not use the patched OpenCode renderer. The shared Go package name
`opencodebridge` is retained: it contains existing turn transport, mappings and
model catalog contracts, with provider-neutral executor aliases in Core.
Renaming that package would not remove an executable dependency.

The operational coupling was optional OpenCode discovery, provider registration
and fallback. An installed managed OpenCode could start `opencode serve` during
native Codex startup and worker capability discovery even when the user selected
the Codex frontend. Native Codex sessions and workers of native Codex/Claude
managed sessions now admit only Codex and Claude providers:

- No `NativeOpenCode` discovery/worker factory or native OpenCode `RoutedRunner`
  is constructed for these paths. Explicit legacy OpenCode sessions retain them.
- `quota.Manager.RestrictedTo` clones the probe map and an immutable private
  scope, checks it **before cached reads**, and refuses dispatch, invalidation
  and quota mutation outside that scope. Nested restriction cannot widen its
  parent. Existing managers and shared quota history are not deleted or edited
  by restriction; nil scope preserves the existing legacy provider contract.
- The native picker filters a copy of the catalog. Worker capabilities,
  schemas, planning, retries and conservation use the same quota scope, including
  when an injected/stale registry contains an eligible OpenCode entry.
- Native OpenCode planner/turn/task requests fail with an explicit deprecation
  message. Codex/Claude route changes still use existing separate approval gates;
  no scheduler, memory store or alternative authorization system is introduced.

The retained session is inspected before provider discovery on native
resume/recovery and coordinator startup. OpenCode primary/current execution,
tasks, workers or typed recovery-plan profiles/intent require explicit legacy
continuation or a new native session with reviewed evidence. Saved tasks,
checkpoints, worktrees and evidence are not rerouted, rewritten or replayed by
this gate. The canonical bounded checkpoint loader is reused for both current
and legacy paths, now with `O_NONBLOCK` in addition to `O_NOFOLLOW`: a FIFO must
be rejected as non-regular before it can block admission. This does not add a
second checkpoint parser or a universal ancestor/owner integrity guarantee.

## Installation and compatibility

Default client setup, including candidate setup called by the updater, uses
`ClientSetupCatalog(false)` and excludes OpenCode. It does not install, repair,
probe, adopt or uninstall the retained component. The full reviewed catalog and
existing component pins remain available to an explicit compatibility choice:

```sh
ivoai setup --mode client --legacy-opencode
```

The flag is refused for server setup. Existing binaries, ownership entries,
configuration and history remain intact when ordinary setup runs again.
`ivoai opencode` and the `ivoai auto` legacy alias remain operational explicit
commands, with deprecation messages directing native sessions to `ivoai codex`.
No personal Codex, Claude or OpenCode configuration or credentials are removed.
No public JSON/TOML schema or dependency pin changes.

Bootstrap inspection found no hidden OpenCode preload: `install.sh` downloads
only the platform Core archive and checksums, then registers Core ownership and
the existing release-owned native capability pack. `update.Checker.Prepare`
also downloads only the Core archive/checksums. No installer or authenticated
provider was executed to reach this source-code conclusion.

This is **not global OpenCode removal**. CI/release jobs still build and publish
the legacy assets. The updater retains existing managed component/pointer
transaction participants for rollback safety, including historical OpenCode.
Inconsistent retained state/ownership can still block an update; the candidate
doctor can still perform an optional OpenCode version probe when installed.
Those safeguards were not removed to claim a cleaner migration result.

## Claude limitation

AUTO handoff from the native Codex path to Claude previously selected the
OpenCode renderer implicitly. It now fails before acquiring a new lease,
creating a session, changing history or dispatching an executor, with
`CLAUDE_NATIVE_MANAGED_FRONTEND_UNAVAILABLE`. Initial
`ivoai codex --planner claude` also fails clearly. The existing disabled-core
configuration gate remains authoritative before initial planner admission.

Direct official Claude use and explicit legacy OpenCode paths remain available.
This change does not silently substitute headless execution for a native TUI,
and does not certify native Claude presentation/control parity. A future verified
Codex presentation plus Claude Core execution contract would need separate
model/continuity/approval evidence before enabling that explicit initial mode.

## Validation and adverse results

Hermetic tests assert zero OpenCode lookup, runner execution, discovery process
and provider-probe calls with the component installed and its quota/catalog
cached. They cover native startup, unavailable native subscriptions, explicit
planner refusal, Codex/Claude worker assembly, injected plan/retry capabilities,
low-quota approval, turn admission and read-only migration refusal. Retained
files/ownership and checkpoint bytes are checked for preservation; FIFO and
symlink checkpoints fail closed in both locations. Existing route approval and
explicit legacy tests remain in the applicable suites.

Setup tests use first-party test-mode fixtures: fresh default setup omits
OpenCode; explicit compatibility setup installs its fixture; a later default
setup preserves it. These are not real third-party installation certifications.
No native subscription credential or paid inference is used by this increment.

The first full concurrent Go run retained three failures:

1. CLI expected a deprecation notice only for `auto`; the new contract also
   deprecates explicit `opencode`. Its assertion now covers both legacy commands
   and still verifies shared disabled-core/direct behavior.
2. The old background-writer fixture could create its marker **before** Close.
   A separate causal probe demonstrated `OLD_FIXTURE_MARKER_BEFORE_CLOSE=true`
   after a deliberate delay. This establishes oracle ambiguity, not a runtime
   cleanup defect. The replacement handshake has a live control that writes
   after a trigger, and a closed case that verifies the child identity is no
   longer running before releasing the post-Close trigger. No timeout was
   increased and an escaped/live writer still fails the assertion.
3. The primary-only low-quota alert test timed out in the broad run. Its manager
   has nil provider scope, and no scope restriction was invoked in that case.
   The unchanged alert test passed in one isolated diagnostic run; this does
   not explain the original failure conclusively. The original oracle remains,
   with failure-only callback/cache diagnostics. A separate restricted-scope
   test proves that low quota still waits for approval without workers or an
   OpenCode probe. The broad failure remains recorded as inconclusive.

Early new-test preflights also failed on a wrong fixture snapshot filename,
missing required task-tier metadata, an unused import and an invalid direct
comparison of an ownership structure containing a slice. Those test construction
errors were corrected without relaxing runtime assertions or counting them as
provider attempts. All adverse logs and final source-bound outcomes are listed
in `evidence/native-provider-independence/validation.json`.

## Rollback and remaining gates

Revert this code to restore the old default catalog and implicit provider
registration. Existing files are still present; no data migration or destructive
rollback is required. Such a revert restores the old operational coupling and
must not be presented as the target architecture. An older CLI will not know
the new `--legacy-opencode` flag.

The diagnostic Evidence Runner production guard remains intact. Official TUI
fixture tests exercise the real pinned client and synthetic Core output, not a
real authenticated task, whole-session savings, cross-client parity or ARM/no-AVX
runtime certification. No economy is claimed. Final CI, integration of adjacent
adaptive/admin changes and global migration certification remain separate gates.
