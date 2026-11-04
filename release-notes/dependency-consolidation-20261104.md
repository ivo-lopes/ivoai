# JavaScript dependencies and reviewed destination prototype protection

This increment updates the private Node runtime to the latest stable26.13.0,
Ruflo to3.71.1, pnpm to12.16.1, React/DOM to19.3.1, Playwright to1.64.1 and
axe to4.14.0, alongside the selected current documentation/runtime overrides.
Node27alpha is excluded. The original Braces and Zod compatibility security
patches remain; the latter already contains the latest upstream4.6.7 sources.
Go1.27.3 and the actual imported Go requirements are current in this snapshot;
no unused module is added solely to change an upstream test dependency graph.

The new Ruflo graph initially introduced six moderate audit entries. Explicit
current Glob14.0.3/Minimatch11.0.4 pins remove the reported vulnerable ranges.
OpenTelemetry/core3.0/API1.10, slim-bindings3.3.9 and the other explicitly owned
overrides remain in the locked graph. A real isolated installer admitted the
graph, checked the launcher and repeated setup idempotently in38.512s, with no
inference or personal runtime/configuration change. Runtime audit is zero.

Documentation's remaining deepmerge4.3.1 advisory has no published fixed release.
The [reviewed local MIT fork](../patches/deepmerge/README.md) patches both packaged
entrypoints and retains exact provenance/license. Ten tests pass; the same tests
on unmodified upstream fail six attack cases. The lock now resolves that exact
artifact, and the documentation audit reports zero known advisories. This is a
local patch, not an upstream release or a general absence-of-vulnerabilities claim.

pnpm selected one new exact recency exception for Joi18.3.2 while upgrading.
Its npm registry signature and integrity were independently verified. The
exception covers only this reviewed pin; it is not an audit ignore, open-ended
package/version permission, signature attestation or new install-hook allowance.

The Node change advances the managed Graft build identity to0.21.1-ivoai.2,
keeping the reviewed upstream source, source checksum and native/WASM license
inventory. Candidate build/verifier/bootstrap SHA values are updated together;
historical stable-release fixtures and published artifact bytes remain unchanged.
The local GCC14 build reached actual library/grammar/viewer smoke but the existing
ABI checker rejected GLIBCXX3.4.31. Required GCC12 hosted builds, ARM64 and no-AVX
gates remain mandatory. They are not represented as local successes.

[Validation and unfavorable observations](evidence/dependency-consolidation-20261104/validation.json)
bind this partial dependency increment to its source. Focused Go tests and vet,
157 Python tests,47 artifact checks,23 Zod fixtures, both documentation locales
and registry-signature verification passed. npm12.2.1 was tried but rejected a
transitive registry tarball under its new remote policy; it is not claimed to be
the managed package manager. Headroom/Python and the remaining component updates
are separate work. This increment does not complete the whole dependency request,
provider certification, administrative parity or the economic gate.

Rollback selects the previous Core and its matching component/runtime profile;
existing profiles are retained. No system Node/npm installation or user-owned
client credentials are replaced. Finish or cancel active work before downgrade.

The updated accessibility engine also exposed a brand-label mismatch: the A
was rendered only as a vector, leaving the text label incomplete. The wordmark
now retains its complete text while preserving the glyph. The full browser
suite passed152 checks with zero skips on the frozen fix; earlier failed
candidates are retained in the [separate evidence record](evidence/dependency-consolidation-20261104/wordmark-accessibility.json).

CI41907394705 passed the native Graft/architecture/install gates but failed
the separate administrative UI accessibility check. Both downstream server
gates were skipped, and are not successes. Local reproduction identified the
old administrative brand aria-label; removing that redundant label passed all
six hermetic UI layouts/check groups with the same axe engine. Fresh full CI
is required; see the separate admin-brand-followup evidence.
