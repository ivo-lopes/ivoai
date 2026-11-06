# Adaptive evidence fidelity with the default Caveman installation

The managed adaptive Primary now carries the existing `EXACT` compression
contract to its CLI runner when Evidence Gateway or authoritative shared
knowledge is enabled. The real Caveman provider returns Direct before probing
or starting its proxy for that request. Other requests retain the existing
`Compressible` default; the stored default provider remains Caveman. No new
scheduler, planning protocol, admission path or authorization grant is added.
The adaptive and Gateway feature flags remain opt-in.

Previously the CLI runner unconditionally labeled every compression request
`Compressible`, including authoritative typed code evidence. Three small real
Gateway payloads happened to survive the installed proxy, but that is not a
safety guarantee for arbitrary evidence. Preserving the existing fidelity
contract closes that gap without changing the global compression configuration.
If a provider nevertheless reports compression for a non-compressible request,
the runner closes its lease and rejects the turn before launching the executor.

A turn-local policy value binds observation fidelity to its turn, attempt,
frontend, executor, native thread and process trace. The Mapping callback takes
that value **before** entering `store.Update`; it never acquires the turn-state
mutex under the session-store lock. Stale or foreign mappings cannot borrow the
current turn's `EXACT` selection. The single final compression event records
requested Caveman/default, effective Direct, `EXACT`, `bypassed`, and no fallback
reason. Actual compressed output is never described as a bypass.

## Real execution and preserved controls

The existing public FullCLI Gateway scenario was reused, with one opt-in
installation helper. The helper installs the reviewed Caveman proxy/MCP pair
through the real immutable Installer and verifies production/test compiled
metadata, archive SHA256, current source identity, idempotence and actual proxy
readiness. It serves the already-reviewed local archive only for its compiled
versioned URL; it does not substitute a provider or accept an arbitrary hash.
Caveman is `4.0.2-ivoai.1`, archive
`25aa3e352c301fdaf303001487663d71a874e50f9211232a9cdc293b503b3b65`.

All three inference sequences used the reviewed official Codex 0.167.0 Linux
amd64 CLI and Code Mode pair, the public production Core entrypoint and existing
subscription admission. HOME/CODEX_HOME and personal authentication remained in
place. Only Core XDG paths and fixture repositories were isolated. No credential
was copied and no API-key or paid-API fallback was used.

| Recorded sequence | Result | Wall time | Observed compression |
| --- | --- | ---: | --- |
| Initial report-path precheck | FAIL before admission, zero model turns | 0.00 s | None; private parent mode was corrected |
| Baseline, before fidelity propagation | PASS, three official turns | 57.63 s | Caveman used in all three small-payload turns |
| Initial EXACT candidate | PASS, three official turns | 51.82 s | Direct, three coherent EXACT bypass events |
| Final source, after lock-order and identity checks | PASS, three official turns | 54.24 s | Direct, three coherent EXACT bypass events |

Each real sequence kept one Core session, frontend and native thread; completed
three attempts/receipts without DAG or workers; exited the native backend and
public wrapper normally with status zero; and preserved fixture data except for
the deliberate host code update. Gateway observed three resolves, one hit, two
misses and two provider inspections. The third turn invalidated the cached
evidence after the host changed the code source. The final saved compression
configuration still equals `config.Default().Compression`.

The installation/readiness-only control passed separately in 2.23 s and performed
no inference. The failed report-path precheck and both earlier inference receipts
remain alongside the final receipt; historical results are not rewritten.
[Receipts and source identities](evidence/adaptive-gateway-caveman-20261106/source-manifest.json)
bind the tested source files, official executable pair and final production
binary. Raw model/PTY transcripts, auth data and private session storage are not
published.

## Validation and limits

Focused App/bridge/Caveman package tests passed. The final targeted race checks
passed for concurrent policy snapshots, stale/foreign identity rejection, all
three exact runner turns, legacy zero-value fidelity and fail-closed rejection of
a violating provider. The exact runner test uses the real Caveman provider and
proves that its integrity hook, process probe and proxy runtime creation are
never reached; its executor is an offline fixture, separately from the official
inference results above. Focused `go vet` passed.

This is a functional fidelity fix, not an economic benchmark. Reported token
values retain the official client's `cli_invocation` scope and are not summed
into an invented session total. The runs were sequential, single observations,
not AB/BA replications under equivalent cache conditions. Their elapsed times
and small-payload baseline do not prove session savings, general compression
safety or quality on unrelated tasks. The global economic gate remains
**INCOMPLETE**. No new cross-process resume, platform or client certification is
claimed by this specific test; the surrounding rollout restrictions remain.

Rollback uses the existing feature flags for future turns. Preserve admitted
receipts and attempt history; deleting state to replay an admitted operation is
not rollback. Pending integration, hosted CI and release publication belong to
the parent delivery and are not claimed here.
