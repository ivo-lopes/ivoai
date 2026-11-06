# Orchestrated frontends

Managed frontends share one Core admission boundary, session store, permission
policy, orchestration protocol and scheduler. A native client renders the input,
output and approvals; it does not own a separate economic router or memory store.

## Commands

| Command | Admission and execution |
| --- | --- |
| `ivoai codex` | Official native Codex TUI with Core admission |
| `ivoai auto` | Compatibility alias for managed native Codex |
| `ivoai codex --direct` | Official direct client without managed turn orchestration |
| `ivoai claude` | Official direct Claude client |
| `ivoai claude run --prompt ...` | Experimental managed headless Claude host |
| `ivoai antigravity run --prompt ...` | Experimental managed ACP host |
| `ivoai opencode` or `--legacy-opencode` | Explicit removal/migration error; no execution or installation |

Put `--direct` before `--`; arguments after `--` belong to the official client.
New session starts accept supported native executors. Retained OpenCode session
and checkpoint metadata remains readable for inspection and rollback, while
resume/recovery fails with migration guidance rather than rerouting or replaying
saved tasks. No user's OpenCode binary, configuration, credentials or history is
removed. See [executors](executors.md).

## Native Codex terminal

Enter a natural request in the official composer, for example:

```text
Explain how authentication works in this project.
```

Formal acceptance headings are optional. Admission still checks input integrity,
authorization, scope and isolation. The adaptive Primary is experimental and
off by default until controlled certification and rollout. With
`IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY=1`, eligible reviewed sessions can complete
a read-only request without creating a DAG or worker. A
request needing changes or multiple steps escalates through the existing planning
protocol; Core validates the plan, approvals, sandbox and worker permissions.
Choosing a simple strategy never grants writes or privileged execution.

The native model/reasoning picker uses runtime-verified selections. Plan and
material quota-routing decisions remain separate approvals. Composer, navigation,
scrolling and terminal presentation stay native; IVOAI has no pre-composer or
conversational web frontend. The App Server façade admits turns before official
execution and rejects unsupported methods/config overrides. `--direct` is an
explicit separate operation, never a fallback after a claimed managed request.

Reviewed runtime pairs, platforms and rollouts remain explicit. Refer to the
release's actual inference/TUI evidence; a version/help startup or synthetic
provider test does not certify inference. The private per-project Codex home
stores official native history without copying authentication. The native
`/resume` picker and `ivoai session resume` use the existing owned mappings.
Durable request claims, terminal audit and completed-plan history prevent replay
and preserve identity across eligible continuation. Failed, cancelled, foreign
or incomplete histories cannot acquire new authority by eviction from a bounded
metadata projection.

## Evidence and workers

When explicitly enabled with `IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY=1`, the same
Evidence Gateway can wrap admitted native read-only CodeIntel queries. Repeated queries may reuse exact typed results in
one process after fresh receipt checks. Source changes, permission revocation,
wrong owner/worktree or runtime drift deny reuse/recovery. Index/diagnostic actions
are never replayed from this cache. Gateway cache hits and official provider token
cache observations are distinct. No global token/time saving is presumed.

Complex tasks use the same existing Core scheduler and isolated workers. Ruflo's
provider-free coordination profile does not introduce another scheduler. Frontends
cannot issue extra grants, bypass plan approval or start another scheduler.
Memory and Context remain separate retrieval capabilities with existing source
and permission boundaries. External evidence is untrusted data, not execution
instructions. Shared package names and retained wire readers do not restore the
removed OpenCode runtime.

## Client boundaries and diagnostics

`ivoai claude` remains the official direct TUI. Managed Claude headless uses the
off-by-default `IVOAI_EXPERIMENTAL_CLAUDE_HEADLESS=1` flag and the reviewed
Claude Code 2.1.318 runtime on Linux amd64. Its inference certification is pending an
authorized user account; offline tests do not certify it.

The off-by-default `IVOAI_EXPERIMENTAL_ANTIGRAVITY=1` flag enables the reviewed
`agy` 1.5.2 ACP host on Linux amd64. Exact binary identity remains mandatory.
Headless protocols do not provide transparent native TUI control, universal
client parity or cross-process resume. Do not substitute a fake provider or a
paid API for the existing official authentication. A refused initial planner/handoff remains a refusal until
its native presentation contract is separately supported and certified.

Use `ivoai monitor --watch` or the administrative console for sanitized session,
worker, quota, evidence and approval state. The console consumes the same backend
and does not render a second conversation. Cancelling/revoking an admitted turn
never requests implicit replay or automatic paid-provider fallback. A failed
frontend or final validation remains a failure even if a child exits zero.
