# Automatic orchestration

`ivoai auto` is the quota-aware conversational mode. It supervises official Codex
CLI and Claude Code processes; it does not implement a third chat interface and it
does not send inference requests through Ruflo.

## Start and conversation ownership

```sh
ivoai auto
ivoai auto --planner codex
ivoai auto --planner claude
```

Without a flag, the prompt shows cached subscription quota and uses the configured
planner (`codex` by default) when Enter is pressed. The selected official TUI is all
of the following for the current conversation:

- the interface seen by the user;
- the planner and primary agent;
- the source of task signals and proposed decomposition;
- the only writer/conversation owner;
- the consolidator of bounded worker results.

IvoAI retains final authority over whether delegation is economical and over quota,
provider, model, and effort selection.

Codex is given session-local developer instructions with a process-scoped `-c`
override. Claude is given the same policy using its official
`--append-system-prompt-file`; a private `--settings` file installs a statusline
command only for that process. The user's persistent Codex and Claude configuration
is not rewritten.

The same session-local instructions define a strict research-source gate. For any
research or external verification, the primary attempts `ivoai-memory` first and
`ivoai-context` second before using web search, a browser, or another external
connector. Empty, unavailable, insufficient, or stale results allow the web step;
self-contained work does not trigger artificial lookups. The worker adapters receive
the same policy through the official process-scoped instruction mechanisms.

## Startup sequence

1. Create a private metadata-only session record.
2. Probe Codex and Claude without provider API keys.
3. Resolve the requested provider through the quota/capability gate.
4. If it has a confirmed hard limit, select an eligible alternate and record the
   startup failover. If both are exhausted or unauthenticated, enter `BLOCKED`
   without starting a primary or worker.
5. Verify Ruflo safe mode and initialize a real provider-free swarm.
6. Register the primary's opaque lifecycle task.
7. Attach the session-local `ivoai-orchestrator` MCP and open the official TUI,
   through Headroom when its compatibility preflight succeeds.

