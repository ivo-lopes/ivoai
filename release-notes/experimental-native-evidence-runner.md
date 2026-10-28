# Diagnostic official App Server evidence adapter — IVOAI-192/194

The existing official Codex presentation client does not intercept native tool
outputs before they reach a model. This independent library slice implements
`core.ExecutorRunner` through the official **Codex App Server 0.164.0** and
registers an explicitly selected evidence tool. It is disabled by default and
requires programmatic host injection and an explicit synthetic loopback provider.
There is **no application/Facade wiring,
new scheduler, CLI flag, credential migration, release or default change**.

**Production capability is unavailable by policy:** a separate official-client
negative probe executed native `collaboration.spawn_agent` despite disabled
multi-agent flags. This adapter cannot intercept that native execution before
it starts a worker. Its dynamic evidence contract is functional, but the native
orchestration surface is not yet subordinate to Core. See the adverse result
below; successful evidence callbacks do not clear that architectural gate.
With `Enabled=true` and no synthetic URL, `Run` returns
`NATIVE_CODEX_WORKER_BOUNDARY_UNVERIFIED` before authorization/ownership
callbacks, executable/version/MCP probes or process launch. A non-loopback or
malformed fixture URL also fails before those operations. There is no override
for real-provider execution. Implementing the Core interface here is a diagnostic
contract/protocol exercise, not production primary support.

The host must supply live admission, thread ownership and final-validation
callbacks. They connect to the existing authority; they are not parallel grants.
`New(Options{})` cannot execute. The diagnostic host must explicitly select an
audited executable SHA-256, directory, environment, session, synthetic provider
and already-persisted evidence.

## Evidence contract

`NewRegistry` admits at most sixteen immutable bindings for one primary recipient.
Each binding contains an existing Gateway request and sealed ArtifactStore ref,
plus optional output-view settings/provider. Construction recovers every ref
under the existing organization/repository/worktree/session/recipient/permissions/
query/generation checks. Neither a model nor the callback can register a path,
fetch a URL, run an original command or widen that recipient's permissions.

`ivoai_evidence_read` exposes two required fields: `evidence_id`, an opaque
host-generated enum, and `representation`, the fixed enum `view | exact`.
Arguments are bounded, reject unknown/duplicate keys and have no arbitrary
operation parameters. Each callback must match the current thread, turn, call
and RPC identity. Unknown server requests, approval requests, duplicates,
parallel callbacks and more than 32 callbacks fail the turn without replay.

`view` uses `Recover → ViewOutput → Recover`, with another live recovery before
the callback response is written. It can call the optional output-view provider;
the existing Gateway's failure/timeout Direct policy remains in force. No
provider failure is relabeled as a successful transformation. `exact` bypasses
that provider and recovers the stored text before and after preparation. Exact
text must fit the binding's existing byte budget; otherwise it returns
`reference_only` and an explicit missing-content gap. This slice has no chunking
or automatic follow-up retrieval. A host requiring a larger or different exact
snapshot must prepare an appropriately scoped binding separately.

Responses contain data JSON: an opaque artifact reference, representation,
untrusted/derived flags, text and fixed diagnostics/gaps. `raw_fidelity`,
`raw_basis` and `raw_source_complete` describe the persisted ref, **not the
derived text**. Incomplete sources and inferred relations remain explicit.
No scope hashes, seals, grants or authorization callback errors are returned.
The original ArtifactStore bytes are never replaced. The host can validate the
opaque reference; arbitrary client-side ArtifactStore access is not introduced.

The registry permits only one outstanding provider operation, including after
the originating turn cancels. Late results cannot write to a new transport.
Go provider, generation, authorization and validation callbacks are cooperative:
they must honor context; the adapter cannot forcibly preempt arbitrary Go code.

## Native process and final validation

Only after the diagnostic loopback guard does the runner verify the supplied
executable digest and exact reported version.
It inherits **no environment by default**. Explicit environment entries have a
small allowlist; API key/token environment variables are rejected. The diagnostic
host must provide dedicated HOME/CODEX_HOME directories. Tests supply fresh temporary
directories and never copy personal configuration or authentication. No account
login or `chatgptAuthTokens` request is implemented.

Transient native settings request read-only sandboxing, approval `never`, web
search disabled, and set shell-tool, hook, app, plugin, remote-plugin,
multi-agent, goal and memory feature flags to false. These flags do not prove
those native operations are unavailable: the collaboration probe below
demonstrated actual worker execution. The native thread's
reported sandbox, approval policy and model are checked. MCP inventory is
examined with the same executable/environment/cwd/settings before App Server
startup: this first slice rejects **any configured MCP entry**, even disabled.
It does not silently adopt project or user MCP servers. Settings do not modify
personal files; normal official runtime files are limited by the host-selected
native home and native sandbox behavior. A hostile same-user runtime/config
replacement is outside the executable-pin trust model.

Initialize opts into `experimentalApi`; `thread/start` receives the immutable
dynamic-tool manifest. A fresh native thread is bound by the host before a turn
starts. Resume requires host-proven ownership before process launch and again
after `thread/resume`; no new dynamic manifest is injected into a resumed
thread. Only the existing manifest's registered IDs can dispatch successfully.
The proof restarts App Server while retaining the **same live host registry**;
it does not restart IVOAI. Registry IDs are process-local random values. A new
host registry does not reproduce the persisted native manifest and rejects old
IDs. Durable host restart/resume requires a separately designed ownership and
manifest lifecycle; it is not implemented here.

