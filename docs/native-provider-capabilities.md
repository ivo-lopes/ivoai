# Native clients and optional providers: research boundary

Research date: 2026-10-27. Roadmap: IVOAI-150, 190, 194, 196, 200, 201 and
23–26, under IVOAI-191. This document records contracts and integration risks;
it does not implement an adapter, certify runtime parity, approve a dependency
upgrade, or change a default. No inference, third-party installer, credential
extraction, or personal configuration edit was performed for this research.

The repository baseline is v0.12.1 (`3ff6db8b`). Existing implementation must be
distinguished from these upstream possibilities. The economic acceptance gate
remains unproven; upstream compression percentages are not IVOAI session savings.

## Evidence and client matrix

Local probes used only `--version` and `--help`. A listed option establishes a
CLI surface, not successful authentication, usable quota, effective isolation,
or correct execution. Local installations differ from the repository's managed
pins; none was promoted into the component catalog by this research.

| Client | Locally observed version and surface | IVOAI baseline / unresolved work |
|---|---|---|
| Codex | 0.164.0; `app-server --help` exposes stdio, Unix socket and WebSocket transports, process configuration overrides and schema generation | `internal/codexfrontend` already provides managed native integration. Its implementation still references shared `opencodebridge` types. Native protocol presence does not prove that OpenCode can already be removed. |
| Claude Code | 2.1.251; help exposes print mode, JSON/stream input/output, resume/session ID, explicit tools, permission mode, process settings and strict MCP configuration | Official CLI executor/worker paths exist. IVOAI-194 still requires managed native conversation mediation and real parity evidence. Headless flags alone do not establish transparent native TUI control. |
| Antigravity | `agy` 1.4.8; help exposes print/stream formats, conversation resume, model/effort, execution mode, sandbox, log path and slash-command control | Research input for IVOAI-150/196. No Antigravity integration or inference certification follows from these probes. |

The observed Antigravity executable SHA-256 was
`70b1d27f1812f5ebccc8f13daf2209112447413437dd1ab6d87c58b8a04fe35b`.
This identifies local bytes; it is not an independently verified release signature
or approved redistribution pin.