No `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, or other PAYG credential is accepted as a
fallback. Provider-key environment variables are removed from quota probes and
workers. Ruflo keeps `provider_execution=false` and receives no prompts/results.

## First-turn planning and shared knowledge

The first substantive request has an enforced protocol rather than an optional prompt
convention:

1. attempt exactly one bounded `ivoai-memory` lookup and then one bounded
   `ivoai-context` lookup before any Web lookup;
2. save a session-scoped, secret-free SharedContextBrief through
   `orchestration_bootstrap`;
3. inspect quota and runtime capability state;
4. decompose non-overlapping work into a dependency DAG and provide seven bounded
   task signals;
5. call `orchestration_plan`, which calculates scores, execution tiers, economic
   delegation, and profiles;
6. queue independent work with `orchestration_spawn_batch`, continue useful primary
   work, then wait by notification rather than polling;
7. validate results, escalate only with evidence, synthesize, and checkpoint.

The brief content is held only in a private runtime file. Session JSON stores a hash,
timestamp, source health, and reference count. Workers receive the same brief, which
avoids repeating the same initial Memory/Context query. They can perform an additional
lookup when the bounded brief genuinely lacks necessary detail. Related later turns
use delta planning; a material objective or project change refreshes the brief.

## DAG scheduling and delegation

`orchestration_delegate` remains available for backward-compatible synchronous
delegation. The default automatic protocol uses `orchestration_plan`, asynchronous
`orchestration_spawn`/`orchestration_spawn_batch`, `orchestration_wait`, and
`orchestration_primary_complete`. Tasks are capped at 12 and workers at three (two by
default). Unknown dependencies, cycles, duplicate work, unsafe labels, arbitrary
fields, or out-of-range scores are rejected.

IvoAI calculates the capability score and maps it to LIGHT, BALANCED, STRONG, or MAX.
It separately compares parallel/quality gain with worker startup and context-transfer
overhead. A trivial task stays `primary` even when a model asks to delegate it.
Dependency-ready workers start concurrently and return IDs immediately; dependent
tasks remain queued until all prerequisites complete.

Every worker passes through the quota and capability router. Official clients run
inference (`codex exec` or `claude --print`); Codex workers use a read-only sandbox
plus MCP read allowlists, Claude workers use strict process-scoped MCP configuration
and plan mode with mutation tools disabled, and Ruflo records only opaque lifecycle
state. Results are bounded by execution tier and retained in bridge memory.
See [Automatic scheduler and model routing](auto-scheduler.md).

## Progressive escalation

Work begins at the lowest sufficient profile. The primary may call
`orchestration_escalate` only after a completed or failed result and must provide an
evidence-based reason. Each call advances exactly one tier. Unsupported effort falls
back to the client default without a false capability claim; an exhausted exact model
causes another sufficient model/provider to be considered before the task is blocked.

## Checkpoints and failover

When automatic checkpoints are enabled, the primary is instructed to save a bounded
secret-free summary after materially completed work. It contains objective,
decisions, completed work, changed file names, important checks, outstanding work,
blockers, and next step. It never contains the complete prompt, response, transcript,
credential, header, or provider auth response. Secret-shaped content and terminal
control bytes are rejected. The private runtime checkpoint gives the supervisor an
immediate failover boundary; normal ai-memory hooks provide durable operational
continuity around the same official agent session.

If the active provider reports a hard subscription limit, the supervisor:

1. blocks new work for that provider and marks the quota cache;
2. terminates and reaps only the recorded primary process group;
3. checks the alternate provider again;
4. loads the last checkpoint, or creates an explicit interrupted fallback;
5. reads bounded `git status` and diff-stat metadata without altering the worktree;
6. starts the alternate official TUI with the handoff;
7. records current primary, reason, time, phase, and failover count.

The automatic loop stops after two consecutive failovers without a new successful
checkpoint. Network failures are not classified as quota exhaustion. Context-window
pressure is a separate metric and never marks subscription quota as exhausted.

## Observability and state

Use a second terminal:

```sh
ivoai monitor --watch
ivoai monitor --session <session-id> --json
```

The monitor renders Codex 5-hour and weekly windows by official duration, an
individual provider-wide row without inventing its cadence, and any other rolling
duration such as 1h or 1d. It renders Claude Code 5-hour and weekly rows. It adds context and
model-specific buckets and reset times only when authoritative data exists.
It reports source and observation time, current/initial primary, failovers,
checkpoint availability, bootstrap health/reference count, task DAG, score, tier,
model/effort provenance, execution mode, dependencies, duration, Headroom use,
workers, Ruflo, context, ai-memory, server state, and the latest bounded secret-free
control-plane events. `status` reads the bounded quota cache while running short, parallel Server
and Ruflo health checks; it does not perform a heavy provider quota probe. `doctor`
performs deeper active capability checks. Headroom version/help probes establish
installation and compatibility only, not an interactive launch validation.

Session JSON lives below `$XDG_STATE_HOME/ivoai/sessions`; quota cache lives below
`$XDG_STATE_HOME/ivoai/quota`. Directories are `0700`, files are `0600`, writes are
atomic, and concurrent quota writers use an advisory file lock.

Several automatic or explicit sessions may run simultaneously. Session IDs,
runtime directories, primary PID/start markers, local orchestrator sockets and Ruflo
homes are independent, so cleanup and stop operations cannot select another live
session by recency. Shared ai-memory is common by design; lifecycle observations use
the main repository name so Codex and Claude agree even when their cwd path aliases
differ.

## Failure isolation

- Server, context, or ai-memory outage does not stop the official primary.
- Headroom failure uses the existing direct-client fallback.
- Ruflo failure stops automatic orchestration but does not affect `ivoai codex` or
  `ivoai claude`.
- Pending, not-exposed, and stale quota are distinct and never converted to `0%`.
- Both confirmed exhausted providers produce a bounded waiting/blocked state; ivoai
  does not retry forever or activate PAYG inference.
