# Automatic scheduler and model routing

`ivoai opencode` uses an in-process policy engine and the session-local
`ivoai-orchestrator` MCP. The pinned OpenCode TUI is the frontend; IVOAI remains the
conversation owner and integration authority. Official clients execute bounded
tasks; approved writers use isolated worktrees, never concurrent writes to the
primary checkout. Native AUTO does not depend on Ruflo lifecycle.

## First substantive turn

The primary follows this order:

```text
Prompt readiness -> purpose-auto -> selected Memory/Context -> bounded brief
  -> validated DAG -> scores -> quota/capability routing -> plan approval
  -> host/DAG admission -> scoped workers -> result validation
  -> checked integration -> strong primary synthesis
```

Acceptance criteria are mandatory. Insufficient prompts return missing fields and
wait for refinement, without planning or launching workers. Selected, relevant
Memory and Context attempts occur once. `orchestration_bootstrap` stores a
maximum-64-KiB, secret-free brief in the private session runtime directory. Session
JSON retains only its timestamp, source statuses, reference count, and SHA-256 hash.
Each worker receives only its local objective, acceptance, constraints, permitted
sources/tools and relevant dependency references. The complete primary prompt and
brief are not broadcast. A new managed turn performs a new admission/bootstrap;
native resume still requires proven auth continuity.

Memory and Context failures are independent. If either or both are unavailable, the
brief records a degraded source and the session may continue when the task is still
executable. Retrieved material is always untrusted data.

## Planning and economic delegation

`orchestration_plan` accepts at most 12 tasks. It rejects unsafe identifiers,
unknown dependencies, cycles, duplicate task text without an explicit independent
verification marker, unknown fields, and scores outside `0..100`. It never accepts
an executable, shell command, environment, endpoint, or credential.

The planner supplies seven bounded signals. IvoAI calculates capability as:

```text
score = round((30*complexity + 25*risk + 20*reasoning_depth
             + 15*verification_need + 10*context_breadth) / 100)
```

Non-default non-negative weights are normalized by their positive sum. Tiers are:

| Score | Tier |
| ---: | --- |
| 0–24 | LIGHT |
| 25–49 | BALANCED |
| 50–74 | STRONG |
| 75–100 | MAX |

The planner may propose delegation, but IvoAI has final authority. The deterministic
decision compares:

```text
benefit  = round((45*parallel_value + 20*verification_need + 20*risk
                 + 15*context_breadth) / 100)
overhead = 25 + 20*(100-complexity)/100 + 5*latency_sensitivity/100
```

A read-only worker is used when `benefit > overhead`, parallel execution is enabled, and
the planner marked the work delegable. Otherwise the task stays in the primary. This
keeps a typo fix local while allowing independent inventory, architecture, and
security work to overlap. Writers always use the controlled adapter with explicit
relative write paths, including a single small implementation task.

## Capability and profile resolution

Routing authority is quota, runtime capability registry, configured policy, required
tier, then planner preference. Model names are never invented.

- Codex models and supported reasoning efforts come from the structured
  `codex app-server` `model/list` response. IvoAI passes a selected model with
  `--model` and verified effort through process-scoped
  `model_reasoning_effort` configuration.
- Claude Code discovery uses the official client's model/capability surface and
  help. If it cannot prove a compatible model/effort or safe writer capability,
  that route is ineligible rather than being guessed. A verified effort is passed
  with `--effort`; an unauthenticated optional Claude client does not block Codex.
- Unsupported explicit model/effort selection fails closed. Absent reasoning
  capability is reported as unsupported, never as a verified default.

Capability metadata is saved as a private diagnostic snapshot. Each new discovery
uses the official client; stale snapshots are not routing authority. Empty profile overrides mean
automatic resolution. A non-empty configured model is eligible only if it exists in
the runtime catalog.

For a required tier, the router selects the lowest sufficient catalog tier. It
honors exact model quota windows, tries another sufficient model, and then considers
the alternate authenticated subscription provider. When several profiles satisfy
the quality floor, authoritative remaining quota may preserve the provider under
greater pressure. Unknown or failed telemetry is not zero quota.

## Async DAG runtime

Automatic sessions add these MCP methods:

- `orchestration_bootstrap` — save the private SharedContextBrief;
- `orchestration_capabilities` — inspect safe runtime model/effort metadata;
- `orchestration_plan` — validate and resolve the DAG;
- `orchestration_spawn` — return a worker ID immediately;
- `orchestration_spawn_batch` — queue independent tasks and start eligible work in
  parallel;
- `orchestration_primary_complete` — release dependants of primary-owned work;
- `orchestration_wait` — wait for any/all with a bounded timeout and notifications;
- `orchestration_result` — read a bounded result held in bridge memory;
- `orchestration_escalate` — move one tier upward with an evidence-based reason;
- `orchestration_cancel` — cancel only a worker owned by the session.
- `orchestration_integrate` — check and integrate approved changes before synthesis.

Concurrency uses dependency-ready tasks, CPU, available memory, load, observable
I/O pressure, quota and the user cap, with an absolute bounded limit of 12. Unknown
resources degrade conservatively. Dependencies must complete before a queued task
starts. Prompts and results do not enter session JSON. Worker result budgets are tier-bounded, and worker prompts
ask for conclusions, facts, evidence, issues, and recommendations instead of a long
narrative.

Read workers run with read-only policies. Approved writers run in owned worktrees
with checked write scopes. MCP access is deny-by-default per task; configured does
not mean authorized. Official clients receive only process-local projections and
never another client's credentials. If worktrees cannot initialize, writers run
sequentially as read-only patch producers; IVOAI checks paths and applies patches
under one lease. Unsupported changes fail closed. Conflicts retain evidence and
require resolution; they are not automatically merged away.

The primary defaults to a verified STRONG or MAX catalog model. Workers select the
lowest sufficient tier. At 10% remaining quota or below, a separate confirmation
proposes conservation; it never silently downgrades the primary or changes a
material route. Rejecting keeps the existing route while it remains eligible.

## Escalation, observability, and limits

The initial profile is the lowest sufficient one. A completed or failed task may
advance only one step (`LIGHT -> BALANCED -> STRONG -> MAX`) and only with a bounded
reason such as failed validation, low confidence, missing context, or reassessed
risk. No automatic retry is hidden from the primary.

`ivoai monitor --watch` shows brief readiness, task score, tier, selected provider,
model and source, effort and source, execution mode, dependency state, duration,
Headroom use, and escalation count. JSON contains only this metadata. Token and
Headroom-saving metrics remain unavailable unless an official structured source
provides them; ivoai does not estimate them.

Headroom 0.36.0 is bypassed whenever authoritative shared-knowledge material is in
the primary or worker path because its tool-result protection is not proven safe for
those exact responses. Direct agent modes, Web MCP, ai-memory, and Context remain
independent of the automatic scheduler.

See [Automatic orchestration](auto-orchestration.md) for TUI and CLI policies.
