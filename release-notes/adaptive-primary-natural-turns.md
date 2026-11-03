# Natural managed Codex turns and owned adaptive continuity

This extends the first-turn pilot with a functional backend-first path. The
production change is `889b74a3`; live-test corrections are `94ad77da`. Delivery is
tracked in [PR #69](https://github.com/ivo-lopes/ivoai/pull/69). The earlier pilot
reports and benchmark results remain historical evidence.

Opt in on the IVOAI owner process:

```sh
IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY=1 ivoai codex
```

The flag remains **off by default**. Admission still requires the exact reviewed,
managed **Codex 0.162.1 Linux amd64 executable and Code Mode companion**, with the
[existing hashes and provenance](experimental-adaptive-primary.md). A matching
version banner alone is insufficient. No installer, personal configuration change,
subscription credential extraction or paid API fallback was introduced.

## What works

Managed intake now calls the existing bounded input validator directly. Natural
requests need no formal acceptance criteria. Empty, oversized, invalid UTF-8 or
unsafe control input still fails before executor selection or preparation. The
legacy `Assess`/`RequirePromptGate` diagnostic shape remains compatible; it is not
an authorization or task-quality classifier. The previous `Assess` already
accepted informal text: this change makes that integrity-only contract explicit
throughout admission rather than claiming a newly invented prompt classifier.

An eligible turn starts with a fresh read-only Core receipt. The Primary can
answer directly, retrieve projected Code Intelligence, or call the existing
bootstrap/plan protocol in the same invocation. Plans, task grants, approvals,
scheduler, worktrees, workers, integration and validation remain Core-owned.
Simple mode grants no write or privileged execution permission.

Consecutive turns in the same newly created managed conversation can preserve
the owned native executor thread. Continuation requires the closed previous
receipt, successful recorded attempts, a matching opaque account reference and
native mapping, unchanged configuration/state/scope, and fresh turn/attempt IDs.
Claims and history remain intact. Per-turn transports close; stale children cannot
adopt the next receipt. Admitted failures remain non-replayable and cannot trigger
automatic executor failover.

The native frontend now shuts down with stdin EOF. Cancellation and timeout are
reported separately. Linux cleanup reserves the leader PID until the last group
signal and retires the custom cancellation signal before `Cmd.Wait` can reap and
join its context watcher. Unresponsive owners or descendants retaining stdout
remain failures, with bounded cleanup and private-home retention.

## Actual official-client validation

The [execution receipts](evidence/adaptive-primary/natural-turns-20261103/real-executions.json)
retain **all 16 execution epochs**, including failed lookups, startup tests and the
initial refusal oracle and the public-CLI fixture setup failure. Each records the original receipt/source-manifest/test-log
hashes, changed-source hashes, immutable test-binary identity and source stability.
`889b74a3` matches the D/cancellation source; `94ad77da` matches the latest B/C and
native TUI/refusal source. Documentation and dependency maintenance come later.

The public command was also tested with a compiled **production IVOAI binary**:
`ivoai codex` created its own Core session, facade, bridge and Core MCP child,
completed two natural turns in the same thread, rendered correct fixture answers,
and finalized its session normally after `/exit`. It changed no repository data
and created no DAG/workers. No test entry point or handler was substituted in this
run. Private Core XDG paths were used; HOME/CODEX_HOME stayed in place. This one
fixture explicitly selected the existing Direct compression provider; it does not
change the product default. Its candidate binary is a private validation build,
not a published release asset.

These runs used the official CLI with its existing authentication in place and
real inference. For the component/DAG tests, the integration helper substitutes only the disposable Core
child's Go `TestMain` entry point; its MCP handler, admission, plan approval,
scheduler, worker execution and integration are production code. The TUI run uses
the actual official presentation client, facade and official backend CLI. No fake
provider response supplies these results.

| Scenario | Observed result | Latest elapsed time |
| --- | --- | --- |
| Public `ivoai codex` | Two actual turns; one Core session; same frontend/native thread; two closed receipts; wrapper/backend exit zero; session completed normally | 38.151 s |
| A — natural question | Correct `Authenticate`/fixture-token answer rendered by the native TUI; no DAG/workers | Included in the two-turn TUI run |
| B — code retrieval | One completed host `code.ask` via builtin Direct; correct file/function; no DAG/workers | 13.581 s |
| C — small fix | One explicit plan approval and one code-read approval; one real Codex worker/worktree; only `math.go` changed; fixed tests fail before and pass after | 72.218 s |
| D — complex change | One explicit plan approval; two real Codex workers/worktrees; only `greeting.go`/`format.go` changed; fixed tests fail before and pass after | 121.579 s |
| E — continuity | Two completed turns, fresh closed receipts, same native thread/frontend, response rendered, `/exit`, frontend/backend exit zero and clean App Server EOF shutdown | 35.279 s for the TUI run |
| F — cancellation | Real PID/stdout observed before caller cancellation; cancelled executor reaped; closed receipt; duplicate HTTP 409; no new attempt, worker, pending permission or file change | 3.776 s |
| F — denied read + cancellation | Actual projected read approval denied; zero completed host reads; cancellation confirmed; duplicate 409; files intact | 8.810 s |

The refusal oracle distinguishes intermediate native `agent_message` text from a
`turn/completed` event. The latest denied-read run records response text present,
**no native completion**, and `executor_cancelled`. Text presence is not success.
Configuration/state/receipt revocation remains covered by adverse host tests;
this is not a claim of real subscription credential revocation testing.

Earlier code lookups did not prove successful host retrieval; one epoch recorded
an unavailable provider response. The adaptive instructions now explain exact
symbol, literal and file lookup through the existing provider, preserving
permitted direct reading and honest structural unavailability. The original unsuccessful executions remain in the receipt set.
The TUI startup tests initially failed before inference; their corrected PTY
recognizes the observed model/composer and standard terminal queries without
accepting trust/auth/update dialogs or changing permissions.

## Quality, economics and remaining limits

[Local validation](evidence/adaptive-primary/natural-turns-20261103/local-validation.json):
59 passing Go packages under `-race`, three without tests; `go vet ./...` passed.
Focused final helper/continuity race tests passed. Python comparison tests passed
105/105, plus 9/9 native AUTO smoke tests. Normal opt-in skips are not counted as
inference. Linux amd64 `GOAMD64=v1` and ARM64 compilation passed; native ARM64 and
no-AVX execution require the existing hosted gates. Fresh final-head CI is required
before merge. The first PR CI failed the docs dependency audit; corrected dependency
pins now pass that existing gate without exclusions or weakened thresholds.
The [dependency receipt](evidence/adaptive-primary/natural-turns-20261103/dependency-gate.json)
records `follow-redirects` 1.16.2 and `joi` 18.3.1, registry/lock integrity and npm
signature checks, and EN/PT-BR build/embedded-site validation. Existing low
`deepmerge` and moderate `minimatch` findings remain; high/critical counts are zero.
No new audit exception was added. `govulncheck` found no Go vulnerabilities.

Reported usage is **official CLI `turn.completed` usage**, not billing verification
or estimates. Latest primary input/output/cached-input counts are respectively:
TUI A `71,712/497/59,904`; E `84,624/517/72,576`; B `33,912/197/30,720`;
C `205,064/1,223/156,032`; D `439,350/1,812/402,048`. The public-command run additionally reports `56,345/444/52,608` and
`71,181/464/67,200` for its two turns. Worker usage is unavailable and
is not added as zero. Incomplete cancelled turns have no claimed completed usage.
These are independent functional runs with different prompts and warm histories,
not AB/BA trials. The earlier C took 260.057 s and remains recorded. No speedup,
aggregate token saving, provider cost, quota saving or Gateway cache hit is inferred.

The Evidence Gateway's existing protected owner/scope/recovery controls remain;
no new prefetch/cache path is introduced here. A comparable Gateway OFF/ON cohort
was not executed for this path. **Global economic gate: INCOMPLETE.**

Cross-process resume, switching to an earlier conversation, prior DAG/checkpoint
histories, other Codex versions/platforms and other native clients remain outside
adaptive certification. Those paths retain their existing guarded contracts. The
rollout condition is restricted to the original fresh managed session created in
this process; retained eligible conversation turns are supported within it.
Dynamic primary MCP catalog expansion after planning is not newly certified.

Unset the flag for future launches to roll back. Finish or cancel admitted turns
before downgrading. Preserve receipts, claims, attempts, mappings, checkpoints and
worker evidence; deleting history to force replay is not rollback.

Plane `pessoal-ivo` currently returns `UNAUTHORIZED` requiring reauthentication;
Memory and Context return service errors. The
[pending task updates](evidence/adaptive-primary/natural-turns-20261103/coordination-pending.json)
cover existing IVOAI-191/192/193/197/198/199. No remote task completion, memory write
or Context synchronization is claimed. This delivery does not complete the broader
backend-first roadmap, global OpenCode removal or other-client parity.
