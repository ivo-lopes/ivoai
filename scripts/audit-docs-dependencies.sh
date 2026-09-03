#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Docusaurus 3.10.2 currently pulls image-size 2.0.2 through its build-only
# MDX loader. GHSA-w3rx-r6r6-pgpr and GHSA-5p2g-fcmc-qvqq have no patched
# release. IVOAI does not process local images, and only the generated static
# site is embedded in the production binary. Keep this exception narrow and
# fail it if an affected image format enters the documentation sources.
if find "$repo_root/docs" "$repo_root/website/src" "$repo_root/website/static" \
  -type f \( -iname '*.icns' -o -iname '*.jxl' -o -iname '*.heif' -o -iname '*.heic' \) \
  -print -quit | grep -q .; then
  echo "affected image formats are not permitted while the image-size advisories remain unpatched" >&2
  exit 1
fi

corepack pnpm --dir "$repo_root/website" audit --prod --audit-level high \
  --ignore GHSA-w3rx-r6r6-pgpr \
  --ignore GHSA-5p2g-fcmc-qvqq
