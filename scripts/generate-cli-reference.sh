#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
docs_files=(
  "$repo_root/docs/cli-reference.md"
  "$repo_root/website/i18n/pt-BR/docusaurus-plugin-content-docs/current/cli-reference.md"
)
mode="${1:-write}"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
binary="$tmp_dir/ivoai"

go build -trimpath -o "$binary" "$repo_root/cmd/ivoai"
help="$($binary help)"
for docs_file in "${docs_files[@]}"; do
generated="$tmp_dir/generated.md"

awk -v help="$help" '
  BEGIN {inside=0}
  /<!-- GENERATED-CLI-HELP:START -->/ {
    print
    print "```text"
    print help
    print "```"
    inside=1
    next
  }
  /<!-- GENERATED-CLI-HELP:END -->/ {inside=0; print; next}
  !inside {print}
' "$docs_file" > "$generated"

if [[ "$mode" == "--check" ]]; then
  cmp -s "$generated" "$docs_file" || {
    diff -u "$docs_file" "$generated" || true
    echo "CLI reference is stale; run scripts/generate-cli-reference.sh" >&2
    exit 1
  }
elif [[ "$mode" == "write" ]]; then
  cp "$generated" "$docs_file"
else
  echo "usage: scripts/generate-cli-reference.sh [--check]" >&2
  exit 2
fi
done
