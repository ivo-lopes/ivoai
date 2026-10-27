# Experimental RTK OSS output views — IVOAI-200

This is an opt-in library slice, **not an installed runtime integration**. No
application, client configuration, compression default, orchestration policy or
release changes. `OutputViewOptions{}` is disabled; the two boolean options are
an explicit programmatic opt-in, with no new global CLI flag. This report and
its experiment evidence live outside the Docusaurus documentation tree. The dedicated helper is not
packaged or installed by this change. There is no session-economic gate result.

## Evidence contract

`Gateway.ViewOutput` accepts only a sealed `Ref` already issued after exact
ArtifactStore persistence. It cannot invoke a tool, rerun an original command,
fetch evidence or replay a mutation. It recovers the artifact before and after
transformation, under the existing organization/repository/worktree/session/
recipient/permission/query/generation contract. Revocation, parent-context
cancellation/deadline and generation changes fail closed, including provider
failure paths. An optional provider's own timeout or cancellation cannot fail
an otherwise active request: it falls back to exact Direct/reference-only
after the same final recovery and authorization, with fixed `provider_timeout`
or `provider_failed` diagnostics. Late results from the Gateway's transform
budget are discarded under this fallback policy.

The provider receives a copied byte snapshot and a host-selected filter. The
derived text is explicitly `derived_non_authoritative` and untrusted; the raw
reference retains `exact_required` fidelity. Views do not alter, replace or
duplicate the artifact and are not cached. Their smaller byte count is not a
provider-token measurement or proof of semantic task correctness.

Only complete, extracted `tool_read` plain-text **inventory** snapshots are
eligible, with separate explicit `Enabled` and `AllowDerived` admission. Filters
are limited to `git-status`, `git-log` and `grep`; no automatic filter detection.
Errors, stack traces, diffs, test reports, other evidence sources, incomplete
results, inferred relations and prior compression bypass. The host must label
output kinds correctly; this slice does not infer that a successful process
produced an inventory. The existing Gateway admits read-only retrievals, so
ingesting outputs from mutating operations remains outside this slice.

Unavailable, incompatible, failed, empty, unsafe, oversized or non-reducing views
fall back to the same exact raw representation, or its reference when the raw
exceeds the context budget. Only fixed diagnostic labels escape; provider stderr
is never exposed or persisted. Direct fallback still repeats exact recovery.
There is no Caveman/Headroom stacking or alternate compression authority.

## Immutable provenance and licensing

The previously researched `e05c7bdfacfcfe1f845d11f47339e4eae63eb3f5`
contains Cargo version 0.49.0, but **is not tag v0.49.0**. That tag resolves to
`b1c0dc00649c50fbe8930f849c800d4d6ca12091`; its release has no OSS asset.
These identities must not be substituted for each other.

