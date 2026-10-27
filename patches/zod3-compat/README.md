# Explicit V3-compatible Zod fork

This is `@ivoai/zod3-compat@3.25.76-ivoai.1`, an IVOAI-maintained local fork,
not a new upstream Zod 3 release. Its root API remains V3. The complete upstream
`zod@4.6.7` payload is the base: it supplies the newer V3 compatibility tree and
already-patched V4 subpath implementations. Root JavaScript and declaration
wrappers select V3; `zod/v3` remains V3; V4, mini and compile exports retain the
upstream V4 implementation. No old vulnerable V4 payload from `zod@3.25.76` is
carried forward. Existing V4 dependency ranges must continue to use the official
patched V4 package, not this V3-versioned fork.

Upstream: https://registry.npmjs.org/zod/-/zod-4.6.7.tgz

Upstream source revision: `b500a2ee95134f81d74da7911d7378dd2853d042`.
The archive SHA-256/SHA-512, patch SHA-256 and generated artifact identities are
recorded in `provenance.json`. The upstream MIT license and copyright are retained
in the packaged `LICENSE` and copied here. The checksum trust boundary remains the
reviewed registry archive; no independent publisher-signature verification is
claimed.

## Security change and limits

GHSA-mrhx-c7xc-m3w5 has an official V4 correction, commit
https://github.com/colinhacks/zod/commit/a0ebf970f2d752e5f58527cd2942ff48be4f46bd.
The V3 compatibility tree in official 4.6.6 and 4.6.7 still visits every element
of an oversized constrained collection. Merely importing `zod/v3` from a newer
package does not fix that behavior.

`4.6.7-v3-upper-bounds.patch` adds early rejection to V3 `ZodArray` upper/exact
length checks and `ZodSet` upper-size checks. The existing V3 `too_big` issue,
path, limit, custom message and contextual error-map contract are retained.
An oversized container now aborts before element iteration, Promise creation,
per-element error aggregation or outer refinements. This deliberately changes
error aggregation for rejected oversized input; accepted outputs and ordinary
V3 transforms, defaults and refinements retain their semantics. ESM, CommonJS
and TypeScript source implementations carry the same change.

This is the constrained-container boundary addressed by the referenced upstream
fix. It does not add an arbitrary global limit to intentionally unbounded schemas.
`nonempty()` supplies a minimum, not a maximum: `nonempty().max(n)` is bounded;
`nonempty()` alone retains its existing behavior. V3 Map has no corresponding
public upper-size check; official patched V4 handles its own bounded Map schema.
Applications still need transport/input limits and explicit collection maxima;
recursive inputs, arbitrary user refinements and unbounded schemas are not
certified resource-safe by this patch. A clean npm audit for the explicit fork
name is not evidence of that broader property.

The explicit `@ivoai/zod3-compat` name also means npm advisory matching does not
automatically track future advisories against the upstream `zod` identity. IVOAI
must retain the immutable source/patch pins, provenance and regression tests and
manually review upstream security changes until a suitable upstream V3 fix or
validated consumer migration replaces this fork. No automatic cross-name
advisory monitor is implemented or claimed.

## Reproduction and verification

Download the official source archive to a temporary path without running package
scripts, then run:

```sh
python3 patches/zod3-compat/build.py \
  --upstream /absolute/path/zod-4.6.7.tgz \
  --output internal/components/assets/ivoai-zod3-compat-3.25.76-ivoai.1.tgz
bash scripts/test-zod3-compat.sh
```

The builder requires both reviewed upstream digests and the exact patch digest,
checks normalized duplicate paths and entry/byte budgets while scanning headers,
validates the complete admitted inventory before writing files, applies `patch --fuzz=0`, and
runs no upstream build/install hooks. Every file is packaged with mode 0644,
uid/gid/mtime zero, empty owner names, sorted `package/` paths and gzip mtime zero.
Two independent rebuilds were compared byte-for-byte during preparation. Upstream
package scripts are removed from the local fork metadata.

The fixtures test root/V3 CJS and ESM, large valid/invalid oversized containers,
sync and async validation, exact lengths, nested paths, V3 error maps, admitted
transforms/defaults/refinements, minimum-only behavior, explicit identity/license,
and patched V4 array/set/map exports. Mini aliases, the side-effect-only compile
entry, locale error maps and the core parser are exercised through CJS and ESM.
Archive admission tests cover traversal, links/special files, normalized path
aliases and rejection of oversized headers before reading their payload:

```sh
python3 -m unittest discover -s scripts -p 'test_zod3_compat.py'
```

An optional declaration smoke uses an existing reviewed TypeScript installation
without downloading a compiler. It checks root/V3/V4/mini imports, V3 inference
and ESM/CommonJS consumers with strict NodeNext compilation:

```sh
IVOAI_ZOD_TSC="$PWD/website/node_modules/.bin/tsc" \
  bash scripts/test-zod3-compat.sh
```

Without that explicit executable, type validation is reported as `ZOD_TYPES=NOT_RUN`.
An optional pure consumer test can load the
reviewed `@claude-flow/security` input-validator source from an isolated locked
Ruflo prefix:

```sh
IVOAI_ZOD_SECURITY_VALIDATOR=/absolute/path/to/input-validator.js \
  bash scripts/test-zod3-compat.sh
```

Without that explicit file, consumer validation is reported as `NOT_RUN`; passing
library tests alone does not certify the whole Ruflo CLI, lifecycle or MCP server.
No provider authentication, personal configuration or installed user runtime is
read or modified by these fixtures.
