# Retained Graft WASM grammars

This directory records the redistribution notices for the 16 WASM grammars
referenced by the pinned Graft runtime: its 15 `GENERIC_LANGS` rows and the Vue
container grammar. Fourteen use MIT, Elixir uses Apache-2.0, and Clojure uses
CC0-1.0. Their complete license texts, including supplied copyright notices,
are retained without modification. Do not label the complete bundle MIT.

The inputs shipped by IVOAI are pinned **prebuilt artifacts**:

- npm `tree-sitter-wasm@1.1.6`, with its exact SRI from Graft's reviewed lock;
- individual WASM SHA-256 values in `inventory.json`;
- builder repository revision `10ceafc2240651aa51ca87627d3befc4813a2811`, read
  from the bundled Sigstore certificate metadata. IVOAI does not claim an
  independent verification of those signatures.

`sourceDeclared` reproduces that bundle's package.json. Some declarations are
Git commits and others are npm semver ranges. The builder's committed lock does
not attest the resolved grammar source versions, and its build cache can reuse
older binaries. Therefore **sourceRevisionAttested is false** throughout: this
inventory does not claim reproducible compilation from exact grammar sources.
For Git-declared dependencies the license was retrieved at the declared commit.
For npm declarations the full license was retrieved from the explicitly named
reference version's integrity-verified npm tarball. `licenseReference` and
`licenseURL` identify that evidence separately from the actual WASM hash. The
license reference is not presented as the proven source version of the binary.

The source-reference licenses grant redistribution, subject to their preserved
notices and terms. The Elixir reference npm archive contained its full Apache
license and no additional root NOTICE file. Native tree-sitter grammars and the
web-tree-sitter runtime remain separately licensed by their own package notices.

`prepare_grammars.py` verifies all retained hashes and license bytes before
changing the disposable build stage. It checks that the runtime's declared WASM
set matches this inventory and refuses new dynamic loader callers. It removes
96 unused WASM grammars and all bundled editor `.scm` queries. Graft reads its own
`src/graph/queries` / `dist/graph/queries` files, so these removals preserve every
registered runtime language. Bundled signatures are retained as upstream data.
The package manifest is updated to describe the retained files accurately.

No source repository, license, query or binary is fetched at user runtime.
