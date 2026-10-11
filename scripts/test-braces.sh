#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf -- "$fixture"' EXIT
npm install --prefix "$fixture" --ignore-scripts --no-audit --no-fund \
  "$repo_root/internal/components/assets/braces-3.0.4-ivoai.1.tgz" \
  fill-range@7.1.1 to-regex-range@5.0.1 is-number@7.0.0
IVOAI_BRACES_DIR="$fixture/node_modules/braces" node --test "$repo_root/scripts/test-braces.cjs"
