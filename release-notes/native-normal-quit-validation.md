# Public native startup and normal quit — scoped validation

One separately reviewed, single-use public `ivoai codex --planner codex` startup
passed with only `/quit` + Enter. It used clean source
`55ec7ab98683bf0c8784b93baebc966f2910772b`, Core SHA256
`5973758fb84bb2abe77ca39cbdd4f48d309ef7433d06b34fe5c646d55f54f6c7`, and
the pinned official Codex 0.162.1 CLI/companion pair. It is an observation of
those inputs, separate from the candidate's documentation/embedded-site bytes.

The driver waited for the real native composer and conversation phase, typed
only `/quit`, waited for the fresh exit menu, and sent one Enter. Natural Core
Wait returned0; Session and frontend both persisted completed with exit0.
Duration was2.147s. No Core attempt, question, worker, task, decision, DAG,
provider execution trace, checkpoint or failover appeared. Fixture content and
metadata stayed intact; the owned SID residue guard found zero members. The
driver sent no SIGINT, SIGTERM or SIGKILL. A bounded failure-only cleanup was
prepared but was not used.

This operation issued no manual login/status command and no model prompt.
Ordinary Core startup authentication/catalog/quota metadata probes still ran;
their aggregate retries or quota effects are not certified. Requested model and
effort were startup declarations only. Tokens are NULL/NOT_APPLICABLE for this
zero-prompt observation, not provider usage0 or a savings result. No personal
configuration or credentials were extracted, copied or rewritten. Live terminal
bytes stayed in bounded RAM and were discarded; no raw transcript or backend
stderr is archived.

The [byte-preserved result](evidence/native-normal-quit/zero-prompt-result.json)
has SHA256 `8291154696c0674626048fe588c96a4fb87325cb8f1dd9b9cde13012917082a0`.
The [validation receipt](evidence/native-normal-quit/validation.json) has SHA256
`8c82be55e862c119efd0bb5247f0683a3a9ff303a23f4d490890332eeda0fa69`.
Only four allowlisted receipts were copied, with their
[hash manifest](evidence/native-normal-quit/artifacts-sha256.json). The source
runner, seed configuration, authentication, private sessions and raw live traffic
are not included.

This resolves the concrete question whether supported public startup and normal
idle quit can finish as completed0. No production status-enum, cancellation
mapping or historical-session migration is introduced. A separate offline
no-auth native `/quit` contract had also passed with Wait0 and active host context;
it is not reassigned to this public run.

The earlier supported real math turn remains TURN/ORACLE_PASS with intentional
parent-only SIGINT cleanup and persisted failed/fallback1. That value maps a
context error and does not establish a directly measured native process exit1.
Its immutable summary SHA256
`44df9bbf2a7c1a27d06e684169c558fba22f6d5d022d4f16b3011c61ed5819e4`
is unchanged. A real-model turn followed by normal quit in the same session has
not been run. The original `0bab9b34` failure, UNKNOWN cause, scoped Core MCP
child-cleanup uncertainty and every adverse observation remain preserved.

This report supersedes the pending idle-normal-quit classification in the
[earlier composition snapshot](v0.15.0-composition58-59.md); its historical
receipts are unchanged. Root independently verified that snapshot's31 artifact
hashes and587 Go/module files against its runtime epoch and PR59 source. The
report's first source bullet called122642e1 the final documentation snapshot;
the runtime-check epoch is122642e1 and the later documentation epoch is615a0a12,
as its validation section and receipt already record.

Publication remains HOLD for the separate managed MCP configuration-context
correction, investigation of the current native-close CI failure, exact combined
source gates and Release/distribution validation. PR56's run40795567122 has13
passing jobs, one failed quality job and two skipped jobs; its closed-writer
failure is not retried or classified as a fixture defect without evidence.
The [draft release notes](v0.15.0.md) retain other compatibility limits. No new
tag, asset, installation or production deployment follows this observation.

No canary or synthetic fixture joins the126 comparative backend-first turns.
Global economics remains INCOMPLETE, Context sync BLOCKED/IVOAI-175, and general
native client/TUI/model parity remains unproved. Published v0.14.0 is unchanged.
