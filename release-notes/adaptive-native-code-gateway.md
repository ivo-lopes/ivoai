# Experimental adaptive native CodeIntel Gateway

Managed Codex adaptive read-only code tools can now enter the existing Evidence
Gateway when its explicit feature flag is enabled. The existing MCP schemas and
typed CodeResult payload remain unchanged. The host carries its admitted receipt
controller into code-handler construction; no RPC argument or repository content
can supply that controller. Other sessions retain their established handlers.

Only normalized ask/skeleton/grep/callers/map/blast reads are cacheable. Diagnostics
and index actions (check/build/viz) execute through their original path, without
cache replay. Every wrapped query repeats the current receipt/provider-root/scope
and read-permission checks. Cache keys include session, organization, canonical
worktree, permissions/configuration/runtime identity, dispatch and query; source
inventory/content generation is checked before and after retrieval/recovery.
Distinct worktrees and workers are not asserted equivalent.

The Gateway stores the original typed JSON, recovers its exact protected artifact
under the current receipt owner and verifies byte-equivalent decoding before
return. Content sensitivity is checked before JSON escaping as well as on the
stored envelope. Gateway cache observations and actual wrapper duration are
separate from the original CodeResult's provider/cache/duration fields. These
original fields retain historical source-result meaning on a Gateway hit.

The feature requires both `IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY=1` and
`IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY=1` in the managed host. It remains opt-in;
disabling the Gateway flag restores the established code-handler construction
for newly admitted turns. Revoking that flag during an admitted wrapped turn
denies subsequent retrieval and recovery instead of silently changing authority.
The cache is process-local, while exact artifacts use the existing protected
WorkingContext/ArtifactStore. Session continuation does not imply cache survival.

On 2026-11-06 the actual public `ivoai codex` command, official native Codex
0.166.0 TUI and existing subscription authentication completed three read-only
turns in one session, followed by normal `/exit`, in 54.805 seconds. The first
code query missed, the identical second query hit under a fresh receipt, and an
explicit test-host source edit before the third query invalidated the cache.
There were three Gateway resolves, two provider inspections, one cache hit,
three closed receipts and no DAG or workers. The model returned the updated
source evidence. The isolated fixture's files remained intact apart from the
explicit host edit. This test does not claim cross-process cache reuse.

A separate official inference test completed in 30.916 seconds. It made two
admitted real code turns, recovered the exact typed protected result during the
second cache hit, and revoked that still-active receipt as its owner. The native
CLI completed normally with exit zero; Core withheld its final response, denied
subsequent exact recovery and rejected the duplicate request without a third
attempt, replay or automatic failover. This is receipt revocation, not provider
credential revocation, and uses the real managed bridge rather than the public
TUI command. Both proofs used the reviewed Linux amd64 0.166.0 executable and
companion pair. Certification of a newer integrated runtime is separate.

The public proof froze source `3864613c46318efbd13a1b59d1e5198c4b076866`;
the revocation proof froze `c33d4afbe70b2e9571b62df7ecf3453b6958400e`, which
only adds its test. Frozen source inventories, exact binary hashes, scalar
receipts and test logs are in
[the evidence directory](evidence/adaptive-native-code-gateway-20261106/README.md).
Affected package tests and focused race checks passed. Adversarial coverage
includes foreign scope/worktree/owner, permissions/config/runtime revocation,
in-flight source changes, concurrent deduplication, non-cacheable actions,
reference-only exact recovery, sensitive content and unchanged native MCP
schemas/payloads. Earlier failing test iterations are retained with their causes.

No token/time saving, global rollout, primary-to-worker reuse, Graft-specific
inference coverage or new-client parity is claimed. These native proofs used
the existing Direct fallback. Reported input/cache/output values are official
client invocation observations, not billing data or an OFF/ON economic
comparison. The negative 54-attempt cold-prefetch cohort remains unchanged and
is not rerun. The global economic gate remains **INCOMPLETE**.
