# Managed Codex configuration scope guard

Managed Codex execution now rejects `--ignore-user-config` with the explicit
`CODEX_MANAGED_CONFIG_SCOPE_UNSUPPORTED` diagnostic. The shared argument guard
runs before MCP inventory; `autoBridgeArgs` also checks it before a managed
frontend starts. It avoids combining an inventory that reads user configuration
with an executor that omits that configuration. No replacement inventory or
configuration-scope equivalence is implemented.

For example, `ivoai codex -- --ignore-user-config` is refused instead of proceeding
with an unverified managed scope. Omit that flag to use the supported managed
path, for example `ivoai codex`; no personal configuration edit is required.
Normal managed calls retain the existing MCP
isolation, native-worker/feature denials, read-only primary policy and Core
admission, approval and orchestration authority. This is one shared validation
rule at the relevant entry boundaries. The official direct CLI and historical
standalone pilots are outside this managed restriction; their artifacts and
results are not rewritten. No personal settings, credentials, executables,
configuration files or histories are changed.

## Evidence and exact limits

The [copied public stage summary](evidence/managed-codex-config-scope/offline-public-stage-summary.json)
and [allowlisted historical receipt](evidence/managed-codex-config-scope/historical-causal-receipt.json)
refer to frozen source `0bab9b3409a287917f73a5c0620a3758a4be5c36`, before this guard.
They include source, private harness/control and argument digests without copying
raw arguments, stderr, environments, credentials or personal configuration.
Private raw captures remain outside the repository.

The causal fixture uses the reviewed official Codex/Code Mode pair0.162.1, an
actual frozen Core MCP command, fresh synthetic session/catalog, and a network
namespace with only loopback and no personal HOME or authentication mount.
Exactly one synthetic stdio MCP is defined only in its fresh user configuration.
System configuration is absent and there is no project MCP configuration.

The existing inventory boundary reads that user entry and emits a disabled-name
override. Execution with `--ignore-user-config` lacks its transport definition;
the override can therefore create a transport-less table that the client rejects.
A single fixture-specific control omits only that known user-only override,
retaining ignore-user-config, all other restrictions and the MCP start tripwire.
It is not a production skip policy.

| Historical experiment | CLI substage | Overall result |
| --- | --- | --- |
| Full flags, empty user configuration | One synthetic request and parsed final; stderr0 | FAIL: Core inventory expectation |
| One synthetic user MCP plus the real inventory boundary | `CONFIG_INVALID_TRANSPORT`; zero requests/events; stderr78 bytes | FAIL: CLI primary proof, before the Core inventory assertion |
| Single-setting user-only control | One synthetic request, four JSON events; stderr0; user MCP never started | FAIL: Core inventory expectation |

The controlled difference establishes this **offline startup failure**. The
remaining Core inventory assertion was preserved and was not certified by a
synthetic final response. The inventory exposes transport metadata but no
source/scope provenance enum in this fixture. These results do not justify
skipping all inventory or treating an empty inventory as equivalent isolation.

The earlier single real public-CLI canary remains **FAIL**: one submitted
question/attempt, zero Core workers/tasks/failovers, closed receipt, executor
exit1, zero JSON events and72 stderr bytes. No final answer or usage was reported;
inference and correctness were not established. Its stderr and effective
inventory were not retained. The offline78-byte result does not attribute the
real72-byte failure. Its cause remains UNKNOWN; no further real call was made by
the offline investigation. The separate earlier child-cleanup uncertainty also
remains INCONCLUSIVE.

## Configuration context and compatibility

Current official documentation describes layered configuration, command-line
precedence and trust-dependent project loading. That is useful context for why
user configuration is only one part of the effective scope; it does not attest
pinned0.162.1 behavior or prove that two invocations have equivalent inventories.
[OpenAI configuration basics](https://learn.chatgpt.com/docs/config-file/config-basic).

A future scope-equivalent inventory needs explicit final environment and working
directory, together with trust, project, system and applicable cloud-policy
semantics. The current adapter takes arguments without all that final context.
Using a temporary empty CODEX_HOME can change authentication-location metadata,
relative paths, trust and built-in state. Dropping all inventory can miss active
non-user sources. Neither is introduced by this guard. `--ignore-rules` is a
separate client option and is not evidence of equivalent configuration scope.

The compatibility boundary is explicit rejection of this managed flag, not
silent fallback to an unmanaged client or another executor. This change does
not establish a working authenticated adaptive canary, TUI parity or token/time
savings. Published releases and earlier proof epochs remain unchanged.

## Validation and rollback

The historical experiments above are not final-source checks for this fix.
The [local receipt](evidence/managed-codex-config-scope/local-validation.json)
binds the final six-file source manifest separately: Go checks passed for 59
packages (56 cached; three additional packages have no tests), all-package vet
passed, and the two App policy tests passed with race detection. The three
affected executor/parser packages passed their race suites in the preceding
epoch; their dependency closures are unchanged by the final App-only import
correction. This is not a full App race run or an authenticated integration.

The original admission regression, missing startup diagnostic, original-parser
boundary failure and intermediate missing-import build failure remain archived.
The boundary regression proves unit admission behavior, not a demonstrated
official-client worker escape. No test expectation or native restriction was
weakened. Fresh head CI, native ARM/no-AVX execution and a supported real canary
are not certified by this local receipt. The guard precedes frontend construction
and MCP inventory, while other App preflight effects can occur earlier.

Rollback is a Git revert of the eventual coalesced fix commit. It needs no
configuration, credential, history or evidence migration. A revert restores the
unsupported scope combination's prior failure risk; it does not implement a
safe ignore-config mode. Preserve failed attempts and receipts instead of
deleting them to retry. A future real canary requires separately reviewed
supported arguments, validation and specific authorization.
