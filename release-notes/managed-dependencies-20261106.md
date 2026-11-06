# Managed Headroom and JavaScript refresh, November 6

The optional Headroom runtime now pins upstream0.43.1 with independently checked
architecture-specific wheels and96-package hashed locks. Actual isolated
production installation and repeated admission passed in30.04s. CPython3.14.10
and uv0.13.7 remain the reviewed runtime. Fresh binary-only resolution on stable
Python3.15.2 failed because required PyYAML wheels are unavailable; that failure
is retained rather than enabling unreviewed native source builds.

Ruflo advances to3.72.2, pnpm to12.18.0, Playwright to1.65.0, OpenTelemetry/core
to3.1.0, Glob to14.0.4 and Minimatch to11.0.5. Real isolated locked Ruflo install
and repeated admission passed in36.99s. Registry signatures and integrity were
verified for these exact packages. The three new Playwright recency exceptions
are exact versions with that verification; the release-age policy remains intact.
Node26.13.0, bundled npm11.20.0 and the reviewed Braces/Zod/deepmerge compatibility
patches remain. Runtime and documentation audits both report zero known entries.

Focused Go packages and vet, the patch regression tests and six administrative
browser groups passed, including four actual backend/OAuth/Store scenarios with
Chromium158. No provider inference, personal configuration change or production
deployment occurred. See the [source-bound validation record](evidence/managed-dependencies-20261106/validation.json)
and its retained installer, audit and failure evidence.

Headroom remains optional and managed wrapping remains unavailable_by_policy;
Caveman remains the compression default with Direct fallback. This update does
not certify compression quality, ARM/no-AVX execution, provider parity or session
economy. Latest Caveman binary4.0.2 was investigated but is excluded from active
pins because independent binary vulnerability checks found vulnerable x/net and
klauspost/compress code. Its potential repair is separate work, not an ignored
advisory. Historical benchmark results are unchanged.

Rollback selects the previous Core with its matching retained runtime profiles.
Full composed CI and the final regenerated documentation remain release gates.
