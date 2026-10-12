# Reviewed Graft library build

The source is `trailhq/Graft` commit
`fe30ead39d5e6f0c921018d364da2bdbc9d4b3ad`, version `0.21.1` (MIT).
The source tar SHA-256 is
`ab27f9123eeb59b3987a342a24b7e357ad3412f41c556fde3a76ca9e250d2db1`;
the npm lock SHA-256 is
`c05064ea18ed1b538e27cc788f1d6ea2cae00b2c00c79ef552fc1bb4c54fd5f5`.
The managed artifact identity is `0.21.1-ivoai.1`. No upstream tag is moved.

Run `scripts/build-managed-graft.sh OUTPUT_DIRECTORY` on Linux amd64 or arm64.
It requires bubblewrap, Python 3, curl, GNU tar/patch, binutils, make and a C++20 compiler.
Release and CI builds select the native GCC 12 toolchain with
`GRAFT_GCC_MAJOR=12`, installed only in ephemeral runners.
The pinned Node 26.11.1 distribution provides both the runtime and build headers.
The runtime is an external managed component; it is not duplicated in this tar.
An amd64 build uses `-march=x86-64 -mtune=generic -mno-avx -mno-avx2`;
an arm64 build uses `-march=armv8-a`. Cross compilation is not supported.

To use already verified downloads and the npm content cache without a network:

```sh
GRAFT_SOURCE_ARCHIVE=/cache/upstream.tar.gz \
GRAFT_NODE_ARCHIVE=/cache/node-v26.11.1-linux-x64.tar.gz \
GRAFT_NPM_CACHE=/cache/npm-cache GRAFT_OFFLINE=1 \
  scripts/build-managed-graft.sh /cache/artifacts
```

Local archives still undergo the same pinned SHA checks. Only `_cacache` is
copied from the supplied npm cache. The installer has an empty environment and
private HOME/XDG paths, and cannot access the operator's configuration or tokens.
The source tree is fresh. `npm ci --ignore-scripts --no-audit --no-fund` installs
only the pinned lock; every package must have an npm registry URL, SHA-512 SRI
and license. Registry fetching is the only sandbox phase allowed networking;
`GRAFT_OFFLINE=1` also removes networking from that phase.

Reviewed execution is limited to:

- TypeScript's compiler for the root and viewer tsconfigs.
- `node-gyp rebuild` with managed Node headers for tree-sitter and its nine native
  grammar packages. Their reviewed binding.gyp files compile parser/scanner and
  binding sources, with the node-addon-api target lookup; no lifecycle scripts
  run. All upstream native prebuilds are removed from the payload.
- `scripts/build-viewer.mjs`, which invokes esbuild for the viewer and copies its
  HTML/CSS and the graph query files.
- `npm prune --omit=dev --ignore-scripts --offline`, followed by the reviewed
  `check-runtime.mjs` library smoke against the final cleaned runtime payload.

`tree-sitter-node26.patch` changes only two Linux C++ standard flags in
`tree-sitter@0.21.1` from C++17 to C++20, required by Node 26's headers. It does
not change parser behavior. The patch applies with zero fuzz and is hashed in
provenance. No upstream CLI, `init`, MCP, telemetry stamping, package prepare or
postinstall entry point is executed. The host system is read-only in the build
sandbox, and compilation and runtime smoke have no network namespace access.

The tar contains the IVOAI `graft-runtime` version-only executable and `dist/` (including viewer assets and `.scm` queries), runtime
`node_modules/`, upstream `package.json`, the pruned `package-lock.json`, LICENSE,
`dependencies.json`, `provenance.json`, `abi.json` and `files.json`. Dependency inventory
includes all locked build/runtime/optional packages and their original SRI and
license declarations; actual payload files have individual SHA-256 hashes.
Package license files and grammar node-types JSON remain in their packages.
TypeScript sources/declarations, sourcemaps and compiled C/C++ source files are
omitted from the runtime payload; upstream source is identified by its pinned
archive and dependency SRI values. Tar order, ownership, modes,
timestamps and gzip header are canonicalized; host compiler and build-script
hashes are recorded. Repeatability applies to the same recorded toolchain and
architecture, not arbitrary Linux distributions. libc/libstdc++ compatibility
must be validated on release targets. `check-abi.py` blocks packaging if any
addon requires GLIBC newer than 2.35, GLIBCXX newer than 3.4.30, CXXABI newer
than 1.3.13, or a private/non-numeric ABI version. These limits are unconditional;
a newer host compiler may compile successfully and still fail this release gate.
The local GCC 14 experiment demonstrated that failure (GLIBCXX 3.4.31 and
CXXABI 1.3.15). Use GCC 12 on each native architecture and require the ABI gate for release;
the build does not silently fall back to incompatible host artifacts.

`python3 scripts/test-graft-abi.py` checks compatibility version boundaries.
`python3 scripts/test-build-managed-graft.py` tests archive boundaries and
reproducibility without installing or executing upstream software. The build
smoke imports the graph library, parses with every native grammar, requires all
registered WASM and container grammars to warm, and checks viewer/query assets.
`scripts/test-graft-baseline.sh ARCHIVE METADATA` exercises the exact pinned
managed Node and all native/WASM grammars under QEMU Nehalem, in the isolated
no-network sandbox. CI and release run it on amd64 and build natively on arm64;
the local environment did not have QEMU or GCC 12 available for these gates.
Native arm64 and real no-AVX execution require their respective runners; compiler flags alone do not certify the entire Node/runtime platform.
The version probe accepts only `--version`; every other invocation exits 2.
The archive is `ivoai-graft_linux_ARCH.tar.gz`, with SHA-256 sidecar and
`managed-graft-ARCH.json` containing the supply-chain object identity. Its
revision hashes source, reviewed build-input aggregate and archive SHA-256
values, each followed by a newline, as in the managed OpenCode build.
This script does not publish, install, enable or initialize Graft for a user.