Codex's documented App Server exposes thread/turn lifecycle and approval handling;
the IVOAI adapter must continue using the core as its authority. See the
[official App Server documentation](https://learn.chatgpt.com/docs/app-server)
and the existing [frontend contract](orchestrated-frontends.md).

Claude's observed help includes `--mcp-config`, `--strict-mcp-config`,
`--setting-sources`, `--settings`, `--allowedTools`, `--disallowedTools`,
`--tools`, `--permission-mode`, `--resume`, and `--no-session-persistence`.
These are candidates for process-scoped projection, subject to version-specific
tests. Its `--bare` help explicitly excludes OAuth/keychain authentication;
therefore it is not a transparent substitute for an existing subscription login.
This is local CLI evidence, not a new claim about supported account terms.

## Antigravity contract and limits

The [official headless contract](https://antigravity.google/docs/cli/headless/)
supports `--input-format stream-json --output-format stream-json`. Input uses
`user` events; output uses `init`, `step_update`, and terminal `result`. Resume
uses `--conversation`. In a persistent process, usage, duration and turn counts
are cumulative: subtract prior counters without double-counting step and result
usage. `control_request`/`control_response` messages are unsupported. `/usage`
must run outside the stream. A tool can be soft-denied while the process exits
zero; require the terminal status, intended result and relevant diagnostics.
Headless workspace writes can be allowed by default. These contracts do not
establish an external approval RPC or native TUI interception.

The [execution-mode documentation](https://antigravity.google/docs/cli/modes)
describes `plan` as an instruction prefix. It is not a read-only security
boundary. The [CLI reference](https://antigravity.google/docs/cli/reference/)
also says the sandbox flag can have no effect when the platform sandbox is
unavailable. Worker integration needs verified OS containment, scoped tools and
explicit write protection; neither flag alone meets the IVOAI advisory-worker
contract. The adapter must set a bounded timeout rather than assume a default:
local 1.4.8 help and the reference show zero, while the headless guide describes
five minutes.

[Native authentication](https://antigravity.google/docs/install/) uses the OS
keyring and interactive login when needed. Preserve that flow without reading
or copying token material. [Quota documentation](https://antigravity.google/docs/cli/commands/usage/)
describes a refreshed `/usage` panel, not a proven stable JSON quota API.
Unavailable quota must remain unknown. No paid API fallback is implied.

[MCP documentation](https://antigravity.google/docs/mcp) supports local stdio and
remote Streamable HTTP; the CLI does not support legacy SSE. It names global
`~/.gemini/config/mcp_config.json` and workspace `.agents/mcp_config.json`.
A private process-scoped projection equivalent to Codex/Claude has not been
demonstrated here. Do not edit the user's configuration to disguise that gap.

Acceptance for IVOAI-196 still requires authentication, streaming, cancellation,
resume, permission denial, quota behavior, source isolation and native-client
tests on the actual supported version. Report headless and TUI capabilities
separately.

## Immutable upstream research identities

These commits were inspected for source/license evidence. They are proposed
experiment inputs, not approved runtime upgrades. Licensing conclusions below
describe the inspected files and do not certify every transitive artifact.

| Project | Inspected commit | License evidence |
|---|---|---|
| RTK 0.49.0 | `e05c7bdfacfcfe1f845d11f47339e4eae63eb3f5` | [Root Apache-2.0](https://github.com/rtk-ai/rtk/blob/e05c7bdfacfcfe1f845d11f47339e4eae63eb3f5/LICENSE); engine and proxy have separate BUSL-1.1 files |
| Codebase Memory MCP | `83109dde52b3d5c766451f84d346cf98ad05bf3f` | [MIT](https://github.com/DeusData/codebase-memory-mcp/blob/83109dde52b3d5c766451f84d346cf98ad05bf3f/LICENSE) |
| Headroom | `17d38e5699b7d7c69ac6addcd762f39e42dd7f00` | [Apache-2.0](https://github.com/headroomlabs-ai/headroom/blob/17d38e5699b7d7c69ac6addcd762f39e42dd7f00/LICENSE) |
| Caveman | `9b68f7ed12b05e7efaff18e65c5a3fc2849fd0a4` | [Apache-2.0](https://github.com/JuliusBrussee/caveman/blob/9b68f7ed12b05e7efaff18e65c5a3fc2849fd0a4/LICENSE) |
| OpenViking | `54d4c0fac91fae976325ebed618d7278ff9b5c77` | [AGPL-3.0](https://github.com/volcengine/OpenViking/blob/54d4c0fac91fae976325ebed618d7278ff9b5c77/LICENSE) |

## RTK-OSS: filter stored output without repeating commands

The inspected [Cargo manifest](https://github.com/rtk-ai/rtk/blob/e05c7bdfacfcfe1f845d11f47339e4eae63eb3f5/Cargo.toml)
enables `engine` by default. An Apache-only experiment must use the OSS artifact
or a locked build with `--no-default-features`, then verify that `rtk-engine`
is absent from the linked dependency graph. The root package's Apache label
alone cannot identify the full binary's license boundary. The
[edition documentation](https://github.com/rtk-ai/rtk/blob/e05c7bdfacfcfe1f845d11f47339e4eae63eb3f5/docs/guide/resources/editions.md)
distinguishes OSS filters from the BUSL engine/proxy. No RTK proxy is proposed.

The [pipe implementation](https://github.com/rtk-ai/rtk/blob/e05c7bdfacfcfe1f845d11f47339e4eae63eb3f5/src/cmds/system/pipe_cmd.rs)
supports `rtk pipe --filter <name>`. It consumes bounded UTF-8 stdin and returns
filtered output without executing the original command. Filters include Git
status/log/diff, rg, Go build/test, pytest, Cargo tests, tsc, vitest and logs.
The Go test filter expects JSON. Invalid UTF-8, oversized input, unsupported
filters or process failure must use the direct representation.

The experiment boundary is: execute an authorized command once; preserve original
stdout/stderr and exit status in the existing ArtifactStore; pass eligible bytes
to an allowlisted filter; return the representation with authorized exact refs.
Never rerun a mutation to obtain raw output. Preserve failures, diffs and test
evidence under the existing fidelity policy. Do not invoke `rtk init` or create
another recall database. Use isolated runtime/config paths, resource limits and
an initially disabled feature flag. IVOAI-200 remains pending real quality and
session-cost comparison.

## Codebase provider: shared-daemon implications

The inspected [README](https://github.com/DeusData/codebase-memory-mcp/blob/83109dde52b3d5c766451f84d346cf98ad05bf3f/README.md)
describes daemon coordination even for ordinary CLI operations. A subprocess is
therefore not automatically disposable or isolated. The
[configuration contract](https://github.com/DeusData/codebase-memory-mcp/blob/83109dde52b3d5c766451f84d346cf98ad05bf3f/docs/CONFIGURATION.md)
uses a canonical per-account cache; a different cache root can be refused while
another session is active. `CBM_ALLOWED_ROOT` restricts indexing, but does not
replace OS isolation. Watchers are enabled by default; automatic indexing is
disabled. File-count and total-source-size index limits are off by default.

IVOAI-190 needs a private daemon/rendezvous namespace and cache, bounded resource
admission, no automatic UI, explicit indexing and freshness receipts. Verify the
behavior without disturbing a user's existing daemon. Project/worktree/ACL
isolation must be tested across concurrent sessions. Expose only approved query
operations through `CodeIntelligenceProvider`; keep index mutation, project
deletion, ADR writes and trace ingestion outside worker grants. Prefer direct
reading or Graft when sufficient; there is no mandatory Graft-to-Codebase chain.

## Compression and Context decisions retained

Headroom's inspected [README](https://github.com/headroomlabs-ai/headroom/blob/17d38e5699b7d7c69ac6addcd762f39e42dd7f00/README.md)
offers ContentRouter and locally recoverable CCR originals. This is an experiment
candidate for selective compression, not a reason to add another memory system
or replace ArtifactStore. Benchmark it separately from Caveman and Direct.

Caveman's inspected [README](https://github.com/JuliusBrussee/caveman/blob/9b68f7ed12b05e7efaff18e65c5a3fc2849fd0a4/README.md)
states that current CLI usage telemetry is enabled by default and can be disabled
with `DO_NOT_TRACK=1`. That observation concerns this inspected upstream revision,
not a finding that the existing IVOAI pin sends telemetry. Any upgrade must audit
the actual executable and environment. IVOAI-201 retains Caveman as default,
Headroom as optional and Direct as fallback; no consecutive proxy stack or
Headroom removal is approved by this research.

For IVOAI-23–26, retain the existing ContextBackend and durable ai-memory as
separate capabilities. OpenViking remains optional/shadow. Its AGPL obligations
need evaluation against the intended distribution and deployment before adoption;
no automatic cutover is justified. Context ingestion/retrieval validation remains
a separate IVOAI-175 concern, and a memory write is not proof of Context sync.
