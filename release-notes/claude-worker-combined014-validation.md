# Combined v0.14.0 source validation: Claude, admin and legacy `.3`

This is a new source epoch, not a rewrite of the earlier Claude or OpenCode
receipts. Publication is still blocked on the final source CI and tagged Release
pipeline. No tag, release, deployment, inference or authentication was performed.

## Source composition

- Main `a7db2d90b5d503bada40f0c7cb8f495b75313061` contains merged PR #48.
  Its reviewed `.3` source passed 15/15 checks in
  [CI 40729664838](https://github.com/ivo-lopes/ivoai/actions/runs/40729664838),
  including native ARM and amd64/no-AVX/offline gates. That result belongs to
  that epoch and is not reassigned to this combined candidate.
- Claude correction `7489bdf008f3106b2432fece64b044f422325c8d` closes the readonly
  builtin catalog and proves the Agent/Task delegation boundary.
- Merge `f4259374` combines those sources. Cherry-pick `aef0e4c3` incorporates
  root's `cd43fcbd0361b0e3da06025c99cc4af398c8da0f`: the Release build executes
  version/help from the exact Core about to be archived, with private HOME/XDG,
  on native ARM or amd64 Nehalem emulation before packaging.
- Current English/Portuguese architecture and release pages describe the Claude
  compatibility boundary and preserve the economic failure and native TUI limits.
  PRs #45, #46 and #47 remain excluded; no runtime from them was merged.

The Release workflow retains admin OAuth, managed OpenCode, Claude, Graft and
architecture gates. Verify prepares bubblewrap/AppArmor/Python/curl explicitly
before both OpenCode and Claude protocol tests. This consolidation removes only
redundant package installation; it does not omit sandbox setup or weaken a gate.
The official Claude job remains a distinct required CI proof.

## Actual combined-source checks

| Check | Result | Scope |
|---|---|---|
| `go test ./... -count=1` after site generation | PASS: 59 packages; three without tests | Includes config/migration schema contracts, CLI, docsserver, admin and worker tests; ordinary opt-in skips are not official-client proof |
| `go test -race` workers/App/externalmcp/agents/quota after final embed | PASS: five packages | Separate pre-embed race also passed, but is not substituted for this run |
| `go vet ./...` | PASS | Full repository |
| Generated CLI reference `--check` | PASS | Actual compiled help versus canonical reference |
| Own frozen website build and `--check` | PASS: EN/PT | Node 26.11.1, pnpm 12.11.2, repository lockfile; own node_modules/store, no other worktree's dependencies |
| Official Claude checksum wrapper against combined factory | PASS: five mandatory cases, zero SKIP | Pinned 2.1.296; controls reproduce Agent/Task delegation, corrected forged calls spawn zero children |
| Managed TUI semantic tests | PASS | Presentation/keyboard/event source fixtures; no real provider |
| Python discovery | PASS: 49 tests | No provider authentication or inference |
| Workflow YAML, duplicate keys and embedded Bash syntax | PASS | CI/Release: 78 Bash blocks; also checks component manifest for duplicate keys |
| Core AMD64v1 and ARM64 compilation | PASS | Local candidate builds, not distribution artifacts |
| Local AMD64 Core version/help with private HOME/XDG | PASS | Candidate version; no real provider call |

The website input and embedded markers both equal
`cfdbdf12c35db4b6feb58e9531346bbc7ab3e5cee88df90341a32b874ff5aaec`.
The second frozen build compared non-JavaScript outputs, including HTML and CSS,
against the embedded site. CSS files are unchanged from reviewed main; only the
necessary generated content/JavaScript references changed. A fresh, unprepared
Corepack cache initially selected pnpm 12.16.1 and was rejected before the site
build. The failure was retained; preparing the required 12.11.2 fixed the invocation
without changing the lockfile, relaxing checks or touching personal settings.

The [combined machine receipt](evidence/claude-worker-tools/combined014-validation.json)
binds this epoch's source/assets, logs and artifacts. The [official-client receipt](evidence/claude-worker-tools/combined014-official-2.1.296-amd64.json)
is separate from the earlier receipt. The exact generated factory arguments are
hash-bound by that receipt; no handwritten corrected argv replaces the factory.
No synthetic token/cost figure is used as real economic evidence.

## Stable dependencies and unresolved gates

The `.3` upstream patches, builder, managed pin and component manifest are byte
identical to reviewed main. The existing `.3` amd64 archive remains SHA256
`8ea481d8a2c2d1f11417a78a3a8121e3d790630b9ed3c99b010b2cb4d24999a2`;
the source manifest remains
`1b784a2efd59eee24351880fd83d3a75a55d92582f93ac43fd07f3175c56de7b`.
No frontend ELF was rebuilt from altered TypeScript. The earlier `.2` readiness
failures, controlled registry holds and malformed classifier experiments remain
in their original records, including adverse results.

Final-source **16-check CI remains required**. Local ShellCheck is unavailable;
its remote gate remains required. The new tagged-Core execution gate was syntax
validated but is not claimed as an executed release result: local qemu is absent,
and local ARM work was cross-compilation only. The local candidate binaries do
not embed reviewed component release metadata and are not distribution artifacts.
Release must rebuild the clean tag and execute/checksum its exact artifacts.

Readonly Claude migration deliberately removes implicit WebFetch/WebSearch,
scheduling/workflow/skill and future builtin access; approved Core/MCP capabilities
supply additional research. Write workers and exact host MCP grants are preserved.
This establishes a delegation boundary, not a file/MCP mutation exploit, account
eligibility, native TUI mediation, transparent final validation, OpenCode removal
or economic savings. A rollback that restores the known Claude gap must keep
those readonly workers unavailable until an equivalent verified boundary exists.
