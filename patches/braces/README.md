# Bounded braces traversal

Upstream: npm `braces@3.0.3`, MIT, official tarball
`https://registry.npmjs.org/braces/-/braces-3.0.3.tgz`.
Original integrity:
`sha512-yQbXgO/OSZVD2IsiLlro+7Hf6Q18EJrKSEsdoMzKePKXct3gvD8oLcOQdIzGupr5Fj+EDe8gO/lxc1BzfMpxvA==`.

CVE-2026-93687 / GHSA-vfj7-8cjw-p6xm has no upstream patched release as of
2026-10-11. `3.0.3-depth-limits.patch` produces the local
`3.0.4-ivoai.1` package, not an upstream release. The original license is retained.

The parser bounds both brace and parenthesis nesting before pushing containers.
All AST consumers validate depth (256), node budget (65536) and parent hops
(256) before recursive traversal; supplied AST values must be strings. Unsafe inputs fail with a descriptive
SyntaxError; options cannot disable the limits. Ordinary alternatives, ranges,
products, escaping, quotes, incomplete patterns and shallow siblings are tested.
This patch addresses recursive resource exhaustion, not every possible costly
brace expansion; upstream range limits remain in force.

The deterministic archive at
`internal/components/assets/braces-3.0.4-ivoai.1.tgz` contains only `package.json`,
`index.js`, `lib/*.js`, README and LICENSE. Its tar entries have mode 0644,
uid/gid/mtime zero, sorted paths and the `package/` prefix; gzip mtime is zero.
Apply the patch with `patch --fuzz=0 -p1` to the verified original package to
reproduce those contents. No install hook is used.

Both documentation and Ruflo resolve to this artifact. Ruflo embeds it, verifies
its SHA-512 against the lock, and stages it before `npm ci --ignore-scripts`.
No general local-file exception was added to dependency validation.
Run `bash scripts/test-braces.sh` to test the packaged artifact in isolation.
