# IVOAI-188 — Claude Red integration evidence

Validation date: 2026-10-11. Branch: `feat/ivoai-188-claude-red`.
Baseline: clean `main`, v0.10.4, commit
`7812a14f0298b306c333c062bf21d11ce9fc7441`.

## Implementation and inventory

The existing shared Registry, Skill Gate, policy resolver, worker adapters,
`skillupdate`, `supplychain`, Skills CLI and Skills & Capabilities TUI are reused.
No personal skill installation, upstream executable execution, production deploy,
new permission grant or replacement orchestration subsystem was performed.

Upstream: `https://github.com/SnailSploit/Claude-Red`, MIT, immutable revision
`739512a8588b28ff3b554e669391c22508602878`, revalidated against GitHub.
Downloaded archive SHA-256:
`1db2f5ae7d65d6a32e534b51b63acb7df5c36ea320978b1cbe705da1d20ea699`.
This digest is integrity evidence, not an independent signature.

Dynamic discovery found 79 SKILL.md files, versus 78 in the upstream manifest.
The omitted manifest entry is `offensive-netexec`. The reviewed inventory contains:

| Disposition | Count | Default behavior |
| --- | ---: | --- |
| Valid, catalogued, embedded verbatim | 51 | Compatible metadata for Codex and Claude |
| Eligible reporting skill | 1 | Moderate risk; read-only advisory use |
| Restricted operational skills | 50 | 30 high, 20 critical; denied before body loading |
| Invalid legacy files | 28 | Missing frontmatter; rejected, metadata-only diagnosis |

The 28 rejected paths and their original SHA-256 are retained in the catalog.
The 51 accepted document digests and MIT license were compared byte-for-byte with
the upstream snapshot. No accepted document requires an auxiliary local file.
Four existing trailing-whitespace lines in two upstream documents are retained
to preserve original bytes/digests; staged whitespace checks pass outside the
verbatim vendor tree.
All 14 pre-existing source definitions remain unchanged. There are now 15 catalog
sources, of which 14 use the native capability menu.

Principal implementation files:

- `internal/skillcatalog/catalog.json`, `catalog.go`, `native.go`, `selection.go`:
  immutable inventory, bounded rejection metadata and existing native selection.
- `internal/skillcatalog/native/claude-red/`: 51 original documents and MIT notice.
- `scripts/claude_red_intake.py`: bounded, review-only dynamic inventory; does not
  fetch, activate, follow skill symlinks or execute upstream code.
- `internal/app/native_capabilities.go`: per-skill purpose, risk, compatibility,
  capabilities, policy reason, provenance, digest and rejection diagnostics.
- `internal/skillcatalog/claude_red*_test.go`, `internal/app/claude_red_test.go`,
  CLI/menu/artifact tests and `scripts/test_claude_red_intake.py`: new evidence.
- `docs/skill-control-plane.md`, its Portuguese translation,
  `THIRD_PARTY_NOTICES.md` and regenerated embedded documentation.

## Executor evidence

| Path | Evidence and limit |
| --- | --- |
| Codex worker | Real Codex 0.162.1 received one selected reporting document through `developer_instructions` and produced the required synthetic report structure. No task files changed; personal authentication hash unchanged. |
| Claude worker | Shared Gate, policy, worker preparation and adapter contract tests pass. Claude Code 2.1.222 real inference failed with expired OAuth. User confirmed no access and authorized proceeding with this pending. |
| `ivoai codex` | Orchestrated entrypoint and shared worker preparation tested; native pack excluded from primary context. |
| `ivoai opencode --planner codex` / `--planner claude` | Entrypoint routing and worker Gate fixtures pass. Planner choice does not force all workers to that executor. OpenCode live harness results are recorded below. |
| `ivoai claude` | Real direct binary/version launch passes. This legacy direct path intentionally does not inject native worker packs; use OpenCode with planner Claude for managed selection. |
| Codex/OpenCode direct modes | Real artifact launch tests pass; native packs deliberately excluded. |

The live reporting test uses supplied redacted synthetic evidence, temporary
provider homes and private temporary authentication copies. No target discovery,
attack, external assessment or helper execution is part of the test. Provider
transcripts and credentials are excluded from this report.

## Integrity, administration and recovery

