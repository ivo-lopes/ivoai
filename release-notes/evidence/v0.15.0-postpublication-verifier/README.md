# Reviewed future post-publication verifier — offline archive

This is an archive of the private utility reviewed by root after PR57
head `03492d1dbc5a640454c39f65172a44dad4c33feb`. It does not authorize
publication: the separate real canary failed and v0.15.0 remains unpublished.
The package is prepared for a later reviewed publication, not verification of
v0.15.0 assets. Their verification remains **NOT_RUN**. Nothing was downloaded
or executed from release assets to prepare or archive this package.

The original bounded v0.14 verifier, Go reader, 26-test file and adverse PAX fixture
are byte-preserved copies. The wrapper and nine added tests are unchanged from
the private reviewed sources. The wrapper supplies an explicit expected stable
tag and checks a required saved API snapshot against an expected release ID,
canonical UTC publication time and `prerelease=false`. It delegates to the original
API/run/main/readers and preserves every archive/decompression/Go-resource cap.
The source/module/asset/component contracts remain fail-closed; absent API digests
stay UNKNOWN, not fabricated. There is no network downloader or asset execution.

The recorded earlier proof passed 35 unit tests, one full LOCAL v0.14 positive
control (12 assets/API digests and 11 payload checksums) and six CLI refusal controls
(wrong tag, source commit, digest, API size, release ID and time). Those are the
private utility's source epoch, not new v0.15 publication or canary results.
All 12 control asset hashes before/after matched. No downloaded Core, native client,
authentication or provider ran; only the reviewed Go toolchain and trusted compiled
buildinfo reader ran. The original adverse PAX fixture/results remain preserved.

`archive-source-map.json` maps the private names to these archive paths and exact
hashes. The Go helper uses `.go.txt` so repository Go discovery adds no package.
`frozen-source-manifest.json` and `control-report-hashes.json` retain original names
for reproduction; materializing the mapping below restores those names. The
separate archive hash manifest binds actual archived filenames.

## Materialize and test in a new private directory

```sh
verifier_archive=/path/to/ivoai/release-notes/evidence/v0.15.0-postpublication-verifier
control_source_tree=/path/to/clean/reviewed/v0.14.0-source
scratch=$(mktemp -d /var/tmp/ivoai-release-verifier.XXXXXXXX)
for file in verify_release014.py verify_release.py test_verifier.py test_runner.py old-pax-4MiB-fixture.tar.gz; do
  cp "$verifier_archive/$file" "$scratch/$file"
done
cp "$verifier_archive/buildinfo_reader.go.txt" "$scratch/buildinfo_reader.go"
python3 - "$scratch" "$control_source_tree" <<'PYTEST'
import importlib.util
from pathlib import Path
import sys
import unittest
folder, source = map(Path, sys.argv[1:])
suites = []
for name in ('test_verifier', 'test_runner'):
    spec = importlib.util.spec_from_file_location(name, folder / (name + '.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    if name == 'test_verifier':
        module.SOURCE = source
    suites.append(unittest.defaultTestLoader.loadTestsFromModule(module))
result = unittest.TextTestRunner(verbosity=2).run(unittest.TestSuite(suites))
raise SystemExit(not result.wasSuccessful())
PYTEST
```

Verify the source hashes against the archived manifests before use. Do not alter
original tests, reader caps or component pins to make an unexpected input pass.

## Future invocation — publication remains blocked

Only after a separately reviewed actual publication, supply independently reviewed
actual metadata and a clean checkout matching the full peeled tagged commit.
Keep saved API JSON outside the directory of exactly 12 expected assets. Candidate
branches/metadata or expected future values do not substitute for published ones.
The following variables must be set to those ACTUAL reviewed values; no downloader
or publication command is included here. Use a private empty HOME/minimal PATH.

```sh
python3 "$scratch/verify_release.py" \
  --expected-tag v0.15.0 \
  --expected-release-id "$reviewed_release_id" \
  --expected-published-at "$reviewed_published_at" \
  --assets "$downloaded_assets" \
  --api-json "$saved_release_api" \
  --source-tree "$published_source_tree" \
  --source-commit "$reviewed_peeled_commit" \
  --go "$reviewed_go_binary" \
  --output "$new_private_output"
```

Output must not exist. Do not tune expectations or caps after an adverse result.
Metadata/embedded-byte association is not source attestation, reproducible build
or proof of runtime pointer binding. Runtime version/help, native ARM/no-AVX, TUI,
client authentication, actual canary and provider quality remain separate gates.
