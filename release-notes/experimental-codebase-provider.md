# Optional Codebase Memory reader — IVOAI-190

This is a small, disabled-by-default library adapter implementing
`core.CodeIntelligenceProvider`. It adds no App routing, runtime distribution,
installer, listener, daemon, scheduler, database, client/TUI setting or credential
handling. Existing Graft and direct inspection are unchanged. Construction with
zero `codebase.Options{}` cannot call a transport or host policy callback.
The host must explicitly inject an enabled, authorized binding; there is no
user-environment flag that activates this adapter in the product.

## Actual supported contract

- `ask`: a complete, case-sensitive ASCII symbol identifier or qualified name,
  queried with an anchored escaped name/qn pattern. It returns bounded function
  or method identities with repository-relative files and source spans. It is
  not general natural-language reasoning, text search, class coverage or a proof
  that a symbol absent from an index is absent from source.
- `callers`: inbound static calls at depth 1 only, including test nodes. A first
  exact search must resolve one indexed function; ambiguity/partial lookup
  refuses traversal. The second request names its exact qualified identity.
  Normalized rows retain hop, test flag, resolver strategy/confidence when
  supplied, and explicitly identify inferred static relationships rather than
  runtime execution. Totals count callers; the pinned upstream excludes anchor
  nodes (`hop > 0`). The host limit is at most 20 rows, not a CPU/evaluation bound.
- Other operations, outbound/deeper traversal, file/line scopes, free-form
  questions, case-insensitive matching and unsupported arguments return
  `ErrUnavailable`. `skeleton`, `map`, `blast`, `check`, `build`, mutation and
  arbitrary Cypher are not exposed. `Probe` advertises only exact symbols and
  direct callers, retains unknown health/compatibility and does not contact CBM.

The injected `Caller` returns the bounded MCP `tools/call` result envelope. It
uses an existing host-owned SDK/session or capability transport; the adapter
implements no MCP framing or process management. It can select only
`search_graph` and `trace_path`, never index/delete/ADR/traces/UI/watch operations.
The host remains responsible for containing its actual transport and rejecting
unauthorized actions. A malicious host callback is not contained by a Go function
interface. No fixed Graft → Codebase chain or automatic fallback execution exists.
The future selector must compare intent, available evidence, freshness, quality
and measured costs and may keep a sufficient direct answer.

## Prior index and host authority

`New` copies a value receipt containing existing Evidence scope digests,
WorkingContext ownership, a host project key, source generation, published index
identity, reviewed runtime identities and an index-inventory completeness flag.
That value is not a token or a parallel permission mechanism. Enabled construction
requires the host's `Authorize` and `CurrentGeneration` callbacks. They must use
the existing capability/session authority and independently verify the binding;
`CurrentGeneration` must cover additions, edits/deletions, index and runtime
replacement, recipient, organization/repository/worktree and permission changes.
For SQLite, index identity must cover all published data including any relevant
WAL/blobs, or a checkpointed immutable index; hashing only an active main database
is insufficient. Query never builds, repairs, refreshes or authorizes indexing.
A missing/stale receipt returns unavailable/denied so the host can make a separate
indexing decision under its existing authorization and resource policy.

Authorization and generation checks run before/after every transport call,
including provider failures, and once more before the final result. Each provider
admits one Query at a time without a new queue, before invoking any host policy
or generation callback. The first check already receives the bounded query
context, and reported duration includes that initial work. A superseded binding, cancellation
or revocation produces no result text. The host must publish replacements
atomically; these read-side checks do not lock an external writer or eliminate
every possible change after the final observation.

MCP output supplies no authority for `Fresh`, scope, source digest or runtime
provenance. `Fresh=true` means the host-attested generation is current; it does
not certify extraction correctness or complete coverage. Host-incomplete indices
and upstream continuation/lower-bound/output truncation remain `Truncated=true`.
Results are always untrusted. No raw provider failure text is returned or logged.
The provider does not cache or store responses, fabricate usage/cost measurements
or report CPU/RSS it did not measure; optional resource fields stay nil and graph
size fields remain unknown/zero. `Installed` in Probe reflects the host's valid
runtime/index attestation, not discovery or installation performed here.

