# Antigravity1.5.2 CPU admission and isolated startup evidence

The current official binary fails even `--version` and `--help` on the tested
emulated Nehalem CPU without PCLMUL: both exit132/SIGILL, with the upstream
`go/sigill-fail-fast` diagnostic. CPUID independently reports AVX0/AVX2=0/PCLMUL0.
These failures are retained, not represented as compatible startup.

The same binary succeeds with AVX and AVX2 still disabled when PCLMUL is enabled:

| Emulated CPU | CPUID AVX / AVX2 / PCLMUL | Version | Help |
| --- | --- | --- | --- |
| Nehalem, AVX/AVX2 disabled | 0 / 0 / 0 | FAIL, SIGILL | FAIL, SIGILL |
| Haswell, PCLMUL enabled, AVX/AVX2 disabled | 0 / 0 / 1 | PASS,2.319s | Exit0, usage on stderr,2.349s |
| Nehalem, PCLMUL enabled, AVX/AVX2 disabled | 0 / 0 / 1 | PASS,2.370s | PASS,2.635s |

The Nehalem control changes only PCLMUL, isolating the observed startup condition
from other Haswell features. This does not certify every CPU without AVX or any
full session, inference, tool, quota or native ARM behavior.

IVOAI now checks the existing x/sys/cpu PCLMUL capability before Antigravity
runtime discovery and before a session lease/attempt. Unsupported CPUs receive
`ANTIGRAVITY_CPU_UNSUPPORTED`, rather than reaching the known upstream SIGILL.
The predicate is private and pure; there is no new binary pin, global mutable
feature override, GODEBUG change, paid-provider fallback or production version
probe. Existing binary, platform, permission and ownership checks remain.

A real Core invocation under QEMU Nehalem, with the experimental flag and an
empty private HOME, returns that error without mounting a client/authentication
or creating any session/attempt/request files. The same Core with PCLMUL enabled
passes the CPU check and refuses the deliberately absent runtime. App/nativeacp
suites and vet passed; these offline checks are separate from Native's existing
authenticated integration evidence.

Every probe uses the audited exact official binary and audited private Debian
QEMU, bubblewrap with no network, clean environment, fresh HOME,60s timeout and
64KiB limit for each output stream. No installer, large download, personal
configuration/authentication or inference was used. Source hashes, exact
commands, bounded streams and the complete result matrix are in
[validation](evidence/antigravity-cpu-20261106/validation.json).

Two unfavorable harness observations are also retained: QEMU rejected the initial
property name `pclmul` before client execution; the correct name is `pclmulqdq`.
The initial Haswell assertion expected help on stdout, although the official
client exited0 and printed valid help to stderr. Its original receipt remains
unchanged beside the explicit stream reassessment. Neither observation changes
the original Nehalem SIGILL failures. Haswell's TCG HLE/RTM warnings are retained.