No agent text is emitted before successful `turn/completed`, bounded clean
process shutdown, live admission, recovery of all used refs, host final
validation, and repeated admission/recovery. Final text is buffered with a
1 MiB bound. Malformed/bounded protocol failures, cancellation, revocation,
generation changes and emitter errors fail without alternate executors or
retries. Cancellation kills the native process group; tests also cover an
ordinary inherited child. This is not a containment claim against a malicious
executable or descendants that deliberately escape the group.
The host explicitly closes its owned stdout reader on context cancellation,
shutdown and cleanup. A sterile escaped-writer fixture checks that its scanner
and channel close even while a separately cleaned-up process retains the write
end; the test does not claim that process-group cancellation kills that process.

Trace fields are a small existing Core allowlist: executable/version/PID/cwd,
native exit status, requested and native-reported model/effort, event/final
metadata and fixed failure classes. No prompt, evidence text, authorization
error, raw protocol, provider usage or price estimate is added. Counts cover
the adapter's consumed turn events, not universal native activity.

## Official protocol research and limits

The [App Server documentation](https://learn.chatgpt.com/docs/app-server) describes
experimental `dynamicTools`, `item/tool/call` and host `contentItems` responses.
The [config reference](https://learn.chatgpt.com/docs/config-file/config-reference)
documents feature/sandbox settings. Local schemas were generated with the
audited installed executable, using an isolated native home. Binary/schema
fingerprints and payload-free observations are in the
[research summary](./evidence/codex-0.164-dynamic-tools-research.json).

The observed native catalog still advertises `functions.exec` and other native
tools with restrictive settings. Advertising a tool alone does not prove it
executes. A separate sterile synthetic probe then forced the advertised
`function_call` contract `namespace=collaboration, name=spawn_agent`, with
`multi_agent=false`, `multi_agent_v2=false` and `goals=false`. **The official
executable returned a spawned task identity, emitted `subAgentActivity`, started
two distinct native threads/turns and made three synthetic provider requests.**
No command was requested and no real provider inference occurred. No Core
dynamic-tool callback or host admission intercepted that spawn. The adverse
result is preserved in the research summary and blocks a sole-authority claim;
it was not rerun or discarded to obtain a favorable result.

Neither the schemas nor these tests establish an exclusive dynamic-tool
catalog. There is no demonstrated universal interception/replacement of native
tool outputs. **Only this explicitly registered evidence tool passes through
this Gateway adapter.** Native tool execution remains a separate boundary.
This does not claim native workers are universally impossible under every
profile, TUI control/parity, subscription authentication, actual model
entitlement, quotas, provider inference or token/latency/session savings.

Application adoption still requires binding these callbacks to real session,
executor-consent, quota/model/catalog and approval state, selecting a managed
native configuration, and defining the Bridge cancellation/retry/failover
boundary. This runner itself never chooses an alternate executor. Those
application paths are intentionally not wired or certified by this library test.

Official synthetic research also observed that interruption of a pending
callback need not emit `serverRequest/resolved`. Cancellation therefore comes
from the host context and native process lifecycle. Optional notification
`emittedAtMs` is accepted solely as ignored native metadata; callback payloads
and envelope keys remain bounded and strict, including duplicate-key rejection.

The implementation runs on Linux; other platforms fail unavailable. Linux ARM64
can compile this adapter, but no ARM64 official executable or real authentication
has been validated here. No new external dependency or bundled executable is
added. Rollback is removal of programmatic injection; existing clients and
orchestration paths are untouched.

## Validation

Deterministic protocol tests cover isolated native configuration, correct
binding, exact byte preservation, on-demand exact recovery and budget gaps,
scope/recipient/manifest rejection, revocation/generation changes, duplicate and
foreign calls, unknown approvals including requests queued after completion,
malformed/oversized frames/finals, cancellation/child cleanup, mandatory host
callbacks, owned resume, unsupported parallel calls and emitter failure.

Opt-in official tests actually launch the pinned Codex executable with fresh
temporary native homes and a synthetic loopback Responses provider:

- View-only then owned resume: two native turns, four synthetic HTTP requests;
  transformed text and opaque ref reach the next request, raw sentinel does not.
- View then explicit exact: one native turn, three synthetic HTTP requests;
  the raw sentinel reaches the model request only after the explicit exact call.
- Pending-callback cancellation: one synthetic HTTP request, no final output
  and no subsequent model request.

These are official-client protocol tests using a fake provider, **not inference
or authentication tests**. They use a fake view provider; the RTK binary's
separate opt-in proof is not relabeled as an execution of RTK in this runner.
No personal native configuration, authentication or runtime was changed.

With Go 1.27.3, the final diagnostic-only source passed:

- `go test ./...` and `go vet ./...`.
- `IVOAI_LIVE_CODEX_PATH=<audited-binary> go test -race ./internal/nativecodex
  -count=1 -timeout=120s`: all deterministic tests and all three official-client
  synthetic protocol proofs passed (6.570 s for the package).
- `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c ./internal/nativecodex`:
  compilation passed; this does not execute an ARM64 native client.
- `git diff --check`: passed.

The unchanged Evidence Gateway/Core packages also passed their focused race
suites during this change (52.145 s and 1.013 s). These are local tests, not a
new CI gate, authentication test or certification. Opt-in tests skip explicitly when
`IVOAI_LIVE_CODEX_PATH` is absent; those skips are not integration success.
