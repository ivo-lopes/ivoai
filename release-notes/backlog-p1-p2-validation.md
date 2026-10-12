# Backlog P1/P2 — candidate validation, 2026-10-12

This is a review candidate based on v0.11.0, not a production deployment or a new
release announcement. Final validation evidence is recorded below.

## Initial reconciliation

- Clean `main` at `7e322d3fb8b28096053ffa60539d69bdb9420689`; no local changes
  or unpushed commits. Latest published release: v0.11.0.
- Work branch: `feat/backlog-p1-p2`.
- Draft PR #25 (`fix/v0.10.4-hook-preflight`, `980ffdb4`) used a v0.10.3 base.
  Its executable/shebang checks were useful requirements, but broad
  `ReconfigureMemory` was not an acceptable ownership boundary for repair.
  This candidate replaces that approach with exact registration/asset receipts.
- Memory research returned historical continuity context; Context search returned
  no relevant documents. Neither was treated as current implementation evidence.
- The user confirmed that **Pessoal - Ivo** is now the canonical Plane workspace.
  The available connector still resolves `IVOAI-172` to `workspace__slug=mindsite`,
  project `c8b183f7-f52c-4562-a0f6-9f2b3a560493`. Its project/search schemas expose
  no `workspace_slug`. Plugin discovery returned no alternative Plane connector.
  No tasks or workspace settings were modified in the old workspace. This is a
  routing/access limitation; the old descriptions are historical acceptance input.

## Implementation and remaining acceptance

| Task | Changes in this candidate | Acceptance boundary |
| --- | --- | --- |
| IVOAI-172 | Receipt-owned hooks, isolated rendering, preflight, failure latch, explicit transactional repair and TUI diagnostics | Unreceipted legacy hooks require review; external legacy credential-revocation evidence remains unknown |
| IVOAI-138 | Versioned correlated events, monotonic replay, bounded authenticated SSE, inotify, reconnect/watchdog, unavailable/stale states | Ring is bounded to 128 events; no permanent transcript or exhaustive audit archive |
| IVOAI-132 | OpenCode consumes operational events; actual task dependencies/duration, approval and lifecycle visibility; cached quota reads | Native-binary and real-provider evidence distinguished below |
| IVOAI-131 | Existing native model/effort, diffs, keyboard and navigation preserved; operational feedback completed | Parity matrix in `docs/ux-audit.md`; no invented provider-only quota/context fields |
| IVOAI-133 | Seven profiles with preview, effective/custom values, bounded application and restoration | Profiles preserve provider, model preferences, policy and server/auth settings; affect future sessions |
| IVOAI-135 | Public entrypoints inventoried, CLI/help and TUI coverage checks, PR acceptance template and CI event/harness checks | Raw bridge transport and backup paths remain administrative surfaces with documented rationale |
| IVOAI-171 | Stable configured-source scope, exact resume/picker matching, account-verified explicit adoption and private reversible backup | Unmanaged external history is not imported without verified provenance; historical first-run reset cause remains UNKNOWN |
| Homologation v0.11.0 / IVOAI-136 | Proportional install/update/integrity/rollback and managed-integration checks | No production deployment or ai-memory 1.x database migration was performed |

**IVOAI-188 is only a deferred real-Claude validation item.** Its v0.11.0
implementation is retained. Claude subscription access is unavailable; no Claude
login, paid fallback or API key was requested. It must remain open.

## Architecture and file map

- `internal/memory/hooks*.go`, `internal/components/components.go`,
  `internal/doctor`, `internal/app/hooks.go`: managed hook ownership and repair.
  App/session/AUTO launch environments carry a session-local failure latch.
- `internal/observability`, `internal/session/events.go`, `internal/orchestrator`,
  `internal/opencodebridge/events.go` and `assets/events.mjs`,
  `internal/app/operational_events.go`: shared operational state and projection.
- `internal/config/automation_profiles.go`, `internal/cli/automation_profiles.go`:
  preset allowlist, atomic rollback receipt and shared CLI/TUI service.
- `internal/app/session_continuity_*.go`, `conversation_binding.go`,
  `conversation_resume.go`, `auto_turn.go`, CLI conversation actions:
  identity/scope checks and explicit reversible metadata adoption.
