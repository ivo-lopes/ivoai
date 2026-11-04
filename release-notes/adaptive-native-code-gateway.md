# Experimental adaptive native CodeIntel Gateway checkpoint

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

This is a compiled checkpoint with existing focused regressions passing. New
adversarial tests, official native two-turn reuse, exact recovery, code-change
invalidation and revocation are still pending certification. No token/time saving,
global rollout, primary-to-worker reuse or new-client parity is claimed. The
negative 54-attempt cold-prefetch cohort remains unchanged and is not rerun.