Replies are bounded to 64 KiB on this interface, checked for UTF-8, duplicate
keys, depth and malformed/conflicting text/structured content. The parser accepts
only the pinned grouped schema and projects bounded typed facts. Paths cannot be
absolute/traversing/control-bearing or use bidi override/isolate formatting.
Ordinary Unicode paths remain supported. Unsupported or inconsistent replies fail
closed rather than appearing to be empty valid evidence. Requested output is
512–32768 bytes; an oversized normalized result is refused rather than sliced
through an identity/relation. No model tokenizer claim follows from CBM's
`max_output_tokens` estimate. The host must additionally bound transport ingress
before allocation. The two-second query context and all callbacks are
cooperative: an ignoring callback can delay return; late evidence is rejected.
The adapter cannot terminate a remote process by cancelling a Go context alone.
`Probe` also calls host policy/generation callbacks, under its caller context;
those callbacks must be concurrency-safe, and Probe has no independent hard
callback deadline or Query admission slot.

## Pinning, licenses and isolation

The approved artifact identity is narrowly Linux amd64 portable v0.12.1:

| Identity | Value |
| --- | --- |
| Source/tag commit | `a7158ac2f48ca646b4527341b76f930f2aeff87e` |
| Archive SHA-256 | `8c3d0341064301e78ecc84cf7348d4e2c6562a0f57448243ea560aaa34de36c1` |
| Executable SHA-256 | `77745491f0a9c346d6da23eed4d92b5feaa31a27a883c9faa4fa9f47c8183705` |
| Executable bytes | 330641600 |

The [source/license audit](evidence/codebase-v0.12.1/source-audit.json) records MIT
at the root plus vendored and UI notices. The normal portable and UI-portable
assets have the same digest, and the bundle carries 194 UI package-notice
sections. Retain the upstream license and third-party notices before any future
distribution. This change vendors no external executable or source implementation.
The local 1083-entry vendored checksum check is consistency with the pinned
source manifest, not independent upstream verification, binary SBOM, signature
verification, reproducible-build attestation or universal license certification.
The declared `webgl-constants` UNKNOWN heading contains MIT text underneath;
license metadata still needs review when producing a distribution. Broader Adobe
PDF-derived table rights and all distribution requirements remain part of that
future review. The reference-only Eclipse LSP listing is not a bundled EPL server.

