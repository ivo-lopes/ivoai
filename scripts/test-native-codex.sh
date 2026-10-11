#!/usr/bin/env bash
# Opt-in official native TUI/App Server smoke; no provider login or real model calls.
set -euo pipefail
test_root="$(mktemp -d "${TMPDIR:-/tmp}/ivoai-native-codex-test.XXXXXXXX")"
readonly test_root
trap 'echo "Native Codex test workspace: $test_root" >&2' EXIT
case "$(uname -m)" in
  x86_64) target=x86_64-unknown-linux-musl; digest=86f268d81b898f3e144c5ecff2ad2fda2c5802e042d6195b72538fe0fa125057 ;;
  aarch64) target=aarch64-unknown-linux-musl; digest=bf3ca2719f1143b1440fbd85c149379e696c782c50ad7991f8bfcec60b2600c1 ;;
  *) echo 'native Codex smoke requires Linux amd64 or arm64' >&2; exit 1 ;;
esac
readonly target digest
curl --fail --location --silent --show-error \
  "https://github.com/openai/codex/releases/download/rust-v0.162.1/codex-${target}.tar.gz" \
  -o "$test_root/codex.tar.gz"
printf '%s  %s\n' "$digest" "$test_root/codex.tar.gz" | sha256sum -c -
tar -xzf "$test_root/codex.tar.gz" -C "$test_root"
IVOAI_LIVE_CODEX_PATH="$test_root/codex-$target" IVOAI_LIVE_CODEX_TUI=1 \
  go test -race ./internal/codexfrontend -count=1 -timeout 2m
