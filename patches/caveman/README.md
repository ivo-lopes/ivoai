# Reviewed Caveman security rebuild

`4.0.2-ivoai.1` rebuilds Apache-2.0 upstream commit
`b4531dfc05a3ef0ba93b57e8bfa5f48564a4b4d6` (runtime `bin-v4.0.2`).
The source tar SHA-256 is
`29ef4c5bccee70dbc486bae266f966b8bd53132c6e12e27e9b06b36cf35c51f4`.
The dependency patch SHA-256 is
`5f95e80d41d5744f16e09d534b1d56585fe746f9d5aed1e14c2586bbe953810d`.
It updates x/net0.60→0.62 and compress1.20.1→1.20.2, with the Go module
solver's required x/sys0.49, x/sync0.24 and x/text0.43 graph/sum changes.
It changes no compression, security, client, model or orchestration policy.

Independent binary govulncheck1.7 found compiled vulnerable functions for
GO-2026-6860, GO-2026-6865 and GO-2026-6866 in the latest upstream assets.
The upstream tag verification and binary digests do not make these findings
safe. Original reports remain in the validation evidence; no advisory is ignored.
The repaired binary scans have zero findings. This does not prove absence of
unknown vulnerabilities or exploitability of every original symbol in IVOAI.

Run `scripts/build-managed-caveman.sh OUTPUT` on native Linux amd64/ARM64 with
Go1.27.3, bubblewrap, Python3, curl, patch and binutils. The script fetches
checksum-pinned source and Zig0.16.0. Module fetching uses the exact patched
Go sums inside a clean sandbox; compilation and tests have no network access.
`CAVEMAN_SOURCE_ARCHIVE`, `CAVEMAN_ZIG_ARCHIVE` and
`CAVEMAN_GO_MODULE_CACHE` allow verified private caches; `CAVEMAN_OFFLINE=1`
removes network access from module fetching too. No upstream installer runs.
No personal home, credentials or configuration is mounted in the build sandbox.
The native build never drops CGO to obtain an easier green result: the proxy
retains tree-sitter with static musl. The MCP remains pure Go, as upstream.
Zig baseline CPU plus Go amd64v1 exclude AVX/AVX2; ARM uses baseline ARMv8-a.
The upstream release-shape tests require TypeScript/JavaScript/Python compression.

Each archive includes both programs, original Apache/MIT/NOTICE/license history,
Go/runtime/module notices, Zig and musl notices, embedded font notices,
`provenance.json`, and a SHA-256/size inventory of all regular files except the
inventory itself. Source/patch/artifact digests jointly define the immutable
revision. Build metadata initially has an empty URL; release automation supplies
an exact versioned IVOAI release URL before compiling Core. The compiled
validator rejects changed source/patch, platform, mutable URL or artifact identity.
No upstream binary signature or source attestation is claimed for the local fork.

The existing supply-chain executable allowlist explicitly includes only these
two files; unknown executable entries remain rejected. Release metadata is
required for new installations. Development builds without it diagnose Caveman
unavailability and retain Direct fallback, without downloading an upstream binary.
Existing objects are not deleted. Rollback uses the previous Core and its retained
component objects; finish or cancel active work before downgrade.

The live tests exercise native installation, immutable admission, structured
probes and the existing22-scenario fidelity canary without inference. Independent
QEMU probes distinguish ARM64 emulation from native ARM certification. Compression
of synthetic tool output and estimated tokens are not session economic evidence.
