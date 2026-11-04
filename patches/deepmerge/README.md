# Safe destination writes for deepmerge

Upstream `deepmerge@4.3.1` (MIT) remains the latest published release at the
04 November 2026 dependency snapshot. GHSA-gcqx-w48m-w2h4 has no published fix.
The official tarball, integrity, reviewed patch and deterministic local archive
are pinned in `provenance.json`. No upstream install or build hook is executed.

The local `@ivoai/deepmerge-safe@4.3.1-ivoai.1` fork rejects `__proto__` before
copying keys from either merge operand into its new destination. This also
covers null-prototype operands, which bypass the upstream inherited-property
check, and custom mergeability/clone options. The existing protection for other
inherited/nonenumerable properties is retained. Ordinary own properties,
symbols, callbacks, arrays and date ownership retain their previous behavior.
This is a local mitigation, not a published upstream release or a claim that
arbitrary attacker-supplied callbacks are safe.

Both distributed CJS and UMD entry points are patched. Ten executable tests run
against the packaged artifact; the same tests against upstream fail six attack
cases and pass the ordinary compatibility cases. The first test harness attempt
used an incomplete UMD host; its failure is retained separately from the corrected
upstream control. The original MIT license is included in the archive.

Rebuild from a separately downloaded, verified official tarball:

```sh
python3 patches/deepmerge/build.py --upstream /verified/deepmerge-4.3.1.tgz \
  --output /tmp/ivoai-deepmerge-safe.tgz
node --test scripts/test-deepmerge.cjs
```

The archive has an explicit seven-file allowlist, normalized ownership, times
and modes. Its builder reuses the existing bounded extraction helper. The
documentation lock resolves only this dependency to the local artifact. No audit
threshold, advisory ignore or network installer exception is added. Remove the
override only after an upstream fix passes these same adverse tests and a full
documentation build.
