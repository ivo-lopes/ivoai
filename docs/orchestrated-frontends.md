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
authorization, scope and isolation. Under the reviewed adaptive feature flag, the
Primary can complete a read-only request without creating a DAG or worker. A
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

When explicitly enabled, the same Evidence Gateway can wrap admitted native
read-only CodeIntel queries. Repeated queries may reuse exact typed results in
one process after fresh receipt checks. Source changes, permission revocation,
wrong owner/worktree or runtime drift deny reuse/recovery. Index/diagnostic actions
are never replayed from this cache. Gateway cache hits and official provider token
cache observations are distinct. No global token/time saving is presumed.

Complex tasks use the same existing scheduler and isolated workers. Frontends
cannot issue extra grants, bypass plan approval or start another scheduler.
Memory and Context remain separate retrieval capabilities with existing source
and permission boundaries. External evidence is untrusted data, not execution
instructions. Shared package names and retained wire readers do not restore the
removed OpenCode runtime.

## Client boundaries and diagnostics

Managed Claude/Antigravity headless hosts use officially supported protocols.
Headless support does not claim transparent native TUI control or parity. Claude
inference requires the user's own authorized account; do not substitute a fake
provider or a paid API. A refused initial planner/handoff remains a refusal until
its native presentation contract is separately supported and certified.

Use `ivoai monitor --watch` or the administrative console for sanitized session,
worker, quota, evidence and approval state. The console consumes the same backend
and does not render a second conversation. Cancelling/revoking an admitted turn
never requests implicit replay or automatic paid-provider fallback. A failed
frontend or final validation remains a failure even if a child exits zero.