- `scripts/smoke-native-auto.py`: opt-in real terminal lifecycle proof;
  `scripts/test-smoke-native-auto.py`: deterministic harness regressions.
- Existing operational, continuity, frontend, development, CLI and UX documents
  updated in English and pt-BR, with regenerated embedded documentation.

## Safety and compatibility

No personal agent installation, authentication, server profile, production
service or existing session was migrated or removed. Test installation uses
private HOME/XDG directories. Real Codex smoke uses its official account without
copying credentials; the personal config is checked by before/after digest.

Hooks preserve unknown/foreign registrations and modified files. Explicit repair
requires a managed component, the same receipt binary path and matching digest.
Missing files and modes can be restored transactionally; changed binaries require
component repair. The exit-127 latch only affects receipt-owned optional hooks.

Profile rollback restores only its allowlist and refuses later custom changes.
Scope adoption is explicit, account verified and backed up before mutation;
restoration validates session identity, digest, safe path and scope format. Native
provider history stays with its owner and follows the explicitly adopted scope.
No executable rollback is represented as database rollback.

OpenCode's distributed artifact and upstream pin are unchanged. The reviewed
Node 26.11.1 executable resolves `libatomic.so.1` on this host; no system package
was installed. Linux amd64/ARM64 support and the non-AVX2 artifact are preserved.

## Review findings

A causal regression proved that prompt-local routing overwrote persistent scope;
the assignment was removed and a turn-followed-by-resume test added. Scope backup
preview now rejects control characters. The event stream has an initial 5-second
deadline and a 45-second heartbeat watchdog. A suspected late-turn HTTP race was
rejected after examining the existing writer serialization; redundant gates were
not added.

## Validation results

Logs are retained outside the repository under the operator's private validation
workspace. Executed checks (provider acceptance remains separate):

| Check | Result / evidence |
| --- | --- |
| `GOMAXPROCS=2 go test -p 1 ./...` | PASS on final implementation 07e42e3c; 50 packages, including the final quota-display and live-fixture regressions |
| `go vet ./...` | PASS |
| `go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...` | PASS: no vulnerabilities found |
| Node presentation/keyboard/events | 37 tests passed after initial-snapshot regression fix |
| Python native smoke harness | Eight deterministic tests passed; consumed-output, initial/resumed composer detection, admission-dialog rejection and recovery checkpoints |
| `scripts/install-smoke.sh` | PASS: clean client/server setup, repetition, ownership and private permissions |
| `scripts/upgrade-matrix.sh` | PASS: v0.5.0 client/server and unversioned v0.6.0 client → candidate → rollback → candidate |
| Real ai-memory 2.6.3 hook rendering | PASS: `TestReviewedMemoryHookRendererIsolated`, 0.857s; no provider authentication |
| Managed OpenCode real binary | HTTP/SSE, permissions, native executor lifecycle, fixture MCP Memory/Context and cancellation passed |
| Managed OpenCode TUI rerun | PASS: original 60/100/160-column panel assertions plus keyboard/resize, 50.315s |
| `shellcheck install.sh scripts/*.sh` | PASS |
| Race detector | Application, CLI, memory, config, session, observability, bridge, orchestrator, components, doctor and workers passed; focused post-review scope/transaction regressions also passed |
| Official Codex 0.162.1 native contract | PASS with real binary and mock provider, including race detector; 10.429s, not provider inference |
| Documentation | English/pt-BR production build, embedded-site comparison and generated CLI reference check passed |
| Linux ARM64 | Local cross-build passed; PR CI cross-builds passed on amd64 and ARM64 |

The first Go run overlapped an installation API edit and failed to compile one
package; the complete stabilized rerun passed. The first install smoke correctly
rejected different hashes from two builds while edits were in progress; the
stabilized rerun passed. Native OpenCode tests caught missing initial status when
optional SSE callbacks were absent; one authenticated bounded snapshot fixed it,
with original assertions retained. These failures were investigated, not exempted.

