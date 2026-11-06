# Current Caveman runtime with a reviewed dependency repair

The managed release now uses `4.0.2-ivoai.1`, built from current Apache-2.0
Caveman runtime4.0.2 source with minimal x/net/compress security updates.
The upstream4.0.2 binaries were rejected after independent binary scans found
GO-2026-6860/6865/6866; the repaired proxy/MCP scans have zero findings. This is
an explicitly named IVOAI rebuild, with checksum-only provenance and retained
licenses, rather than an upstream signature claim.

The build preserves static musl and the proxy's CGO tree-sitter grammars. Two
fresh amd64 builds produced identical archive bytes. Actual installation and
repeated admission through the production immutable manager passed, followed by
native structured probes and the22-scenario fidelity canary. QEMU tested both
programs' versions, proxy readiness and actual MCP log compression on ARM64 and
amd64 without AVX/AVX2. Native ARM hosted CI remains a release gate.

The same corpus passed on historical2.1.0 and the repaired candidate with exactly
28131 context bytes and7033 estimated tokens, one compression and16 passthroughs.
These are local deterministic tool-output checks, without provider inference;
there is no demonstrated incremental compression or session economy claim.
Exact artifacts and authoritative evidence remained recoverable with zero
semantic/exact/artifact mismatches or observability leaks.

Caveman remains default, Headroom optional and Direct the fallback. Development
builds require reviewed release metadata to install the repaired component;
missing metadata never selects an unreviewed upstream fallback. No personal
configuration, hooks, memory service or client authentication is changed.
The existing supply-chain allowlist now expressly names both packaged binaries;
unexpected executables are still rejected.

[Validation](evidence/caveman-security-rebuild-20261106/validation.json) retains
unfavorable observations: rejected upstream advisories, initial Zig flag/link
failures, permission-mode canary refusal, extra-executable admission refusal and
a first successful build whose read-only module-cache cleanup failed. Corrected
repetitions passed; those earlier results are not counted as successes. Full
composed CI, the native ARM build and public release publication remain separate
integration gates. See the [build contract](../patches/caveman/README.md).

The Core's actual imported Go requirements were also checked against current
module metadata and govulncheck: all are current and zero findings were reported.
Two upstream test-only graph modules expose newer releases but are not imported
by this project; no unused requirement was added solely to change another
module's tests. The first Core scan used an incomplete Go PATH and failed;
the corrected invocation is the reported successful scan.

The isolated published2.1 raw-object → repaired4 archive → published2.1 rollback
also passed in5.32s, including exact prior state restoration, repeated rollback
without side effects and the restored22-scenario canary. The private build now
checks exact patched go.mod/go.sum hashes before and after module fetching, and
a regression test covers read-only cache cleanup without following external links.
See [the additional rollback evidence](evidence/caveman-security-rebuild-20261106/rollback-and-lock-validation.json).

App integration now has a private per-instance catalog seam for isolated fixtures;
nil always selects the production catalog and its compiled release identity.
The original Caveman-default/Headroom/Direct assertions remain. New tests check
missing metadata with retained historical state, no MCP selection, no unsafe
installation/ownership, and READY status with explicit optional-compressor
fallback. The earlier full Go run failed two outdated App fixtures; its complete
log is retained. After the fixture correction, the full App suite, complete Go
suite and App vet passed. These are offline contract checks, separate from the
actual Caveman artifact tests. See [App contract evidence](evidence/caveman-security-rebuild-20261106/app-catalog-contract/validation.json).
