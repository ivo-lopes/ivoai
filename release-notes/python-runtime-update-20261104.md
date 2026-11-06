# Optional Headroom runtime dependency update

Headroom moves to0.42.0 and its private installer to uv0.13.7. Both Linux
architecture wheels and uv archives are pinned to the official release digests.
Binary-only, hashed transitive locks are refreshed independently for amd64 and
ARM64 with an explicit reviewed cutoff. The stale input files now match the
actual wheel instead of referencing the older0.36 release.

The selected interpreter is CPython3.14.10, the latest compatible stable patch
verified for this graph. Latest stable CPython3.15.2 was actually evaluated:
resolution failed for upstream PyYAML wheels; a private pure-PyYAML experiment
then exposed missing orjson wheels too. Neither experiment is distributed or
represented as supported. Avoiding an unsupported interpreter preserves the
existing binary-only installation policy.

The production installer now selects an environment identified by the exact
wheel, lock, uv, Python and cutoff, retaining earlier environments for rollback.
A real isolated production installation and repeated admission passed in29.16s.
Official version and wrapper-help probes passed without creating private client
configuration. Components, Headroom and compression tests and vet passed.
ARM64 lock resolution passed; the real installer run here is Linux amd64.

This update does not enable managed Headroom wrapping. Existing isolation and
exact-recovery restrictions still return unavailable_by_policy and keep Direct
fallback. Caveman remains the default. No inference, compression savings,
provider quota or global session economics are claimed by these installation
checks. A guessed compress CLI probe failed and remains recorded as a failed
probe, rather than a benchmark.

[Source-bound validation](evidence/python-runtime-update-20261104/validation.json)
records actual checks and limits. Previous component/runtime profiles remain
available; no personal Python installation, credential, client configuration,
external service or production store is changed.
