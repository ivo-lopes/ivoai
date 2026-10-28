# Experimental local VERSION metadata turns

Preparatory IVOAI-193/192 increment, based on PR #32 plus its Go 1.27.3 security update (`5949f20d`). This is a
deterministic metadata response from IVOAI Core, not general primary reasoning
without a DAG. It is disabled unless the host environment explicitly sets
`IVOAI_EXPERIMENTAL_LOCAL_EVIDENCE_TURNS=1`; no configuration schema changes or
personal client configuration edits are needed. Unsetting the flag restores the
existing route. Code rollback requires no stored-data conversion.

## Narrow supported operation

Only a finite full-match set of Portuguese/English questions is recognized,
including `Qual versão deste projeto?` and `What is the project version?`.
Core reads the literal file `VERSION` from the canonical launch directory. The
file must be regular, not a symlink, at most 256 bytes, and contain an ASCII
SemVer 2.0 value with at most one terminal LF or CRLF. Extra instructions,
different paths, compound questions and invalid data cannot become local
execution instructions. Root identity is pinned and FIFO/directory/symlink
sources are rejected using a bounded, nonblocking, descriptor-relative read.

The route is limited to the first claimed turn of a new managed Codex session
created by the current host process. It requires a matching active session,
canonical directory, organization/source scope, current running turn/attempt
receipt, and the actual matching Codex presentation thread. This presentation
thread is created by the official client; Core creates no backend thread.
Resumes, handoffs, other sessions/frontends, earlier attempts, backend mappings,
plans, tasks, workers, decisions, recovery and checkpoints remain ineligible.
The Bridge marks a literal single-user-message envelope in process-local
context; system/developer/history or multipart envelopes cannot use this route.
The marker is an observation of input shape, not a permission grant. In the
native Codex path, the existing Facade submits the prompt admitted at
`turn/start`; it does not forward system/developer/history items from the
official process's Responses body. The marker describes the complete Bridge
envelope. Fresh host/session/claim/attempt checks provide the separate history
boundary; this pilot does not inspect or certify the entire native Responses
input envelope.
Checkpoint absence is inspected with `session.Store.CheckpointAbsent`: both
current and legacy namespace entries must be absent, using `Lstat` without
opening contents. Regular files, directories, symlinks, FIFOs and inspection
errors all deny this route; the general checkpoint loader is unchanged.

Before admission, any ineligible request or absent/unsafe/invalid source uses
the existing path, including its DAG completion and approval requirements.
After admission, cancellation, changed source/directory/configuration/component
state, superseded receipt, revoked scope or flag, new checkpoint or storage
failure fails closed. Core never retries an admitted metadata turn through a
CLI. HTTP duplicate-response caching also marks Core metadata as non-replayable
independently of an authentication callback: the same completed request gets
409, even after source/scope/flag changes. It cannot replay stale evidence or
launch an executor. Existing non-local response replay behavior is preserved.
Quota cancellation after local admission returns an error without selecting an
alternate or constructing a handoff; a local-source result cannot use the
provider `RateLimited` retry path either. Legacy executor failover remains
unchanged. The prepared turn has its own non-nil final validator and retains the
existing 1 MiB final buffer, cancellation and final-emitter failure behavior.

## Evidence and attribution

The generic Evidence Gateway stores the complete original bytes in the existing
ArtifactStore, with session ownership and host-derived organization, repository,
worktree and permission digests. Authorization and exact source generation are
checked during retrieval and exact recovery, and again before final output.
No plan-dependent code-evidence adapter or compressor is used. Facts remain
extracted, untrusted source data with exact fidelity; no inferred relationships
are claimed. The answer identifies `IVOAI Core local metadata (no model
execution)`, the `VERSION` source, the validated literal and a short opaque artifact
reference. Scope digests, the signed gateway capability and provenance stay
inside the host. The native client transport does not yet expose an artifact
recovery endpoint; the reference identifies exact bytes retained in ArtifactStore
and does not promise retrieval from the client UI. Artifact recovery retains the existing ownership checks and TTL.
The signed gateway capability itself is process-local and valid only while its
admitted turn receipt and source remain current; it is not a durable new API.