Offline installation uses the reviewed embedded baseline only; upstream `main`
cannot become an active identity. Existing staged verification and atomic
promotion retain the previous revision. Tests prove idempotent installation,
test-local revision A → B → rollback A, Registry consistency and denial after
selected content tampering. First-install rollback has no prior revision and
correctly cannot restore one. Future upstream changes require review and release.

TUI tests exercise inspect/install/disable/pin; artifact tests cover all native
packs and preserve personal capabilities/preferences. Existing source-level
enable/disable, update, pin/unpin, rollback, list/show/doctor contracts are reused.
There is no new public per-skill enable preference. Enabling the source is never
operational authorization; restricted capabilities remain denied.

## Validation results

Toolchain: Go 1.27.0 (the repository's existing version), Node 20.19.2,
isolated Corepack cache with pnpm 10.17.1. Commands used temporary homes/artifacts
where applicable; no operator installation was updated.

| Check | Result |
| --- | --- |
| `go test ./...` | PASS, including existing policy, updater, supply-chain, missing-file/network-failure and configuration preservation tests |
| Focused new catalog, application and CLI tests | PASS |
| `go vet ./...` | PASS |
| `go test -race ./... -timeout 20m` | FAILED: existing `TestNativeDAGWorkersUseIsolatedWorktreesAndDependencyView/automatic-dispatch` hit its 10-second deadline under concurrent load; no data race was reported |
| Isolated repeat of that DAG test with `-race -count=1` | PASS, 4.12 seconds; this does not relabel the earlier full run as passing |
| Entire orchestrator package repeated with `-race -count=1` | PASS, 101.77 seconds |
| Latest new tests with `-race`, including setup idempotence | PASS |
| Python intake tests | PASS, 2 tests, including malformed metadata and symlinks |
| `shellcheck install.sh scripts/*.sh` | PASS |
| Native TUI JavaScript presentation/keyboard tests | PASS, 30 tests |
| `scripts/generate-cli-reference.sh --check` | PASS |
| `scripts/build-docs.sh` and `--check` | PASS, English/Portuguese build and parity |
| Linux amd64 build and arm64 cross-build | PASS |
| Final binary native-capability TUI and direct frontend artifact tests | PASS; personal capability/preferences preserved |
| `scripts/test-native-codex.sh` | PASS, pinned official Codex 0.153.4 TTY/AppServer smoke |
| `scripts/install-smoke.sh` | PASS, isolated installation/setup repeated |
| `scripts/upgrade-matrix.sh` | PASS, historical client/server and unversioned upgrade → rollback → re-upgrade |
| Authenticated Claude Red reporting test | Codex PASS; Claude blocked by expired OAuth, pending access |
| `govulncheck ./...` | FAILED: 12 reachable standard-library findings on Go 1.27.0; the exact original baseline produces the same 12 IDs |

The pre-existing Go findings are GO-2026-6617, 6613, 6612, 6611, 6610, 6609,
6608, 6607, 6605, 6603, 6600 and 6599. The scanner identifies Go 1.27.2 as
the fixed version. This integration changes neither dependencies nor the platform
toolchain; a platform update and subsequent security scan remain release gates.

`scripts/test-managed-opencode.sh`: initial runs failed intermittently at provider
readiness under concurrent validation load. A comparison run on the untouched
baseline passed. The final candidate repeat with `GOFLAGS=-p=1` passed the complete
unchanged harness: HTTP/SSE, prompt routing, real attach TUI at 60/100/160 columns,
full/interactive permissions, resize/keyboard, executor HTTP contract, lifecycle
and native AUTO/MCP/cancellation. Packages completed in 117.01 and 64.76 seconds.
No production timeout or permission setting was relaxed. Initial failures remain
recorded here rather than being reported as successful runs.

Reproduce the optional authenticated proof after Claude access is restored:

```sh
IVOAI_CLAUDE_RED_LIVE=1 go test ./internal/skillcatalog \
  -run 'TestClaudeRedLiveSelectedSkill/claude' -count=1 -v
```

Sanitized execution logs for this workspace are under
`/tmp/ivoai-188.IGzMds/`; they are temporary evidence, not a required runtime
dependency or a portable substitute for these committed reproduction steps.

## Acceptance state

IVOAI-188 remains **In Progress**, not Done. Real Claude inference requires
restored provider access; the user explicitly accepted recording this pending
while continuing the delivery. Structural issues IVOAI-13/14/15/16/17/18/49/128/135/137
were related in Plane without recreating tasks. No production deployment is
authorized by this evidence.
