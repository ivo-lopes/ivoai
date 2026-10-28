# Seven-case real comparison: economic targets failed

This additional comparison preserves all **42 official Codex CLI attempts**
(seven scenarios, three alternating AB/BA repetitions, 21 pairs). Every strict
objective oracle passed in both arms. The candidate used **2.85% more input
tokens** and **1.44% less elapsed time**; no repetition improved both measures.
The subset diagnostic is **FAIL** against the predeclared 10% / 5% targets. The
global 16-scenario gate remains **INCOMPLETE**. No attempt was retried, excluded
or rescored into success; all prior 72 comparative attempts remain unchanged.

| Client-reported / measured value | A, v0.12.1 runtime | B, candidate runtime |
| --- | ---: | ---: |
| Correct / attempted turns | 21 / 21 | 21 / 21 |
| Input tokens, including cache | 533,847 | 549,059 |
| Cached input tokens, subset of input | 243,584 | 268,288 |
| Output tokens | 1,625 | 1,658 |
| Prefetch + provider elapsed ms | 136,153 | 134,193 |
| Observed model tool calls | 15 | 17 |
| Host prefetch ms | 1,124 | 1,166 |
| Provider-command ms | 135,015 | 133,014 |
| Linux command user CPU ms | 30,814 | 30,550 |
| Linux command system CPU ms | 12,067 | 12,015 |
| Maximum individual-process peak RSS bytes | 397,352,960 | 385,765,376 |

The CPU/RSS values cover the test command and descendants reaped by Linux
wait4. Peak RSS is **not** simultaneous summed process-tree RAM. Whole-command
elapsed totals, including test/fixture setup, are separately retained as
136,649 / 134,682 ms. Process count was observed only for the whole host before
the experiment; per-attempt process counts and repeated-command identities
remain unknown. This primary-only harness does not measure workers or
delegations, warm/persistent reuse, client-TUI lifecycle, interruption or other
providers. Monetary cost and quota consumption remain unknown.

The observed cache totals differ and cache equivalence was not established.
Requested selection was gpt-6.1-sol/low; independent backend/effective model
identity was not attested. Editing, remote CI and ordinary host activity could
continue; other agents held new heavy local checks, but this was not an idle
host. Timing variation includes one repetition with 14.82% higher candidate
elapsed and another with 12.96% lower elapsed. No causal saving is attributed.

The dependency/Python case observed 3 / 5 tool calls and 89,999 / 95,116 input
tokens across its three repetitions. Monorepo had zero prefetch bytes in both
arms and 6 / 6 tool calls, yet 133,301 / 140,921 input tokens. Those differences
are not proof of a Gateway effect. The no-parser and non-code cases also had no
host prefetch. The [resource diagnostic](evidence/backend-first/matrix16-v1/resource-diagnostic.json)
retains every scenario without excluding adverse rows or changing gate scoring.

## Frozen source and equivalent executable contracts

The public runtime baseline is v0.12.1, source
`3ff6db8bb0174b1ae6f781f6754e2653a6dae5db`, with **only** the common frozen test
harness and `internal/benchmarkfixtures` package added in local commit
`8f7be69f`. Its 19 shared files match the candidate byte-for-byte. Reconstruct
that tests-only tree from the public tag and the same files in public commit
`fe2e65806e92121fd7440f106a2d55b20a837b52`; no production baseline source is
changed. Candidate runtime is main `49445919` plus that frozen test scaffolding.
These are App test binaries, **not full published executable comparisons**.

Both binaries were compiled before inference with Go1.27.3, CGO disabled,
GOAMD64=v1, `-trimpath` and `-buildvcs=false`:

- A SHA-256: `1217a92bf3744ee485f5e4595e068dc84780315070e9368769b323bb20ef789b`.
- B SHA-256: `2aceba564734c97b42dafaa86e83c9d4a0821d25fe90755831a9d654ddd0df57`.

The same official Codex0.164, pinned Node26.11.1, reviewed Graft distribution,
sandbox helper, explicit PATH, schemas, fixture bytes and process environment
were used on both sides. Exact source/fixture/binary/helper/distribution hashes
are in [provenance](evidence/backend-first/matrix16-v1/provenance.json) and the
[driver manifest](evidence/backend-first/matrix16-v1/manifest.json). The old
offline-only binary built without trimpath is not substituted for this measured
candidate. Both production implementations retain Graft; this does not compare
Graft removal or alternate code-intelligence providers.

`IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY=1` was explicit for both processes; the old
baseline ignores the feature it does not implement, and the candidate exercises
it. All client environments use the frozen explicit allowlist, with no arbitrary
API keys, MCP/OTEL, loader/Python, proxy or custom CA inheritance. Only the
official client accesses its own existing authentication. Personal Codex config
bytes and mode remained unchanged. The agent/harness did not read or copy auth
files. Config contents and their fingerprint are omitted from public evidence.

Both binaries passed offline capability discovery and the four new host-prefetch
paths before any inference. The actual-input
[16-scenario preflight](evidence/backend-first/matrix16-v1/matrix16-preflight.json)
correctly exited nonzero: 7 READY, 7 UNSUPPORTED, 2 BLOCKED and zero provider
attempts. READY was then exercised only for the seven declared read-only cases;
it never certified the nine unavailable lifecycles. See the
[readiness report](benchmark-matrix16-readiness.md) for their exact blockers.

The completed [42 raw numeric rows](evidence/backend-first/matrix16-v1/runtime-comparison.json),
[subset preflight](evidence/backend-first/matrix16-v1/preflight.json) and
[original summary](evidence/backend-first/matrix16-v1/summary.json) retain every
attempt. Final checks confirmed source still at fe2e6580 and a clean worktree
before adding this evidence commit. New native callbacks, RTK, VERSION turns and
reported-usage PR36 were not used in this experiment. This is new negative
evidence for IVOAI-198/138, not completion of the roadmap or approval of global
economics.
