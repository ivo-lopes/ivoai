# Claude readonly workers: close native delegation at the client boundary

IVOAI-194, under IVOAI-191. Base: `3be82befea6ed3875373afcb1bfaa450710310ac`.
This change is independent of the OpenCode `.3` correction and requires combined
release certification before any tag. No release was published by this work.

## Problem and change

The readonly Claude worker factory denied Bash/file mutators but retained the
default builtin tool catalog. `--permission-mode plan` did not prohibit native
`Agent` delegation: with the official pinned 2.1.296 client and a permitted
synthetic classifier decision, both `Agent(Explore)` and its legacy `Task` alias
created a native background subagent outside Core worker admission.

Readonly advisory and scoped workers now pass `--tools Read,Glob,Grep` and
explicitly deny `Agent,Task`, preserving the existing mutator denials. The primary
already uses this builtin whitelist. Write workers keep their existing restricted
worktree/catalog contract; Codex, OpenCode and direct native client modes are
unchanged. The process-local strict MCP projection and exact host grants remain
independent of the builtin list.

This intentionally narrows compatibility: WebFetch/WebSearch, native scheduling,
workflows, skills and future builtins no longer enter readonly workers implicitly.
Research beyond file reading/searching must use approved Core capabilities and
explicit MCP grants. This is a migration of the advisory-worker contract, not a
claim that a prompt or plan mode enforces readonly behavior. Do not restore a
broad builtin catalog as a fallback if a task lacks an approved capability.

This finding proves a second delegation authority. It does **not** demonstrate
file mutation, secret access or institutional MCP mutation. Managed MCPs already
use task-scoped host whitelists; external tools retain their exact inventory and
approval policy. It does not certify transparent native TUI control.

## Reproduction and provenance

The managed catalog pin is unchanged: official Claude Code **2.1.296**. The local
personal installation 2.1.251 was used only for read-only help/version/status
research and was never substituted for the managed pin.

- [Official release](https://github.com/anthropics/claude-code/releases/tag/v2.1.296).
- Linux amd64 archive SHA256:
  `45fe2b33f5cbdd114309618970bcd90b8b163e34252d66cf100d7e068d9b831d`.
- Extracted executable SHA256:
  `24972e3bc859fab2b46ed4c1e51f7d6130f06d3bd550811a114640de3370d0de`.
- The checksum matches the existing component catalog and official release
  metadata. Release signature verification was not performed in this audit.
- The protocol fixture pins these bytes and checks `--version`; the download
  wrapper verifies archive checksum and the single-entry archive layout before
  extraction. No third-party installer is executed and no new dependency is
  distributed with IVOAI.

Run on Linux amd64 with Go from go.mod, Python 3 and working bubblewrap:

```sh
scripts/test-claude-worker-tools.sh --output /private/proof-directory
# Or reuse the exact downloaded archive; its digest is still mandatory:
scripts/test-claude-worker-tools.sh --archive /path/claude-linux-x64.tar.gz
```

The script calls `TestClaudeWorkerOfficialDelegationBoundary`, which generates
**actual corrected workerArgs plus isolateScopedMCPs arguments**. The fixture
reconstructs the previous contract by removing only the new builtin whitelist and
Agent/Task denials. It does not use handwritten corrected flags to replace the
factory. The positive control adds an Agent/Task allow rule to that predecessor.
Both before/after cases retain plan mode, the same system instructions and an
empty strict MCP projection.

The official client and a synthetic Anthropic HTTP endpoint run in one private
network namespace (`bwrap --unshare-all`), without personal HOME, keyring,
configuration or inherited credentials. Only generated private data and system
runtime files are mounted. The API key and provider responses are fake. The
classifier receives its valid synthetic `<block>no</block>` response rather than
being disabled. Both a child request containing the exact child prompt and the
client's `subagent_stats` must agree; an exit code or a marker string alone cannot
pass the test. Corrected cases require the three-reader catalog, no child request,
zero spawned subagents and an explicit disabled-tool result for forged calls.

| Mandatory case | Before/after catalog | Child API requests | Spawned/completed | Result |
|---|---|---:|---:|---|
| Positive Agent control | 22 builtins | 1 | 1/1 | PASS |
| Previous readonly, Agent | 22 builtins | 1 | 1/1 | PASS: reproduces defect |
| Previous readonly, Task alias | 22 builtins | 1 | 1/1 | PASS: reproduces defect |
| Corrected factory, forged Agent | Read/Glob/Grep | 0 | 0/0 | PASS: disabled |
| Corrected factory, forged Task alias | Read/Glob/Grep | 0 | 0/0 | PASS: disabled |

All five cases were executed locally, without SKIP, via the checksum wrapper.
The [machine receipt](evidence/claude-worker-tools/official-2.1.296-amd64.json)
contains exact counters and the hash of generated factory arguments. The
standalone initial audit also reproduced the Agent result; see the
[contract audit](native-client-contract-audit-2026-10-28.md).

Initial attempts with malformed classifier responses were denied, with zero
subagents, and are retained by hash in the validation receipt. An early negative
case also consumed the prompt through a variadic flag; it failed before any
request and was corrected with the `--` separator. These construction failures
are not counted as passing denial tests. Their raw artifacts remain in the private
audit directory, not silently discarded. The official binary's synthetic token
and list-price figures are not real consumption or economic evidence.

The default unit suite skips this explicit opt-in integration test when no binary
is supplied; that skip is **not** its proof. Dedicated CI and release verification
invoke the wrapper and require all five cases and a passing JSON receipt. CI uses
the existing restricted AppArmor setup only on ephemeral runners; no global
AppArmor disablement or personal policy change is needed.

## Validation and remaining limits

Factory regression covers advisory/scoped-readonly modes, preservation of exact
MCP grants and private token references, and the unchanged restricted write-worker
catalog. Affected package tests, race and vet results are recorded in the
[validation receipt](evidence/claude-worker-tools/validation.json).

This certifies the stated protocol boundary on the pinned Linux amd64 bytes. It
is not an ARM/no-AVX client execution proof, real account/quotas test, production
App lifecycle test, worker performance benchmark, or general security audit of
all native customization hooks. No subscription credentials were extracted,
shared or changed; no real inference or authentication occurred.

Current official [subagent](https://code.claude.com/docs/en/sub-agents) and
[CLI contracts](https://code.claude.com/docs/en/cli-reference) support restricting
builtin tools. The broader [audit](native-client-contract-audit-2026-10-28.md)
records version-specific hook limitations and the absence of demonstrated Core
final-validation authority over native TUI output. Antigravity remains research
only; its current official binary provenance is recorded separately without
adding a provider, changing a pin or claiming parity.

Rollback is the previous commit, which restores the known delegation gap. A
release rollback must therefore keep readonly Claude workers unavailable until
this boundary is restored or another verified equivalent is active; do not
silently claim the predecessor's plan mode provides the same guarantee.