Initial real Codex runs are not acceptance evidence: the first failed an outdated
composer detector before submitting a prompt; the second passed prompt rejection
but encountered an executor startup failure. Both verified zero mutations to
personal Codex configuration. The underlying startup bug was reproduced with the official binary: plugin MCP
inventory was collected before plugins were disabled, leaving transport-less
server overrides. Inventory now uses the exact execution feature flags; restricted
configuration admission passes without weakening tool isolation. A subsequent
run exposed another conflation: `account/read` proved identity while the usage
endpoint failed. Identity admission now always requires the official account
fingerprint independently of quota telemetry; quota routing/enforcement is
unchanged. The current candidate passed a real Codex plan, native approval,
controlled task and final synthesis. Continuity, frontend switching and approved recovery also passed, as detailed below.


## Post-review corrections

- Hook planning compares the current file with the bytes/mode used for the merge;
  rollback does not overwrite a later foreign edit. Two deterministic RED→GREEN
  tests reproduce the formerly lost changes.
- Profile application/restoration takes the same private advisory lock as managed
  config writes, loads inside that transaction, and checks for external edits
  before promotion. No-receipt restoration leaves an absent config absent.
- Interactive scope adoption/restoration validates the exact preview before any
  account probe or backup. CLI `--confirm` explicitly selects the current scope.
- Missing quota percentages are omitted from event JSON; genuinely observed zero
  and stale observed values remain distinguishable. Cache read failures report an
  error without exposing raw diagnostics; explicit not-exposed data stays distinct
  from missing, stale and unknown observations.
- The race-instrumented DAG fixture previously ignored a successful tool reply
  containing `timed_out:true`. It now asserts completion within 30 seconds;
  concurrency, ownership, result-count and DAG assertions are unchanged. The
  complete orchestrator race suite passed in 109.460 seconds.

## Review delivery and task system

Candidate PR: https://github.com/ivo-lopes/ivoai/pull/28 (review candidate; check the PR for the current CI status). PR #25 has a reconciliation comment linking the
replacement and remains open for review:
https://github.com/ivo-lopes/ivoai/pull/25#issuecomment-6131162788

No canonical Plane task status was changed. Code completion is not reported as
Plane Done. This document supplies a reconciled evidence record until the connector
can address Pessoal - Ivo. No new duplicate task was created.


## Hook diagnosis: evidence versus history

The old `hooksInstalled` accepted any nonempty directory, without validating
registrations, executability, interpreter or binary integrity. The actual ai-memory
2.6.3 renderer registers absolute native `ai-memory` commands, so checking only
extracted scripts was insufficient. Its upstream ownership heuristic was also
broader than the IVOAI-specific receipts now required.

Exit 127 was reproduced using controlled failing fixtures; the shared session
latch limits repeated optional-hook failures to one attempted invocation and
warning. **The cause of a historical exit 127 in the personal installation was
not established.** Missing binaries/interpreters and invalid paths are now
explicitly diagnosed, not retroactively asserted as that historical cause.

The real 2.6.3 renderer passed isolated Codex/Claude configuration and repeated
rendering preserved registration bytes. This is neither authenticated Claude
execution nor proof of delivery to a production memory server. Unreceipted legacy
registrations remain untouched until ownership can be established.


## Artifact identity

The Codex resolver selected official Codex 0.162.1 from the existing standalone
installation. Its executable SHA-256 matches the independently extracted reviewed
release artifact byte-for-byte:
`628e35487d888a28f0427fc37501da529dd3d81fcbad7a2c3421c1e8670d92e1`.
The reviewed archive SHA-256 was
`86f268d81b898f3e144c5ecff2ad2fda2c5802e042d6195b72538fe0fa125057`.
This verifies artifact equality/integrity; it is not claimed as an independent
cryptographic signature. The distributed OpenCode remains 1.19.0-ivoai.1.


## Real Codex acceptance

The real-provider candidate was built from `e8c3ffcc`, without diagnostic
instrumentation. Executable SHA-256:
`ef0a64b414b0d580975156341256002f3a7b2e2f529c99c2819b1ef1febbfba1`.
Later changes only corrected test fixtures and differentiated quota error display;
those changes have their own regression tests.

