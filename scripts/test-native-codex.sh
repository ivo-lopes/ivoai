#!/usr/bin/env bash
# Opt-in official native TUI/App Server smoke; no provider login or real model calls.
set -euo pipefail
test_root="$(mktemp -d "${TMPDIR:-/tmp}/ivoai-native-codex-test.XXXXXXXX")"
readonly test_root
trap 'echo "Native Codex test workspace: $test_root" >&2' EXIT
case "$(uname -m)" in
  x86_64) target=x86_64-unknown-linux-musl; digest=a0bf572d2f93f056c6712a6da348e96078dc84f3885fd7be67807ef389cb3978 ;;
  aarch64) target=aarch64-unknown-linux-musl; digest=94eb707605ec2646b2e05b7edf7c4e7b3ccf02b16237c34ae9beb8cfb7f810a9 ;;
  *) echo 'native Codex smoke requires Linux amd64 or arm64' >&2; exit 1 ;;
esac
readonly target digest
curl --fail --location --silent --show-error \
  "https://github.com/openai/codex/releases/download/rust-v0.166.0/codex-${target}.tar.gz" \
  -o "$test_root/codex.tar.gz"
printf '%s  %s\n' "$digest" "$test_root/codex.tar.gz" | sha256sum -c -
tar -xzf "$test_root/codex.tar.gz" -C "$test_root"
IVOAI_LIVE_CODEX_PATH="$test_root/codex-$target" IVOAI_LIVE_CODEX_TUI=1 \
  go test -race ./internal/codexfrontend -count=1 -timeout 2m
