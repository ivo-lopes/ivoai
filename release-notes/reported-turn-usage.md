# Reported executor usage — IVOAI-138 / IVOAI-198

The normal managed Codex CLI runner previously discarded the `usage` object in
`turn.completed`; only the separate live benchmark harness consumed it. The
Core `ExecutionTrace` now has optional `usage` and `usage_status` fields, using
the existing progress/final trace and session/attempt persistence paths. This
adds no parallel telemetry store, observer, scheduler or provider call.

The report retains input/output counts and an optional cached-input count.
`basis=client_reported`, `source=codex_exec_turn_completed` and
`scope=cli_invocation` identify its origin and aggregation boundary. Cached
input is included in input, never an additional charge. Missing cache remains
unknown; a reported zero is retained as zero. No byte-based token estimate,
price, billing cost, quota consumption or whole-session saving is inferred.
An official client's report is not independent attestation of backend/model
identity or provider accounting. Requested/effective model metadata keeps its
existing separate contract.

Counters must be JSON integer lexemes in the nonnegative JavaScript-safe range;
fractions, exponent notation, strings, booleans, missing required counters and
cached input larger than total input are rejected. The private CLI scanner uses
`json.Number` to avoid silently rounding a fractional or oversized counter.
The exported `ScanJSONLines` callback retains its historical float64 behavior.
Unrelated usage fields are discarded rather than persisted.

An identical repeated terminal report is retained once, without addition.
Conflicting reports clear usage and set `usage_status=conflicting`; malformed
reports clear it and set `invalid`. Subsequent apparently valid events cannot
repair that invocation's failed provenance. These statuses describe metric
availability and do not change otherwise valid executor output. No report
preserves the old JSON shape: optional fields remain absent, meaning unknown.
Progress observers receive a copy, including the nullable cache counter.
An absent or `null` usage object means no new report: it neither fabricates zero
nor removes a previously observed valid report. A present non-null malformed
object instead invalidates metric provenance as described above.

This slice covers managed `codex exec --json` only. Claude, Antigravity, worker
aggregation, the native presentation App Server, retry/session totals and phase
CPU/RAM measurements are separate pending work. It does not backfill historical
traces or alter any retained economic dataset. Existing estimated compression
metrics remain separate from these reported counts.

Validation includes normal Core/bridge contracts, invalid/precision/cache/
duplicate/conflicting-report cases, observer mutation isolation and trace
allowlisting. An opt-in integration test launches the actual official Codex
0.164.0 executable (SHA-256
`b93f6811d3f653f0fb83be4050d857fb9abbb111dfa2ce160563327daff7f306`)
with temporary HOME/CODEX_HOME/XDG, a cleared environment, no personal login and
one local synthetic Responses request. The real client mapped the fixture's
100 input / 60 cached input / 2 output to the retained trace; the test passed.
This proves protocol mapping, not real inference, subscription/quota accounting,
economic acceptance or another client's parity. Without an explicit official
binary path it is NOT_RUN, never counted as a real-integration success.

Reproduce the explicit protocol check with:

```sh
IVOAI_LIVE_CODEX_PATH=/absolute/reviewed/codex go test ./internal/opencodebridge -run '^TestLiveCodexReportedUsageLocalProvider$' -v -count=1
```

The addition is backward compatible for existing JSON consumers that accept
optional fields. Rollback is a normal code revert; existing traces can still be
read by old code that ignores unknown fields. No credential, persisted schema
migration, client configuration or default orchestration policy changes.