This independent experiment pins official release **v1.1.0** and its direct
[tag commit 753a3b0e939f2e4490fc24e3a1984010f40a103c](https://github.com/rtk-ai/rtk/tree/753a3b0e939f2e4490fc24e3a1984010f40a103c).
The CLI package and text crate declare Apache-2.0; the optional engine uses
BUSL-1.1. Cargo enables the engine by default. The inspected
[manifest](https://github.com/rtk-ai/rtk/blob/753a3b0e939f2e4490fc24e3a1984010f40a103c/Cargo.toml)
and [release workflow](https://github.com/rtk-ai/rtk/blob/753a3b0e939f2e4490fc24e3a1984010f40a103c/.github/workflows/release.yml)
separate OSS assets using `cargo build --release --no-default-features --target …`.
An independently reproduced source build should additionally specify
`--locked -p rtk`. A default build or the full `rtk-*` archive is not an approved substitute.

| Item | SHA-256 |
| --- | --- |
| Official `rtk-oss-x86_64-unknown-linux-musl.tar.gz` | `4dec94a64035e08189ae82fd5408fe4ae9ae27a665fc07e3e3ec3c4c2d8e8a3e` |
| Extracted executable, 13,256,352 bytes | `0b80810970ce2c79c92deaa474bbcf6f1cf01ac93baab0e273ee6cb4ef950d10` |
| Included Apache LICENSE, 10,756 bytes | `33f694a31d9f9df3e292a9413a3cdb0113fa72698e2c719f657ff6b53165dcc4` |

The [publisher's OSS release asset](https://github.com/rtk-ai/rtk/releases/download/v1.1.0/rtk-oss-x86_64-unknown-linux-musl.tar.gz)
was downloaded without installation and matched the GitHub asset digest. Archive
inspection and extraction allowed only those two exact regular files, with
explicit sizes; links, additional entries and traversal were not admitted.
Cargo/source/reported binary version all agree on 1.1.0. `pipe_cmd.rs`, `lib.rs`
and `release.yml` are byte-identical between the older e05c source and this pin;
the Cargo version and lock are distinct. The inspected build script validates
and concatenates bundled TOML; no build script, installer or Rust build ran.

The [declared dependency inventory](./evidence/rtk-oss-v1.1.0-dependency-licenses.json)
covers all 251 registry entries in the pinned workspace lock, a conservative
superset of the OSS graph. All have registry checksums matching their official
crates.io version metadata and declared licenses. Four local workspace members
are separate from that registry inventory. MPL-2.0 appears for `colored` and
`option-ext`; CDLA-Permissive-2.0 appears for two `webpki-roots` versions. Permissive
and Unicode licenses also occur. This is declared metadata review, **not a
source-license audit, legal opinion, binary SBOM, signature verification or
reproducible-build attestation**. The release publisher's OSS build separation
is trusted for this local test; redistribution still requires license notices
and a complete distribution review. No third-party binary is vendored here.

## Execution boundary

`rtk pipe --filter <allowlist>` transforms stdin without receiving an original
command or arguments. The audited [pipe implementation](https://github.com/rtk-ai/rtk/blob/753a3b0e939f2e4490fc24e3a1984010f40a103c/src/cmds/system/pipe_cmd.rs)
reads bounded UTF-8 input and dispatches text filters; it does not call the
command runner, Tracker or recall. Startup telemetry is explicitly stopped
before configuration access by `RTK_TELEMETRY_DISABLED=1`, as implemented in
[telemetry](https://github.com/rtk-ai/rtk/blob/753a3b0e939f2e4490fc24e3a1984010f40a103c/src/core/telemetry.rs)
and [its opt-out contract](https://github.com/rtk-ai/rtk/blob/753a3b0e939f2e4490fc24e3a1984010f40a103c/src/core/telemetry_cmd.rs).
Hook warnings are suppressed and `RTK_RECALL=0`/`RTK_TEE=0` are explicit.

The provider snapshots the host-selected executable into a private temporary
directory. A minimal Bubblewrap root exposes only the static trusted helper,
that read-only snapshot and private devices/directories. HOME/XDG/cwd are empty;
environment is allowlisted, PATH nonexistent, all namespaces isolated including
network, and the root is remounted read-only. No personal files/configuration,
repository, credentials, host executables or provider API environment is mounted.

**Hash verification, `--version` and `pipe` all run inside this containment.**
The strict helper verifies the hardcoded executable hash, accepts only version
or those three filters, seals pipe input into a RAM file capped at 1 MiB and
uses the existing `codesandbox` Landlock/seccomp launcher. No write grants,
network or child processes are allowed after exec. A separate RTK tracking
database, telemetry marker or other file cannot be created. No service runs.
Missing Bubblewrap, Landlock, seccomp, static helper, expected hash or exact
version yields a fixed unavailable result, with no weaker execution fallback.

The provider allows one concurrent transform per instance, a 1.5-second process
deadline for version+pipe, 1 MiB stdout and 4 KiB private stderr. That timer starts
**after executable snapshot filesystem I/O**, not before all provider work, and
does not bound all filesystem I/O. The Gateway passes a **cooperative 2-second
context budget** to `Transform`; an arbitrary Go callback can ignore cancellation
and return later. The host discards late output but cannot forcibly terminate
that callback. Parent cancellation likewise prevents evidence emission without
guaranteeing an immediate callback return.

RTK inherits 2 CPU seconds, 512 MiB virtual address space, 32 file descriptors,
zero file/core size; managed process groups are killed on cancellation. Rlimits
and output-size limits are enforced bounds; context deadlines are cooperative
cancellation budgets, not strict callback-return deadlines. These are not
measurements of consumption. Containment does not protect against kernel
compromise or a malicious host process with the same user privileges.

Only Linux amd64 has an admitted binary pin. Other platforms fail unavailable
and retain Direct. No claim of ARM64 certification or testing on a CPU without
AVX/AVX2 is made. Backend defaults and supported architectures remain unchanged.

## Reproduction and observations

After independently verifying and safely extracting the exact OSS archive,
build only the trusted helper in a private temporary directory. No RTK install,
hook or Cargo build is needed:

```sh
CGO_ENABLED=0 GOAMD64=v1 go build -o /private/tmp/ivoai-output-view-sandbox ./cmd/ivoai-output-view-sandbox
chmod 0500 /private/tmp/ivoai-output-view-sandbox
IVOAI_TEST_RTK_OSS=/private/tmp/rtk IVOAI_TEST_RTK_HELPER=/private/tmp/ivoai-output-view-sandbox go test ./internal/rtkview ./internal/evidence -run 'TestRealPinnedOSSView|TestRealRTKGatewayABBA' -count=1 -v
go test ./internal/evidence ./internal/rtkview ./cmd/ivoai-output-view-sandbox
go test -race ./internal/evidence ./internal/rtkview
go vet ./internal/evidence ./internal/rtkview ./cmd/ivoai-output-view-sandbox
```

Real tests require explicit paths and otherwise report **NOT_RUN / skipped**.
Skipped runs are not real-integration passes. Each AB/BA arm uses the same
already-persisted synthetic snapshot, authorization, generation and 32 KiB
budget; six repetitions alternate baseline/candidate order and retain every
numeric observation. Both arms use exact Gateway recoveries. No inference,
paid API, tool command or original operation runs.

Validation completed locally: `go test ./...`, the focused race tests and vet
passed. The explicit real-RTK tests passed separately, including a wrong binary
hash, oversized helper stdin and a disallowed filter being rejected. Linux ARM64
evidence tests cross-compiled successfully; they were not executed on ARM64.

The [recorded run](./evidence/rtk-oss-v1.1.0-output-abba.json) with Go 1.27.3
retains all 60 observations and produced:

| Snapshot | Raw bytes | Candidate view bytes | Baseline total µs, 6 runs | Candidate total µs, 6 runs |
| --- | ---: | ---: | ---: | ---: |
| Repeated grep inventory | 1,850 | 314 | 760 | 272,057 |
| Tiny grep, non-reducing bypass | 9 | 9 | 687 | 275,295 |
| Diff, policy bypass | 81 | 81 | 707 | 1,161 |
| Test report, policy bypass | 45 | 45 | 614 | 653 |
| Stack/error, policy bypass | 57 | 57 | 596 | 590 |

An earlier completed run recorded baseline/candidate totals in µs of
781/287,807 (grep), 644/285,372 (tiny), 685/682 (diff), 619/1,607 (test report)
and 628/634 (stack/error). A subsequent observation-logging run recorded
809/282,431 (grep) and 695/725 (test report). Variation and unfavorable latency
remain part of the evidence; these small timings are not statistical estimates.

The separate [independent review observations](./evidence/rtk-oss-v1.1.0-independent-review-observations.json)
preserve scenario aggregates and one unfavorable row from an additional race
run. This is an explicitly declared **subset**, not a replacement or complete
copy of the original 60 observations. In `tiny_grep`, repetition 4, candidate
arm 1 returned `provider_failed` after **28,943 µs**, with the original 9-byte
Direct representation correctly preserved. That observation is an availability
failure of the optional optimization, **not a successful RTK transformation**.
No rerun was performed to erase or replace the failure.

The same review's non-cooperative callback probe used a 100 ms parent deadline
and a callback taking 300 ms: return occurred after 301 ms with no output and
`context deadline exceeded`. This demonstrates the cooperative cancellation
limit described above; it does not demonstrate a strict 100 ms or 2-second
callback-termination guarantee.

Every raw snapshot was recovered byte-for-byte. The duplicate grep fixture's
83.0% output-byte reduction coexists with about 45–48 ms additional local latency;
the tiny case gains nothing while paying that cost. These are narrow fixtures,
not representative tasks, economic certification, token savings, CPU/RAM usage
measurements or quality parity. Provider input/output/cache tokens, model cost,
quotas and whole-session performance were not measured. Security tests verify
revocation/change/cancel handling, scope/ownership/seal rejection, copied input,
exact-required bypass, budget fallback, unsafe view rejection and wrong-binary
fingerprint rejection. No external security certification is claimed.

Unfavorable/preflight results are retained: the first real invocation was
rejected before RTK execution because the locally compiled helper inherited mode
0775; changing only that temporary artifact to 0500 satisfied the intended
writeability guard. The first AB/BA test fixture used its initial 1 KiB context
budget, correctly returned reference-only for the 1,850-byte baseline, and failed
its full-text assertion; both arms were subsequently given the same 32 KiB
budget. Neither failure was reclassified as a successful observation.

The 1 KiB preflight now has its own regression: disabled views and provider-only
timeouts return reference-only with no text, while all 1,850 original bytes stay
recoverable. Production evidence/context limits were not changed. Tests also
distinguish parent cancellation/deadline (no result) from provider cancellation,
provider timeout and the Gateway's own transform-budget timeout (validated
fallback), and retain revocation/generation-change-after-error rejection.

Before application adoption: independent review; full baseline/candidate task
matrix and quality/security gates; distribution/license review; non-AVX and
ARM64 validation where supported; real git-status/git-log coverage; and explicit
host routing with sufficient expected benefit. Rollback is immediate: keep
`Enabled=false` or omit the provider/helper; exact ArtifactStore recovery and
Direct remain available. Do not infer that this experiment completes IVOAI-200.
