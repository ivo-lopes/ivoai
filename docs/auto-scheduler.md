# Automatic scheduler and model routing

`ivoai auto` uses an in-process policy engine and the session-local
`ivoai-orchestrator` MCP. The official Codex or Claude Code TUI remains the
conversation owner and the only authoritative writer. The scheduler only launches
bounded, read-only advisory workers through the same official subscription clients.

## First substantive turn

The primary follows this order:

```text
Memory lookup -> Context lookup -> SharedContextBrief -> task analysis
  -> validated DAG -> scores -> quota/capability routing -> async dispatch
  -> result validation -> primary synthesis -> bounded checkpoint
```

The first Memory and Context attempts occur once. `orchestration_bootstrap` stores a
maximum-64-KiB, secret-free brief in the private session runtime directory. Session
JSON retains only its timestamp, source statuses, reference count, and SHA-256 hash.
Workers receive the brief automatically and query shared knowledge again only if a
detail is missing. A material objective or project change requires a fresh bootstrap;
related later turns use delta planning.

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

A worker is used only when `benefit > overhead`, parallel execution is enabled, and
the planner marked the work delegable. Otherwise the task stays in the primary. This
keeps a typo fix local while allowing independent inventory, architecture, and
security work to overlap.

## Capability and profile resolution

Routing authority is quota, runtime capability registry, configured policy, required
tier, then planner preference. Model names are never invented.

- Codex models and supported reasoning efforts come from the structured
  `codex app-server` `model/list` response. IvoAI passes a selected model with
  `--model` and verified effort through process-scoped
  `model_reasoning_effort` configuration.
- Claude Code exposes verified effort choices in its official CLI help. It has no
  equivalent structured model catalog in the validated client, so IvoAI leaves the
  model empty and uses the official client default. A verified effort is passed with
  `--effort`.
- If explicit effort is unsupported, IvoAI sends none and records
  `effort_source=unsupported`; it never labels the client default as a confirmed
  effort.

Capability metadata is cached in a private XDG cache keyed by official client
version. An update or version change invalidates it. Empty profile overrides mean
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

The scheduler defaults to two workers and hard-caps concurrency at three. Dependencies
must complete before a queued task starts. Prompts and results stay in bridge memory,
not session JSON or Ruflo. Worker result budgets are tier-bounded, and worker prompts
ask for conclusions, facts, evidence, issues, and recommendations instead of a long
narrative.

Codex workers run with `--sandbox read-only`. Claude workers use plan permission mode
and disallow Bash/Edit/Write/NotebookEdit. They inherit the project directory and
the same managed Memory/Context MCP configuration and scoped server environment.
Ruflo receives only opaque lifecycle IDs.

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
