#!/usr/bin/env bash
# Pure library fixtures: no npm install, lifecycle hooks, credentials or providers.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf -- "$fixture"' EXIT
python3 - "$repo_root" "$fixture" <<'PY'
import base64,hashlib,json,pathlib,runpy,sys
root,fixture=map(pathlib.Path,sys.argv[1:])
provenance=json.loads((root/'patches/zod3-compat/provenance.json').read_text())
body=(root/'internal/components/assets'/provenance['archive_file']).read_bytes()
if hashlib.sha256(body).hexdigest()!=provenance['archive_sha256']:
    raise ValueError('fork archive SHA-256 mismatch')
if 'sha512-'+base64.b64encode(hashlib.sha512(body).digest()).decode()!=provenance['archive_integrity']:
    raise ValueError('fork archive SHA-512 mismatch')
helper=runpy.run_path(str(root/'patches/zod3-compat/build.py'))
helper['extract_regular'](body,fixture)
(fixture/'node_modules').mkdir()
(fixture/'node_modules/zod').symlink_to(fixture/'package',target_is_directory=True)
(fixture/'entry.mjs').write_text('''
export * as root from "zod";
export * as v3 from "zod/v3";
export * as v4 from "zod/v4";
export * as mini from "zod/mini";
export * as v4Mini from "zod/v4-mini";
export * as v4MiniNested from "zod/v4/mini";
export * as compile from "zod/compile";
export * as locales from "zod/locales";
export * as v4Locales from "zod/v4/locales";
export * as core from "zod/v4/core";
''')
PY
IVOAI_ZOD_FIXTURE="$fixture" node --test "$repo_root/scripts/test-zod3-compat.mjs"
