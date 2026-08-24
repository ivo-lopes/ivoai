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
- the delegation decision maker;
- the only writer/conversation owner;
- the consolidator of bounded worker results.

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

## Delegation

The primary can ask `orchestration_delegate` for bounded analyst, researcher,
reviewer, tester, or security-review work. Every request passes through the quota
manager before a worker slot or Ruflo task is created. An exhausted requested
provider is replaced by the eligible alternate; if neither is eligible, no worker
starts. Official clients run the inference (`codex exec` or `claude --print`), while
Ruflo records only opaque swarm/task lifecycle state. Default concurrency is two and
the hard supported maximum is three.

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

The monitor renders Codex session/weekly/monthly only when those official buckets
apply, and renders Claude Code 5-hour and weekly rows. It adds context and
model-specific buckets and reset times only when authoritative data exists.
It reports source and observation time, current/initial primary, failovers,
checkpoint availability, Headroom use, workers, Ruflo, context, ai-memory, and server
state. `status` reads the bounded quota cache while running short, parallel Server
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
