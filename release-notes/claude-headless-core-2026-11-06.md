# Claude managed headless Core — 2026-11-06

The new opt-in bounded entrypoint reuses the Antigravity/Core host, Session lease,
PrimaryTurnReceipt, durable turn journal, admission bridge and existing planning
protocol/scheduler. It is not interception or transparent control of the Claude
TUI. Existing `ivoai claude` direct launching remains available. A literal native
prompt `run` can be forwarded with `ivoai claude -- run`.

```sh
IVOAI_EXPERIMENTAL_CLAUDE_HEADLESS=1 ivoai claude run \
  --directory /path/to/repository --prompt 'Explique a autenticação deste projeto.'
```

The rollout stays disabled by default. Only a managed official **Claude Code
2.1.318, Linux amd64** runtime with the reviewed executable SHA-256 is admitted:
`708c77b204db30086ff6f1ed34577bda03e1be74ced80557cbf7b54249ebf0bd`.
The verified file descriptor is the executable passed to the child; replacing its
path cannot substitute another executable between verification and launch.
Runtime/config/state/root identities and the original live receipt are checked
throughout the Core projections and before releasing the answer. No automatic
replay, executor failover, subscription credential copy or paid API fallback is
introduced. The official client uses its own existing cached authentication.
Provider API keys, alternate endpoint routes and cloud-provider selectors are
removed from the production child environment. `--bare` is deliberately absent
because that official mode does not use cached OAuth/keychain authentication.

The private strict MCP projection exposes only Core tools. Native builtins are
empty; shell, writes, editing, Agent/Task/Skill/Workflow delegation, ambient hooks,
configuration sources and native auto-memory are disabled. A complex task can
ask the existing orchestration MCP to plan and execute approved Core workers;
choosing a simple turn grants no native write or privileged execution capability.
The same exact existing MCP schemas, source scopes and approval decisions apply.
Simple turns need no DAG. Up to eight eligible read-only `--follow-up` turns use
the same owned native ID and process-epoch mapping. Cross-process resume,
post-DAG continuation, other platforms/versions, model aliases, quota telemetry
and inference/economic equivalence remain uncertified. Use exact native model
IDs for explicit `--model`; successful init metadata is the observed model,
while requested CLI effort is configuration evidence rather than server reasoning
telemetry. The Claude stream parser does not yet populate authoritative usage
counters; those remain unavailable, never zero or estimated-as-official.

The user explicitly has no Claude account and requested that real Claude
inference remain **PENDING** (IVOAI-194/197). No login was requested. This increment
therefore does not claim a subscription-backed vertical integration pass, real
Claude DAG execution or certified Claude/Codex parity.

An official-client **offline protocol** proof used the production projection
arguments, pinned 2.1.318 executable, private HOME and isolated network namespace.
A synthetic loopback provider demonstrated all three mandatory cases:

- Core-only MCP tool discovery and recoverable evidence, normal exit0.
- Official `--resume` retaining the same native session ID, normal exit0.
- Rejected native Agent exposure, zero delegated child activity and ignored
  malicious ambient SessionStart hook, normal exit0.

This proof contains no real model inference, subscription auth, external network
or economic evidence. The synthetic endpoint/key exists only inside the explicit
opt-in test's private network; production strips such overrides and cannot select
this fixture. [Sanitized summary](evidence/native-clients-2026-11-06/claude-primary/summary.json).
Hermetic wrapper tests separately cover lost permission, late revocation, foreign
resume identity, failed start/exit and withholding answers. These are clearly
separate from official-client and real-provider certification.

```sh
IVOAI_CLAUDE_PRIMARY_OFFICIAL_BINARY=/owned/reviewed/claude \
IVOAI_CLAUDE_PRIMARY_PROOF_DIR=/owned/private/proof \
  go test ./internal/app -run '^TestClaudePrimaryOfficialOfflineProtocol$' -count=1 -v
```

Default tests skip this opt-in executable proof; a skip is not certification.
The previous worker-delegation proof and all Antigravity failure epochs remain
unchanged. No vendor code/binary is copied into IVOAI. Revert this increment or
leave its flag unset for rollback. References: official [CLI flags](https://code.claude.com/docs/en/cli-reference)
and [hook controls](https://code.claude.com/docs/en/hooks).
