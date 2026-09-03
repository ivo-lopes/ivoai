#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
site_dir="$repo_root/internal/docsserver/site"
mode="${1:-write}"

corepack pnpm --dir "$repo_root/website" install --frozen-lockfile
corepack pnpm --dir "$repo_root/website" build
test -s "$repo_root/website/build/index.html"
test -s "$repo_root/website/build/search-index.json"

if [[ "$mode" == "--check" ]]; then
  diff -qr "$repo_root/website/build" "$site_dir"
elif [[ "$mode" == "write" ]]; then
  rm -rf "$site_dir"
  mkdir -p "$(dirname "$site_dir")"
  cp -a "$repo_root/website/build" "$site_dir"
else
  echo "usage: scripts/build-docs.sh [--check]" >&2
  exit 2
fi
