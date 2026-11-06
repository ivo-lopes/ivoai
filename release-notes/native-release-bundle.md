# Native release bundle and preserved v0.16 baseline

New candidates contain exactly twelve public files: two Core archives, two Graft
archives and their two metadata files, two rebuilt Caveman archives and their two
metadata files, the Memory/Context skill ZIP and checksums. No managed OpenCode
archive or metadata can enter the current publisher or strict verifier. Caveman
is explicitly an IVOAI local rebuild, not a signed upstream binary. The current
verifier checks source/patch/archive associations, exact executable architecture,
inventory, provenance and linked-module notices without executing downloaded
assets. Actual native CPU, vulnerability and installer gates remain separate.

The old v0.16.0 twelve-file contract is preserved independently under
`scripts/release-baselines/v0.16.0/`. Its verifier and original adversarial tests
are byte-identical to `685c5740ec046dcbcf7eeec77c6fd88885975d8d`, with pinned hashes.
The original published AMD/ARM assets passed that verifier again locally. No old
Core was rebuilt and no historical receipt, asset, tag or benchmark was changed.

CI/release jobs replace managed OpenCode build/attach jobs with an exact official
Codex 0.167.0 pair, native App Server/TUI tests and baseline CPU startup. On AMD,
startup explicitly disables AVX/AVX2 under QEMU; ARM release/CI jobs run on native
ARM runners. Local ARM emulation is reported as emulation, and starting each
binary separately does not certify cross-architecture child `execve` behavior.
Protocol tests with a synthetic runner remain offline tests, not official
inference. Existing Core, Graft, Claude boundary, protected-evidence rollback,
installer, race, security and server gates remain required.

Caveman jobs build the reviewed source and dependency patch, verify a sealed copy
of the real archive, scan both binaries with pinned govulncheck text mode (findings
fail), perform version startup on the baseline CPU, and run the real installer
and executable fidelity canary. Missing or skipped opt-in installer tests fail the
qualification instead of being counted as success. Old managed OpenCode helper
commands now return explicit removal errors with nonzero status; they do not
download, build, probe or run that frontend.

The stable upgrade driver now accepts only the reviewed v0.16.0→v0.17.0 pair.
Published baseline Core hashes and its original setup assets are pinned separately
from current toolchain assets. Disposable preparation primes npm/UV wheel caches
and the exact reviewed Python interpreter without promoting a candidate active
profile. The network-isolated phase must still perform real CLI dry-run refusal,
upgrade, diagnostics, rollback and reapply, with previous Core/profiles and
synthetic user-owned OpenCode configuration/history/binary markers preserved.
Changes to the candidate require repeating that gate against the final bundle.

At this checkpoint, Python control/adversarial tests, native Codex pair startup
and offline verification of the real AMD Caveman archive have passed. Full
candidate bundle publication, native ARM jobs and real v0.16→v0.17 upgrade remain
pending the final composed source/assets and must not be described as approved
based on these control tests. No economic gain or provider parity is claimed.
