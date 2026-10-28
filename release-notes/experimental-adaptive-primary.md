# Experimental primary-only admission

This is a partial IVOAI-192/193 delivery. The feature is **off by default** and
does not establish economic savings, full native-client parity or completion of
the backend-first roadmap.

Set `IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY=1` on the IVOAI owner process to opt in.
The first slice admits only the first literal user-message envelope of a new
managed Codex session, without history, recovery, pending decisions, checkpoints,
existing tasks or workers. Prompt wording, language and length do not grant
authority. A complex first request starts with the same read-only capability;
the primary requests the existing plan protocol when needed.

The reviewed runtime is the existing managed **Codex 0.162.1 Linux amd64 pair**:

| Asset | Executable SHA-256 |
| --- | --- |
| Codex | `628e35487d888a28f0427fc37501da529dd3d81fcbad7a2c3421c1e8670d92e1` |
| Code Mode host | `ec71830690a7c0024916ee0c99fbdc635bc6e747ff665ea4f03a8389bbcb6b30` |

The source/archive pins and adverse native-worker controls are recorded in
[the managed-worker boundary audit](managed-codex-worker-boundary.md). A version
banner alone, a user-owned executable, another architecture or another version
does not admit this pilot. Unsupported runtimes receive an explicit diagnostic
and retain the existing DAG path **before** admission. No installer, native-client
configuration change, credential extraction or new dependency is added.

## Authority and lifetime

The existing Core turn contract now carries a transient host `ExecutionPolicy`.
It is excluded from JSON, preserving the historical ExecutorResult wire shape.
The optional typed `primary_turn` receipt uses the existing Session Store; it is
operational metadata, not a credential, scheduler or new database. The Core
validates its structure without selecting a provider. App admission and the
pilot's consumers enforce the specific Codex/runtime boundary.

Admission binds the session, immutable receipt ID, observation TurnID/AttemptID,
frontend identity, configuration/state digests, canonical working directory and
directory device/inode. It hashes both pinned executables at admission and final
validation. Calls check the same file identities, sizes, modes, modification and
change timestamps. Files are regular, executable, bounded, and opened without
following a final symlink or blocking on a FIFO. The 384 MiB bound accommodates the
reviewed 294,415,048-byte Codex executable and 74,036,112-byte companion. Hashing and
I/O cancellation are cooperative; this is not protection against a privileged
local user controlling the host's own code and state.

The prepared owner checks its opt-in and live configuration/state/scope on use.
The owned stdio MCP server captures the receipt identity once when it starts and
revalidates it through a host callback. The child does not need the admission
environment variable and cannot admit, substitute or adopt another turn's receipt.
Changing another process's environment does not revoke it instantly. Receipt
closure, recorded-turn changes and policy/state changes are checked by the child.
The captured identity is checked again inside the existing session lock before
DAG admission, plan persistence or bootstrap persistence. The shared brief is
written within that checked bootstrap update. A stale child cannot overwrite a
later turn's brief merely because an earlier callback succeeded.

The owner consumes the receipt under Store.Update **before emitting the final
answer**, preventing a late plan during slow frontend delivery. Release closes
all per-turn knowledge transports and consumes the receipt on failure as well.
An error during attempted Store admission is conservatively non-replayable,
because a storage error cannot prove that no atomic rename reached disk.

A closed receipt cannot revive within its TurnID. On a later recorded running
turn, only the closed operational pointer is retired. Existing attempts, request
claims, checkpoints, plans, worker evidence and history remain. Later turns use
the existing DAG workflow; general history/resume admission is outside this slice.

## Read capabilities and escalation

There is no prompt-language classifier or automatic second primary invocation.
The current primary may answer a self-contained question directly or use its
permitted local inspection and projected host Code Intelligence.

The selected institutional router filters both tools/list and tools/call. It
accepts only reviewed read names: Memory query/recent/read_page/status and Context
search/get_document/recent/health. Unknown and mutating institutional tools are
denied, even after DAG escalation in this first slice. REST hook routes on this
agent capability are denied; independent existing owner-operated hooks are not
granted by it. Backend checks do not rely on a client hiding a tool. Forged hidden
calls, duplicate JSON keys, case aliases of RPC identity keys, excessive nesting
and revoked or late results fail closed.

External MCP tool calls are denied before a plan. An upstream readOnlyHint is not
an independent host allowlist. After escalation, existing exact primary task
grants and approvals remain authoritative. Protocol initialization/discovery of
configured servers retains the existing behavior; this is not a promise of zero
upstream metadata requests or of third-party server internals having no effects.
An initial client inventory may remain cached: dynamic expansion of that primary
catalog is not certified. Existing worker projections are prepared separately by
the same scheduler. The filtered transport accepts finite POST, including finite
JSON/SSE responses; GET/DELETE and unsupported MCP methods do not bypass it.
Policy-bound tool results are buffered within the existing 16 MiB limit and
rechecked after the final byte, before delivery. A header-only check was found
insufficient by a flushed SSE response that revoked its capability before its
body; the failed regression proof is retained. Oversized results fail before
partial evidence reaches the client. The legacy streaming contract is unchanged.