The [pinned CLI source](https://github.com/DeusData/codebase-memory-mcp/blob/a7158ac2f48ca646b4527341b76f930f2aeff87e/src/main.c)
shows that even one-shot CLI calls can bootstrap a temporary daemon, despite a
stale opening comment. CLI is not a way to bypass lifecycle containment. The
query-only test uses the existing MCP SDK over bounded IO transport, a private
PID/network/mount namespace and an analysis profile. It seeds UI, auto-index,
auto-watch and watcher settings false before launch; only a separately prepared
synthetic index is copied into temporary staging. HOME/XDG/cache/IPC are private,
source is read-only and no personal configuration or host sockets are mounted.
No installer, hook, client configuration or paid API is used. CPU/AS/file/process
rlimits are explicit bounds, not measured consumption; NPROC is per UID. PID
namespace teardown bounds the whole daemon tree even if a child detaches.
Future host wiring must implement and validate equivalent lifecycle containment;
this SPI does not make an arbitrary externally supplied transport safe.

## Preserved feasibility evidence and validation

The [local feasibility summary](evidence/codebase-v0.12.1/local-feasibility.json)
retains all four original attempts without rescore:

1. Bubblewrap failed before the driver under NPROC 256. Its exact stderr cause
   was not retained; concurrent UID threads are a suspected explanation only.
2. MCP initialized but indexing `/repo` was refused by the shallow-root policy.
3. Moving the same sources to `/workspace/project` allowed real staging indexing
   and reader calls, but the original oracle required a trace file field absent
   from the pinned schema: it remains `ORACLE_NOT_PROVEN`.
4. A separate query-only proof on that existing index used the documented
   grouped name/hop model and confirmed Helper, one Entry caller at hop 1,
   absence of Unused in that traversal, and Entry's outline.

The original 332.617 ms index observation and 804.058 ms initialization remain
separate from the approximately 11–12.4 ms warm reader calls. Sampled index-run
peak was three processes and 76263424 bytes summed RSS; shared pages can be
counted repeatedly and peaks/short-lived CPU can be missed. Graft's existing
adapter also processed the same source bytes, but Node starts per call and
contracts/measurement scopes differ; its outer wait4 values establish launcher
accounting, not Node CPU/RSS. No causal speedup, equivalent quality, token/session
savings, multilingual/infra/cross-repository certification or completed IVOAI-190
follows from this five-file fixture. The frontend was intentionally cancelled
for cleanup; no graceful frontend-exit certification is claimed.

The [adapter validation report](evidence/codebase-v0.12.1/adapter-validation.json)
records Go 1.27.3 focused tests, vet and race passing locally. The explicit real
reader also passed under race: one symbol, one direct caller, three permitted
MCP calls and a byte-identical prior index. After that proof, the final incomplete-
receipt projection change was checked by focused tests/race/vet; the unchanged
complete-receipt transport case was not rerun. Package test binaries cross-compiled
for Linux amd64 (`GOAMD64=v1`) and ARM64 with `CGO_ENABLED=0`; they do not establish
ARM64 runtime or non-AVX-machine certification. No full Go suite, CI, release or
product/client certification is claimed by this local slice. The report retains
actual source hashes and distinguishes a skipped ordinary invocation from the
explicit real pass.

The [development history](evidence/codebase-v0.12.1/adapter-validation-history.json)
retains failed schema probes: the initial adapter parser/fixture wrongly expected
an anchor at hop 0, while the pinned backend excludes anchors. After correction,
a [review-stage race run](evidence/codebase-v0.12.1/adapter-review-deadline-failure.json)
hit the existing two-second budget because its host test callback rehashed the
330 MB executable on every admission recheck. That is a failed host-harness run,
not measured CBM query CPU or a passing real test. The budget was not increased;
the approved read-only snapshot integrity check described below removed redundant
per-check hashing while preserving full-session verification. A test-helper local
variable compile error was also corrected before execution. None of these failed
runs or the original four feasibility attempts was rescored.

Skipping the opt-in test is NOT_RUN, not a real pass. A real reader run requires
the approved binary and the separately indexed five-file synthetic fixture
described in the committed evidence:

```sh
IVOAI_TEST_CODEBASE_BINARY=/private/tmp/codebase-memory-mcp \
IVOAI_TEST_CODEBASE_FIXTURE=/private/tmp/previously-indexed-synthetic-fixture \
  go test ./internal/codebase -run '^TestRealPinnedCodebasePreviouslyIndexedFixture$' -v -count=1 -timeout=60s
go test ./internal/codebase -count=1
go test -race ./internal/codebase -count=1
go vet ./internal/codebase
```

The opt-in test invokes the real server through the SDK and asserts three
read-only calls, exact symbol/source span and one direct caller. Its receipt
checks hash source/index on every check. The actual 330 MB pinned executable is
hashed before/after the session; intermediate callbacks use non-following Lstat,
SameFile, size, mode, mtime and ctime checks on the previously approved read-only
bind. This assumes the host preserves its approved snapshot and does not protect
against a privileged host/root or same-UID actor able to replace the source or
namespace. The query budget includes the initial admission check. The prior-index
copy remains isolated; the database must remain byte-identical. It never obtains indexing permission
from Query or starts a new index. Rollback is simply to omit/disable the injected
provider; no stored-data migration, Graft/default change or client rollback is
required. No ARM64 runtime or execution on a machine lacking AVX/AVX2 is certified.
