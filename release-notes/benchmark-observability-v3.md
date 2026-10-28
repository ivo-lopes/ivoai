# Benchmark observability v3 — IVOAI-138 / IVOAI-198

This test-only increment adds allowlisted terminal-tool counts and exact-payload
repetition observations, partial native collaboration metadata, and an optional
bounded Linux process sampler. **No new model inference was performed. The 126
historical comparative turns and their results remain unchanged.** It is not a
new runtime observability stack, scheduler, daemon, database or client configuration.

## Client projection and privacy

`NativeEventMetrics` schema1 counts terminal items by five fixed types:
command execution, MCP, file change, web search and collaboration. Failed or
declined terminal calls are still observations, not successful executions.
Legacy `ToolCalls` remains the command+MCP subset; `ToolResultBytes` covers command
aggregated output, not every result or session context.

For command/MCP items, repetition means the same decoded command or canonical
MCP server/tool/arguments within one invocation. Bounded RAM-only fingerprints
identify repeats; their digests, commands, names, arguments, paths, prompts and
IDs are never included in measurements. This does not identify equivalent work
with different arguments, cross-turn reuse, transport retries or safe replay.
Nothing is automatically reexecuted or served from a tool-result cache.

Completed item IDs deduplicate identical terminal retransmissions; conflicting
IDs, duplicate JSON keys, invalid UTF-8/numbers, duplicate/missing turn terminals,
incomplete active tools and exceeded bounds cannot produce successful evidence.
Unknown events/items make the stream partial. The decoder is limited to2MiB,
1,024 item identities, 48 nested levels and bounded containers/token counts.
`stream_status=complete` attests the checked framing, not complete native coverage.

The reviewed [official0.162.1 JSONL schema](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/exec/src/exec_events.rs)
provides four collaboration tool types. The [projection implementation](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/exec/src/event_processor_with_jsonl_output.rs)
omits several other collaboration operations and does not preserve typed retry
metadata in its generic errors. Therefore only observed projected collaboration
completions and distinct successful-spawn receiver IDs are counted. Recipient IDs
are not worker-process proof; absent recipient evidence remains null. Actual
worker/delegation/retry totals always remain UNKNOWN/null. Error-message wording
is never interpreted as a retry contract or persisted.

The real pinned client/companion with a loopback synthetic provider passed six
protocol cases under race: four positive controls each proved one distinct child
request, while two managed controls refused it. **All six JSONL streams reported
zero collaboration terminal items.** This directly demonstrates why an observed
zero cannot be relabeled as zero real workers. Typed fixtures separately cover
positive collaboration projections; they are not actual-worker measurements.
All official protocol cases used disposable homes, no authentication or inference.

## Optional Linux process observations

`--sample-processes` is OFF by default. It records only numeric aggregates for
the owned App-command root and discovered descendants, reading stat/children
metadata across bounded tasks. It never reads argv, environment, credentials,
descriptors, working directories or unrelated-process contents. PID/start-time
identities stay in bounded RAM and parent/identity changes are rejected.

Bounds: target interval100ms;128 processes and128 tasks/process;512 reads/sample;
3,000 samples and4,096 observed identities;8KiB/read;25ms cooperative window budget.
The real observed span/window and observer CPU/wall overhead are recorded.
Scheduling or procfs delays may exceed the cooperative budget; that loss is
reported. Unavailable observations are null. Coverage/status remain partial even
when no read loss is detected. The sampler does not pause/freeze tasks or scan
unrelated host processes, and it does not change the existing timeout/kill policy.

The result is a sampled process-count peak and an estimated RSS sum, **not an
exact simultaneous peak, total process count, unique physical RAM or whole-host
measurement**. Linux documents that [children enumeration can omit surviving children during exit races](https://man7.org/linux/man-pages/man5/proc_tid_children.5.html)
and [stat RSS is approximate](https://man7.org/linux/man-pages/man5/proc_pid_stat.5.html).
Short-lived/reparented processes, inaccessible/capped reads and sequential sample
windows add uncertainty; shared pages can be counted more than once. No upper or
lower bound on actual concurrent memory/process totals is claimed. This scoped,
bounded diagnostic trades completeness for privacy and cost.

Legacy wait4 maximum-individual RSS remains separate. Controller sampler CPU is
reported separately from App/child CPU; sampling can delay observation/reaping.
When GNU time wraps the App, the sampled root includes that wrapper; the existing
`LocalResourceSource` identifies the resource path used for each attempt.
Do not subtract its wall time mechanically, mix enabled/disabled conditions or
treat either RSS metric as the other. The cost/accuracy needs its own future
comparison before choosing this flag for an economic experiment.

## Versioning, checks and next comparison

Harness discovery, driver manifests/preflights/summaries now use version3. The
unchanged16-case catalog remains version2; event/process observations each use
schema1. The embedded contract hash now covers the observation decoder source.
Both runtime arms must be rebuilt with the same final harness/package/data.
Freeze the driver, catalog loader **and** sampler module, whose SHA is recorded.
Use explicit `--scenarios`/`--matrix16` discovery; the sampler flag itself also
forces discovery before any client starts. Old reports are not silently upgraded
or rescored; their original frozen readers/derivations remain available.

Tests cover enum/payload privacy, duplicate/conflicting/unknown/incomplete streams,
recipient versus actual totals, FIFO/limits/PID reuse/reparenting/nonleader-task
fixtures, live local parent/child RSS and runner integration. Python39 tests,
Go race for the fixture/App packages, vet for three packages, six pinned official
protocol cases, common baseline/candidate AMD64v1 builds and ARM64 cross-compile
passed. A full four-turn FAKE edit smoke passed with real executable oracles and
sampling; a missing-terminal FAKE client failed all four turns. The final
post-admission-guard FAKE two-turn smoke also passed. Their tool events and usage
are fabricated fixtures, not native economic results; sampled OS processes are
real local test/compiler/oracle processes.

[validation.json](evidence/backend-first/observability-v3/validation.json) preserves
source/tool hashes, privacy checks, exact coverage, manifests and scalar results.
The four-turn smoke precedes the final sampler-only admission guard; the final
two-turn smoke and focused regression cover that guard. No old artifact is
relabeled as a final-source proof. No inference should be launched from this
increment until both arms, pins, flags and observer overhead are reviewed/frozen.
All economic/model/backend/cache/quota conclusions remain unproven; global gate
is INCOMPLETE. Context sync remains BLOCKED / IVOAI-175. Rollback is an ordinary
revert of this benchmark increment; there is no persistent migration.