Bootstrap can retrieve the static Core DAG protocol and record its bounded brief
without escalating authority. A read/metadata-only turn can complete after it.
Only orchestration_plan makes the monotonic transition to DAG validation,
**before** plan effects. A malformed, rejected, cancelled or incomplete plan
cannot return to simple completion. The same invocation continues using existing
bootstrap, plan approval, quotas, task grants, worker routing, isolated writes,
acceptance and integration. The primary never receives worker grants or writes.

Checkpoint persistence requires this turn's DAG and plan binding. The host checks
them under the session lock before writing checkpoint bytes or metadata, including
checkpoints produced by planning and integration. Recovery is unavailable for
this fresh-turn pilot. Cancellation requires a live DAG receipt and checks it
under the same lock before signaling a worker. The existing legacy checkpoint
and recovery contracts remain available for sessions without a receipt.

The existing 1 MiB final buffer remains. Excess fails with a fixed experimental
failure class, without truncating or emitting unvalidated partial text. Emit and
cleanup failures retain the host policy. Semantic correctness still requires the
task's applicable validation; a text answer does not pretend to have universal
semantic verification or an unnecessary authoritative artifact.

## Replay and compatibility

An admitted turn carries NoReplay and NoAutomaticFailover. JSON and SSE quota
cancellation stop it without an alternate executor, automatic handoff or second
Runner call. Success or failure consumes the existing request claim; duplicates
return 409. Real model/usage traces remain real client reports and are not relabeled
as local metadata. The official client's own transport retry policy is separate;
the first failed synthetic CLI attempt retained six requests inside one invocation.

With the flag off, the existing DAG/approval gates remain. The receipt is an
optional additive Session JSON field, and legacy ExecutorResult JSON stays
unchanged. There is no destructive migration. Finish or cancel an admitted turn
before changing the running binary; mid-turn downgrade/resume is not certified.
Linux amd64 v1/ARM64 build checks do not constitute an ARM or non-AVX runtime proof
of the new mode. The ARM route remains the existing workflow before admission.

## Validation and limits

[Sanitized validation evidence](evidence/adaptive-primary/validation.json) records
successful checks, failed preflights/harnesses and exact source/log hashes. Skipped
opt-in tests are NOT_RUN, not provider passes.

The hermetic checks cover language-independent admission, receipt revocation,
configuration/executable/turn changes, preparation failure, bounded final/emit
failure, duplicate 409, JSON/SSE quota cancellation, first-to-second-turn retirement,
strict institutional/host transport admission and stale MCP servers. Existing
DAG acceptance, permissions and integration tests remain required.

Pinned no-auth protocol proofs are separate scopes:

- Actual Core stdio child plus reviewed executable identity: no child admission
  flag; approval-gated DAG in the same turn; acceptance/integration; closed and
  stale child denied. The primary and its task are synthetic; this is not model
  inference.
- Official Codex exec plus synthetic loopback Responses: production argument and
  MCP-isolation factories, mandatory owned Core MCP, one scripted response, no DAG
  or workers, validated final, duplicate 409. The test executable substitutes only
  its private TestMain helper entrypoint for `_orchestrator-serve`; that helper
  calls production OrchestratorServe. This is not a complete frontend/TUI session,
  subscription authentication, paid API or real coding-task benchmark.

The initial owner instruction measured 4,114 UTF-8 bytes versus 8,313 for the prior
instruction. The 8,313-byte static DAG protocol is delivered only on bootstrap.
These are serialized instruction bytes, **not** tokens, session cost or speedup.
Tool schemas, lifecycle/probe overhead, runtime hashing and native caching still
matter. The global economic gate remains INCOMPLETE with prior adverse results
preserved. No new saving percentage is asserted.

The initial CLI proof failed 502 with six native requests. It remains FAIL. A
later proof used a complete synthetic usage envelope and explicit zero transport
retries; both complete-usage and absent-usage cases passed independently. Several
fixture/protocol/root changes occurred between those attempts, so the initial
cause is not established. Absence of usage is not a requirement for failure and
does not authorize invented consumption.

The first stricter child-shutdown race run also remains FAIL: protocol assertions
completed, but Wait was nonzero. An isolated repetition classified exit signal 13
(SIGPIPE) with empty stderr; that alone did not identify the writer. An intermediate
test-only stdout redirection passed but could hide a late write, so it was removed.
The final harness uses a dedicated private TestMain entrypoint that runs the
production stdio server without a Go test summary. Stdout stays exclusively MCP;
Wait must be zero; stderr is retained and race builds explicitly preserve a
nonzero runtime.RaceErrors verdict. Product cleanup and timeouts were not changed.

Review also reproduced a stale bootstrap overwriting an existing shared brief
after replacement, removal or closure of its receipt. All three initial failures
are retained. The checked, locked brief write now preserves both the prior brief
and session metadata; late plan persistence and stale child admission have their
own regression checks.

Claude (IVOAI-194), Antigravity, historical-turn admission, production institutional writes,
dynamic primary tool-catalog growth, full native-TUI behavior and actual economic
benefit remain unproven or outside this increment. Unset the opt-in for subsequent
launches to return to the existing workflow. Existing request claims and evidence
must not be deleted to force a retry.
