#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
site_dir="$repo_root/internal/docsserver/site"
marker="$repo_root/website/static/build-input.sha256"
mode="${1:-write}"

input_hash="$({
  cd "$repo_root"
  # Hash repository-owned inputs only. Local Playwright reports, screenshots,
  # package-manager caches, and other ignored files must not make the embedded
  # production site differ from a clean CI checkout.
  git ls-files -z docs website \
    | while IFS= read -r -d '' path; do
        [[ "$path" == "website/static/build-input.sha256" ]] && continue
        sha256sum "$path"
      done
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
corepack pnpm --dir "$repo_root/website" check:i18n
corepack pnpm --dir "$repo_root/website" build
test -s "$repo_root/website/build/index.html"
test -s "$repo_root/website/build/pt-BR/index.html"
test -s "$repo_root/website/build/search-index.json"
test -s "$repo_root/website/build/pt-BR/search-index.json"
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
  python3 "$repo_root/scripts/check-embedded-docs.py" "$repo_root/website/build" "$site_dir"
elif [[ "$mode" == "write" ]]; then
  rm -rf "$site_dir"
  mkdir -p "$(dirname "$site_dir")"
  cp -a "$repo_root/website/build" "$site_dir"
else
  echo "usage: scripts/build-docs.sh [--check]" >&2
  exit 2
fi
