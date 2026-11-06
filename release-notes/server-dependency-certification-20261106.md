# Verified server dependency delivery — 6 November 2026

This supersedes the dependency **targets** of checkpoint `3f7d55747`; its earlier
3.3.3 proofs and failed attempts remain preserved. The implemented feature is the
explicit `server upgrade` / `server upgrade --rollback-receipt` path and its two
confirmed administrative-panel actions. Ordinary setup cannot silently migrate
existing stores or reset a valid operational configuration.

## Current sources and pins

Primary release metadata was checked again on 6 November. ai-memory advanced to
[3.4.2](https://github.com/akitaonrails/ai-memory/releases/tag/v3.4.2) on 5 November,
so the final target replaces 3.3.3. Its MIT license was read at that release.
Official AMD64/ARM64 archives were downloaded and verified against adjacent
upstream SHA-256 assets, and the OCI index digest/platforms were independently
verified against registry content. No upstream installer or credential-sharing
wrapper was run.

| Component | Final target | Integrity / compatibility |
| --- | --- | --- |
| ai-memory | 3.4.2 | AMD64 archive `15a4f4a346a27e6fd63a36d1b3de23c5cbe142b80b3bce561e21b1f225a1e645`; ARM64 `164f7c38bb9bafca218dd958c3c60febcd5d2f5200ad138c6f128576cedebfb6` |
| ai-memory OCI | 3.4.2 | `sha256:cf09de9f40d8d4f5214698826530e61e9061ad5e02001defc8a77d42ab4e50b7`; linux/amd64 and linux/arm64 |
| Qdrant | [1.20.0](https://github.com/qdrant/qdrant/releases/tag/v1.20.0) | `sha256:9bfc89f9914fc14876a88c05270685921a55b8877b254a98bcc1cc2f0a424cd2`; both server architectures |
| Docker Compose | [5.7.0](https://github.com/docker/compose/releases/tag/v5.7.0) | Both official binary/checksum pairs verified; native AMD64 and emulated ARM64 version execution passed; both deployment YAML configurations parsed with the official 5.7.0 executable |
| Docker Engine | [29.9.3](https://docs.docker.com/engine/release-notes/29/) | Observed on the private daemon; operator-owned engines are not silently replaced |
| TEI | [1.9.4](https://github.com/huggingface/text-embeddings-inference/releases/tag/v1.9.4) | Existing CPU AMD64/ARM64 OCI digests reconfirmed unchanged on 6 November |
| multilingual-e5-small | `614241f622f53c4eeff9890bdc4f31cfecc418b3` | Current model main revision reconfirmed unchanged, MIT; no new model/embedding cutover |

The standalone ai-memory binaries require at most GLIBC 2.34 according to their
ELF version tables. This ABI inspection does not substitute for native operating-
system certification. No new ARM native-hardware claim is made.

## Migration and recovery delivered

A kernel lock covers setup/upgrade/rollback and the stopped-service transaction.
An expired lock context cannot bypass another active transaction. Upgrade creates
a Core backup and separate private Qdrant snapshot, then restores isolated copies.
The **previous** approved pinned images must read those copies too, ensuring the
rollback backup is usable before a candidate is admitted. Candidate images then
start on the copies with network none, the preserved non-root identity, transient
private authentication and a staged trusted Core probe. MCP/collection access
without credentials must fail, authenticated database/index reads must succeed,
and authoritative wiki content must remain unchanged. Failed/exited runtimes stop
validation without fallback or another execution of the requested mutation.

Only then are new assets installed. The existing service-stability check is reused
after activation and rollback; a failed stage or unstable start leaves services
stopped for deliberate recovery. Candidate cleanup failure cannot be presented as
a successful validation. Existing operational settings survive both upgrade and
subsequent setup. A private receipt binds the exact backup/index hashes to the
previous runtime/unit assets. Explicit rollback restores data, index and pins
together and preserves the current forward index for inspection. Core Restore's
executable-configuration whitelist remains unchanged.

Prior baseline pins outside the reviewed 2.6.3/1.19.2 and current target require
separate certification and are rejected. The prior images must already be present
locally. The production CLI also requires Docker to report working memory/CPU/PID
cgroup support before changing any store. It does not weaken that requirement to
make a rootless test environment pass.

## Real validation and measured limits

The [real Docker result](evidence/server-dependency-upgrade-20261106/real-docker-upgrade-rollback.json)
and [execution log](evidence/server-dependency-upgrade-20261106/real-docker-upgrade-rollback.log)
record **71.80 seconds** for this isolated fixture. It exercised official images
through ai-memory **2.6.3 → 3.4.2 → 2.6.3** and Qdrant **1.19.2 → 1.20.0 → 1.19.2**,
using actual Core Backup/Restore, stopped runtime observation, baseline/candidate
copy startup and coordinated rollback. The approved document and a
vector-store contract fixture were read correctly, including the exact stored
vector/payload marker. The numeric fixture vector has three components; this is
not an embedding-model accuracy test. UID/GID 1000 were used across the preserved
stores and official runtimes. No production store/service/account was accessed.

The private daemon initially could not create runtime tasks because its systemd
cgroup driver requested host authorization. Those attempts were failures before
entrypoint execution. Only that disposable daemon was reconfigured; original
state/logs/images were preserved. It subsequently reported **cgroup none**, so its
CPU/RAM limits are explicitly **not claimed as applied**. The real fixture used
network isolation, non-root execution, no-new-privileges, process/file ulimits and
bounded deadlines. Production systemd and enforced production cgroup isolation
remain separate deployment gates.

Resource observation limitations are retained rather than turned into zeros.
`docker top` was unavailable on that daemon; the first observations record that
fact. Later observations read the unique runtime process inside each container:

| Observation | ai-memory RSS KiB | Qdrant RSS KiB |
| --- | ---: | ---: |
| Baseline after readiness | 36,400 | 94,220 |
| Candidate after exact reads | 49,844 | 141,260 |
| Restored baseline after exact reads | 40,168 | 141,236 |

These are point observations, **not peaks or equivalent-load comparisons**. The
baseline snapshot preceded fixture seeding while later snapshots followed reads;
CPU ticks are retained without converting them into a fabricated phase cost.
The higher observed candidate RSS has not been discarded. No session-token saving,
quota reduction or global economic pass is claimed.

[No-AVX execution](evidence/server-dependency-upgrade-20261106/ai-memory-noavx.json)
used the official 3.4.2 AMD64 release binary under QEMU 10.0.13 with Nehalem and
AVX/AVX2 disabled. A guest CPUID helper observed both flags at zero. Actual server
startup, authenticated status/read/query, 401 refusal, exact wiki preservation and
normal stop passed in 2.520 seconds, with observed emulator-process RSS 109,412 KiB.
This is ai-memory compatibility evidence, not an end-to-end no-AVX certificate for
every server dependency or native hardware performance.

[ARM64 execution](evidence/server-dependency-upgrade-20261106/ai-memory-arm-emulated.json)
used the verified official ARM binary under Cortex-A53 user-mode emulation and
verified private Debian ARM libraries. The same server/read/query/authentication
checks and post-stop wiki comparison passed in 3.322 seconds, with emulator-process
RSS 113,004 KiB. This is stronger than an archive checksum but **is not native ARM
execution certification**. No binfmt handler, host package or personal settings
were installed/changed.

Reviewed 3.4.2 hook rendering passed twice/idempotently under isolated HOME/settings.
Focused server/servercmd/components race and vet passed; required-input, flag/stdin
and actual CLI service matrix checks include the two new panel actions. Their
privileged production HTTP/systemd execution is not relabeled as certified by
those offline checks. The [evidence index](evidence/server-dependency-upgrade-20261106/evidence-index.json)
preserves public-safe hashes and original positive/partial results.
