# Combined controls, Memory SPI and publication documentation validation

This later integration epoch checks source
`0b073ec180e7c3e9e1e59f83813c5c10781077d1`, tree
`ccb6aa8274d08f9e84d488f4c62228b060619dc6`, after the local merges of the reviewed
Memory head `6dbdfa21` and publication documentation head `fb4b7a17` into the
controls/TUI-sync head `9a695e0d`. It does not replace their earlier receipts.

The [new receipt](evidence/backend-first/gateway-controls-memory-publication-v1/validation.json)
and source map verify all **665** changed paths against the three frozen heads:
112 controls, 47 Memory and 506 publication paths. All ten deleted paths remain
absent. Product code was not edited during this validation.

With Go1.27.3, local full Go tests passed **59 packages**, with three no-test
packages recorded separately. Race tests passed all six affected App, Evidence,
Knowledge Router, orchestrator MCP, WorkingContext and bridge packages. Full vet,
65 Python tests, CLI-reference generation, formatting and diff checks passed.
Opt-in skips are listed explicitly and are not successful live integrations.
The mandatory real legacy/current protected-artifact rollback gate passed its
one named test with zero skip events.

The docs markers, build/check scripts and all embedded files match reviewed
`fb4b7a17`. **No new local Node build was run** and no node_modules were borrowed
or installed; fresh hosted CI must rebuild EN/PT. The four actual pinned TUI
scenarios were also not rerun: the reviewed `72bd76ef` test and `.3` assets remain
byte-identical. Their prior adversarial failures and successful checks retain
their own scopes and timestamps.

No provider inference, institutional authentication, ingestion or production
operation occurred. No dependency, audit threshold, runtime pin, release body,
tag or asset changed. The prior LOW docs advisory remains `needs_review`; this
epoch does not claim a new audit or zero vulnerabilities. The published v0.14.0
source and assets remain immutable. No economic, client-parity, ARM/no-AVX or
institutional Memory certification follows from these local checks. Separate
PR52/53 gates and merge requirements remain, and the combined PR51 source still
requires its fresh 16 hosted checks.