No hidden primary CLI, MCP transport, hook or worker is started by this turn.
Results/attempts carry no effective model, provider usage, backend thread,
authentication claim, CLI exit code or fabricated executor trace. The existing
configuration-source field and a capability event identify Core metadata.
Requested catalog selection remains routing metadata. The actual Codex frontend
process, existing session initialization, authentication/quota/model preflight
and normal consent gates still run. A whole session therefore does not become a
zero-process, zero-probe or zero-cost session.

## Validation and limits

Executed locally with Go 1.27.3 on Linux amd64:

- `go test ./...` passed, including existing approval, sandbox, DAG and native
  frontend contracts.
- `go vet ./...` passed.
- A Linux ARM64 cross-build of `./cmd/ivoai` passed; no ARM64 runtime or
  provider test is claimed. Linux amd64 tests used the default `GOAMD64=v1`.
- Focused race tests passed for the new metadata/input-envelope paths and the
  existing prepared-turn lifecycle, concurrency and production DAG validators,
  checkpoint namespace checks and legacy HTTP replay behavior. The official
  App Server opt-in test also passed under `-race`.
- Real loopback Bridge/SubmitTurn tests use temporary project/config/session
  state and forbidden-process counters. They verify zero executor discovery,
  subprocess calls and legacy-runner calls for an eligible turn; exact original
  bytes and cross-session artifact denial; explicit Core attribution; and no
  fabricated DAG, worker, thread, model or CLI exit.
- Negative tests cover conservative fallback, source boundaries/special files,
  history, active-session scope, prior task/worker/decision state, and revocation
  before retrieval and before final validation. Admitted failures emit no final
  metadata and never replay through a CLI. The second turn keeps its claims and
  attempts and returns to the existing DAG path.
- Independent review found and this increment corrects three pilot defects:
  completed Core prose could be replayed by a Bridge without an auth callback,
  checking checkpoint absence via the general loader could block on a FIFO,
  and quota arbitration could cancel an admitted local turn and reinterpret it
  as work for an alternate CLI. These were found before the disabled pilot
  was merged or released; no production incident is claimed.
  Regression tests now require duplicate 409/no replay and nonblocking namespace
  admission for every supported checkpoint location and entry type. Quota-monitor
  and `RateLimited` error regressions cover JSON and streaming responses and
  require one runner call with zero alternate selections/handoffs. An App fixture
  synchronizes the monitor after a real local receipt has been acquired, then
  verifies cancellation without CLI/DAG execution and duplicate 409.

An explicit official App Server integration run passed using Codex 0.164.0,
SHA-256 `b93f6811d3f653f0fb83be4050d857fb9abbb111dfa2ce160563327daff7f306`.
The test launches that binary as an App Server via `codexfrontend.Start` and
uses a WebSocket test driver to send native initialization, thread and turn
requests through the Facade to its stdio transport. The official process creates
the actual presentation thread and calls the isolated local Responses provider;
the production Core/Bridge turn then produces the VERSION metadata answer.
This is an official App Server proof with a test driver, not a native TUI proof.
The App session and model catalog are synthetic (including a supported `high`
effort required by the official turn API); no provider/model capability or
subscription inference is certified by this fixture.

Only a temporary HOME and NativeHome are passed to the official process, with
no inherited personal config or provider credentials. The test asserts Core
attribution, literal source/value and an opaque reference; zero hidden primary
runner/discovery calls; no project-MCP marker from hostile project config;
no tasks, plans, workers, decisions or backend thread; and the real frontend
thread mapping with no fabricated model/CLI trace/exit. Existing startup
authentication/quota/model probes are not performed by this synthetic App
fixture and remain unchanged in production.

The opt-in command is:

```sh
IVOAI_LIVE_CODEX_PATH=/path/to/verified/codex \
  go test ./internal/app -run '^TestLiveLocalMetadataOfficialAppServer$' \
  -v -count=1 -timeout=60s
```

No token, latency or whole-session economic benefit is claimed. Claude and
Antigravity parity is unchanged. The IVOAI repository at the base commit has no
`VERSION` file, so this pilot deliberately falls back there; it does not infer a
version from Git tags or build metadata. Broader primary-only reasoning remains
separate work.
