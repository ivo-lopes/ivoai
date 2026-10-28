# Reproduce the post-download verifier

These are byte-preserved sources of the offline utility used for the public
v0.14.0 downloads. `source-manifest.sha256` binds their original bytes and the
synthetic fixture. This archive is post-publication evidence: it is not contained
in the v0.14.0 tag/assets and is not a new application/runtime dependency.

The helper is stored as `buildinfo_reader.go.txt` so this evidence adds no Go
package and is not picked up by repository-wide gofmt. Copy it to its expected
`.go` filename in a new private directory before use. No downloaded executable,
archive script, provider login, personal client configuration or production
service is executed by the verifier. Go1.27.3 and Linux Python 3 are required;
Python3.13 was used for the archived utility tests.

```sh
source_tree=/path/to/clean/ivoai-checkout
source_commit=6d99e320912f3f7325c3009209a10e9df86610d4
# This evidence directory is in a later docs commit; use that checkout to read
# the archive, and a separate clean v0.14.0/equal-tree checkout for source_tree.
verifier_archive=/path/to/later-docs-checkout/release-notes/evidence/v0.14.0-publication/verifier
scratch=$(mktemp -d /var/tmp/ivoai-v014-verifier.XXXXXXXX)
cp "$verifier_archive/verify_release014.py" "$scratch/"
cp "$verifier_archive/buildinfo_reader.go.txt" "$scratch/buildinfo_reader.go"
cp "$verifier_archive/test_verifier.py" "$scratch/"
cp "$verifier_archive/fixtures/old-pax-4MiB-fixture.tar.gz" "$scratch/"
```

The test source retains its historical `SOURCE` path to preserve its original
hash. Override only that imported module constant for portable tests; do not edit
the copied source bytes. These 26 tests exercise the utility, including hostile
PAX/ZIP headers, paths/links, size limits and expected refusals. Their success is
not a release-artifact or performance result.

```sh
python3 - "$scratch" "$source_tree" <<'PY'
import importlib.util
from pathlib import Path
import sys
import unittest
folder, source = map(Path, sys.argv[1:])
spec = importlib.util.spec_from_file_location('test_verifier', folder / 'test_verifier.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
module.SOURCE = source
result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromModule(module))
raise SystemExit(not result.wasSuccessful())
PY
```

For a fresh independent verification, existing GitHub authentication stays with
`gh`; these read-only requests download public release data, not credentials.
Keep API metadata outside the assets directory, which must contain exactly the
12 release files. Supply the full approved peeled commit, not target_commitish.
No upload, tag, asset replacement or publication command is involved.

```sh
mkdir -m 700 "$scratch/assets"
gh api repos/ivo-lopes/ivoai/releases/tags/v0.14.0 > "$scratch/release-api.json"
gh release download v0.14.0 --repo ivo-lopes/ivoai --dir "$scratch/assets"
python3 "$scratch/verify_release014.py" \
  --assets "$scratch/assets" --api-json "$scratch/release-api.json" \
  --source-tree "$source_tree" --source-commit "$source_commit" \
  --go /path/to/reviewed/go1.27.3/bin/go --output "$scratch/verified"
```

The output must be new; the utility refuses to overwrite an existing directory.
It validates all 11 checksum entries, computes all 12 asset hashes, checks each
API SHA256 when exposed, inspects bounded Core archives and ELF/Go module data,
compares 12 module versions/h1 sums with the reviewed source, and associates the
exact embedded component payloads/URLs with metadata and downloaded archives.
The skill must match the reviewed source. Missing API digests remain UNKNOWN;
they are never fabricated. The published run exposed and matched all 12.

Limits: 128-MiB Core archives, 256-MiB OpenCode archives, 512-MiB Graft archives,
2-GiB total; metadata/checksums 64 KiB each, API JSON 2 MiB, ZIP 512 KiB.
Core decompression is bounded before header processing; PAX/GNU extensions,
sparse/nonregular headers and a second member are refused. ZIP entry count and
central-directory budgets are checked before ZipFile construction, followed by
canonical stored-compression/content checks. Child reader CPU/address-space/
output limits and a global wall timeout are enforced. Larger or unknown inputs
fail; there is no automatic relaxation. These limits accommodated the actual
v0.14.0 assets.

Exact embedded base64 bytes establish metadata association, not pointer binding,
source attestation or reproducible compilation. Runtime/no-AVX/native-ARM gates
belong to the tagged CI. The separate local downloaded-Core smoke covered AMD64
version/help under private HOME/XDG only; it did not execute ARM, TUI or providers.

The tiny `old-pax-4MiB-fixture.tar.gz` is synthetic and is never executed. The
first utility accepted its hidden extended header; the bounded utility refuses
it after 512 decompressed bytes. The preserved observation and 26-test log keep
that adverse utility result separate from successful real release verification.
