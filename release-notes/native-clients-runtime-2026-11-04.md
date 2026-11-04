# Native client consolidation — 2026-11-04

## Claude Code 2.1.318

The managed installer pins the official v2.1.318 release for Linux amd64 and
ARM64, with the independently downloaded archive SHA-256 for each platform.
The amd64 executable reports `2.1.318 (Claude Code)` and its digest is retained
in the protocol proof. ARM64 archive integrity was checked; ARM64 execution was
not tested on the amd64 host.

The existing worker factory was exercised against that official amd64 client
inside a private bubblewrap network/HOME namespace and a synthetic loopback
Anthropic endpoint. All five mandatory cases passed. Positive controls prove
that native delegation really executes when exposed; the managed factory
advertises only `Read`, `Glob`, and `Grep`, blocks both `Agent` and `Task`, and
performs zero delegated child requests. This is an offline protocol proof,
**not provider inference, subscription certification, or economic evidence**.
The previous-version evidence remains unchanged.

Run:

```sh
scripts/test-claude-worker-tools.sh --archive /path/to/claude-linux-x64.tar.gz \
  --output /owned/private/proof-directory
```

The user explicitly has no Claude account and requested that real Claude
inference remain pending. No authentication was requested, no paid API fallback
was introduced, and no personal Claude settings were modified. Managed Claude
TUI interception, adaptive frontend admission and real inference certification
remain pending under IVOAI-194/197. Existing direct launching and bounded Claude
workers remain available under their existing contracts.

Evidence: [offline summary](evidence/native-clients-2026-11-04/claude-offline/summary.json).

## Antigravity: managed Core entrypoint (experimental)

The public command is a bounded **headless** integration with the official ACP
interface. It does not intercept, attach to or transparently control the native
Antigravity TUI:

```sh
IVOAI_EXPERIMENTAL_ANTIGRAVITY=1 ivoai antigravity run \
  --directory /path/to/repository --prompt 'Explique a função Add deste projeto.'
```

`--follow-up TEXT` accepts up to eight related turns under the same managed host
lease. Self-contained requests can finish without a DAG or worker. A complex
request uses the same Primary, `orchestration_bootstrap`, existing plan protocol,
Core scheduler, approved worker worktrees, ArtifactStore/ResultRef and integration
engine. No OpenCode process, second scheduler, subscription credential copy or
paid API fallback is involved.

The path remains opt-in and pins the reviewed installed Antigravity **1.5.0,
Linux amd64** executable SHA-256
`b9043860a06a3bfa70b68f06c3d4527abc16207767f14ae9d1e06e3026081862`.
This records the installed artifact used by the proofs; it is not an assertion
that an independently downloaded, signed distribution was audited. IVOAI does
not redistribute or install Antigravity in this increment. Other versions and
platforms require their own certification.

The private native agent exposes the documented `view_file` capability and the
Core-projected MCP servers. Native shell, writing and subagent tools are excluded.
`view_file` approval permits only generated MCP schema/instruction files under
the private profile; repository evidence is retrieved by the existing Code
Intelligence provider. Source responses remain untrusted data. A denied native
operation or lost Core grant withholds the answer and prohibits replay/failover.
Sessions, receipts, original attempt journals and process ownership use the
existing Core contracts. Normal completion uses official `session/close` when
advertised, then stdin EOF; success requires normal exit without a signal.

The first HTTP-MCP/custom-agent proof failed: tools were discovered but absent
from the active model's toolset. The existing authorized HTTP projections now
reach this client through `_native-mcp-proxy`, a small SDK stdio transport. It
preserves the original schemas/annotations/results and rechecks the same receipt
before and after calls. Short-lived loopback capabilities remain in private
0600 files scoped to the receipt, never arguments or telemetry; the normal
runtime cleanup removes them. It is not a new policy engine, evidence cache,
provider integration or planning protocol.

The configured Core approval policy remains authoritative. Existing Bridge
permission endpoints handle both plan/routing decisions and Gateway requests.
An interactive terminal can approve an exact ID by typing `yes`; without a
terminal, an approval request is denied. `permission_mode=full` does not approve
plans or unknown/mutating tool grants. Controlled automation can explicitly
configure its existing Core policies in its own isolated config.

Known boundaries:

- Same-process continuation is certified for eligible read-only turns.
  Cross-process native resume is intentionally unavailable while ACP does not
  expose an account identity epoch. Post-DAG follow-up remains separately gated.
- Executable native workspace customizations (hooks, MCP configs, agent or
  plugin directories, including relevant ancestors) fail admission until their
  exclusion or Core migration is certified. User files are not changed.
- Models are checked against the runtime catalog. Third-party model access,
  effort combinations, quotas and other platforms are not inferred from the
  main CLI's help output. No authoritative Antigravity quota API is claimed.
- The functional DAG proofs ran a real Codex 0.166 worker and integrated its
  change, but its sandbox could not find `go`. The Primary declared that limit;
  it did not claim a successful worker test. Independent host `go test ./...`
  passed after integration. Worker toolchain projection is a separate Core fix.
- A metadata regression paired automatic selection with an empty
  `requested_model`, so the existing Session validator refused persistence and
  withheld answers. That failed epoch is retained. The correction preserves the
  literal `auto`, requested/observed model, catalog revision and owned mapping.

Reproduce public inference (consumes the existing authorized subscription):

```sh
IVOAI_ANTIGRAVITY_CLI_LIVE=1 go test ./internal/app \
  -run '^TestAntigravityPublicManagedCLI$' -count=1 -v
```

The default test run skips this opt-in inference proof; a skip is not a
certification pass. Offline protocol/adversarial tests are separately labeled.
The official CLI proof covers a natural prompt, owned continuation, real Direct
Code Intelligence observation with verifiable lines, and denied data access.
Development cohorts, including failures and interrupted attempts, remain in
[evidence](evidence/native-clients-2026-11-04/antigravity/development-epochs.json).
Timing in those development scripts includes the external verification phase,
not solely model time. Reported tokens are official-client reports scoped to one
invocation; unavailable counters remain unavailable. There is no aggregated
session-saving claim, AB/BA conclusion, or passed global economic gate.

Official interface references: [ACP](https://antigravity.google/docs/ide/extensions/acp),
[permissions/settings schema](https://antigravity.google/schemas/v1/settings.json),
[custom agent tool allowlists](https://antigravity.google/docs/subagents), and
[ACP session lifecycle](https://agentclientprotocol.com/protocol/v1/session-setup).

The corrected public epoch passed all three mandatory scenarios with actual
inference: natural query plus same-session continuation (15.799s), Code
Intelligence Direct with `calc.go:4` evidence (20.700s), and denied read with no
leak or DAG (172.262s). These are measured whole CLI invocations. The two success
cases require exit0, no signal, owned native IDs, and unchanged fixture files.
The denied case records failed state and no accepted protected content. See
[public proof log](evidence/native-clients-2026-11-04/antigravity/metadata-fix-epoch14-public-cli.log).
The earlier automatic-selection metadata failure remains separately retained.

Licensing/provenance boundary: this increment distributes only IVOAI integration
code and reuses the existing MCP Go SDK dependency. It does not copy or ship
Antigravity binaries, installers, built-in skills or vendor code. Antigravity's
separate installed client remains subject to the applicable
[Google product terms](https://policies.google.com/terms); redistribution rights
for that executable are not certified here. Its local digest and official ACP
contract are recorded separately from publisher/signature attestation. No
personal configuration or executable was replaced by these tests.
