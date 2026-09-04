# CompressionProvider: Caveman, Headroom, and Direct bypass

## Decision

IVOAI treats compression as a mutually exclusive choice for each session. The
selection has three possible outcomes: Caveman, legacy Headroom, or Direct
execution. Caveman and Headroom never form a chain. Caveman is the default for
new installations and legacy configurations without an override; Direct remains
the safety fallback, and Headroom remains temporarily available for compatibility
and rollback during the observation window.

Caveman is a selectable `CompressionProvider` implementation and the requested
default. The effective provider can still be Direct when fidelity, health, or
compatibility require it. It is not a MemoryBackend, ContextBackend, ArtifactStore,
Skill Registry, executor, orchestrator, policy engine, or secret manager.

## Fidelity and WorkingContext

Representations are classified as `compressible`, `exact_required`, `bypass`, or
`unsupported`. Only the first class is eligible for a provider. WorkingContext and
ArtifactStore continue to preserve byte-exact evidence; compression may reduce only
the representation placed in context. Authoritative Memory/Context responses, Skill
Registry metadata, security evidence, errors, stack traces, and test failures remain
recoverable without loss and are treated as exact-required or bypass.

For the primary session, any authoritative Memory or Context source present in the
session's own MCP projection forces Direct execution while the compression path does
not provide selective byte-exact protection for tool results. The rule is
provider-neutral: it applies equally when Caveman or Headroom was requested and does
not depend on `headroom.enabled`. It is evaluated after source selection; unselected
servers do not affect another session, while explicit federation protects every
selected source. Observability records only the requested/effective provider, bypass,
reason, and source count, never Memory/Context content.

## Lifecycle and fallback

Before using Caveman, IVOAI revalidates the active immutable supply-chain object,
runs `caveman-proxy version --json`, creates a private session runtime, starts the
managed proxy directly, and waits for `/health/ready`. The process listens only on
`127.0.0.1` on an ephemeral port. `CAVEMAN_HOME` and `CAVEMAN_CONFIG` point to
`<session-runtime>/caveman/proxy-*`; directories use mode `0700`, and configuration
uses `0600`. Capture is never enabled.

A preflight, health, or startup failure before the agent effectively starts allows
an explicit fallback to the official client in Direct mode. Once the agent has
started, a proxy failure terminates that session and is reported; IVOAI does not
silently open a second session. Ctrl-C, SIGTERM, cancellation, and executor exit
terminate the proxy and remove its transient runtime.

Managed versions use staging, provenance, atomic promotion, and rollback from the
existing supply-chain manager. Headroom remains available during the compatibility
window; removing it requires a later observation release.

## Configuration and migration

`compression.provider` accepts `caveman`, `direct`, or `headroom`. When the key is
absent, IVOAI resolves Caveman and records `compression.source=default` on a new
installation or `migration` when normalizing a legacy configuration. Persisted
overrides are recorded as `explicit` and are never replaced. Because v0.6.0 already
materialized `provider=headroom` without recording whether it represented operator
intent or the historical default, the upgrade conservatively preserves that value.
Unknown TOML fields remain intact.

## Ownership, authentication, and licensing

Credentials belong to the official CLIs. A proxy may carry authentication in memory
to the provider, but IVOAI does not persist bearer tokens, cookies, authentication
headers, or payloads in config, state, journals, observability, or logs.

For Codex, process-local configuration points to a compatible provider with
`requires_openai_auth=true` for the `/chatgpt` route; Codex itself continues to own
`Authorization` and `ChatGPT-Account-ID`. For Claude Code, only the base URL is
redirected, and no synthetic token is set. The pinned Caveman runtime's OpenCode
profile requires its own OpenAI/Anthropic provider credentials and cannot reuse
subscription logins owned by the Codex/Claude CLIs. Because IVOAI does not accept
PAYG keys or share credentials between executors, OpenCode fails Caveman preflight
before the proxy and runs Direct exactly once. No global or project file is changed.

In Caveman v2.3.1, the CLI and skills are MIT, while the runtime/proxy used for
compression is BSL-1.1. The observed Additional Use Grant permits internal
evaluation, local development, CI, integration, and first-party self-hosted traffic,
including production; a hosted/managed service for third parties requires a
commercial license. IVOAI records this classification per artifact and does not
describe the proxy as MIT. This is an upstream technical record, not legal advice.

## Non-goals for this phase

This phase does not enable Caveman memory, MCP, browse, learn, pixel conversion,
skills, hooks, statusline, or global setup; it does not modify `~/.codex`,
`~/.claude`, OpenCode configuration, or shell rc files. Selective compression of
Memory/Context results remains out of scope until it can demonstrably preserve bytes
and call associations.

## Validated provenance

- Caveman product: `v2.3.1`, immutable commit
  `b5ec6351396b643a17cbbec4a6eee8b3fb9dd782`.
- Runtime bundle: `bin-v1.1.3`, immutable commit
  `0d2f052babfd613ec9b4186c86ec6f133cdfd4d7`.
- Linux amd64 proxy: SHA-256
  `d883b9ab4b559e0c1935335c0e24400deb5c61d5e247f1ca239c4149f57885b0`.
- Official source: [Caveman](https://github.com/JuliusBrussee/caveman).
- Licensing: `LICENSING.md` from the product revision above.

The pinned asset responds to the structured probe but reports `version: "dev"`.
IVOAI therefore does not present it as a runtime-verified semantic version: the
immutable revision and supply-chain digest remain authoritative.

## Safe telemetry

Compression events use only bounded dimensions: executor/provider, payload type,
fidelity class, bytes before/after, estimated tokens before/after, ratio, latency,
recovery count, result, bypass, and fallback. Tokens returned by Caveman are always
labeled `inferred`/`estimated`; they do not represent billing or authoritative
provider telemetry. Prompt, response, raw output, code, diff, paths, environment,
cookies, and authentication headers are not part of the event schema.

Doctor/status show the configured provider and component health. Monitor shows the
provider actually used in the session and the latest bounded compression result,
including fidelity and bytes; telemetry failures never prevent execution or replace
the original evidence.

The corpus, byte-exact gates, and opt-in smoke tests for pinned artifacts are in
[Caveman canary and fidelity evaluation](caveman-canary.md). A performance result
never supersedes those gates. The requested default is Caveman; policy continues to
choose Direct when required.
