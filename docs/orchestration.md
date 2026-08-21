# Session control and orchestration

ivoai has two explicit session modes. Neither mode adds a pay-as-you-go provider
credential, and neither replaces the official Codex or Claude Code interface.

## Direct mode

The established commands remain the shortest path:

```sh
ivoai codex
ivoai claude
```

They transfer the current directory, stdin, stdout, stderr, terminal and signals to
the selected official client. Ruflo is not initialized. Headroom is used only after
its compatibility probe succeeds, with the existing safe direct-start fallback.

For the same runtime with lifecycle observability, start a direct session:

```sh
ivoai session start --executor codex --mode direct
ivoai session start --executor claude --mode direct
```

The control plane records non-sensitive metadata such as the session ID, PID,
executor, model provenance, Headroom use, service state and exit code. It does not
record prompts or responses.

## Orchestrated mode

Use this mode explicitly for work that benefits from bounded delegation:

```sh
ivoai session start --executor codex --mode orchestrated
# or
ivoai session start --executor claude --mode orchestrated
```

Before the primary client opens, ivoai verifies the installed Ruflo version and safe
profile, confirms that provider execution and durable Ruflo memory are disabled,
executes a real `swarm init`, obtains and verifies its Swarm ID, and registers an
opaque primary lifecycle task. A failed gate stops the launch with a non-zero status;
there is no silent direct-mode fallback and the UI never labels that launch as
orchestrated.

The official primary receives one session-local stdio MCP named
`ivoai-orchestrator`. It is injected with a process-scoped Codex config override or a
private temporary Claude MCP file. It is not added to the remote gateway and is
removed when the session runtime directory is deleted.

The bridge offers:

- `orchestration_status` — safe session and swarm status;
- `orchestration_agents` — primary and worker metadata;
- `orchestration_delegate` — bounded delegation to an official Codex or Claude
  worker;
- `orchestration_result` — an in-memory bounded result;
- `orchestration_cancel` — cancellation of a worker owned by the session.

Delegation tasks and results never enter Ruflo or the session JSON. Ruflo receives
only opaque session/worker IDs through provider-free task lifecycle commands. The
worker adapter uses `codex exec --json --output-last-message` or
`claude --print --output-format json`, selected from trusted component paths. Worker
provider-key environment variables are removed; subscription authentication stays
inside each official client.

## Roles of the components

- **ivoai:** session lifecycle, safe preflight, process identity, worker adapter,
  observability and cleanup.
- **Codex / Claude Code:** all inference, reasoning, tools and user interaction.
- **Ruflo:** ephemeral swarm topology and opaque lifecycle coordination only.
- **Headroom:** optional wrapper for primary and worker processes; telemetry records
  whether it was actually used.
- **ai-memory:** durable operational memory and cross-session continuity. Ruflo uses
  `CLAUDE_FLOW_MEMORY_BACKEND=memory`, never a competing durable store.
- **IvoAI Context:** independent RAG/context service. The session control plane only
  reports its health and leaves the existing MCP integration intact.

## Model provenance

Model names are never inferred from a binary version, subscription or vendor. The
reported sources, in priority order, are:

1. `runtime_verified` — only when structured runtime evidence explicitly confirms it;
2. `argument` — `--model`/`-m` supplied to the official client;
3. `configured` — the official client's configuration file;
4. `unknown` — no reliable evidence.

The current adapters do not promote ordinary client output to `runtime_verified`.
Therefore `unknown` is expected when neither an argument nor supported configuration
contains a model.

## Monitor and lifecycle commands

```sh
ivoai session list
ivoai session list --json
ivoai session show <session-id>
ivoai session stop <session-id>
ivoai monitor
ivoai monitor --watch
ivoai monitor --session <session-id> --json
```

`monitor --watch` is intended for a second terminal. It follows state changes until
the selected session ends and reuses ivoai's responsive terminal presentation. JSON
is newline-delimited while watching, contains no ANSI, and includes metadata only.

Session files live below `$XDG_STATE_HOME/ivoai/sessions` (normally
`~/.local/state/ivoai/sessions`) in mode-`0700` directories and mode-`0600` atomic
JSON files. Session and worker IDs are random. Linux process start markers prevent a
recycled PID from being killed. A default of two concurrent workers and a hard limit
of three prevent unbounded delegation.

## Configuration

The backward-compatible defaults are:

```toml
[orchestration]
enabled = true
provider_execution = false
default_mode = "direct"
primary_executor = "codex"
review_executor = "claude"
max_workers = 2
```

`provider_execution=true`, unknown executors, unknown modes and worker limits outside
1–3 are rejected. The interactive Configuration menu manages these preferences, or
automation can use `ivoai config set orchestration.<field> <value>`.

## Failure isolation

Context, ai-memory and the remote server may be degraded without preventing the
primary client or workers from starting. A Headroom preflight/start failure uses the
documented direct-agent fallback. In contrast, Ruflo health, profile, swarm or
primary-registration failure is fatal only to an explicitly orchestrated session.
The original `ivoai codex` and `ivoai claude` commands remain independent of Ruflo.
