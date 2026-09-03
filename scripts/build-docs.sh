#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
site_dir="$repo_root/internal/docsserver/site"
marker="$repo_root/website/static/build-input.sha256"
mode="${1:-write}"

input_hash="$({
  cd "$repo_root"
  find docs website \
    -type f \
    ! -path 'website/node_modules/*' \
    ! -path 'website/build/*' \
    ! -path 'website/.docusaurus/*' \
    ! -path 'website/static/build-input.sha256' \
    -print0 | sort -z | xargs -0 sha256sum
} | sha256sum | awk '{print $1}')"

if [[ "$mode" == "--check" ]]; then
  [[ "$(cat "$marker" 2>/dev/null)" == "$input_hash" ]] || {
    echo "documentation input marker is stale; run scripts/build-docs.sh" >&2
    exit 1
  }
elif [[ "$mode" == "write" ]]; then
  printf '%s\n' "$input_hash" >"$marker"
fi

corepack pnpm --dir "$repo_root/website" install --frozen-lockfile
corepack pnpm --dir "$repo_root/website" build
test -s "$repo_root/website/build/index.html"
test -s "$repo_root/website/build/search-index.json"
[[ "$(cat "$repo_root/website/build/build-input.sha256")" == "$input_hash" ]]

if [[ "$mode" == "--check" ]]; then
  [[ "$(cat "$site_dir/build-input.sha256" 2>/dev/null)" == "$input_hash" ]] || {
    echo "embedded documentation input marker is stale; run scripts/build-docs.sh" >&2
    exit 1
  }
  # Webpack's minifier may choose different local variable names and therefore
  # content hashes on different machines. Compare every non-JavaScript output,
  # normalizing only JS asset hash references in rendered HTML. The full
  # production build still runs above, while the deterministic input marker
  # binds the embedded site to the exact documentation/config/lockfile inputs.
  python3 - "$repo_root/website/build" "$site_dir" <<'PY'
import pathlib
import re
import sys

generated = pathlib.Path(sys.argv[1])
embedded = pathlib.Path(sys.argv[2])

def files(root):
    return {
        path.relative_to(root).as_posix(): path
        for path in root.rglob("*")
        if path.is_file() and not path.relative_to(root).as_posix().startswith("assets/js/")
    }

left, right = files(generated), files(embedded)
if left.keys() != right.keys():
    raise SystemExit("embedded documentation file set is stale")

asset_hash = re.compile(rb"(?<=\.)([0-9a-f]{8})(?=\.js)")
for name in sorted(left):
    a, b = left[name].read_bytes(), right[name].read_bytes()
    if name.endswith(".html"):
        a, b = asset_hash.sub(b"HASHHASH", a), asset_hash.sub(b"HASHHASH", b)
    if a != b:
        raise SystemExit(f"embedded documentation differs: {name}")
PY
elif [[ "$mode" == "write" ]]; then
  rm -rf "$site_dir"
  mkdir -p "$(dirname "$site_dir")"
  cp -a "$repo_root/website/build" "$site_dir"
else
  echo "usage: scripts/build-docs.sh [--check]" >&2
  exit 2
fi
