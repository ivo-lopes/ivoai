# Claude 2.1.318 startup on emulated non-AVX amd64

The already-reviewed official Claude Code **2.1.318 Linux amd64** executable
successfully ran `--version` and `--help` under **QEMU 10.0.13 user-mode**, CPU
`Nehalem,-avx,-avx2`. The CPUID helper observed `avx=0` and `avx2=0` inside the same
emulation boundary. Both commands returned exit0 without a signal or stderr:
version: 1.145 s / 22 output bytes; help: 3.361 s / 22,344 output bytes.

The binary SHA-256 remains unchanged:
`708c77b204db30086ff6f1ed34577bda03e1be74ced80557cbf7b54249ebf0bd`.
No alternate asset, hash exception, downgraded runtime or personal configuration
was needed. This result does not support presuming an AVX/AVX2 startup requirement
for that official asset.

The QEMU executable was reused from the prior private admin validation workspace.
Its Debian package digest was rechecked against that workspace's existing
APT-metadata provenance receipt; no package was installed globally. Bubblewrap
provided separate network/PID/user namespaces, an empty private HOME and a clean
environment. Only `--version`/`--help` were executed; no authentication, inference,
provider endpoint, personal configuration or subscription credential was used.
No vendor executable is included in these evidence files.

This certifies **emulated startup only**, not native Nehalem hardware, runtime JIT
behavior throughout an agent session, tool execution, inference or full Claude
parity. The user's explicit lack of a Claude account leaves real Claude inference
pending under IVOAI-194/197. Existing offline protocol evidence remains separate.

[Startup summary](evidence/claude-noavx-startup-20261106/summary.json),
[QEMU package provenance](evidence/claude-noavx-startup-20261106/qemu-provenance.json)
and [rechecked artifact/isolation metadata](evidence/claude-noavx-startup-20261106/provenance.json)
retain exact hashes, CPU observations, command outcomes and timings. Full stdout
and stderr remain in the private proof workspace; the public summary stores only
bounded metadata and digests.
