# Matrix16 edit contracts: twelve real Codex turns — IVOAI-198 / IVOAI-138

**This cohort validates the edit contracts and their executable oracles through
the official headless client. It does not evaluate an activated optimization.**
Both arms used native workspace inspection/editing: injected evidence, source
bytes, prefetch calls and structural calls were zero in all twelve rows. Their
aggregate host prompts were both 6,009 bytes. No task intent, prompt, route or
condition was changed after observing this. Gateway/Graft savings were not
exercised or established.

All twelve planned turns ran, with six correct results in each arm and six
matched pairs. The unchanged script's **subset economic diagnostic is FAIL**
against 10% input / 5% phase-time targets; **global acceptance is INCOMPLETE**.
The 36-token aggregate difference has no attributed cause. These observations do
not establish equivalent host/provider conditions or global/session savings.

## Frozen execution and quality

The approved comparison ran on 2026-10-28, 16:25:30.999392–16:29:58.261392 UTC,
after a fresh offline preflight. It used the same two synthetic tasks and three
counterbalanced repetitions: ABBA, BAAB, ABBA. No attempts were discarded, no
host retries occurred, and no prompt tuning or favorable rerun followed a result.
Native retries, workers, delegations and HTTP-request counts were not instrumented.

The runtime baseline is v0.12.1 / `3ff6db8bb0174b1ae6f781f6754e2653a6dae5db`;
the candidate runtime is `c6fb6675672cb85e1dea6704af68c6886553911b`. Both compile
the identical final test-only harness/package/data from
`6294172995385849154cc6ec6d106b4448c0f56c`, including the special-permission
snapshot correction. The candidate excludes later administrative/release
patches; their effects are not attributed to these binaries. These are compiled
App test binaries, not a comparison of published frontend executables.

Both use Go1.27.3, CGO0, AMD64v1, `-trimpath -buildvcs=false`. Binary SHA-256:

| Input | SHA-256 |
| --- | --- |
| Baseline App test | `b2ff934c421d2776be1ac76f54215cf564544e3bf42577c4ada4b0e23134ef92` |
| Candidate App test | `ad1bb4545ce4074442d30e92bc14e7ce102cb6e8c0b67b9678d8cb45ab011443` |
| Shared C6 code-sandbox helper | `16894a928d8b454c4256ed7fb4b6108974446e9c3f5c52b6671234dafb7928be` |

The official Codex0.162.1 pair was rehashed against its pinned release archives
(CLI `628e3548…`, companion `ec718306…`); personal Codex0.165 was not substituted.
Node26.11.1 matches the exact official archive required by the reused Graft013
metadata. Graft's archive/checksums, all 2,376 inventory files and ten native ABI
bindings were verified; a no-network library/native/WASM smoke passed. It remained
available as a frozen shared input, with zero calls in this cohort. Full hashes
and the source projection are in
[freeze-public.json](evidence/backend-first/matrix16-edit-live-v1/freeze-public.json).

Existing official-client subscription authentication was used without extracting
or copying credentials, changing personal configuration, or paid API fallback.
An earlier read-only, network-isolated official status call reported ChatGPT
authentication; actual tasks then completed with client-reported usage. Requested
model/effort were `gpt-6.1-sol` / `low`; effective backend/model, quota and cache
equivalence remain unverified. Matching requested names is not backend attestation.

Every result required exact typed answer JSON and the independently validated
patch: one Go file or three TypeScript files, unchanged remaining bytes/Git
metadata/permissions/directories, restricted source shape and an executable
oracle over a private immutable copy. All twelve edit oracles passed. This proves
the declared narrow synthetic contracts, not arbitrary project correctness,
security certification, native TUI parity, Claude or Antigravity support.

## Observed consumption and time

| Metric, six turns per arm | Baseline A | Candidate B |
| --- | ---: | ---: |
| Client-reported input tokens | 286,352 | 286,316 |
| Cached input, already included above | 201,600 | 173,952 |
| Client-reported output tokens | 2,181 | 2,332 |
| Host `TotalMS` measured phases | 133,587 | 130,540 |
| App-process `LocalElapsedMS` | 134,523 | 131,207 |
| Provider duration, ms | 99,620 | 97,958 |
| Independent validation duration, ms | 33,955 | 32,574 |
| Local user / system CPU, ms | 70,161 / 13,638 | 70,505 / 14,451 |
| Maximum individual-process RSS, bytes | 265,293,824 | 265,015,296 |
| Counted native tool events | 17 | 18 |

Observed reductions are 0.01257% in input, 2.28091% in measured phase time and
2.46501% in App-process wall time. Cache is a subset of input, never added to it.
Usage comes from official-client `turn.completed` events, without independent
billing/provider attestation. Monetary cost and quota consumption were not measured.

| Repetition | Input reduction | Phase-time reduction | Process-time reduction |
| --- | ---: | ---: | ---: |
| 1 | −14.52272% | 7.01787% | 7.48545% |
| 2 | 14.52406% | 2.45599% | 2.42591% |
| 3 | 0.03560% | −3.27997% | −3.23323% |

Negative reductions mean increases. Only one repetition improved both input and
time. All pair-level results, including unfavorable ones, are retained. No cause
is assigned to the opposing input changes or the small aggregate difference.

`TotalMS` covers prefetch/provider/validation after snapshot, oracle identity and
containment-probe admission. `LocalElapsedMS` covers the App subprocess including
that admission; Linux `wait4` supplies reaped-child CPU and maximum process RSS,
not simultaneous process-tree RAM or whole-host attribution. Whole native-session
time, local process counts and tool-name repetition frequencies were not measured.

The host was shared, with 12 logical CPUs and no reservation or affinity.
Load averages started at 10.36/6.16/4.63 and ended at 4.39/5.81/4.95. Per-turn
load/timestamps are retained. Time is an observation of this changing shared
cohort; even a numerical script PASS would not prove equivalent conditions.

## Evidence, reproduction and remaining work

[validation.json](evidence/backend-first/matrix16-edit-live-v1/validation.json)
indexes the unchanged allowlisted rows, per-attempt files, original driver
summary/manifest, discovery, source/pin projection and scalar supervision record.
Raw prompts, answers, native stderr, configuration values, credentials and auth
scope values were not retained in these artifacts. Input identities were checked
before/after preflight and after execution; a metadata watch detected no changes
during the run. The source worktree remained clean throughout execution.

The independent observation derivation reproduces exactly:

```sh
python3 release-notes/evidence/backend-first/matrix16-edit-live-v1/derive_observed_metrics.py release-notes/evidence/backend-first/matrix16-edit-live-v1
```

A future live reproduction must rebuild the same common source over both runtime
snapshots, revalidate the full pin manifest, run the existing driver with the two
scenario IDs and `--repetitions 3`, and use a new private output directory. The
fixed final native controls, model/effort and Gateway-off condition are recorded
in the manifest. A new live run consumes the official subscription; this artifact
does not automatically rerun it or weaken any missing-provider gate.

There are now 126 historical real comparative official-client turns across
heterogeneous cohorts: 114 previous plus these twelve. Their performance is not
combined as a causal gain. Nine scenario contracts have real execution evidence
across different snapshots/conditions; this is not a completed equivalent
16-scenario matrix. Five lifecycle/large-project cases remain UNSUPPORTED and
two institutional cases BLOCKED. Context sync stays BLOCKED / IVOAI-175.

The absence of retrieval for these edit intents informs future adaptive source
selection and coverage. It is not repaired by retrospectively changing this
cohort's prompts, routing or interpretation. No production default, client
configuration, release or architecture cutover is changed by this evidence commit.