| Path / scenario | Evidence |
| --- | --- |
| Managed native Codex, orchestrated | Prompt rejection without workers; real plan, native approval, controlled VERSION read and final synthesis: PASS |
| Native picker and CLI resume | Reopened the same conversation and recalled the previous value without file discovery; logical/provider identity preserved: PASS |
| Codex → managed OpenCode, same Codex planner | Idle interruption, frontend switch, real additional turn, remembered provider history and no replay: PASS |
| Approved interrupted execution | Two readonly primary tasks; interrupt after one completes; reconcile checkpoint; require fresh plan approval; complete only pending work and integrate: PASS |
| Final integrity | Five turn attempts (base, resume, switch, interruption, recovery); both recovery tasks completed; same logical/provider/scope identities; fixture VERSION, Git HEAD and clean worktree preserved |
| Personal Codex configuration | Before/after digest unchanged: zero mutations |
| Direct modes | Existing direct-mode admission/resume/isolation contracts passed in Go tests; native-binary contract uses a mock provider. No new claim of real-provider direct-mode frontend portability |
| Claude paths | Contract/fixture coverage only; no Claude authentication or inference attempted |

This was **segmented validation on one private fixture**, not a claim that one
uninterrupted invocation of the harness passed. The initial run passed the first
two real turns but stopped on an outdated composer detector (`/model` is absent
on resumed Codex). The continuation passed switching but the recovery prompt was
rejected before dispatch. Both harness defects are corrected and covered by
regression tests; a Go test validates the actual Python recovery prompt against
Prompt Gate. Continuation drivers reused the same verified fixture and committed
harness blocks, without editing metadata or fabricating checkpoints.

Sanitized logs: `live-final-continuity.log`, `live-final-remaining.log`, and
`live-final-recovery.log` in the private validation directory. The final log records
`APPROVED_READONLY_INTERRUPTION=PASS`, `FRESH_RECOVERY_APPROVAL=PASS`,
`COMPLETED_TASKS_PRESERVED=PASS`, `RECOVERY_INTEGRATED=PASS`,
`RECOVERY_IDENTITY=PASS`, and `PERSONAL_CODEX_CONFIG_MUTATIONS=0`.

The complete opt-in reproduction uses `TestLiveNativeAUTOArtifact` with
`IVOAI_NATIVE_SMOKE_CONTINUITY=1`, `IVOAI_NATIVE_SMOKE_SWITCH=1`, and
`IVOAI_NATIVE_SMOKE_RECOVERY=1`, reviewed Codex/OpenCode artifact paths, a freshly
built candidate, and an empty private `IVOAI_NATIVE_SMOKE_ROOT`.

OpenCode executable SHA-256:
`e5f85adc8baddf889884b5d6ec6c5b7054a0c3d7164080c3ad016c4f309f291f`.

## Residual boundaries

- Canonical Plane updates require a connector routed to Pessoal - Ivo. Historical
  task descriptions were reconciled with Git, not treated as current task status.
- IVOAI-188 remains deferred exclusively for real Claude validation.
- No ai-memory 1.x database migration, production service operation, deployment or
  new release was performed. Tested executable/config rollback does not establish
  database rollback compatibility.
- Unreceipted/modified personal hooks and unmanaged external provider histories
  are preserved and require verified ownership/provenance before adoption.
- The historical personal exit-127 and first-run history-reset causes remain
  unproven; the implemented fixes address reproducible defects.
- Final remote CI status is visible in PR #28; local evidence above must not be
  read as a claim that an unfinished remote job passed.


## Implementation commits

```text
fef4f942 fix(memory): repair only receipt-owned hooks transactionally
06d03d78 feat(config): add bounded automation profiles and safe rollback
f09f1cd1 feat(telemetry): stream bounded operational events to managed OpenCode
008bebce feat(app): integrate governed profiles and scope-safe session continuity
139adbda fix(opencode): retain initial status when event streaming is unavailable
fd6e29bd fix(telemetry): omit quota percentages without observations
2686298a docs: document backlog capability contracts and acceptance evidence
edee3d91 test(orchestrator): require batch completion before checking artifacts
ce9603e1 fix(config): preserve concurrent edits during profile transactions
f068a214 fix(memory): preserve foreign hook edits during commit and rollback
0245ff1f fix(codex): align MCP inventory with execution feature restrictions
ff640bf3 test(sessions): exercise managed Codex continuity and approved recovery
b6f5b0df fix(sessions): bind scope confirmation to the displayed preview
e8c3ffcc fix(sessions): verify identity independently of quota telemetry
ac45c3af test(codex): recognize resumed composer without startup hints
c176ebae test(recovery): validate the live fixture against prompt intake
07e42e3c fix(telemetry): distinguish read failures from absent quota evidence
```
