#!/usr/bin/env bash
set -euo pipefail

# This smoke deliberately uses the exact managed OpenCode artifact shipped by
# IVOAI. It exercises the real HTTP provider path and the real attach TUI; it
# never uses or inspects provider credentials.
readonly version="1.18.25"
readonly digest="58a3729a6f3432dd6d2917fcc4a949788891a035818646ad480e12c947f56e78"
readonly url="https://github.com/anomalyco/opencode/releases/download/v${version}/opencode-linux-x64.tar.gz"

root="$(mktemp -d)"
trap 'rm -rf -- "$root"' EXIT
command -v script >/dev/null 2>&1 || {
  echo "util-linux script is required for the managed OpenCode PTY smoke" >&2
  exit 1
}
archive="$root/opencode.tar.gz"
curl --proto '=https' --tlsv1.2 --fail --location --silent --show-error \
  --retry 3 --retry-all-errors --output "$archive" "$url"
printf '%s  %s\n' "$digest" "$archive" | sha256sum --check --status
tar -xzf "$archive" -C "$root"
binary="$root/opencode"
test -x "$binary"
test "$($binary --version)" = "$version"

IVOAI_LIVE_OPENCODE_PATH="$binary" \
IVOAI_LIVE_OPENCODE_VERSION="$version" \
IVOAI_LIVE_OPENCODE_TUI=1 \
  go test ./internal/opencodebridge \
    -run 'TestLiveManagedOpenCode(RoutesPromptThroughIVOAI|AttachRendersIVOAIPlugin)$' \
    -count=1 -v
