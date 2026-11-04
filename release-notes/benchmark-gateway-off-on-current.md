# Current Gateway OFF/ON: 54 real attempts, no economic gain established

The same frozen current-runtime binary ran nine existing scenarios in three
counterbalanced OFF/ON repetitions through the official Codex 0.166.0 CLI.
**52 of 54 attempts were correct.** Both arms failed the third Go-correction
pair with a failed native stream, 11 unclassified error events and no final JSON
or official usage. Their cause is unestablished; neither attempt was estimated,
retried, discarded or rescored. The following read-only attempts succeeded.

Among the **26 predeclared correctness-matched pairs**, Gateway ON used **6.05%
more input tokens and 11.51% more measured phase time**. The 10% input / 5% time
targets were not met. The unchanged driver reports subset/global **INCOMPLETE**
because the two attempts failed and seven matrix lifecycles remain unavailable.
No saving or causal regression is attributed to the Gateway from this shared-host
observation. All 54 attempts, including missing usage and unfavorable rows, remain
in the [immutable evidence](evidence/gateway-off-on-current-20261104/validation.json).

| Observation | OFF A | ON B |
| --- | ---: | ---: |
| Actual model attempts / correct | 27 / 26 | 27 / 26 |
| Official-client input, 26 reported rows each | 706,970 | 749,717 |
| Cached input, already included above | 360,576 | 415,104 |
| Output tokens, 26 reported rows each | 3,406 | 3,708 |
| Phase time, all 27 attempts, ms | 364,281 | 401,868 |
| Phase time, 26 matched-correct attempts, ms | 326,749 | 364,347 |
| Client command/MCP events, all attempts | 31 | 33 |
| Reaped-command user/system CPU, ms | 92,189 / 26,069 | 95,407 / 26,190 |
| Maximum individual-process peak RSS, bytes | 262,287,360 | 266,010,624 |
| Observed Gateway resolves / hits / misses | 0 / 0 / 0 | 12 / 0 / 12 |

The ON arm did not exercise Gateway retrieval in 15 of its 27 invocations.
The twelve observed resolutions were cold misses; no warm reuse was measured.
Provider cache conditions differ and remain unverified. Requested selection was
gpt-6.1-sol/low; effective backend/model identity was not independently attested.
Token reports are official-client invocation usage, not billing or quota data.
The two failed invocations have unknown usage, not zero usage. Native retries,
workers and delegations are not proven by the partial event projection.

The bounded process sampler retains observed identities/RSS/process peaks,
explicit partial coverage and read-loss/cap conditions. Its sampled RSS sum can
include shared pages; wait4 peak RSS is an individual-process maximum. Neither
is an exact simultaneous physical-RAM total. Admission/prefetch/provider/validation
phase timing, App-process elapsed time and whole observer interval are distinct.

Source `29f0bb0a8` builds runtime `a6df869f5` without production-source changes.
The existing PR68 admission, catalog, schemas, permissions and executable edit
oracles were reused. A/B were byte-identical compiled App test executables; the
only host treatment was Gateway OFF/ON. Node26.13.0 was checked against its pinned
archive, and the CI73 Graft0.21.1-ivoai.2 archive/inventory (2,376 files) was verified.
All source and runtime input bytes matched before and after execution. Existing
subscription authentication was preserved without copying credentials, changing
personal configuration or falling back to a paid API.

A four-invocation API/implementation smoke first passed AB/BA with actual OFF0 /
ON1 Gateway resolutions. An offline sixteen-case preflight reported nine READY,
five UNSUPPORTED and two BLOCKED, with zero model attempts. The final cohort
contains exactly the 54 planned supported-case invocations. The initial offline
host-check command omitted opt-in flags and correctly skipped; that NOT_RUN log
is retained alongside the subsequent explicitly enabled passing host checks.
Driver Python85 tests are synthetic mechanics and are not counted as inference.

The host had 12 CPUs. Initial root browser/Admin-Go work overlapped the start;
later AGY race/vet/compile activity overlapped another recorded window. Exact
reported file timestamps, derived-time limitations and host load/memory/process
snapshots are retained. No row was omitted because of shared-host activity.

This cohort evaluates the existing App prefetch path. The current adaptive
read-only native CodeIntel path still bypasses Gateway retrieval; connecting that
path safely is the next functional increment. Workers, persistent reuse,
interrupted recovery, concurrency, a representative large project, institutional
Context and ai-memory history remain separate matrix contracts. Mindsite
CONTEXT_SYNC remains BLOCKED/IVOAI-175; this report asserts no ingestion or parity.
