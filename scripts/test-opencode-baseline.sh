#!/usr/bin/env bash
# Exercise the distributed executable on an x86-64 CPU without AVX/AVX2.
set -euo pipefail
readonly archive="${1:?usage: test-opencode-baseline.sh ARCHIVE METADATA}"
readonly metadata="${2:?usage: test-opencode-baseline.sh ARCHIVE METADATA}"
[[ "$(jq -er '.platform' "$metadata")" = linux/amd64 ]] || exit 0
command -v qemu-x86_64 >/dev/null
expected_sha="$(jq -er '.sha256' "$metadata")"
printf '%s  %s\n' "$expected_sha" "$archive" | sha256sum -c -
test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT
tar -xzf "$archive" -C "$test_root"
mkdir -p "$test_root/home"
ulimit -c 0
actual="$(env -i PATH="$PATH" HOME="$test_root/home" \
  timeout 60 qemu-x86_64 -cpu Nehalem "$test_root/opencode" --version)"
[[ "$actual" = "$(jq -er '.version' "$metadata")" ]]
echo 'Managed OpenCode non-AVX CPU smoke: PASS'
