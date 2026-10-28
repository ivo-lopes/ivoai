# Managed Codex native-worker boundary

IVOAI-193/194: managed primary and worker CLI turns now disable the native Codex
worker handler with both `agents.enabled=false` and
`features.multi_agent_v2=false`. The old `features.multi_agent=false` setting did
not disable the namespaced `collaboration.spawn_agent` handler in the pinned
official Codex 0.162.1. A native worker could therefore bypass Core's scheduling
authority even with a read-only sandbox and approval policy `never`.

This change is confined to the existing CLI adapter and argument projection.
It adds no scheduler, memory store, authentication or model provider. Core-owned
MCP projections keep their existing grants and ordering. The same managed
restrictions are used for MCP inventory and execution. Existing sandbox,
approval and write-path controls remain in force; workers authorized by Core
continue through the existing worker adapter.

## Precedence and compatibility

The final host settings follow caller `-c` configuration. `ConfigurationArgs`
now also groups compact `-csetting=value` overrides at the same CLI level, so
they cannot silently move behind the host policy. The literal `--` boundary and
input slices are preserved. Native CLI `--enable` is rejected before MCP
inventory because actual Codex feature toggles override `-c` independently of
their textual order. This is an explicit managed-mode policy error. No personal
CLI configuration is edited, and unmanaged native invocation is unchanged.

The AUTO argument gate also rejects `agents` configuration, compact/quoted
variants and attached profile/sandbox/directory options. Its existing ban on
feature toggles and profiles includes both the singular `profile` selector and
plural `profiles` table. The shared adapter's internal projection
still supports a host-owned profile, with the final disabling pair taking
precedence; the real-client proof verifies the profile actually loaded by using
a distinct primary-model sentinel.

The managed catalog and companion remain **0.162.1**. No binary pin, public
schema or session migration changes. The downloaded official amd64 archives
match the existing catalog checksums:

| Asset | Archive SHA256 | Executable SHA256 |
| --- | --- | --- |
| Codex | `86f268d81b898f3e144c5ecff2ad2fda2c5802e042d6195b72538fe0fa125057` | `628e35487d888a28f0427fc37501da529dd3d81fcbad7a2c3421c1e8670d92e1` |
| code-mode-host | `458239ea45c1fc508d89695f180ab352f2834709a8a46929a4466f4f04399ada` | `ec71830690a7c0024916ee0c99fbdc635bc6e747ff665ea4f03a8389bbcb6b30` |

Their URLs are the existing official `openai/codex` release `rust-v0.162.1`.
The opt-in script extracts only each expected bounded regular executable into a
private scratch directory after checking its archive. It executes no installer
and changes no personal setup. No new runtime dependency is distributed.

## Real-client validation

`TestPinnedCodexManagedNativeWorkerBoundary` uses those exact executable hashes,
fresh native homes, an explicit environment allowlist and a synthetic loopback
Responses provider. It requests no commands, external model inference or login.
Every forced tool call has its own task/call/item identity. A different child
model and returned task identity establish execution; mere tool announcement
does not count. Positive controls keep the primary alive until the child
request reaches the local provider. Both primary completion and refusal are
checked.

| Case | HTTP requests | Child requests | Task identity | Result |
| --- | ---: | ---: | --- | --- |
| Legacy flags only | 3 | 1 | returned | Native worker executed |
| `agents.enabled=false`, V2 enabled | 3 | 1 | returned | Native worker executed |
| Both false, CLI `--enable=multi_agent_v2` | 3 | 1 | returned | Native worker executed |
| Hostile separate profile + compact enabling config | 3 | 1 | returned | Native worker executed |
| Managed adapter, fresh home | 2 | 0 | absent | Forced known contract refused as unsupported |
| Managed adapter, loaded hostile profile + compact config | 2 | 0 | absent | Forced known contract refused as unsupported |

The negative cases invoke the actual shared `IsolateCodexMCPs` boundary and real
client MCP inventory before executing its returned arguments. Core MCP grant
ordering, rejected feature toggles before process launch, literal-boundary
handling and AUTO override rejection also have hermetic regression tests.

An earlier reconstructed test used legacy `[profiles.hostile]` configuration.
The client rejected that configuration before provider execution; it is retained
as an invalid preflight, **not** a successful denial. The corrected proof uses
`hostile.config.toml` and verifies its `gpt-6-sol` model sentinel in the actual
request. The initial standalone synthetic legacy probe also executed a worker
and is recorded separately. The previous session's private `/tmp` artifacts
were lost when that directory was cleared; their reported observations are not
silently recertified. This delivery relies on the new executable test and
sanitized evidence below.

Run the optional proof, with Go 1.27.3 available, using:

```sh
TMPDIR=/var/tmp scripts/test-managed-codex-worker-boundary.sh
```

Normal offline tests mark that proof `NOT_RUN` unless an explicit pinned binary
is provided. A skip is not integration success. The helper is intentionally
Linux amd64; ARM64 compile coverage does not certify ARM or no-AVX execution.
See `evidence/managed-codex-worker-boundary/validation.json` for test outcomes,
source hashes and retained adverse attempts.

## Limits and rollback

This closes the tested native collaboration handler in existing managed CLI
turns. It does not establish interception of all native tools, prevent commands
from independently launching arbitrary software, certify every future Codex
version, or prove TUI/real-provider/economic parity. Native client tool catalogs
are not treated as a universal security boundary. Codex 0.164 observations from
the prior research remain historical; the new repeatable proof targets the
currently pinned 0.162.1.

The exported internal `IsolateCodexMCPs` helper still inventories MCPs without
replaying arbitrary caller profile/config selections. A profile can therefore
introduce an MCP server absent from that inventory; this patch does not certify
universal MCP isolation for arbitrary callers of the helper. The loaded-profile
test proves only the native-worker denial. The production caller is
`App.prepareAutoTurn` after `autoBridgeArgs`, which rejects profile options and
selectors. The other internal callers are `Adapter.Run` and its scoped-grant
projection; their `workerArgs` constructor emits no CLI profile or arbitrary
caller config (the `Request.Profile` field is not projected). Future callers
must not assume this helper alone authorizes profile-supplied MCPs.

The separate diagnostic native Evidence Runner stays blocked for production;
this patch does **not** remove `NATIVE_CODEX_WORKER_BOUNDARY_UNVERIFIED` or wire
that runner into App/Facade. No hooks are used as an authorization boundary.

Rollback is a code revert with unchanged configuration and client assets. A
revert restores the prior native-worker bypass, so it must be accompanied by
disabling affected managed CLI turns until a safe replacement is available.
Whole-session token/time gates remain unproven; this patch makes no savings
claim.
