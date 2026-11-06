# Caveman canary and fidelity evaluation

The Caveman cutover is gated by evidence, not by compression ratio alone. The
canary compares Direct, Headroom and Caveman without changing the configured
default. A single byte-exact mismatch, credential crossover, cross-purpose leak
or silent corruption blocks promotion. Headroom's managed wrappers currently use
Direct due to `unavailable_by_policy`; a `headroom` fixture label is not a real run.

`QualityPassed()` checks the existing evidence/fact invariants and may pass for
an intact degraded fallback. Historical `Passed()` remains its quality-only alias.
`OptimizationVerified()` additionally requires a trusted harness's reviewed runtime
SHA-256, complete corpus coverage, accounted provider calls, an applied transform
and no degradation, provider mismatch or compression failure. The opt-in pinned
MCP gate requires both verdicts. Neither is whole-session token/cost certification;
fake runtimes, missing providers and safe fallback are not optimization evidence.

## Layers

The committed canary has three layers:

1. Hermetic tests use fake runners, MCP servers and deterministic fixtures. They
   exercise executor lifecycle, WorkingContext, Memory/Context routing,
   cancellation, fallback and redacted observability without provider access.
2. Opt-in tests can execute the reviewed `bin-v2.1.0` proxy and MCP assets after
   their pinned SHA-256 values are verified. The assets remain temporary and no
   global setup, `npx`, hooks, skills or Caveman memory is enabled.
3. The authenticated executor smoke is explicitly opt-in. It delegates auth to
   each official CLI, never reads auth stores, and does not run in normal CI.

Normal CI skips every test that can consume subscription quota. Reviewers enable
the local smokes only with explicit environment flags and already downloaded,
checksum-verified binaries. Missing authentication or an unavailable executor is
reported as blocked, never as a pass.

## Corpus and contracts

`internal/cavemaneval` supplies a bounded deterministic corpus for JSON, JSONL,
YAML, logs, stack traces, Go, Python, shell, TypeScript, diffs, Git output,
search results, MCP results, tables, test/compiler failures, long text, large and
repetitive output, high-entropy text and mixed content. Fixtures contain no
Voicecorp or Mindsite production data.

Every output is written to ArtifactStore before projection. Recovery is compared
by SHA-256 and byte equality. `exact_required` entries additionally require zero
mismatch, while non-exact entries retain declared facts in their bounded
representation. Token counts in the harness use a documented four-bytes heuristic
and are labelled estimated; they are not provider-reported or billed usage.

Binary/non-UTF-8 output is always exact-required and never reaches the string-only
compression interface. Failed and cancelled results retain their critical status,
exit reason and exact ResultRef.

## Safety gates

- primary Memory/Context protection is provider-neutral and uses only the
  session-selected MCP projection;
- workers bypass Headroom when their session-local Memory or Context is active;
- unknown Memory tools are treated as potential writes, preventing federation or
  redundancy retry until their read-only semantics are reviewed;
- Caveman preflight failure may fall back to Direct before launch;
- a Caveman failure after executor launch terminates that session and never starts
  a duplicate executor;
- the pinned proxy listens on loopback, creates private per-session state and is
  cleaned after the lease closes;
- the MCP helper is local stdio, uses a private per-call runtime and is never
  exposed automatically to the primary;
- observability contains bounded enums, sizes, ratios and reasons only.

## 2026-08-31 local evidence

The reviewed amd64 proxy digest
`44436d69705ad9968563c181cd11d396d6ae5eeccd8dbf228f6225d92dfadcb1`
passed its structured `version --json` probe (which truthfully reports `dev`),
loopback readiness and cleanup for the supported Codex and Claude adapter shapes.
The reviewed MCP digest
`c5c9a850f388570e2b822ac86ac35ad0e9f2c8ec0162b966f5536013042c058d`
processed all 22 corpus scenarios with zero artifact, exact-required, semantic or
observability mismatch. Its measured bounded-context ratio was approximately
`0.028615` (983,089 input bytes to 28,131 context bytes); token figures were
heuristic estimates.

Authenticated local Caveman smokes passed for Codex and Claude Code. The managed
OpenCode executable later passed its live Direct smoke through an explicit Caveman
unsupported-path fallback. The pinned Caveman OpenCode profile can only redirect
OpenAI/Anthropic providers; IVOAI does not request their API keys or reuse Codex or
Claude credentials. This dated result is supporting evidence only; volatile full
reports live outside the repository.

## Non-goals

The canary does not promote Caveman, migrate defaults, remove Headroom, activate
Caveman memory/tools, add OpenCode AUTO/worker support, access production, or
publish a release.
