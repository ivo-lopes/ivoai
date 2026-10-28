# G0 — combined main after PR49

This epoch integrates main `6d99e320912f3f7325c3009209a10e9df86610d4` into PR50 in local merge `04245f18886195c0bbd7c33c037742a71a372d40`. The Release workflow had one context conflict: the protected rollback step is retained before upstream's consolidated sandbox preparation and Claude boundary gate. Removing only the four-line protected rollback step from each resulting workflow reproduces the corresponding main workflow byte for byte. CI retains its separate Claude job; Release retains both required protocol gates and `build` depends on `verify`.

The annotated `v0.14.0` object remains `3943e54ecbecf380429b89ad5a28a0500d138891`, targeting main `6d99e320912f3f7325c3009209a10e9df86610d4`. G0/PR50 remain outside that tag. No tag, release, deployment or PR47 change is made here. The original G0 and main-a7db epoch receipts/failures remain unchanged; their results are not relabelled as results for this head.

Actual local combined-source checks:

- Full Go tests passed in 59 packages, with three packages without tests. Opt-in skips remain explicit and do not establish live integration.
- Race checks passed in eight affected packages: workers, App, Orchestrator, Evidence, WorkingContext, external MCP, agents and quota. Full repository race on the exact final PR head remains a hosted CI gate; the preceding a7db epoch's full race is not substituted for it.
- The source-pinned offline rollback proof passed with the required named test, validator and zero skips.
- The official Claude 2.1.296 checksum wrapper passed all five mandatory protocol cases against the combined factory. It used the reviewed cached archive, synthetic loopback provider, private HOME and isolated network. No real provider authentication or inference occurred; this does not prove authenticated native-client parity or G0 evidence consumption by Claude.
- Full vet, all shell scripts under audited ShellCheck, Python discovery and Linux amd64 v1/ARM64 builds passed. These are validation binaries, not release packages or ARM/no-AVX hardware certification.

The [new machine receipt](evidence/protected-evidence-recovery/epoch-main-6d99/validation.json) binds source, logs, archive/binary provenance and exact checks. The workflows and complete embedded site inherited from main remain subject to exact-head hosted CI before merge. No G1 reader, live Memory/Context retrieval adapter, retention policy, scheduler or worker-dispatch change is implemented in this epoch.
